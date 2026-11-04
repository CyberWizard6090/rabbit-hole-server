package service

import (
	"context"

	"rabbit-hole-server/internal/domain"
)

type ChecklistService interface {
	CreateChecklist(ctx context.Context, userID, taskID uint, name string) (*domain.Checklist, error)
	ListChecklists(ctx context.Context, taskID uint) ([]domain.Checklist, error)
	RenameChecklist(ctx context.Context, taskID, checklistID uint, name string) (*domain.Checklist, error)
	DeleteChecklist(ctx context.Context, userID, taskID, checklistID uint) error
	ReorderChecklists(ctx context.Context, taskID uint, ids []uint) error

	CreateItem(ctx context.Context, userID, taskID, checklistID uint, text string, assigneeID *uint) (*domain.ChecklistItem, error)
	UpdateItem(ctx context.Context, userID, taskID, checklistID, itemID uint, update domain.ChecklistItemUpdate) (*domain.ChecklistItem, error)
	DeleteItem(ctx context.Context, taskID, checklistID, itemID uint) error
	ReorderItems(ctx context.Context, taskID, checklistID uint, ids []uint) error
}

type checklistService struct {
	repo     domain.ChecklistRepository
	activity ActivityRecorder
}

func NewChecklistService(repo domain.ChecklistRepository, activity ActivityRecorder) ChecklistService {
	return &checklistService{repo: repo, activity: activity}
}

func (s *checklistService) CreateChecklist(ctx context.Context, userID, taskID uint, name string) (*domain.Checklist, error) {
	list, err := s.repo.CreateChecklist(ctx, taskID, name)
	if err != nil {
		return nil, err
	}
	recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: taskID, ActorID: userID, Action: domain.ActivityChecklistCreated,
		NewValue: mustJSON(map[string]any{"checklist_id": list.ID, "name": list.Name}),
	})
	return list, nil
}

func (s *checklistService) ListChecklists(ctx context.Context, taskID uint) ([]domain.Checklist, error) {
	return s.repo.ListByTask(ctx, taskID)
}

func (s *checklistService) RenameChecklist(ctx context.Context, taskID, checklistID uint, name string) (*domain.Checklist, error) {
	return s.repo.RenameChecklist(ctx, taskID, checklistID, name)
}

func (s *checklistService) DeleteChecklist(ctx context.Context, userID, taskID, checklistID uint) error {
	existing, err := s.repo.GetChecklist(ctx, taskID, checklistID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteChecklist(ctx, taskID, checklistID); err != nil {
		return err
	}
	recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: taskID, ActorID: userID, Action: domain.ActivityChecklistDeleted,
		OldValue: mustJSON(map[string]any{"checklist_id": existing.ID, "name": existing.Name}),
	})
	return nil
}

func (s *checklistService) ReorderChecklists(ctx context.Context, taskID uint, ids []uint) error {
	return s.repo.ReorderChecklists(ctx, taskID, ids)
}

func (s *checklistService) CreateItem(
	ctx context.Context, userID, taskID, checklistID uint, text string, assigneeID *uint,
) (*domain.ChecklistItem, error) {
	item, err := s.repo.CreateItem(ctx, taskID, checklistID, text, assigneeID)
	if err != nil {
		return nil, err
	}
	s.recordAssignment(ctx, userID, taskID, item)
	return item, nil
}

func (s *checklistService) recordAssignment(ctx context.Context, userID, taskID uint, item *domain.ChecklistItem) {
	if item.Assignee == nil {
		return
	}
	recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: taskID, ActorID: userID, Action: domain.ActivityChecklistItemAssigned,
		NewValue: mustJSON(map[string]any{
			"checklist_id": item.ChecklistID, "item_id": item.ID, "text": item.Text, "assignee_id": item.Assignee.ID,
		}),
	})
}

func (s *checklistService) UpdateItem(
	ctx context.Context, userID, taskID, checklistID, itemID uint, update domain.ChecklistItemUpdate,
) (*domain.ChecklistItem, error) {
	before, err := s.repo.GetItem(ctx, taskID, checklistID, itemID)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.UpdateItem(ctx, taskID, checklistID, itemID, userID, update)
	if err != nil {
		return nil, err
	}
	if item.Resolved != before.Resolved {
		action := domain.ActivityChecklistItemReopened
		if item.Resolved {
			action = domain.ActivityChecklistItemResolved
		}
		recordActivity(ctx, s.activity, domain.ActivityEntry{
			TaskID: taskID, ActorID: userID, Action: action,
			NewValue: mustJSON(map[string]any{"checklist_id": checklistID, "item_id": itemID, "text": item.Text}),
		})
	}
	if item.Assignee != nil && (before.Assignee == nil || before.Assignee.ID != item.Assignee.ID) {
		s.recordAssignment(ctx, userID, taskID, item)
	}
	return item, nil
}

func (s *checklistService) DeleteItem(ctx context.Context, taskID, checklistID, itemID uint) error {
	return s.repo.DeleteItem(ctx, taskID, checklistID, itemID)
}

func (s *checklistService) ReorderItems(ctx context.Context, taskID, checklistID uint, ids []uint) error {
	return s.repo.ReorderItems(ctx, taskID, checklistID, ids)
}
