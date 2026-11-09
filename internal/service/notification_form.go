package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"rabbit-hole-server/internal/domain"
)

const formPathTemplate = "%s/workspaces/%d/forms/%d/submissions/%d"

type FormSubmissionNotice struct {
	WorkspaceID  uint
	FormID       uint
	FormName     string
	SubmissionID uint
	Recipients   []uint
}

type formSubmissionEvent struct {
	WorkspaceID  uint   `json:"workspace_id"`
	FormID       uint   `json:"form_id"`
	FormName     string `json:"form_name"`
	SubmissionID uint   `json:"submission_id"`
	Recipients   []uint `json:"recipients"`
}

func (d *NotificationDispatcher) NotifyFormSubmission(notice FormSubmissionNotice) {
	if len(notice.Recipients) == 0 {
		return
	}
	raw, err := json.Marshal(formSubmissionEvent(notice))
	if err != nil {
		return
	}
	d.Enqueue([]domain.ActivityEntry{{Action: domain.ActivityFormSubmissionCreated, NewValue: raw}})
}

func mapFormSubmissionCreated(_ context.Context, _ *NotificationDispatcher, e domain.ActivityEntry) (*notificationPlan, error) {
	var event formSubmissionEvent
	if err := json.Unmarshal(e.NewValue, &event); err != nil || event.WorkspaceID == 0 || event.SubmissionID == 0 {
		return nil, nil
	}
	targets := make([]directTarget, len(event.Recipients))
	for i, id := range event.Recipients {
		targets[i] = directTarget{UserID: id, Type: domain.NotificationFormSubmitted}
	}
	return &notificationPlan{
		WorkspaceID: event.WorkspaceID, Direct: targets,
		Payload: map[string]any{
			"form_id": event.FormID, "form_name": event.FormName, "submission_id": event.SubmissionID,
		},
	}, nil
}

func (d *NotificationDispatcher) processWorkspaceEvent(ctx context.Context, plan *notificationPlan) error {
	recipients := resolveRecipients(plan, nil)
	ids := make([]uint, 0, len(recipients))
	for id := range recipients {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, userID := range ids {
		typeDef, ok := domain.NotificationTypeByKey(recipients[userID])
		if !ok {
			continue
		}
		allowed, err := d.perms.HasWorkspacePermission(ctx, userID, plan.WorkspaceID, domain.PermWorkspaceRead)
		if err != nil {
			d.logger.Warn("notifications: cannot check access", "user_id", userID, "error", err)
			continue
		}
		if !allowed {
			continue
		}
		d.deliver(ctx, userID, Delivery{
			RecipientID: userID,
			Content: NotificationContent{
				Type: typeDef, WorkspaceID: plan.WorkspaceID, Payload: plan.Payload,
			},
		})
	}
	return nil
}

func payloadID(payload map[string]any, key string) uint {
	switch value := payload[key].(type) {
	case uint:
		return value
	case float64:
		return uint(value)
	}
	return 0
}

func notificationLink(frontendURL string, c NotificationContent) string {
	if c.TaskID == 0 {
		return fmt.Sprintf(formPathTemplate, frontendURL, c.WorkspaceID,
			payloadID(c.Payload, "form_id"), payloadID(c.Payload, "submission_id"))
	}
	return fmt.Sprintf("%s%s%d", frontendURL, taskPathPrefix, c.TaskID)
}
