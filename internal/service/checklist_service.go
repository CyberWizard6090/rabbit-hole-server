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
	tx       TransactionRunner
}

func NewChecklistService(repo domain.ChecklistRepository, activity ActivityRecorder) *checklistService {
	return &checklistService{repo: repo, activity: activity}
}

func (s *checklistService) WithTransactions(tx TransactionRunner) *checklistService {
	s.tx = tx
	return s
}

func (s *checklistService) CreateChecklist(ctx context.Context, userID, taskID uint, name string) (*domain.Checklist, error) {
	var list *domain.Checklist
	err := runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		created, err := s.repo.CreateChecklist(txCtx, taskID, name)
		if err != nil {
			return err
		}
		list = created
		return recordActivity(txCtx, s.activity, domain.ActivityEntry{
			TaskID: taskID, Actor: domain.UserActor(userID), Action: domain.ActivityChecklistCreated,
			NewValue: mustJSON(map[string]any{"checklist_id": created.ID, "name": created.Name}),
		})
	})
	if err != nil {
		return nil, err
	}
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
	return runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		if err := s.repo.DeleteChecklist(txCtx, taskID, checklistID); err != nil {
			return err
		}
		return recordActivity(txCtx, s.activity, domain.ActivityEntry{
			TaskID: taskID, Actor: domain.UserActor(userID), Action: domain.ActivityChecklistDeleted,
			OldValue: mustJSON(map[string]any{"checklist_id": existing.ID, "name": existing.Name}),
		})
	})
}

func (s *checklistService) ReorderChecklists(ctx context.Context, taskID uint, ids []uint) error {
	return s.repo.ReorderChecklists(ctx, taskID, ids)
}

func (s *checklistService) CreateItem(
	ctx context.Context, userID, taskID, checklistID uint, text string, assigneeID *uint,
) (*domain.ChecklistItem, error) {
	var item *domain.ChecklistItem
	err := runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		created, err := s.repo.CreateItem(txCtx, taskID, checklistID, text, assigneeID)
		if err != nil {
			return err
		}
		item = created
		return s.recordAssignment(txCtx, userID, taskID, created)
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *checklistService) recordAssignment(ctx context.Context, userID, taskID uint, item *domain.ChecklistItem) error {
	if item.Assignee == nil {
		return nil
	}
	return recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: taskID, Actor: domain.UserActor(userID), Action: domain.ActivityChecklistItemAssigned,
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
	var item *domain.ChecklistItem
	err = runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		updated, err := s.repo.UpdateItem(txCtx, taskID, checklistID, itemID, userID, update)
		if err != nil {
			return err
		}
		item = updated
		if updated.Resolved != before.Resolved {
			action := domain.ActivityChecklistItemReopened
			if updated.Resolved {
				action = domain.ActivityChecklistItemResolved
			}
			if err := recordActivity(txCtx, s.activity, domain.ActivityEntry{
				TaskID: taskID, Actor: domain.UserActor(userID), Action: action,
				NewValue: mustJSON(map[string]any{"checklist_id": checklistID, "item_id": itemID, "text": updated.Text}),
			}); err != nil {
				return err
			}
		}
		if updated.Assignee != nil && (before.Assignee == nil || before.Assignee.ID != updated.Assignee.ID) {
			return s.recordAssignment(txCtx, userID, taskID, updated)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *checklistService) DeleteItem(ctx context.Context, taskID, checklistID, itemID uint) error {
	return s.repo.DeleteItem(ctx, taskID, checklistID, itemID)
}

func (s *checklistService) ReorderItems(ctx context.Context, taskID, checklistID uint, ids []uint) error {
	return s.repo.ReorderItems(ctx, taskID, checklistID, ids)
}
