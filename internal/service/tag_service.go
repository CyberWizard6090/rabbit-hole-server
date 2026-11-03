package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

var (
	// Not-found style errors wrap gorm.ErrRecordNotFound so the HTTP layer
	// answers 404 and does not reveal whether the ID exists in another space.
	ErrTagNotFound      = fmt.Errorf("tag not found: %w", gorm.ErrRecordNotFound)
	ErrTagSpaceMismatch = fmt.Errorf("tag does not belong to this space: %w", gorm.ErrRecordNotFound)
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
	CreateTag(ctx context.Context, spaceID uint, params CreateTagParams) (*domain.Tag, error)
	GetAllBySpace(ctx context.Context, spaceID uint) ([]TagWithUsage, error)
	GetTag(ctx context.Context, spaceID, tagID uint) (*TagWithUsage, error)
	UpdateTag(ctx context.Context, spaceID, tagID uint, params UpdateTagParams) (*domain.Tag, error)
	DeleteTag(ctx context.Context, spaceID, tagID uint) error

	GetOrCreateByName(ctx context.Context, spaceID uint, name string, defaultColor string) (*domain.Tag, error)

	MergeTags(ctx context.Context, spaceID, sourceTagID, targetTagID uint) error
}

type tagService struct {
	tagRepo domain.TagRepository
}

func NewTagService(tagRepo domain.TagRepository) TagService {
	return &tagService{tagRepo: tagRepo}
}

func (s *tagService) CreateTag(ctx context.Context, spaceID uint, params CreateTagParams) (*domain.Tag, error) {
	tag := &domain.Tag{SpaceID: spaceID, Name: params.Name, Color: params.Color}
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		if errors.Is(err, domain.ErrTagNameTaken) {
			return nil, err
		}
		return nil, fmt.Errorf("create tag in repo: %w", err)
	}
	return tag, nil
}

func (s *tagService) GetOrCreateByName(ctx context.Context, spaceID uint, name string, defaultColor string) (*domain.Tag, error) {
	existing, err := s.tagRepo.GetByName(ctx, spaceID, name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lookup tag by name: %w", err)
	}

	tag := &domain.Tag{SpaceID: spaceID, Name: name, Color: defaultColor}
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		if errors.Is(err, domain.ErrTagNameTaken) {
			existing, lookupErr := s.tagRepo.GetByName(ctx, spaceID, name)
			if lookupErr != nil {
				return nil, fmt.Errorf("lookup tag after concurrent create: %w", lookupErr)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("create tag on the fly: %w", err)
	}
	return tag, nil
}

func (s *tagService) GetAllBySpace(ctx context.Context, spaceID uint) ([]TagWithUsage, error) {
	tags, err := s.tagRepo.GetAllBySpace(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("get all tags by space from repo: %w", err)
	}

	usage, err := s.tagRepo.CountUsageBySpace(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("count tag usage: %w", err)
	}

	result := make([]TagWithUsage, len(tags))
	for i, t := range tags {
		result[i] = TagWithUsage{Tag: t, UsageCount: usage[t.ID]}
	}
	return result, nil
}

func (s *tagService) GetTag(ctx context.Context, spaceID, tagID uint) (*TagWithUsage, error) {
	tag, err := s.tagRepo.GetByID(ctx, tagID)
	if err != nil {
		return nil, ErrTagNotFound
	}
	if tag.SpaceID != spaceID {
		return nil, ErrTagSpaceMismatch
	}
	usage, err := s.tagRepo.CountUsageBySpace(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("count tag usage: %w", err)
	}
	return &TagWithUsage{Tag: *tag, UsageCount: usage[tagID]}, nil
}

func (s *tagService) UpdateTag(ctx context.Context, spaceID, tagID uint, params UpdateTagParams) (*domain.Tag, error) {
	tag, err := s.tagRepo.GetByID(ctx, tagID)
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

	if err := s.tagRepo.Update(ctx, tag); err != nil {
		if errors.Is(err, domain.ErrTagNameTaken) {
			return nil, err
		}
		return nil, fmt.Errorf("update tag in repo: %w", err)
	}
	return tag, nil
}

func (s *tagService) DeleteTag(ctx context.Context, spaceID, tagID uint) error {
	tag, err := s.tagRepo.GetByID(ctx, tagID)
	if err != nil {
		return ErrTagNotFound
	}
	if tag.SpaceID != spaceID {
		return ErrTagSpaceMismatch
	}
	if err := s.tagRepo.Delete(ctx, spaceID, tagID); err != nil {
		return fmt.Errorf("delete tag from repo: %w", err)
	}
	return nil
}

func (s *tagService) MergeTags(ctx context.Context, spaceID, sourceTagID, targetTagID uint) error {
	if sourceTagID == targetTagID {
		return ErrCannotMergeSame
	}

	source, err := s.tagRepo.GetByID(ctx, sourceTagID)
	if err != nil {
		return ErrTagNotFound
	}
	target, err := s.tagRepo.GetByID(ctx, targetTagID)
	if err != nil {
		return ErrTagNotFound
	}
	if source.SpaceID != spaceID || target.SpaceID != spaceID {
		return ErrTagSpaceMismatch
	}

	return s.tagRepo.Merge(ctx, spaceID, sourceTagID, targetTagID)
}
