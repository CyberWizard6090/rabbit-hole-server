package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type tagRepository struct {
	db *gorm.DB
}

func NewTagRepository(db *gorm.DB) domain.TagRepository {
	return &tagRepository{db: db}
}

func (r *tagRepository) CountUsage(tagID uint) (int64, error) {
	var count int64
	err := r.db.Table("task_tags").Where("tag_id = ?", tagID).Count(&count).Error
	return count, err
}

func (r *tagRepository) Create(tag *domain.Tag) error {
	err := r.db.Create(tag).Error
	if err != nil && isUniqueViolation(err) {
		return domain.ErrTagNameTaken
	}
	return err
}

func (r *tagRepository) Delete(spaceID, tagID uint) error {
	return r.db.Where("id = ? AND space_id = ?", tagID, spaceID).Delete(&domain.Tag{}).Error
}

func (r *tagRepository) GetAllBySpace(spaceID uint) ([]domain.Tag, error) {
	var tags []domain.Tag
	err := r.db.Where("space_id = ?", spaceID).Find(&tags).Error
	return tags, err
}

func (r *tagRepository) GetByID(tagID uint) (*domain.Tag, error) {
	var tag domain.Tag
	err := r.db.First(&tag, tagID).Error
	return &tag, err
}

func (r *tagRepository) GetByName(spaceID uint, name string) (*domain.Tag, error) {
	var tag domain.Tag
	err := r.db.
		Where("space_id = ? AND LOWER(name) = LOWER(?)", spaceID, name).
		First(&tag).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *tagRepository) Merge(spaceID, sourceTagID, targetTagID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
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

		return tx.Where("space_id = ? AND id = ?", spaceID, sourceTagID).Delete(&domain.Tag{}).Error
	})
}

func (r *tagRepository) Update(tag *domain.Tag) error {
	return r.db.Save(tag).Error
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
