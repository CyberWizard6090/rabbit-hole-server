package repository

import (
	"context"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type spaceRepository struct {
	db *gorm.DB
}

func NewSpaceRepository(db *gorm.DB) domain.SpaceRepository {
	return &spaceRepository{db: db}
}

func (r *spaceRepository) GetByID(ctx context.Context, id uint) (*domain.Space, error) {
	var space domain.Space
	if err := r.db.WithContext(ctx).Preload("Lists").First(&space, id).Error; err != nil {
		return nil, err
	}
	return &space, nil
}

func (r *spaceRepository) Create(ctx context.Context, space *domain.Space) error {
	return r.db.WithContext(ctx).Create(space).Error
}

func (r *spaceRepository) CreateWithDefaults(ctx context.Context, space *domain.Space, list *domain.List, statuses []domain.TaskStatus) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(space).Error; err != nil {
			return err
		}

		list.SpaceID = space.ID
		if err := tx.Create(list).Error; err != nil {
			return err
		}

		if len(statuses) == 0 {
			return nil
		}
		for i := range statuses {
			statuses[i].SpaceID = space.ID
			statuses[i].ListID = list.ID
		}
		return tx.Create(&statuses).Error
	})
}

func (r *spaceRepository) GetAll(ctx context.Context, workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	var spaces []domain.Space
	var total int64

	baseQuery := r.db.WithContext(ctx).Model(&domain.Space{}).Where("workspace_id = ?", workspaceID)

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := baseQuery.
		Limit(limit).
		Offset(offset).
		Order("created_at DESC, id DESC").
		Find(&spaces).Error

	return spaces, total, err
}

func (r *spaceRepository) GetDashboard(ctx context.Context, spaceID uint) (*domain.Space, error) {
	var space domain.Space

	err := r.db.WithContext(ctx).
		Preload("Lists").
		Preload("Lists.Statuses", func(db *gorm.DB) *gorm.DB {
			return db.Order("position ASC")
		}).
		Preload("Lists.Tasks").
		Preload("Lists.Tasks.Assignees").
		Preload("Lists.Tasks.Tags").
		Preload("Tags").
		First(&space, "id = ?", spaceID).Error

	if err != nil {
		return nil, err
	}
	return &space, nil
}

func (r *spaceRepository) CreateStatus(ctx context.Context, status *domain.TaskStatus) error {
	return r.db.WithContext(ctx).Create(status).Error
}

func (r *spaceRepository) CreateTag(ctx context.Context, tag *domain.Tag) error {
	return r.db.WithContext(ctx).Create(tag).Error
}

func (r *spaceRepository) CreateList(ctx context.Context, list *domain.List) error {
	return r.db.WithContext(ctx).Create(list).Error
}

func (r *spaceRepository) UpdateStatus(ctx context.Context, status *domain.TaskStatus) error {
	return r.db.WithContext(ctx).Save(status).Error
}

func (r *spaceRepository) GetListByID(ctx context.Context, id uint) (*domain.List, error) {
	var list domain.List
	err := r.db.WithContext(ctx).Preload("Statuses", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&list, id).Error

	if err != nil {
		return nil, err
	}
	return &list, nil
}

func (r *spaceRepository) GetListsBySpace(ctx context.Context, spaceID uint) ([]domain.List, error) {
	var lists []domain.List
	err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("created_at ASC").Find(&lists).Error
	if err != nil {
		return nil, err
	}
	return lists, nil
}

func (r *spaceRepository) GetStatusByID(ctx context.Context, id uint) (*domain.TaskStatus, error) {
	var status domain.TaskStatus
	if err := r.db.WithContext(ctx).First(&status, id).Error; err != nil {
		return nil, err
	}
	return &status, nil
}

func (r *spaceRepository) GetListsByFolder(ctx context.Context, folderID uint) ([]domain.List, error) {
	var lists []domain.List
	err := r.db.WithContext(ctx).Where("folder_id = ?", folderID).Order("created_at ASC").Find(&lists).Error
	if err != nil {
		return nil, err
	}
	return lists, nil
}

func (r *spaceRepository) UpdateList(ctx context.Context, list *domain.List) error {
	return r.db.WithContext(ctx).Save(list).Error
}

func (r *spaceRepository) DeleteList(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&domain.List{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *spaceRepository) Update(ctx context.Context, space *domain.Space) error {
	return r.db.WithContext(ctx).Model(space).Select("Name", "Currency").Updates(space).Error
}

func (r *spaceRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&domain.Space{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
