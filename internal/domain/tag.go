package domain

import (
	"context"
	"errors"
)

var ErrTagNameTaken = errors.New("tag with this name already exists in this space")

type Tag struct {
	ID      uint   `json:"id"`
	SpaceID uint   `json:"-"`
	Name    string `json:"name"`
	Color   string `json:"color"`
}

type TagRepository interface {
	Create(ctx context.Context, tag *Tag) error
	GetAllBySpace(ctx context.Context, spaceID uint) ([]Tag, error)
	GetByID(ctx context.Context, tagID uint) (*Tag, error)
	GetByName(ctx context.Context, spaceID uint, name string) (*Tag, error)
	Update(ctx context.Context, tag *Tag) error
	Delete(ctx context.Context, spaceID, tagID uint) error
	CountUsageBySpace(ctx context.Context, spaceID uint) (map[uint]int64, error)
	Merge(ctx context.Context, spaceID, sourceTagID, targetTagID uint) error
}
