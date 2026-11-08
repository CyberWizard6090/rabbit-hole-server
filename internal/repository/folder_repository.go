package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
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
	if err := database.DBFromContext(ctx, r.db).Create(&m).Error; err != nil {
		return mapErr(err)
	}
	*folder = folderToDomain(m)
	return nil
}

func (r *folderRepository) GetByID(ctx context.Context, id uint) (*domain.Folder, error) {
	var m persistence.Folder
	if err := database.DBFromContext(ctx, r.db).First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	folder := folderToDomain(m)
	return &folder, nil
}

func (r *folderRepository) GetAllBySpace(ctx context.Context, spaceID uint) ([]domain.Folder, error) {
	var rows []persistence.Folder
	if err := database.DBFromContext(ctx, r.db).Where("space_id = ?", spaceID).Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, folderToDomain), nil
}

func (r *folderRepository) Update(ctx context.Context, folder *domain.Folder) error {
	m := folderFromDomain(*folder)
	res := database.DBFromContext(ctx, r.db).Model(&m).Select("Name").Updates(&m)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	folder.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *folderRepository) Delete(ctx context.Context, id uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		return softDeleteFolderTree(tx, id)
	}))
}
