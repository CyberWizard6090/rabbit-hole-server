package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type timeEntryRepository struct {
	db *gorm.DB
}

func NewTimeEntryRepository(db *gorm.DB) domain.TimeEntryRepository {
	return &timeEntryRepository{db: db}
}

type timeEntryRow struct {
	ID        uint
	TaskID    uint
	UserID    uint
	StartedAt time.Time
	EndedAt   *time.Time
	Seconds   int
	Note      string
	CreatedAt time.Time
}

const timeEntryColumns = `e.id, e.task_id, e.user_id, e.started_at, e.ended_at, e.note, e.created_at,
	CASE WHEN e.ended_at IS NULL THEN LEAST(GREATEST(EXTRACT(EPOCH FROM (now() - e.started_at)), 0)::int, 86400) ELSE e.seconds END AS seconds`

// closeStrandedTimers stops the user's running timers on tasks they can no longer reach (the task or its space
// was deleted, or the user left the workspace). Such a timer is invisible and cannot be stopped through the task,
// yet it would block every new timer because a user has one running timer at most.
func closeStrandedTimers(tx *gorm.DB, userID uint) error {
	var tasks []uint
	if err := tx.Raw(`UPDATE task_time_entries e SET ended_at = now(), updated_at = now(),
			seconds = LEAST(GREATEST(EXTRACT(EPOCH FROM (now() - e.started_at)), 0)::int, 86400)
		WHERE e.user_id = ? AND e.ended_at IS NULL AND NOT EXISTS (
			SELECT 1 FROM tasks t
			JOIN spaces s ON s.id = t.space_id AND s.deleted_at IS NULL
			JOIN workspace_members m ON m.workspace_id = s.workspace_id AND m.user_id = e.user_id AND m.deleted_at IS NULL
			WHERE t.id = e.task_id AND t.deleted_at IS NULL)
		RETURNING e.task_id`, userID).Scan(&tasks).Error; err != nil {
		return err
	}
	for _, taskID := range tasks {
		if err := recalcTimeSpent(tx, taskID); err != nil {
			return err
		}
	}
	return nil
}

func recalcTimeSpent(tx *gorm.DB, taskID uint) error {
	return tx.Exec(`UPDATE tasks SET time_spent = (
		SELECT LEAST(COALESCE(SUM(seconds), 0), 2147483647)::int FROM task_time_entries WHERE task_id = ? AND ended_at IS NOT NULL)
		WHERE id = ?`, taskID, taskID).Error
}

func presentEntries(db *gorm.DB, rows []timeEntryRow) ([]domain.TimeEntry, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	users, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.TimeEntry, 0, len(rows))
	for _, row := range rows {
		entry := domain.TimeEntry{
			ID: row.ID, TaskID: row.TaskID, UserID: row.UserID, StartedAt: row.StartedAt, EndedAt: row.EndedAt,
			Seconds: row.Seconds, Running: row.EndedAt == nil, Note: row.Note, CreatedAt: row.CreatedAt,
		}
		if u, ok := users[row.UserID]; ok {
			entry.User = &u
		}
		out = append(out, entry)
	}
	return out, nil
}

func (r *timeEntryRepository) one(ctx context.Context, taskID, id uint) (*domain.TimeEntry, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []timeEntryRow
	if err := db.Raw("SELECT "+timeEntryColumns+" FROM task_time_entries e WHERE e.id = ? AND e.task_id = ?", id, taskID).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrTimeEntryMissing
	}
	entries, err := presentEntries(db, rows)
	if err != nil {
		return nil, mapErr(err)
	}
	return &entries[0], nil
}

func (r *timeEntryRepository) Get(ctx context.Context, taskID, entryID uint) (*domain.TimeEntry, error) {
	return r.one(ctx, taskID, entryID)
}

func (r *timeEntryRepository) Start(ctx context.Context, taskID, userID uint, note string) (*domain.TimeEntry, error) {
	var id uint
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		if err := closeStrandedTimers(tx, userID); err != nil {
			return err
		}
		return tx.Raw(
			`INSERT INTO task_time_entries (task_id, user_id, started_at, note) VALUES (?, ?, now(), ?) RETURNING id`,
			taskID, userID, note).Scan(&id).Error
	})
	if err != nil {
		if strings.Contains(err.Error(), "uq_time_entries_one_running_timer") {
			return nil, domain.ErrTimerRunning
		}
		return nil, mapErr(err)
	}
	return r.one(ctx, taskID, id)
}

func (r *timeEntryRepository) Stop(ctx context.Context, taskID, userID uint) (*domain.TimeEntry, error) {
	var id uint
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var ids []uint
		if err := tx.Raw(`UPDATE task_time_entries SET ended_at = now(), updated_at = now(),
			seconds = LEAST(GREATEST(EXTRACT(EPOCH FROM (now() - started_at)), 0)::int, 86400)
			WHERE task_id = ? AND user_id = ? AND ended_at IS NULL RETURNING id`, taskID, userID).Scan(&ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return domain.ErrNoRunningTimer
		}
		id = ids[0]
		return recalcTimeSpent(tx, taskID)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return r.one(ctx, taskID, id)
}

func (r *timeEntryRepository) Add(ctx context.Context, draft domain.TimeEntryDraft) (*domain.TimeEntry, error) {
	var id uint
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		if err := tx.Raw(`INSERT INTO task_time_entries (task_id, user_id, started_at, ended_at, seconds, note)
			VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			draft.TaskID, draft.UserID, draft.StartedAt, draft.EndedAt, draft.Seconds, draft.Note).Scan(&id).Error; err != nil {
			return err
		}
		return recalcTimeSpent(tx, draft.TaskID)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return r.one(ctx, draft.TaskID, id)
}

func (r *timeEntryRepository) List(ctx context.Context, taskID uint) ([]domain.TimeEntry, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []timeEntryRow
	if err := db.Raw("SELECT "+timeEntryColumns+" FROM task_time_entries e WHERE e.task_id = ? ORDER BY e.started_at DESC, e.id DESC LIMIT 500", taskID).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	entries, err := presentEntries(db, rows)
	return entries, mapErr(err)
}

func (r *timeEntryRepository) Update(ctx context.Context, taskID, entryID uint, seconds *int, note *string) (*domain.TimeEntry, error) {
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var rows []struct {
			EndedAt *time.Time
		}
		if err := tx.Raw("SELECT ended_at FROM task_time_entries WHERE id = ? AND task_id = ? FOR UPDATE", entryID, taskID).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return domain.ErrTimeEntryMissing
		}
		if seconds != nil && rows[0].EndedAt == nil {
			return domain.ErrInvalidTimeEntry
		}
		if seconds != nil {
			if err := tx.Exec(`UPDATE task_time_entries SET seconds = ?, ended_at = started_at + make_interval(secs => ?), updated_at = now() WHERE id = ?`,
				*seconds, *seconds, entryID).Error; err != nil {
				return err
			}
		}
		if note != nil {
			if err := tx.Exec(`UPDATE task_time_entries SET note = ?, updated_at = now() WHERE id = ?`, *note, entryID).Error; err != nil {
				return err
			}
		}
		return recalcTimeSpent(tx, taskID)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return r.one(ctx, taskID, entryID)
}

func (r *timeEntryRepository) Delete(ctx context.Context, taskID, entryID uint) error {
	return mapErr(database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		res := tx.Exec("DELETE FROM task_time_entries WHERE id = ? AND task_id = ?", entryID, taskID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrTimeEntryMissing
		}
		return recalcTimeSpent(tx, taskID)
	}))
}

func (r *timeEntryRepository) Running(ctx context.Context, userID uint) (*domain.RunningTimer, error) {
	var rows []struct {
		EntryID   uint
		TaskID    uint
		TaskTitle string
		StartedAt time.Time
		Seconds   int
	}
	err := database.DBFromContext(ctx, r.db).Raw(`
		SELECT e.id AS entry_id, e.task_id, t.title AS task_title, e.started_at,
		       LEAST(GREATEST(EXTRACT(EPOCH FROM (now() - e.started_at)), 0)::int, 86400) AS seconds
		FROM task_time_entries e
		JOIN tasks t ON t.id = e.task_id AND t.deleted_at IS NULL
		JOIN spaces s ON s.id = t.space_id AND s.deleted_at IS NULL
		WHERE e.user_id = ? AND e.ended_at IS NULL
		  AND EXISTS (SELECT 1 FROM workspace_members m WHERE m.workspace_id = s.workspace_id AND m.user_id = e.user_id AND m.deleted_at IS NULL)`,
		userID).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	return &domain.RunningTimer{EntryID: row.EntryID, TaskID: row.TaskID, TaskTitle: row.TaskTitle, StartedAt: row.StartedAt, Seconds: row.Seconds}, nil
}

func (r *timeEntryRepository) Report(ctx context.Context, workspaceID uint, from, to time.Time, userID *uint) ([]domain.TimeReportRow, error) {
	db := database.DBFromContext(ctx, r.db)
	query := `SELECT e.user_id, COALESCE(SUM(e.seconds), 0)::bigint AS seconds, count(*) AS entries
		FROM task_time_entries e
		JOIN tasks t ON t.id = e.task_id AND t.deleted_at IS NULL
		JOIN spaces s ON s.id = t.space_id AND s.deleted_at IS NULL AND s.workspace_id = ?
		WHERE e.ended_at IS NOT NULL AND e.started_at >= ? AND e.started_at < ?`
	args := []any{workspaceID, from, to}
	if userID != nil {
		query += " AND e.user_id = ?"
		args = append(args, *userID)
	}
	query += " GROUP BY e.user_id ORDER BY seconds DESC, e.user_id"
	var rows []struct {
		UserID  uint
		Seconds int64
		Entries int64
	}
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	users, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.TimeReportRow, 0, len(rows))
	for _, row := range rows {
		item := domain.TimeReportRow{Seconds: row.Seconds, Entries: row.Entries}
		if u, ok := users[row.UserID]; ok {
			item.User = &u
		}
		out = append(out, item)
	}
	return out, nil
}
