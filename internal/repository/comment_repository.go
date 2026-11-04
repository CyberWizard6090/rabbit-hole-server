package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type commentRepository struct {
	db *gorm.DB
}

func NewCommentRepository(db *gorm.DB) domain.CommentRepository {
	return &commentRepository{db: db}
}

type commentUserRow struct {
	ID        uint
	Username  string
	FirstName string
	LastName  string
	AvatarURL string
}

func (u commentUserRow) toDomain() domain.CommentUser {
	return domain.CommentUser{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName, AvatarURL: u.AvatarURL}
}

func validateMentions(tx *gorm.DB, taskID uint, mentionIDs []uint) error {
	if len(mentionIDs) == 0 {
		return nil
	}
	var count int64
	err := tx.Raw(
		"SELECT count(DISTINCT wm.user_id) FROM tasks t "+
			"JOIN spaces s ON s.id = t.space_id "+
			"JOIN workspace_members wm ON wm.workspace_id = s.workspace_id AND wm.deleted_at IS NULL "+
			"WHERE t.id = ? AND wm.user_id IN ?",
		taskID, mentionIDs,
	).Scan(&count).Error
	if err != nil {
		return err
	}
	if count != int64(len(mentionIDs)) {
		return domain.ErrInvalidMentionReference
	}
	return nil
}

func replaceMentions(tx *gorm.DB, commentID uint, mentionIDs []uint) error {
	if err := tx.Exec("DELETE FROM task_comment_mentions WHERE comment_id = ?", commentID).Error; err != nil {
		return err
	}
	for _, userID := range mentionIDs {
		if err := tx.Exec(
			"INSERT INTO task_comment_mentions (comment_id, user_id) VALUES (?, ?)", commentID, userID,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *commentRepository) hydrate(tx *gorm.DB, rows []persistence.TaskComment) ([]domain.Comment, error) {
	if len(rows) == 0 {
		return []domain.Comment{}, nil
	}
	commentIDs := make([]uint, len(rows))
	authorIDs := make([]uint, len(rows))
	for i, row := range rows {
		commentIDs[i] = row.ID
		authorIDs[i] = row.UserID
	}

	var authors []commentUserRow
	if err := tx.Table("users").Select("id, username, first_name, last_name, avatar_url").
		Where("id IN ?", uniqueIDs(authorIDs)).Scan(&authors).Error; err != nil {
		return nil, err
	}
	authorByID := make(map[uint]commentUserRow, len(authors))
	for _, a := range authors {
		authorByID[a.ID] = a
	}

	var mentionRows []struct {
		CommentID uint
		ID        uint
		Username  string
		FirstName string
		LastName  string
		AvatarURL string
	}
	if err := tx.Raw(
		"SELECT m.comment_id, u.id, u.username, u.first_name, u.last_name, u.avatar_url "+
			"FROM task_comment_mentions m JOIN users u ON u.id = m.user_id "+
			"WHERE m.comment_id IN ? ORDER BY m.comment_id, u.id", commentIDs,
	).Scan(&mentionRows).Error; err != nil {
		return nil, err
	}
	mentionsByComment := make(map[uint][]domain.CommentUser)
	for _, m := range mentionRows {
		user := commentUserRow{ID: m.ID, Username: m.Username, FirstName: m.FirstName, LastName: m.LastName, AvatarURL: m.AvatarURL}
		mentionsByComment[m.CommentID] = append(mentionsByComment[m.CommentID], user.toDomain())
	}

	out := make([]domain.Comment, len(rows))
	for i, row := range rows {
		mentions := mentionsByComment[row.ID]
		if mentions == nil {
			mentions = []domain.CommentUser{}
		}
		out[i] = domain.Comment{
			ID: row.ID, TaskID: row.TaskID, Author: authorByID[row.UserID].toDomain(),
			Body: row.Body, Mentions: mentions, Edited: row.UpdatedAt.After(row.CreatedAt),
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	return out, nil
}

func (r *commentRepository) one(tx *gorm.DB, taskID, commentID uint) (*domain.Comment, error) {
	var row persistence.TaskComment
	if err := tx.Where("id = ? AND task_id = ?", commentID, taskID).First(&row).Error; err != nil {
		return nil, err
	}
	comments, err := r.hydrate(tx, []persistence.TaskComment{row})
	if err != nil {
		return nil, err
	}
	return &comments[0], nil
}

func (r *commentRepository) Create(ctx context.Context, taskID, authorID uint, body string, mentionIDs []uint) (*domain.Comment, error) {
	mentionIDs = uniqueIDs(mentionIDs)
	var created *domain.Comment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateMentions(tx, taskID, mentionIDs); err != nil {
			return err
		}
		row := persistence.TaskComment{TaskID: taskID, UserID: authorID, Body: body}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := replaceMentions(tx, row.ID, mentionIDs); err != nil {
			return err
		}
		comment, err := r.one(tx, taskID, row.ID)
		created = comment
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return created, nil
}

func (r *commentRepository) ListByTask(ctx context.Context, taskID uint, limit, offset int) ([]domain.Comment, int64, error) {
	db := r.db.WithContext(ctx)
	var total int64
	if err := db.Model(&persistence.TaskComment{}).Where("task_id = ?", taskID).Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []persistence.TaskComment
	if err := db.Where("task_id = ?", taskID).Order("id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	comments, err := r.hydrate(db, rows)
	if err != nil {
		return nil, 0, mapErr(err)
	}
	return comments, total, nil
}

func (r *commentRepository) GetByID(ctx context.Context, taskID, commentID uint) (*domain.Comment, error) {
	comment, err := r.one(r.db.WithContext(ctx), taskID, commentID)
	return comment, mapErr(err)
}

func (r *commentRepository) Update(ctx context.Context, taskID, commentID uint, body *string, mentionIDs *[]uint) (*domain.Comment, error) {
	var updated *domain.Comment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row persistence.TaskComment
		if err := tx.Where("id = ? AND task_id = ?", commentID, taskID).First(&row).Error; err != nil {
			return err
		}
		if mentionIDs != nil {
			ids := uniqueIDs(*mentionIDs)
			if err := validateMentions(tx, taskID, ids); err != nil {
				return err
			}
			if err := replaceMentions(tx, commentID, ids); err != nil {
				return err
			}
		}
		if body != nil {
			if err := tx.Model(&persistence.TaskComment{}).Where("id = ?", commentID).Update("body", *body).Error; err != nil {
				return err
			}
		}
		comment, err := r.one(tx, taskID, commentID)
		updated = comment
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return updated, nil
}

func (r *commentRepository) Delete(ctx context.Context, taskID, commentID uint) error {
	res := r.db.WithContext(ctx).Where("id = ? AND task_id = ?", commentID, taskID).Delete(&persistence.TaskComment{})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
