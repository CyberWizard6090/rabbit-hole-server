package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type folderRepository struct {
	db *gorm.DB
}

func NewFolderRepository(db *gorm.DB) domain.FolderRepository {
	return &folderRepository{db: db}
}

func (r *folderRepository) Create(ctx context.Context, folder *domain.Folder) error {
	m := folderFromDomain(*folder)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapErr(err)
	}
	*folder = folderToDomain(m)
	return nil
}

func (r *folderRepository) GetByID(ctx context.Context, id uint) (*domain.Folder, error) {
	var m persistence.Folder
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	folder := folderToDomain(m)
	return &folder, nil
}

func (r *folderRepository) GetAllBySpace(ctx context.Context, spaceID uint) ([]domain.Folder, error) {
	var rows []persistence.Folder
	if err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, folderToDomain), nil
}

func (r *folderRepository) Update(ctx context.Context, folder *domain.Folder) error {
	m := folderFromDomain(*folder)
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return mapErr(err)
	}
	folder.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *folderRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&persistence.Folder{}, id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
