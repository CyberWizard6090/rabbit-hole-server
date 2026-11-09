package domain

import (
	"context"
	"encoding/json"
	"time"
)

const (
	AuditWorkspaceUpdated       = "workspace.updated"
	AuditWorkspaceAvatarChanged = "workspace.avatar_changed"
	AuditWorkspaceAvatarRemoved = "workspace.avatar_removed"
	AuditMemberAdded            = "member.added"
	AuditMemberRoleChanged      = "member.role_changed"
	AuditMemberRemoved          = "member.removed"
	AuditMemberLeft             = "member.left"
)

func ValidAuditAction(action string) bool {
	switch action {
	case AuditWorkspaceUpdated, AuditWorkspaceAvatarChanged, AuditWorkspaceAvatarRemoved,
		AuditMemberAdded, AuditMemberRoleChanged, AuditMemberRemoved, AuditMemberLeft:
		return true
	default:
		return false
	}
}

type WorkspaceAuditDraft struct {
	WorkspaceID  uint
	ActorID      uint
	TargetUserID uint
	Action       string
	Changes      json.RawMessage
}

type WorkspaceAuditEntry struct {
	ID        uint            `json:"id"`
	Action    string          `json:"action"`
	Actor     *CommentUser    `json:"actor"`
	Target    *CommentUser    `json:"target"`
	Changes   json.RawMessage `json:"changes"`
	CreatedAt time.Time       `json:"created_at"`
}

type WorkspaceAuditRepository interface {
	Record(ctx context.Context, draft WorkspaceAuditDraft) error
	List(ctx context.Context, workspaceID uint, action string, limit, offset int) ([]WorkspaceAuditEntry, int64, error)
}
