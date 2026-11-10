package service

import (
	"context"

	"rabbit-hole-server/internal/domain"
)

type TaskRelationService struct {
	repo     domain.TaskRelationRepository
	activity ActivityRecorder
	tx       TransactionRunner
}

func NewTaskRelationService(repo domain.TaskRelationRepository, activity ActivityRecorder, tx TransactionRunner) *TaskRelationService {
	return &TaskRelationService{repo: repo, activity: activity, tx: tx}
}

func (s *TaskRelationService) List(ctx context.Context, taskID uint) (*domain.TaskRelations, error) {
	return s.repo.List(ctx, taskID)
}

func (s *TaskRelationService) Add(ctx context.Context, userID, taskID, otherID uint, kind string) error {
	return runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		if err := s.repo.Add(txCtx, taskID, otherID, kind, userID); err != nil {
			return err
		}
		return recordActivity(txCtx, s.activity, domain.ActivityEntry{
			TaskID: taskID, Actor: domain.UserActor(userID), Action: domain.ActivityRelationAdded,
			NewValue: mustJSON(map[string]any{"task_id": otherID, "kind": kind}),
		})
	})
}

func (s *TaskRelationService) Remove(ctx context.Context, userID, taskID, otherID uint, kind string) error {
	return runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		if err := s.repo.Remove(txCtx, taskID, otherID, kind); err != nil {
			return err
		}
		return recordActivity(txCtx, s.activity, domain.ActivityEntry{
			TaskID: taskID, Actor: domain.UserActor(userID), Action: domain.ActivityRelationRemoved,
			OldValue: mustJSON(map[string]any{"task_id": otherID, "kind": kind}),
		})
	})
}
