package realtime

import (
	"strconv"
	"time"

	"rabbit-hole-server/internal/domain"
)

const (
	EventReady       = "ready"
	EventTaskCreated = "task.created"
	EventTaskUpdated = "task.updated"
	EventTaskDeleted = "task.deleted"

	EventCommentCreated = "comment.created"
	EventCommentUpdated = "comment.updated"
	EventCommentDeleted = "comment.deleted"

	EventNotificationCreated     = "notification.created"
	EventNotificationRead        = "notification.read"
	EventNotificationDeleted     = "notification.deleted"
	EventNotificationUnreadCount = "notification.unread_count"
)

type Event struct {
	Type    string       `json:"type"`
	Topic   string       `json:"topic,omitempty"`
	SpaceID uint         `json:"space_id,omitempty"`
	ListID  uint         `json:"list_id,omitempty"`
	TaskID  uint         `json:"task_id,omitempty"`
	Task    *domain.Task `json:"task,omitempty"`

	CommentID uint            `json:"comment_id,omitempty"`
	Comment   *domain.Comment `json:"comment,omitempty"`
	ActorID   uint            `json:"actor_id,omitempty"`
	At        time.Time       `json:"at"`
}

func ListTopic(listID uint) string   { return "list:" + strconv.FormatUint(uint64(listID), 10) }
func SpaceTopic(spaceID uint) string { return "space:" + strconv.FormatUint(uint64(spaceID), 10) }

func UserTopic(userID uint) string { return "user:" + strconv.FormatUint(uint64(userID), 10) }

type NotificationSummary struct {
	ID        uint      `json:"id"`
	Type      string    `json:"type"`
	TaskID    uint      `json:"task_id,omitempty"`
	Title     string    `json:"title"`
	Snippet   string    `json:"snippet,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type UserEvent struct {
	Type string `json:"type"`

	Notification   *NotificationSummary `json:"notification,omitempty"`
	NotificationID uint                 `json:"notification_id,omitempty"`
	IDs            []uint               `json:"ids,omitempty"`
	All            bool                 `json:"all,omitempty"`
	FilterType     string               `json:"filter_type,omitempty"`
	Updated        *int64               `json:"updated,omitempty"`

	UnreadCount *int64           `json:"unread_count,omitempty"`
	ByType      map[string]int64 `json:"by_type,omitempty"`
	At          time.Time        `json:"at"`
}
