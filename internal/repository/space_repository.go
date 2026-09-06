package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type spaceRepository struct {
	db *gorm.DB
}

func NewSpaceRepository(db *gorm.DB) domain.SpaceRepository {
	return &spaceRepository{db: db}
}

func (r *spaceRepository) GetByID(id uint) (*domain.Space, error) {
	var space domain.Space
	if err := r.db.First(&space, id).Error; err != nil {
		return nil, err
	}
	return &space, nil
}

func (r *spaceRepository) Create(space *domain.Space) error {
	return r.db.Create(space).Error
}

func (r *spaceRepository) GetAll(workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	var spaces []domain.Space
	var total int64

	baseQuery := r.db.Model(&domain.Space{}).Where("workspace_id = ?", workspaceID)

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := baseQuery.
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&spaces).Error

	return spaces, total, err
}

func (r *spaceRepository) GetDashboard(spaceID uint) (*domain.Space, error) {
	var space domain.Space

	err := r.db.
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

func (r *spaceRepository) CreateStatus(status *domain.TaskStatus) error {
	return r.db.Create(status).Error
}

func (r *spaceRepository) CreateTag(tag *domain.Tag) error {
	return r.db.Create(tag).Error
}

func (r *spaceRepository) CreateList(list *domain.List) error {
	return r.db.Create(list).Error
}

func (r *spaceRepository) UpdateStatus(status *domain.TaskStatus) error {
	return r.db.Save(status).Error
}

func (r *spaceRepository) GetListByID(id uint) (*domain.List, error) {
	var list domain.List
	err := r.db.Preload("Statuses", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&list, id).Error

	if err != nil {
		return nil, err
	}
	return &list, nil
}

func (r *spaceRepository) GetListsBySpace(spaceID uint) ([]domain.List, error) {
	var lists []domain.List
	err := r.db.Where("space_id = ?", spaceID).Order("created_at ASC").Find(&lists).Error
	if err != nil {
		return nil, err
	}
	return lists, nil
}

func (r *spaceRepository) GetStatusByID(id uint) (*domain.TaskStatus, error) {
	var status domain.TaskStatus
	if err := r.db.First(&status, id).Error; err != nil {
		return nil, err
	}
	return &status, nil
}

func (r *spaceRepository) GetListsByFolder(folderID uint) ([]domain.List, error) {
	var lists []domain.List
	err := r.db.Where("folder_id = ?", folderID).Order("created_at ASC").Find(&lists).Error
	if err != nil {
		return nil, err
	}
	return lists, nil
}

func (r *spaceRepository) UpdateList(list *domain.List) error {
	return r.db.Save(list).Error
}

func (r *spaceRepository) DeleteList(id uint) error {
	return r.db.Delete(&domain.List{}, id).Error
}
