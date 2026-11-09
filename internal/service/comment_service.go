package service

import (
	"context"

	"rabbit-hole-server/internal/domain"
)

type CommentService interface {
	CreateComment(ctx context.Context, userID, taskID uint, body string, mentionIDs []uint) (*domain.Comment, error)
	ListComments(ctx context.Context, taskID uint, limit, offset int) ([]domain.Comment, int64, error)
	UpdateComment(ctx context.Context, userID, taskID, commentID uint, body *string, mentionIDs *[]uint) (*domain.Comment, error)
	DeleteComment(ctx context.Context, userID, taskID, commentID uint) error
}

type commentService struct {
	repo        domain.CommentRepository
	permissions PermissionChecker
	activity    ActivityRecorder
	tx          TransactionRunner
}

func (s *commentService) WithTransactions(tx TransactionRunner) *commentService {
	s.tx = tx
	return s
}

func NewCommentService(repo domain.CommentRepository, permissions PermissionChecker, activity ActivityRecorder) *commentService {
	return &commentService{repo: repo, permissions: permissions, activity: activity}
}

func (s *commentService) record(ctx context.Context, action string, userID, taskID, commentID uint) error {
	return recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: taskID, Actor: domain.UserActor(userID), Action: action,
		NewValue: mustJSON(map[string]uint{"comment_id": commentID}),
	})
}

func (s *commentService) CreateComment(ctx context.Context, userID, taskID uint, body string, mentionIDs []uint) (*domain.Comment, error) {
	var comment *domain.Comment
	err := runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		created, err := s.repo.Create(txCtx, taskID, userID, body, mentionIDs)
		if err != nil {
			return err
		}
		comment = created
		return s.record(txCtx, domain.ActivityCommentCreated, userID, taskID, created.ID)
	})
	if err != nil {
		return nil, err
	}
	return comment, nil
}

func (s *commentService) ListComments(ctx context.Context, taskID uint, limit, offset int) ([]domain.Comment, int64, error) {
	return s.repo.ListByTask(ctx, taskID, limit, offset)
}

func (s *commentService) UpdateComment(ctx context.Context, userID, taskID, commentID uint, body *string, mentionIDs *[]uint) (*domain.Comment, error) {
	existing, err := s.repo.GetByID(ctx, taskID, commentID)
	if err != nil {
		return nil, err
	}
	if existing.Author.ID != userID {
		return nil, domain.ErrCommentForbidden
	}
	var updated *domain.Comment
	err = runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		result, err := s.repo.Update(txCtx, taskID, commentID, body, mentionIDs)
		if err != nil {
			return err
		}
		updated = result
		return s.record(txCtx, domain.ActivityCommentUpdated, userID, taskID, commentID)
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *commentService) DeleteComment(ctx context.Context, userID, taskID, commentID uint) error {
	existing, err := s.repo.GetByID(ctx, taskID, commentID)
	if err != nil {
		return err
	}
	if existing.Author.ID != userID {
		allowed, err := s.permissions.HasTaskPermission(ctx, userID, taskID, domain.PermTaskDelete)
		if err != nil {
			return err
		}
		if !allowed {
			return domain.ErrCommentForbidden
		}
	}
	return runInTransaction(ctx, s.tx, func(txCtx context.Context) error {
		if err := s.repo.Delete(txCtx, taskID, commentID); err != nil {
			return err
		}
		return s.record(txCtx, domain.ActivityCommentDeleted, userID, taskID, commentID)
	})
}
