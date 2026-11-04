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
}

func NewCommentService(repo domain.CommentRepository, permissions PermissionChecker) CommentService {
	return &commentService{repo: repo, permissions: permissions}
}

func (s *commentService) CreateComment(ctx context.Context, userID, taskID uint, body string, mentionIDs []uint) (*domain.Comment, error) {
	return s.repo.Create(ctx, taskID, userID, body, mentionIDs)
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
	return s.repo.Update(ctx, taskID, commentID, body, mentionIDs)
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
	return s.repo.Delete(ctx, taskID, commentID)
}
