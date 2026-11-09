package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

type SavedViewService struct {
	repo        domain.SavedViewRepository
	permissions PermissionChecker
}

func NewSavedViewService(repo domain.SavedViewRepository, permissions PermissionChecker) *SavedViewService {
	return &SavedViewService{repo: repo, permissions: permissions}
}

type SavedViewInput struct {
	Name    string
	ListID  *uint
	Shared  bool
	Filters map[string]string
	Sort    string
}

func viewNameIssue(name string) error {
	switch {
	case name == "":
		return domain.InvalidField(domain.ErrInvalidView, "name", "REQUIRED")
	case utf8.RuneCountInString(name) > domain.MaxSavedViewNameRunes:
		return domain.InvalidField(domain.ErrInvalidView, "name", "TOO_LONG").WithParams(map[string]any{"max": domain.MaxSavedViewNameRunes})
	}
	return nil
}

func (s *SavedViewService) canShare(ctx context.Context, userID, workspaceID uint) (bool, error) {
	return s.permissions.HasWorkspacePermission(ctx, userID, workspaceID, domain.PermSpaceUpdate)
}

func (s *SavedViewService) List(ctx context.Context, workspaceID, userID uint, listID *uint) ([]domain.SavedView, error) {
	return s.repo.List(ctx, workspaceID, userID, listID)
}

func (s *SavedViewService) Create(ctx context.Context, workspaceID, userID uint, in SavedViewInput) (*domain.SavedView, error) {
	name := strings.TrimSpace(in.Name)
	if err := viewNameIssue(name); err != nil {
		return nil, err
	}
	if in.Shared {
		allowed, err := s.canShare(ctx, userID, workspaceID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, domain.ErrViewForbidden
		}
	}
	if in.ListID != nil {
		ok, err := s.repo.ListBelongsToWorkspace(ctx, workspaceID, *in.ListID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, domain.InvalidField(domain.ErrInvalidView, "list_id", "NOT_FOUND")
		}
	}
	owned, err := s.repo.CountOwned(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	if owned >= domain.MaxSavedViewsPerUser {
		return nil, domain.ErrViewLimitReached
	}
	return s.repo.Create(ctx, domain.SavedViewDraft{
		WorkspaceID: workspaceID, ListID: in.ListID, OwnerID: userID, Name: name, Shared: in.Shared, Filters: in.Filters, Sort: in.Sort,
	})
}

func (s *SavedViewService) mutable(ctx context.Context, workspaceID, userID, id uint) (*domain.SavedView, error) {
	view, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if view.OwnerID == userID {
		return view, nil
	}
	if !view.Shared {
		return nil, domain.ErrViewNotFound
	}
	allowed, err := s.canShare(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, domain.ErrViewForbidden
	}
	return view, nil
}

type SavedViewPatch struct {
	Name    *string
	Shared  *bool
	Filters map[string]string
	Sort    *string
}

func (s *SavedViewService) Update(ctx context.Context, workspaceID, userID, id uint, patch SavedViewPatch) (*domain.SavedView, error) {
	view, err := s.mutable(ctx, workspaceID, userID, id)
	if err != nil {
		return nil, err
	}
	update := domain.SavedViewUpdate{Shared: patch.Shared, Filters: patch.Filters, Sort: patch.Sort}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if err := viewNameIssue(name); err != nil {
			return nil, err
		}
		update.Name = &name
	}
	if patch.Shared != nil && *patch.Shared && !view.Shared {
		allowed, err := s.canShare(ctx, userID, workspaceID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, domain.ErrViewForbidden
		}
	}
	updated, err := s.repo.Update(ctx, workspaceID, id, update)
	if err != nil {
		return nil, err
	}
	updated.IsOwner = updated.OwnerID == userID
	return updated, nil
}

func (s *SavedViewService) Delete(ctx context.Context, workspaceID, userID, id uint) error {
	if _, err := s.mutable(ctx, workspaceID, userID, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, workspaceID, id)
}
