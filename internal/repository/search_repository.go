package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type searchRepository struct {
	db *gorm.DB
}

func NewSearchRepository(db *gorm.DB) domain.SearchRepository {
	return &searchRepository{db: db}
}

type searchRow struct {
	Type        string
	ID          uint
	TaskID      uint
	ListID      uint
	SpaceID     uint
	TaskTitle   string
	Text        string
	Description *string
	Score       float64
	UpdatedAt   time.Time
}

func (r *searchRepository) Search(ctx context.Context, params domain.SearchParams) ([]domain.SearchHit, int64, error) {
	hits := []domain.SearchHit{}
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('pg_trgm.word_similarity_threshold', '" + wordSimilarityLimit + "', true)").Error; err != nil {
			return err
		}
		countSQL, countArgs := buildSearchCountQuery(params)
		if err := tx.Raw(countSQL, countArgs...).Scan(&total).Error; err != nil {
			return err
		}
		if total == 0 || int64(params.Offset) >= total {
			return nil
		}
		selectSQL, args := buildSearchQuery(params)
		var rows []searchRow
		if err := tx.Raw(selectSQL, args...).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			hits = append(hits, domain.SearchHit{
				Type: domain.SearchType(row.Type), ID: row.ID, TaskID: row.TaskID, ListID: row.ListID, SpaceID: row.SpaceID,
				TaskTitle: row.TaskTitle, Text: row.Text, Description: row.Description, Score: row.Score, UpdatedAt: row.UpdatedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	return hits, total, nil
}
