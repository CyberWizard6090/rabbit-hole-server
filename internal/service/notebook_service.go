package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

type NotebookService struct {
	repo domain.NotebookRepository
}

func NewNotebookService(repo domain.NotebookRepository) *NotebookService {
	return &NotebookService{repo: repo}
}

type NotebookInput struct {
	Name        string
	Description *string
}

type NotebookPatch struct {
	Name        *string
	Description domain.Nullable[string]
	Position    *int64
}

func nameIssue(err error, field, value string, max int) error {
	switch {
	case value == "":
		return domain.InvalidField(err, field, "REQUIRED")
	case utf8.RuneCountInString(value) > max:
		return domain.InvalidField(err, field, "TOO_LONG").WithParams(map[string]any{"max": max})
	}
	return nil
}

func descriptionIssue(description *string) error {
	if description != nil && utf8.RuneCountInString(*description) > domain.MaxNotebookDescriptionRunes {
		return domain.InvalidField(domain.ErrInvalidNotebook, "description", "TOO_LONG").
			WithParams(map[string]any{"max": domain.MaxNotebookDescriptionRunes})
	}
	return nil
}

// trimmedPtr returns the trimmed text, or nil for a blank description.
func trimmedPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *NotebookService) List(ctx context.Context, workspaceID uint) ([]domain.Notebook, error) {
	return s.repo.List(ctx, workspaceID)
}

func (s *NotebookService) Get(ctx context.Context, id uint) (*domain.Notebook, error) {
	return s.repo.Get(ctx, id)
}

func (s *NotebookService) Create(ctx context.Context, workspaceID, userID uint, in NotebookInput) (*domain.Notebook, error) {
	name := strings.TrimSpace(in.Name)
	if err := nameIssue(domain.ErrInvalidNotebook, "name", name, domain.MaxNotebookNameRunes); err != nil {
		return nil, err
	}
	description := trimmedPtr(in.Description)
	if err := descriptionIssue(description); err != nil {
		return nil, err
	}
	count, err := s.repo.CountInWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if count >= domain.MaxNotebooksPerWorkspace {
		return nil, domain.ErrNotebookLimitReached
	}
	return s.repo.Create(ctx, domain.NotebookDraft{WorkspaceID: workspaceID, UserID: userID, Name: name, Description: description})
}

func (s *NotebookService) Update(ctx context.Context, id uint, patch NotebookPatch) (*domain.Notebook, error) {
	update := domain.NotebookUpdate{Position: patch.Position}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if err := nameIssue(domain.ErrInvalidNotebook, "name", name, domain.MaxNotebookNameRunes); err != nil {
			return nil, err
		}
		update.Name = &name
	}
	if patch.Description.Set {
		description := trimmedPtr(patch.Description.Value)
		if err := descriptionIssue(description); err != nil {
			return nil, err
		}
		update.Description = domain.Nullable[string]{Set: true, Value: description}
	}
	return s.repo.Update(ctx, id, update)
}

func (s *NotebookService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}
