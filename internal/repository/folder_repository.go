package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type folderRepository struct {
	db *gorm.DB
}

func NewFolderRepository(db *gorm.DB) domain.FolderRepository {
	return &folderRepository{db: db}
}

func (r *folderRepository) Create(folder *domain.Folder) error {
	return r.db.Create(folder).Error
}

func (r *folderRepository) GetByID(id uint) (*domain.Folder, error) {
	var folder domain.Folder
	if err := r.db.First(&folder, id).Error; err != nil {
		return nil, err
	}
	return &folder, nil
}

func (r *folderRepository) GetAllBySpace(spaceID uint) ([]domain.Folder, error) {
	var folders []domain.Folder
	err := r.db.Where("space_id = ?", spaceID).Find(&folders).Error
	return folders, err
}

func (r *folderRepository) Update(folder *domain.Folder) error {
	return r.db.Save(folder).Error
}

func (r *folderRepository) Delete(id uint) error {
	return r.db.Delete(&domain.Folder{}, id).Error
}
