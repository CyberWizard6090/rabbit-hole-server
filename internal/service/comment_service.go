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
}

func NewCommentService(repo domain.CommentRepository, permissions PermissionChecker, activity ActivityRecorder) CommentService {
	return &commentService{repo: repo, permissions: permissions, activity: activity}
}

func (s *commentService) record(ctx context.Context, action string, userID, taskID, commentID uint) {
	recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: taskID, ActorID: userID, Action: action,
		NewValue: mustJSON(map[string]uint{"comment_id": commentID}),
	})
}

func (s *commentService) CreateComment(ctx context.Context, userID, taskID uint, body string, mentionIDs []uint) (*domain.Comment, error) {
	comment, err := s.repo.Create(ctx, taskID, userID, body, mentionIDs)
	if err != nil {
		return nil, err
	}
	s.record(ctx, domain.ActivityCommentCreated, userID, taskID, comment.ID)
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
	updated, err := s.repo.Update(ctx, taskID, commentID, body, mentionIDs)
	if err != nil {
		return nil, err
	}
	s.record(ctx, domain.ActivityCommentUpdated, userID, taskID, commentID)
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
	if err := s.repo.Delete(ctx, taskID, commentID); err != nil {
		return err
	}
	s.record(ctx, domain.ActivityCommentDeleted, userID, taskID, commentID)
	return nil
}
