package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/persistence"
)

type taskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) domain.TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(ctx context.Context, task *domain.Task, assigneeIDs []uint, tagIDs []uint) error {
	m := taskFromDomain(*task)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateTaskReferences(tx, task, assigneeIDs, tagIDs); err != nil {
			return err
		}
		if err := lockLists(tx, task.ListID); err != nil {
			return err
		}
		last, err := lastPosition(tx, task.ListID, 0)
		if err != nil {
			return err
		}
		m.Position = last + positionStep
		if len(assigneeIDs) > 0 {
			var users []persistence.User
			if err := tx.Find(&users, assigneeIDs).Error; err != nil {
				return err
			}
			m.Assignees = users
		}

		if len(tagIDs) > 0 {
			var tags []persistence.Tag
			if err := tx.Find(&tags, tagIDs).Error; err != nil {
				return err
			}
			m.Tags = tags
		}

		return tx.Create(&m).Error
	})
	if err != nil {
		return mapErr(err)
	}
	*task = taskToDomain(m)
	return nil
}

func (r *taskRepository) List(
	ctx context.Context,
	listID uint,
	query domain.TaskQuery,
	params pagination.Params,
) ([]domain.Task, bool, bool, error) {
	fetchLimit, err := pagination.FetchLimit(params.Limit)
	if err != nil {
		return nil, false, false, err
	}

	keys := query.SortKeys()
	specs := make([]taskSortSpec, len(keys))
	selects := []string{"tasks.*"}
	for i, key := range keys {
		spec, ok := taskSortSpecs[key.Field]
		if !ok {
			return nil, false, false, fmt.Errorf("unsupported task sort field: %q", key.Field)
		}
		specs[i] = spec
		if spec.alias != "" {
			selects = append(selects, spec.expr+" AS "+spec.alias)
		}
	}

	db := r.db.WithContext(ctx).
		Model(&persistence.Task{}).
		Select(strings.Join(selects, ", ")).
		Where("tasks.list_id = ?", listID).
		Preload("Assignees").
		Preload("Tags")
	db = applyTaskFilter(db, query.Filter)

	isPrev := params.Cursor != nil && params.Cursor.Direction == pagination.DirectionPrev

	if params.Cursor != nil {
		cursor := params.Cursor
		if cursor.Direction != pagination.DirectionNext && cursor.Direction != pagination.DirectionPrev {
			return nil, false, false, fmt.Errorf("unsupported cursor direction: %q", cursor.Direction)
		}
		position, err := pagination.DecodePosition[pagination.TaskCursorPosition](cursor, query.CursorContext(listID))
		if err != nil {
			return nil, false, false, err
		}
		condition, args, err := keysetCondition(keys, specs, position, isPrev)
		if err != nil {
			return nil, false, false, err
		}
		db = db.Where(condition, args...)
	}

	for i, key := range keys {
		db = db.Order(orderClause(specs[i], key.Desc, isPrev))
	}

	var rows []persistence.Task
	if err := db.Limit(fetchLimit).Find(&rows).Error; err != nil {
		return nil, false, false, mapErr(err)
	}

	tasks := mapSlice(rows, taskToDomain)
	hasMore := len(tasks) > params.Limit
	if hasMore {
		tasks = tasks[:params.Limit]
	}

	hasPrev := hasPreviousPage(params.Cursor, hasMore)

	if isPrev {
		for i, j := 0, len(tasks)-1; i < j; i, j = i+1, j-1 {
			tasks[i], tasks[j] = tasks[j], tasks[i]
		}
	}

	hasNext := hasMore
	if isPrev {
		hasNext = len(tasks) > 0
	}

	return tasks, hasNext, hasPrev, nil
}

func (r *taskRepository) Count(ctx context.Context, listID uint, filter domain.TaskFilter) (int64, error) {
	var total int64
	db := r.db.WithContext(ctx).Model(&persistence.Task{}).Where("tasks.list_id = ?", listID)
	if err := applyTaskFilter(db, filter).Count(&total).Error; err != nil {
		return 0, mapErr(err)
	}
	return total, nil
}

type taskSortKind int

const (
	sortKindInt taskSortKind = iota
	sortKindText
	sortKindTime
)

type taskSortSpec struct {
	expr     string
	kind     taskSortKind
	nullable bool
	param    string
	alias    string
}

const (
	textKeyExpr = "left(lower(%s), 100)"

	assigneeSortExpr = "(SELECT min(left(lower(coalesce(" +
		"nullif(btrim(concat_ws(' ', u.first_name, u.last_name)), ''), u.username, u.email)), 100)) " +
		"FROM task_assignees ta JOIN users u ON u.id = ta.user_id WHERE ta.task_id = tasks.id)"
)

var taskSortSpecs = map[domain.TaskSortField]taskSortSpec{
	domain.TaskSortID:           {expr: "tasks.id", kind: sortKindInt, param: "?"},
	domain.TaskSortTitle:        {expr: fmt.Sprintf(textKeyExpr, "tasks.title"), kind: sortKindText, param: fmt.Sprintf(textKeyExpr, "?")},
	domain.TaskSortPriority:     {expr: "tasks.priority", kind: sortKindInt, param: "?"},
	domain.TaskSortTimeEstimate: {expr: "tasks.time_estimate", kind: sortKindInt, param: "?"},
	domain.TaskSortTimeSpent:    {expr: "tasks.time_spent", kind: sortKindInt, param: "?"},
	domain.TaskSortPosition:     {expr: "tasks.position", kind: sortKindInt, param: "?"},
	domain.TaskSortStartDate:    {expr: "tasks.start_date", kind: sortKindTime, nullable: true, param: "?"},
	domain.TaskSortDueDate:      {expr: "tasks.due_date", kind: sortKindTime, nullable: true, param: "?"},
	domain.TaskSortCreatedAt:    {expr: "tasks.created_at", kind: sortKindTime, param: "?"},
	domain.TaskSortUpdatedAt:    {expr: "tasks.updated_at", kind: sortKindTime, param: "?"},
	domain.TaskSortStatus: {
		expr:  "(SELECT ts.position FROM task_statuses ts WHERE ts.id = tasks.status_id)",
		kind:  sortKindInt,
		param: "?",
		alias: "sort_status_position",
	},
	domain.TaskSortAssignee: {
		expr:     assigneeSortExpr,
		kind:     sortKindText,
		nullable: true,
		param:    "?",
		alias:    "sort_assignee",
	},
}

func orderClause(spec taskSortSpec, desc, reverse bool) string {
	if reverse {
		desc = !desc
	}
	clause := spec.expr
	if desc {
		clause += " DESC"
	} else {
		clause += " ASC"
	}
	if spec.nullable {
		if reverse {
			clause += " NULLS FIRST"
		} else {
			clause += " NULLS LAST"
		}
	}
	return clause
}

func decodeCursorValue(spec taskSortSpec, raw json.RawMessage) (any, error) {
	if string(raw) == "null" {
		if !spec.nullable {
			return nil, fmt.Errorf("%w: unexpected null in cursor", pagination.ErrInvalidCursor)
		}
		return nil, nil
	}
	var (
		value any
		err   error
	)
	switch spec.kind {
	case sortKindInt:
		var v int64
		err = json.Unmarshal(raw, &v)
		value = v
	case sortKindText:
		var v string
		err = json.Unmarshal(raw, &v)
		value = v
	case sortKindTime:
		var v time.Time
		err = json.Unmarshal(raw, &v)
		value = v
	}
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor value", pagination.ErrInvalidCursor)
	}
	return value, nil
}

func keysetCondition(
	keys []domain.TaskSortKey,
	specs []taskSortSpec,
	position pagination.TaskCursorPosition,
	prev bool,
) (string, []any, error) {
	if len(position.Keys) != len(keys) {
		return "", nil, fmt.Errorf("%w: cursor does not match sort", pagination.ErrInvalidCursor)
	}
	values := make([]any, len(keys))
	for i, raw := range position.Keys {
		v, err := decodeCursorValue(specs[i], raw)
		if err != nil {
			return "", nil, err
		}
		values[i] = v
	}
	if id, ok := values[len(values)-1].(int64); keys[len(keys)-1].Field == domain.TaskSortID && (!ok || id <= 0) {
		return "", nil, fmt.Errorf("%w: invalid task cursor position", pagination.ErrInvalidCursor)
	}

	var branches []string
	var args []any
	for i := range keys {
		spec, value := specs[i], values[i]

		var cmp string
		var cmpArgs []any
		switch {
		case !prev && value == nil:
			continue
		case !prev:
			op := ">"
			if keys[i].Desc {
				op = "<"
			}
			cmp = spec.expr + " " + op + " " + spec.param
			cmpArgs = []any{value}
			if spec.nullable {
				cmp = "(" + cmp + " OR " + spec.expr + " IS NULL)"
			}
		case value == nil:
			cmp = spec.expr + " IS NOT NULL"
		default:
			op := "<"
			if keys[i].Desc {
				op = ">"
			}
			cmp = spec.expr + " " + op + " " + spec.param
			cmpArgs = []any{value}
		}

		parts := make([]string, 0, i+1)
		branchArgs := make([]any, 0, i+1)
		for j := 0; j < i; j++ {
			if values[j] == nil {
				parts = append(parts, specs[j].expr+" IS NULL")
			} else {
				parts = append(parts, specs[j].expr+" = "+specs[j].param)
				branchArgs = append(branchArgs, values[j])
			}
		}
		parts = append(parts, cmp)
		branchArgs = append(branchArgs, cmpArgs...)

		branches = append(branches, "("+strings.Join(parts, " AND ")+")")
		args = append(args, branchArgs...)
	}
	if len(branches) == 0 {
		return "FALSE", nil, nil
	}
	return "(" + strings.Join(branches, " OR ") + ")", args, nil
}

func applyTaskFilter(db *gorm.DB, f domain.TaskFilter) *gorm.DB {
	if f.Query != "" {
		db = db.Where(`tasks.title ILIKE ? ESCAPE '\'`, "%"+escapeLike(f.Query)+"%")
	}
	if len(f.StatusIDs) > 0 {
		db = db.Where("tasks.status_id IN ?", f.StatusIDs)
	}
	if len(f.Priorities) > 0 {
		db = db.Where("tasks.priority IN ?", f.Priorities)
	}
	db = applyRelationFilter(db, "task_tags", "tag_id", f.TagIDs, f.NoTags)
	db = applyRelationFilter(db, "task_assignees", "user_id", f.AssigneeIDs, f.Unassigned)

	db = applyDateRange(db, "tasks.created_at", f.CreatedFrom, f.CreatedTo)
	db = applyDateRange(db, "tasks.due_date", f.DueFrom, f.DueTo)
	db = applyDateRange(db, "tasks.start_date", f.StartFrom, f.StartTo)

	if f.HasDue != nil {
		if *f.HasDue {
			db = db.Where("tasks.due_date IS NOT NULL")
		} else {
			db = db.Where("tasks.due_date IS NULL")
		}
	}
	return db
}

func applyRelationFilter(db *gorm.DB, table, column string, ids []uint, none bool) *gorm.DB {
	has := fmt.Sprintf("EXISTS (SELECT 1 FROM %s j WHERE j.task_id = tasks.id AND j.%s IN ?)", table, column)
	hasNone := fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s j WHERE j.task_id = tasks.id)", table)
	switch {
	case len(ids) > 0 && none:
		return db.Where("("+has+" OR "+hasNone+")", ids)
	case len(ids) > 0:
		return db.Where(has, ids)
	case none:
		return db.Where(hasNone)
	}
	return db
}

func applyDateRange(db *gorm.DB, column string, from, to *time.Time) *gorm.DB {
	if from != nil {
		db = db.Where(column+" >= ?", from.UTC())
	}
	if to != nil {
		db = db.Where(column+" < ?", to.UTC().AddDate(0, 0, 1))
	}
	return db
}

func hasPreviousPage(cursor *pagination.Cursor, hasMore bool) bool {
	if cursor == nil {
		return false
	}

	switch cursor.Direction {
	case pagination.DirectionNext:
		return true
	case pagination.DirectionPrev:
		return hasMore
	default:
		return false
	}
}

func (r *taskRepository) GetByID(ctx context.Context, id uint) (*domain.Task, error) {
	var m persistence.Task
	err := r.db.WithContext(ctx).Where("id = ?", id).
		Preload("Assignees").
		Preload("Tags").
		First(&m).Error
	if err != nil {
		return nil, mapErr(err)
	}
	task := taskToDomain(m)
	return &task, nil
}

func (r *taskRepository) Update(ctx context.Context, task *domain.Task, addTagIDs []uint, replaceAssigneeIDs *[]uint) error {
	m := taskFromDomain(*task)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		addTagIDs = uniqueIDs(addTagIDs)
		var assigneeIDs []uint
		if replaceAssigneeIDs != nil {
			assigneeIDs = *replaceAssigneeIDs
		}
		if err := validateTaskReferences(tx, task, assigneeIDs, addTagIDs); err != nil {
			return err
		}
		if err := validateParentChain(tx, task); err != nil {
			return err
		}
		if err := tx.Omit(clause.Associations).Save(&m).Error; err != nil {
			return err
		}
		if replaceAssigneeIDs != nil {
			var users []persistence.User
			if ids := uniqueIDs(assigneeIDs); len(ids) > 0 {
				if err := tx.Find(&users, ids).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&m).Omit("Assignees.*").Association("Assignees").Replace(users); err != nil {
				return err
			}
		}
		if len(addTagIDs) == 0 {
			return nil
		}

		var tags []persistence.Tag
		if err := tx.Find(&tags, addTagIDs).Error; err != nil {
			return err
		}
		return tx.Model(&m).Omit("Tags.*").Association("Tags").Append(tags)
	})
	if err != nil {
		return mapErr(err)
	}
	task.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *taskRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&persistence.Task{})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func validateTaskReferences(tx *gorm.DB, task *domain.Task, assigneeIDs, tagIDs []uint) error {
	var statusCount int64
	if err := tx.Model(&persistence.TaskStatus{}).
		Where("id = ? AND list_id = ?", task.StatusID, task.ListID).
		Count(&statusCount).Error; err != nil {
		return err
	}
	if statusCount != 1 {
		return domain.ErrInvalidStatusReference
	}

	if task.ParentID != nil {
		var parentCount int64
		if err := tx.Model(&persistence.Task{}).
			Where("id = ? AND list_id = ?", *task.ParentID, task.ListID).
			Count(&parentCount).Error; err != nil {
			return err
		}
		if parentCount != 1 {
			return domain.ErrInvalidParentReference
		}
	}

	if err := validateTagReferences(tx, task.SpaceID, tagIDs); err != nil {
		return err
	}
	assigneeIDs = uniqueIDs(assigneeIDs)
	if len(assigneeIDs) == 0 {
		return nil
	}

	var assigneeCount int64
	if err := tx.Table("users").
		Joins("JOIN workspace_members ON workspace_members.user_id = users.id").
		Joins("JOIN spaces ON spaces.workspace_id = workspace_members.workspace_id").
		Where("spaces.id = ? AND users.id IN ?", task.SpaceID, assigneeIDs).
		Distinct("users.id").Count(&assigneeCount).Error; err != nil {
		return err
	}
	if assigneeCount != int64(len(assigneeIDs)) {
		return domain.ErrInvalidAssigneeReference
	}
	return nil
}

const maxTaskDepth = 100

func validateParentChain(tx *gorm.DB, task *domain.Task) error {
	if task.ParentID == nil || task.ID == 0 {
		return nil
	}
	current := *task.ParentID
	for i := 0; i < maxTaskDepth; i++ {
		if current == task.ID {
			return domain.ErrInvalidParentReference
		}
		var parent persistence.Task
		err := tx.Select("id", "parent_id").Where("id = ?", current).First(&parent).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && parent.ParentID == nil) {
			return nil
		}
		if err != nil {
			return err
		}
		current = *parent.ParentID
	}
	return domain.ErrInvalidParentReference
}

func validateTagReferences(tx *gorm.DB, spaceID uint, tagIDs []uint) error {
	tagIDs = uniqueIDs(tagIDs)
	if len(tagIDs) == 0 {
		return nil
	}

	var tagCount int64
	if err := tx.Model(&persistence.Tag{}).
		Where("space_id = ? AND id IN ?", spaceID, tagIDs).
		Count(&tagCount).Error; err != nil {
		return err
	}
	if tagCount != int64(len(tagIDs)) {
		return domain.ErrInvalidTagReference
	}
	return nil
}

func uniqueIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
