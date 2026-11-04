package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type spaceRepository struct {
	db *gorm.DB
}

func NewSpaceRepository(db *gorm.DB) domain.SpaceRepository {
	return &spaceRepository{db: db}
}

func (r *spaceRepository) GetByID(ctx context.Context, id uint) (*domain.Space, error) {
	var m persistence.Space
	if err := r.db.WithContext(ctx).Preload("Lists").First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	space := spaceToDomain(m)
	return &space, nil
}

func (r *spaceRepository) Create(ctx context.Context, space *domain.Space) error {
	m := spaceFromDomain(*space)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapErr(err)
	}
	*space = spaceToDomain(m)
	return nil
}

func (r *spaceRepository) GetAll(ctx context.Context, workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	var rows []persistence.Space
	var total int64

	baseQuery := r.db.WithContext(ctx).Model(&persistence.Space{}).Where("workspace_id = ?", workspaceID)

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}

	err := baseQuery.
		Limit(limit).
		Offset(offset).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, 0, mapErr(err)
	}

	return mapSlice(rows, spaceToDomain), total, nil
}

func (r *spaceRepository) GetDashboard(ctx context.Context, spaceID uint, tasksPerList int) (*domain.Space, error) {
	var m persistence.Space

	err := r.db.WithContext(ctx).
		Preload("Lists", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		Preload("Lists.Statuses", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).
		Preload("Tags").
		First(&m, "id = ?", spaceID).Error
	if err != nil {
		return nil, mapErr(err)
	}
	space := spaceToDomain(m)
	if len(space.Lists) == 0 {
		return &space, nil
	}

	listIDs := make([]uint, len(space.Lists))
	for i, list := range space.Lists {
		listIDs[i] = list.ID
	}

	var counts []struct {
		ListID uint
		Count  int64
	}
	if err := r.db.WithContext(ctx).Model(&persistence.Task{}).
		Select("list_id, COUNT(*) AS count").
		Where("list_id IN ?", listIDs).
		Group("list_id").
		Scan(&counts).Error; err != nil {
		return nil, mapErr(err)
	}
	totals := make(map[uint]int, len(counts))
	for _, row := range counts {
		totals[row.ListID] = int(row.Count)
	}

	ranked := r.db.WithContext(ctx).Model(&persistence.Task{}).
		Select("id, ROW_NUMBER() OVER (PARTITION BY list_id ORDER BY created_at DESC, id DESC) AS rn").
		Where("list_id IN ?", listIDs)
	var rows []persistence.Task
	if err := r.db.WithContext(ctx).Model(&persistence.Task{}).
		Select(append([]string{"tasks.*"}, checklistCountSelects...)).
		Joins(checklistCountsJoin).
		Preload("Assignees").
		Preload("Tags").
		Where("tasks.id IN (?)", r.db.WithContext(ctx).Table("(?) AS ranked", ranked).Select("id").Where("rn <= ?", tasksPerList)).
		Order("tasks.created_at DESC, tasks.id DESC").
		Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}

	byList := make(map[uint][]domain.Task, len(space.Lists))
	for _, task := range mapSlice(rows, taskToDomain) {
		byList[task.ListID] = append(byList[task.ListID], task)
	}
	for i := range space.Lists {
		id := space.Lists[i].ID
		space.Lists[i].Tasks = byList[id]
		if space.Lists[i].Tasks == nil {
			space.Lists[i].Tasks = []domain.Task{}
		}
		space.Lists[i].TaskCount = totals[id]
	}
	return &space, nil
}

func (r *spaceRepository) CreateStatus(ctx context.Context, status *domain.TaskStatus) error {
	m := statusFromDomain(*status)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapErr(err)
	}
	*status = statusToDomain(m)
	return nil
}

func (r *spaceRepository) CreateTag(ctx context.Context, tag *domain.Tag) error {
	m := tagFromDomain(*tag)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapErr(err)
	}
	*tag = tagToDomain(m)
	return nil
}

func (r *spaceRepository) CreateList(ctx context.Context, list *domain.List) error {
	m := listFromDomain(*list)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapErr(err)
	}
	*list = listToDomain(m)
	return nil
}

func (r *spaceRepository) UpdateStatus(ctx context.Context, status *domain.TaskStatus) error {
	m := statusFromDomain(*status)
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return mapErr(err)
	}
	status.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *spaceRepository) GetListByID(ctx context.Context, id uint) (*domain.List, error) {
	var m persistence.List
	err := r.db.WithContext(ctx).Preload("Statuses", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&m, id).Error

	if err != nil {
		return nil, mapErr(err)
	}
	list := listToDomain(m)
	return &list, nil
}

func (r *spaceRepository) GetListsBySpace(ctx context.Context, spaceID uint) ([]domain.List, error) {
	var rows []persistence.List
	err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("created_at ASC").Find(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, listToDomain), nil
}

func (r *spaceRepository) GetStatusByID(ctx context.Context, id uint) (*domain.TaskStatus, error) {
	var m persistence.TaskStatus
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	status := statusToDomain(m)
	return &status, nil
}

func (r *spaceRepository) GetListsByFolder(ctx context.Context, folderID uint) ([]domain.List, error) {
	var rows []persistence.List
	err := r.db.WithContext(ctx).Where("folder_id = ?", folderID).Order("created_at ASC").Find(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, listToDomain), nil
}

func (r *spaceRepository) UpdateList(ctx context.Context, list *domain.List) error {
	m := listFromDomain(*list)
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return mapErr(err)
	}
	list.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *spaceRepository) DeleteList(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&persistence.List{}, id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *spaceRepository) CreateWithDefaults(ctx context.Context, space *domain.Space, list *domain.List, statuses []domain.TaskStatus) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sm := spaceFromDomain(*space)
		if err := tx.Create(&sm).Error; err != nil {
			return err
		}

		lm := listFromDomain(*list)
		lm.SpaceID = sm.ID
		if err := tx.Create(&lm).Error; err != nil {
			return err
		}

		rows := make([]persistence.TaskStatus, len(statuses))
		for i := range statuses {
			rows[i] = statusFromDomain(statuses[i])
			rows[i].SpaceID = sm.ID
			rows[i].ListID = lm.ID
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}

		*space = spaceToDomain(sm)
		*list = listToDomain(lm)
		for i := range rows {
			statuses[i] = statusToDomain(rows[i])
		}
		return nil
	}))
}

func (r *spaceRepository) Update(ctx context.Context, space *domain.Space) error {
	m := spaceFromDomain(*space)
	if err := r.db.WithContext(ctx).Model(&m).Select("Name", "Currency").Updates(&m).Error; err != nil {
		return mapErr(err)
	}
	space.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *spaceRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&persistence.Space{}, id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
