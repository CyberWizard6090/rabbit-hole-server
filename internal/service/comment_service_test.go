package service

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

type commentRepositoryMock struct {
	existing    *domain.Comment
	getErr      error
	createdBody string
	createdBy   uint
	mentions    []uint
	updated     bool
	deleted     bool
}

func (m *commentRepositoryMock) Create(_ context.Context, taskID, authorID uint, body string, mentionIDs []uint) (*domain.Comment, error) {
	m.createdBody, m.createdBy, m.mentions = body, authorID, mentionIDs
	return &domain.Comment{ID: 1, TaskID: taskID, Author: domain.CommentUser{ID: authorID}, Body: body}, nil
}

func (m *commentRepositoryMock) ListByTask(context.Context, uint, int, int) ([]domain.Comment, int64, error) {
	return nil, 0, nil
}

func (m *commentRepositoryMock) GetByID(context.Context, uint, uint) (*domain.Comment, error) {
	return m.existing, m.getErr
}

func (m *commentRepositoryMock) Update(_ context.Context, _, _ uint, _ *string, _ *[]uint) (*domain.Comment, error) {
	m.updated = true
	return m.existing, nil
}

func (m *commentRepositoryMock) Delete(context.Context, uint, uint) error {
	m.deleted = true
	return nil
}

type taskPermissionStub struct {
	allow bool
	err   error
	code  string
	calls int
}

func (p *taskPermissionStub) HasWorkspacePermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (p *taskPermissionStub) HasSpacePermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (p *taskPermissionStub) HasFolderPermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (p *taskPermissionStub) HasListPermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (p *taskPermissionStub) HasTaskPermission(_ context.Context, _, _ uint, code string) (bool, error) {
	p.calls++
	p.code = code
	return p.allow, p.err
}

func commentBy(author uint) *domain.Comment {
	return &domain.Comment{ID: 4, TaskID: 9, Author: domain.CommentUser{ID: author}}
}

func TestCommentServiceCreateRecordsAuthorAndMentions(t *testing.T) {
	repo := &commentRepositoryMock{}
	svc := NewCommentService(repo, &taskPermissionStub{}, nil)

	comment, err := svc.CreateComment(context.Background(), 7, 9, "hello", []uint{2, 3})
	if err != nil {
		t.Fatalf("CreateComment error=%v", err)
	}
	if repo.createdBy != 7 || repo.createdBody != "hello" || len(repo.mentions) != 2 || comment.Author.ID != 7 {
		t.Fatalf("repo=%+v comment=%+v", repo, comment)
	}
}

func TestCommentServiceOnlyTheAuthorCanEdit(t *testing.T) {
	repo := &commentRepositoryMock{existing: commentBy(7)}
	svc := NewCommentService(repo, &taskPermissionStub{allow: true}, nil)
	body := "new"

	if _, err := svc.UpdateComment(context.Background(), 8, 9, 4, &body, nil); !errors.Is(err, domain.ErrCommentForbidden) {
		t.Fatalf("other user: err=%v", err)
	}
	if repo.updated {
		t.Fatal("a forbidden edit reached the repository")
	}
	if _, err := svc.UpdateComment(context.Background(), 7, 9, 4, &body, nil); err != nil || !repo.updated {
		t.Fatalf("author: err=%v updated=%t", err, repo.updated)
	}
}

func TestCommentServiceDeleteByAuthorNeedsNoPermissionLookup(t *testing.T) {
	repo := &commentRepositoryMock{existing: commentBy(7)}
	perms := &taskPermissionStub{}
	svc := NewCommentService(repo, perms, nil)

	if err := svc.DeleteComment(context.Background(), 7, 9, 4); err != nil || !repo.deleted {
		t.Fatalf("err=%v deleted=%t", err, repo.deleted)
	}
	if perms.calls != 0 {
		t.Fatalf("permission lookups = %d, want 0", perms.calls)
	}
}

func TestCommentServiceDeleteByAnotherUserNeedsTaskDelete(t *testing.T) {
	repo := &commentRepositoryMock{existing: commentBy(7)}
	perms := &taskPermissionStub{allow: false}
	svc := NewCommentService(repo, perms, nil)

	if err := svc.DeleteComment(context.Background(), 8, 9, 4); !errors.Is(err, domain.ErrCommentForbidden) || repo.deleted {
		t.Fatalf("err=%v deleted=%t", err, repo.deleted)
	}
	if perms.code != domain.PermTaskDelete {
		t.Fatalf("checked permission %q", perms.code)
	}

	perms.allow = true
	if err := svc.DeleteComment(context.Background(), 8, 9, 4); err != nil || !repo.deleted {
		t.Fatalf("moderator: err=%v deleted=%t", err, repo.deleted)
	}
}

func TestCommentServicePropagatesLookupAndPermissionErrors(t *testing.T) {
	repo := &commentRepositoryMock{getErr: domain.ErrNotFound}
	svc := NewCommentService(repo, &taskPermissionStub{}, nil)
	if err := svc.DeleteComment(context.Background(), 7, 9, 4); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: err=%v", err)
	}

	boom := errors.New("boom")
	repo = &commentRepositoryMock{existing: commentBy(7)}
	svc = NewCommentService(repo, &taskPermissionStub{err: boom}, nil)
	if err := svc.DeleteComment(context.Background(), 8, 9, 4); !errors.Is(err, boom) || repo.deleted {
		t.Fatalf("permission error: err=%v deleted=%t", err, repo.deleted)
	}
}
