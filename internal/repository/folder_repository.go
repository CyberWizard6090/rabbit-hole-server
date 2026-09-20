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
	res := r.db.Delete(&domain.Folder{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
