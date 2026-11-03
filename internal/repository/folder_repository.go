package repository

import (
	"context"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type folderRepository struct {
	db *gorm.DB
}

func NewFolderRepository(db *gorm.DB) domain.FolderRepository {
	return &folderRepository{db: db}
}

func (r *folderRepository) Create(ctx context.Context, folder *domain.Folder) error {
	return r.db.WithContext(ctx).Create(folder).Error
}

func (r *folderRepository) GetByID(ctx context.Context, id uint) (*domain.Folder, error) {
	var folder domain.Folder
	if err := r.db.WithContext(ctx).First(&folder, id).Error; err != nil {
		return nil, err
	}
	return &folder, nil
}

func (r *folderRepository) GetAllBySpace(ctx context.Context, spaceID uint) ([]domain.Folder, error) {
	var folders []domain.Folder
	err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).Find(&folders).Error
	return folders, err
}

func (r *folderRepository) Update(ctx context.Context, folder *domain.Folder) error {
	return r.db.WithContext(ctx).Save(folder).Error
}

func (r *folderRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&domain.Folder{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
