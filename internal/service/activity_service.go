package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	"rabbit-hole-server/internal/domain"
)

type ActivityRecorder interface {
	Record(ctx context.Context, entries ...domain.ActivityEntry)
}

type ActivityService interface {
	ActivityRecorder
	ListActivity(ctx context.Context, taskID uint, limit, offset int) ([]domain.Activity, int64, error)
}

type activityService struct {
	repo domain.ActivityRepository
}

func NewActivityService(repo domain.ActivityRepository) ActivityService {
	return &activityService{repo: repo}
}

func (s *activityService) Record(ctx context.Context, entries ...domain.ActivityEntry) {
	if len(entries) == 0 {
		return
	}
	if err := s.repo.Record(ctx, entries); err != nil {
		slog.Warn("activity: cannot record entries", "task_id", entries[0].TaskID, "count", len(entries), "error", err)
	}
}

func (s *activityService) ListActivity(ctx context.Context, taskID uint, limit, offset int) ([]domain.Activity, int64, error) {
	return s.repo.ListByTask(ctx, taskID, limit, offset)
}

func recordActivity(ctx context.Context, recorder ActivityRecorder, entries ...domain.ActivityEntry) {
	if recorder != nil {
		recorder.Record(ctx, entries...)
	}
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

var trackedTaskFields = []string{
	"title", "description", "status_id", "priority", "start_date", "due_date",
	"time_estimate", "parent_id", "assignee_ids", "tag_ids", "list_id",
}

func sortedIDs(ids []uint) []uint {
	out := append([]uint{}, ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func snapshotTask(t *domain.Task) map[string]json.RawMessage {
	assignees := make([]uint, len(t.Assignees))
	for i, u := range t.Assignees {
		assignees[i] = u.ID
	}
	tags := make([]uint, len(t.Tags))
	for i, tag := range t.Tags {
		tags[i] = tag.ID
	}
	return map[string]json.RawMessage{
		"title":         mustJSON(t.Title),
		"description":   mustJSON(t.Description),
		"status_id":     mustJSON(t.StatusID),
		"priority":      mustJSON(t.Priority),
		"start_date":    mustJSON(t.StartDate),
		"due_date":      mustJSON(t.DueDate),
		"time_estimate": mustJSON(t.TimeEstimate),
		"parent_id":     mustJSON(t.ParentID),
		"assignee_ids":  mustJSON(sortedIDs(assignees)),
		"tag_ids":       mustJSON(sortedIDs(tags)),
		"list_id":       mustJSON(t.ListID),
	}
}

func diffTask(taskID, actorID uint, before, after map[string]json.RawMessage) []domain.ActivityEntry {
	var entries []domain.ActivityEntry
	for _, field := range trackedTaskFields {
		if bytes.Equal(before[field], after[field]) {
			continue
		}
		entry := domain.ActivityEntry{
			TaskID: taskID, ActorID: actorID, Action: domain.ActivityTaskUpdated, Field: field,
			OldValue: before[field], NewValue: after[field],
		}
		if field == "list_id" {
			entry.Action = domain.ActivityTaskMoved
		}
		if field == "description" {
			entry.OldValue, entry.NewValue = nil, nil
		}
		entries = append(entries, entry)
	}
	return entries
}
