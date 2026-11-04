package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidMentionReference = errors.New("mentioned user is not a member of the task workspace")
	ErrCommentForbidden        = errors.New("only the author or a user who can delete tasks may change this comment")
)

const MaxCommentMentions = 50

type CommentUser struct {
	ID        uint   `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	AvatarURL string `json:"avatar_url"`
}

type Comment struct {
	ID        uint          `json:"id"`
	TaskID    uint          `json:"task_id"`
	Author    CommentUser   `json:"author"`
	Body      string        `json:"body"`
	Mentions  []CommentUser `json:"mentions"`
	Edited    bool          `json:"edited"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type CommentRepository interface {
	Create(ctx context.Context, taskID, authorID uint, body string, mentionIDs []uint) (*Comment, error)
	ListByTask(ctx context.Context, taskID uint, limit, offset int) ([]Comment, int64, error)
	GetByID(ctx context.Context, taskID, commentID uint) (*Comment, error)
	Update(ctx context.Context, taskID, commentID uint, body *string, mentionIDs *[]uint) (*Comment, error)
	Delete(ctx context.Context, taskID, commentID uint) error
}
