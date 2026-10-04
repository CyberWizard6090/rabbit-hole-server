package domain

import "errors"

var ErrTagNameTaken = errors.New("tag with this name already exists in this space")

type Tag struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	SpaceID uint   `gorm:"index;not null;column:space_id" json:"-"`
	Name    string `gorm:"not null" json:"name"`
	Color   string `gorm:"size:7;not null" json:"color"`
}

type TagRepository interface {
	Create(tag *Tag) error
	GetAllBySpace(spaceID uint) ([]Tag, error)
	GetByID(tagID uint) (*Tag, error)
	GetByName(spaceID uint, name string) (*Tag, error)
	Update(tag *Tag) error
	Delete(spaceID, tagID uint) error
	CountUsage(tagID uint) (int64, error)
	Merge(spaceID, sourceTagID, targetTagID uint) error
}
