package service

import (
	"context"
	"log/slog"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/realtime"
)

const notificationSnippetRunes = 140

type NotificationPublisher interface {
	PublishUser(userID uint, event realtime.UserEvent)
}

type notificationPush struct {
	inbox domain.NotificationRepository
	pub   NotificationPublisher
}

func newNotificationPush(inbox domain.NotificationRepository, pub NotificationPublisher) *notificationPush {
	if pub == nil {
		return nil
	}
	return &notificationPush{inbox: inbox, pub: pub}
}

func (p *notificationPush) counts(ctx context.Context, userID uint) (*int64, map[string]int64) {
	raw, err := p.inbox.UnreadCounts(ctx, userID)
	if err != nil {
		slog.Warn("notification push: cannot count unread entries", "user_id", userID, "error", err)
		return nil, nil
	}
	byType := make(map[string]int64, len(domain.NotificationTypes()))
	var total int64
	for _, t := range domain.NotificationTypes() {
		byType[t.Key] = raw[t.Key]
		total += raw[t.Key]
	}
	return &total, byType
}

func (p *notificationPush) send(userID uint, primary realtime.UserEvent, total *int64, byType map[string]int64) {
	primary.UnreadCount = total
	p.pub.PublishUser(userID, primary)
	if total != nil {
		p.pub.PublishUser(userID, realtime.UserEvent{
			Type: realtime.EventNotificationUnreadCount, UnreadCount: total, ByType: byType,
		})
	}
}

func notificationSnippet(payload map[string]any) string {
	for _, key := range []string{"comment_excerpt", "item_text"} {
		if text, ok := payload[key].(string); ok && text != "" {
			return truncateRunes(text, notificationSnippetRunes)
		}
	}
	return ""
}

func (p *notificationPush) created(ctx context.Context, userID uint, ref domain.NotificationRef, c NotificationContent) {
	if p == nil {
		return
	}
	total, byType := p.counts(ctx, userID)
	p.send(userID, realtime.UserEvent{
		Type: realtime.EventNotificationCreated,
		Notification: &realtime.NotificationSummary{
			ID: ref.ID, Type: c.Type.Key, TaskID: c.TaskID, Title: c.TaskTitle,
			Snippet: notificationSnippet(c.Payload), CreatedAt: ref.CreatedAt,
		},
	}, total, byType)
}

func (p *notificationPush) read(ctx context.Context, userID uint, ids []uint, all bool, typeKey string, updated int64) {
	if p == nil || updated == 0 {
		return
	}
	total, byType := p.counts(ctx, userID)
	event := realtime.UserEvent{Type: realtime.EventNotificationRead, All: all, FilterType: typeKey, Updated: &updated}
	if !all {
		event.IDs = ids
	}
	p.send(userID, event, total, byType)
}

func (p *notificationPush) deleted(ctx context.Context, userID, id uint) {
	if p == nil {
		return
	}
	total, byType := p.counts(ctx, userID)
	p.send(userID, realtime.UserEvent{Type: realtime.EventNotificationDeleted, NotificationID: id}, total, byType)
}
