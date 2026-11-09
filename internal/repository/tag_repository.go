package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type tagRepository struct {
	db *gorm.DB
}

func NewTagRepository(db *gorm.DB) domain.TagRepository {
	return &tagRepository{db: db}
}

func (r *tagRepository) CountUsageBySpace(ctx context.Context, spaceID uint) (map[uint]int64, error) {
	var rows []struct {
		TagID uint
		Count int64
	}
	err := database.DBFromContext(ctx, r.db).Table("task_tags").
		Select("task_tags.tag_id AS tag_id, COUNT(*) AS count").
		Joins("JOIN tags ON tags.id = task_tags.tag_id").
		Where("tags.space_id = ?", spaceID).
		Group("task_tags.tag_id").
		Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}

	usage := make(map[uint]int64, len(rows))
	for _, row := range rows {
		usage[row.TagID] = row.Count
	}
	return usage, nil
}

func (r *tagRepository) Create(ctx context.Context, tag *domain.Tag) error {
	m := tagFromDomain(*tag)
	err := database.DBFromContext(ctx, r.db).Create(&m).Error
	if err != nil && isUniqueViolation(err) {
		return domain.ErrTagNameTaken
	}
	if err != nil {
		return mapErr(err)
	}
	*tag = tagToDomain(m)
	return nil
}

func (r *tagRepository) Delete(ctx context.Context, spaceID, tagID uint) error {
	res := database.DBFromContext(ctx, r.db).Where("id = ? AND space_id = ?", tagID, spaceID).Delete(&persistence.Tag{})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *tagRepository) GetAllBySpace(ctx context.Context, spaceID uint) ([]domain.Tag, error) {
	var rows []persistence.Tag
	if err := database.DBFromContext(ctx, r.db).Where("space_id = ?", spaceID).Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, tagToDomain), nil
}

func (r *tagRepository) GetByID(ctx context.Context, tagID uint) (*domain.Tag, error) {
	var m persistence.Tag
	if err := database.DBFromContext(ctx, r.db).First(&m, tagID).Error; err != nil {
		return nil, mapErr(err)
	}
	tag := tagToDomain(m)
	return &tag, nil
}

func (r *tagRepository) GetByName(ctx context.Context, spaceID uint, name string) (*domain.Tag, error) {
	var m persistence.Tag
	err := database.DBFromContext(ctx, r.db).
		Where("space_id = ? AND LOWER(name) = LOWER(?)", spaceID, name).
		First(&m).Error
	if err != nil {
		return nil, mapErr(err)
	}
	tag := tagToDomain(m)
	return &tag, nil
}

func (r *tagRepository) Merge(ctx context.Context, spaceID, sourceTagID, targetTagID uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO task_tags (task_id, tag_id)
			SELECT task_id, ? FROM task_tags
			WHERE tag_id = ?
			  AND task_id NOT IN (SELECT task_id FROM task_tags WHERE tag_id = ?)
		`, targetTagID, sourceTagID, targetTagID).Error; err != nil {
			return err
		}

		if err := tx.Exec(`DELETE FROM task_tags WHERE tag_id = ?`, sourceTagID).Error; err != nil {
			return err
		}

		return tx.Where("space_id = ? AND id = ?", spaceID, sourceTagID).Delete(&persistence.Tag{}).Error
	}))
}

func (r *tagRepository) Update(ctx context.Context, tag *domain.Tag) error {
	m := tagFromDomain(*tag)
	if err := database.DBFromContext(ctx, r.db).Save(&m).Error; err != nil {
		if isUniqueViolation(err) {
			return domain.ErrTagNameTaken
		}
		return mapErr(err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
