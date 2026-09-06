package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

var (
	ErrTagNotFound      = errors.New("tag not found")
	ErrTagSpaceMismatch = errors.New("tag does not belong to this space")
	ErrCannotMergeSame  = errors.New("cannot merge a tag with itself")
)

type CreateTagParams struct {
	Name  string
	Color string
}

type UpdateTagParams struct {
	Name  *string
	Color *string
}

type TagWithUsage struct {
	domain.Tag
	UsageCount int64 `json:"usage_count"`
}

type TagService interface {
	CreateTag(spaceID uint, params CreateTagParams) (*domain.Tag, error)
	GetAllBySpace(spaceID uint) ([]TagWithUsage, error)
	UpdateTag(spaceID, tagID uint, params UpdateTagParams) (*domain.Tag, error)
	DeleteTag(spaceID, tagID uint) error

	GetOrCreateByName(spaceID uint, name string, defaultColor string) (*domain.Tag, error)

	MergeTags(spaceID, sourceTagID, targetTagID uint) error
}

type tagService struct {
	tagRepo domain.TagRepository
}

func NewTagService(tagRepo domain.TagRepository) TagService {
	return &tagService{tagRepo: tagRepo}
}

func (s *tagService) CreateTag(spaceID uint, params CreateTagParams) (*domain.Tag, error) {
	tag := &domain.Tag{SpaceID: spaceID, Name: params.Name, Color: params.Color}
	if err := s.tagRepo.Create(tag); err != nil {
		if errors.Is(err, domain.ErrTagNameTaken) {
			return nil, err
		}
		return nil, fmt.Errorf("create tag in repo: %w", err)
	}
	return tag, nil
}

func (s *tagService) GetOrCreateByName(spaceID uint, name string, defaultColor string) (*domain.Tag, error) {
	existing, err := s.tagRepo.GetByName(spaceID, name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lookup tag by name: %w", err)
	}

	tag := &domain.Tag{SpaceID: spaceID, Name: name, Color: defaultColor}
	if err := s.tagRepo.Create(tag); err != nil {
		return nil, fmt.Errorf("create tag on the fly: %w", err)
	}
	return tag, nil
}

func (s *tagService) GetAllBySpace(spaceID uint) ([]TagWithUsage, error) {
	tags, err := s.tagRepo.GetAllBySpace(spaceID)
	if err != nil {
		return nil, fmt.Errorf("get all tags by space from repo: %w", err)
	}

	result := make([]TagWithUsage, len(tags))
	for i, t := range tags {
		count, err := s.tagRepo.CountUsage(t.ID)
		if err != nil {
			return nil, fmt.Errorf("count tag usage: %w", err)
		}
		result[i] = TagWithUsage{Tag: t, UsageCount: count}
	}
	return result, nil
}

func (s *tagService) UpdateTag(spaceID, tagID uint, params UpdateTagParams) (*domain.Tag, error) {
	tag, err := s.tagRepo.GetByID(tagID)
	if err != nil {
		return nil, ErrTagNotFound
	}
	if tag.SpaceID != spaceID {
		return nil, ErrTagSpaceMismatch
	}

	if params.Name != nil {
		tag.Name = *params.Name
	}
	if params.Color != nil {
		tag.Color = *params.Color
	}

	if err := s.tagRepo.Update(tag); err != nil {
		if errors.Is(err, domain.ErrTagNameTaken) {
			return nil, err
		}
		return nil, fmt.Errorf("update tag in repo: %w", err)
	}
	return tag, nil
}

func (s *tagService) DeleteTag(spaceID, tagID uint) error {
	tag, err := s.tagRepo.GetByID(tagID)
	if err != nil {
		return ErrTagNotFound
	}
	if tag.SpaceID != spaceID {
		return ErrTagSpaceMismatch
	}
	if err := s.tagRepo.Delete(spaceID, tagID); err != nil {
		return fmt.Errorf("delete tag from repo: %w", err)
	}
	return nil
}

func (s *tagService) MergeTags(spaceID, sourceTagID, targetTagID uint) error {
	if sourceTagID == targetTagID {
		return ErrCannotMergeSame
	}

	source, err := s.tagRepo.GetByID(sourceTagID)
	if err != nil {
		return ErrTagNotFound
	}
	target, err := s.tagRepo.GetByID(targetTagID)
	if err != nil {
		return ErrTagNotFound
	}
	if source.SpaceID != spaceID || target.SpaceID != spaceID {
		return ErrTagSpaceMismatch
	}

	return s.tagRepo.Merge(spaceID, sourceTagID, targetTagID)
}
