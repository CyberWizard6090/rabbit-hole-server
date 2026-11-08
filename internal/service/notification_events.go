package service

import (
	"context"
	"encoding/json"
	"errors"

	"rabbit-hole-server/internal/domain"
)

const commentExcerptRunes = 300

type directTarget struct {
	UserID uint
	Type   string
}

type notificationPlan struct {
	TaskID      uint
	Actor       domain.Actor
	AutoWatch   []uint
	Direct      []directTarget
	WatcherType string
	Payload     map[string]any
	WorkspaceID uint
	Title       string
}

type eventMapper func(ctx context.Context, d *NotificationDispatcher, e domain.ActivityEntry) (*notificationPlan, error)

func defaultEventMappers() map[string]eventMapper {
	return map[string]eventMapper{
		domain.ActivityCommentCreated:        mapCommentCreated,
		domain.ActivityTaskCreated:           mapTaskCreated,
		domain.ActivityTaskUpdated:           mapTaskUpdated,
		domain.ActivityChecklistItemAssigned: mapChecklistItemAssigned,
		domain.ActivityFormSubmissionCreated: mapFormSubmissionCreated,
	}
}

func withoutZero(ids []uint) []uint {
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}

func decodeIDs(raw json.RawMessage) []uint {
	var ids []uint
	if len(raw) == 0 || json.Unmarshal(raw, &ids) != nil {
		return nil
	}
	return ids
}

func addedIDs(before, after []uint) []uint {
	had := make(map[uint]struct{}, len(before))
	for _, id := range before {
		had[id] = struct{}{}
	}
	var added []uint
	for _, id := range after {
		if _, ok := had[id]; !ok {
			added = append(added, id)
		}
	}
	return added
}

func assignedTargets(ids []uint) []directTarget {
	targets := make([]directTarget, len(ids))
	for i, id := range ids {
		targets[i] = directTarget{UserID: id, Type: domain.NotificationAssigned}
	}
	return targets
}

func mapCommentCreated(ctx context.Context, d *NotificationDispatcher, e domain.ActivityEntry) (*notificationPlan, error) {
	var ref struct {
		CommentID uint `json:"comment_id"`
	}
	if err := json.Unmarshal(e.NewValue, &ref); err != nil || ref.CommentID == 0 {
		return nil, nil
	}
	comment, err := d.comments.GetByID(ctx, e.TaskID, ref.CommentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	plan := &notificationPlan{
		TaskID: e.TaskID, Actor: e.Actor, WatcherType: domain.NotificationComment,
		AutoWatch: withoutZero([]uint{e.Actor.UserID(), comment.Author.ID}),
		Payload: map[string]any{
			"comment_id": comment.ID, "comment_excerpt": truncateRunes(comment.Body, commentExcerptRunes),
		},
	}
	for _, m := range comment.Mentions {
		plan.AutoWatch = append(plan.AutoWatch, m.ID)
		plan.Direct = append(plan.Direct, directTarget{UserID: m.ID, Type: domain.NotificationMention})
	}
	return plan, nil
}

func mapTaskCreated(_ context.Context, _ *NotificationDispatcher, e domain.ActivityEntry) (*notificationPlan, error) {
	var created struct {
		AssigneeIDs []uint `json:"assignee_ids"`
		OwnerID     uint   `json:"owner_id"`
	}
	_ = json.Unmarshal(e.NewValue, &created)
	return &notificationPlan{
		TaskID: e.TaskID, Actor: e.Actor,
		AutoWatch: withoutZero(append([]uint{e.Actor.UserID(), created.OwnerID}, created.AssigneeIDs...)),
		Direct:    assignedTargets(created.AssigneeIDs),
		Payload:   map[string]any{},
	}, nil
}

func mapTaskUpdated(ctx context.Context, d *NotificationDispatcher, e domain.ActivityEntry) (*notificationPlan, error) {
	plan := &notificationPlan{TaskID: e.TaskID, Actor: e.Actor, Payload: map[string]any{}}
	switch e.Field {
	case "assignee_ids":
		added := addedIDs(decodeIDs(e.OldValue), decodeIDs(e.NewValue))
		if len(added) == 0 {
			return nil, nil
		}
		plan.AutoWatch = added
		plan.Direct = assignedTargets(added)
	case "status_id":
		plan.WatcherType = domain.NotificationStatusChanged
		var statusID uint
		if err := json.Unmarshal(e.NewValue, &statusID); err != nil || statusID == 0 {
			return nil, nil
		}
		plan.Payload["status_id"] = statusID
		if name, err := d.source.StatusName(ctx, statusID); err == nil {
			plan.Payload["status_name"] = name
		}
	case "due_date":
		plan.WatcherType = domain.NotificationDueDateChange
		var due *string
		_ = json.Unmarshal(e.NewValue, &due)
		if due == nil {
			plan.Payload["due_date"] = ""
		} else {
			plan.Payload["due_date"] = *due
		}
	default:
		return nil, nil
	}
	return plan, nil
}

func mapChecklistItemAssigned(_ context.Context, _ *NotificationDispatcher, e domain.ActivityEntry) (*notificationPlan, error) {
	var item struct {
		ChecklistID uint   `json:"checklist_id"`
		ItemID      uint   `json:"item_id"`
		Text        string `json:"text"`
		AssigneeID  uint   `json:"assignee_id"`
	}
	if err := json.Unmarshal(e.NewValue, &item); err != nil || item.AssigneeID == 0 {
		return nil, nil
	}
	return &notificationPlan{
		TaskID: e.TaskID, Actor: e.Actor,
		AutoWatch: []uint{item.AssigneeID},
		Direct:    assignedTargets([]uint{item.AssigneeID}),
		Payload: map[string]any{
			"checklist_id": item.ChecklistID, "item_id": item.ItemID, "item_text": truncateRunes(item.Text, commentExcerptRunes),
		},
	}, nil
}
