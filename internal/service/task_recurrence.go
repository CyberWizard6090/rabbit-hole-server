package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"rabbit-hole-server/internal/domain"
)

// RecurrenceStore is what repeating tasks need from the database.
type RecurrenceStore interface {
	IsDoneStatus(ctx context.Context, statusID uint) (bool, error)
	OpenStatus(ctx context.Context, listID uint) (uint, error)
	CarryOver(ctx context.Context, fromTaskID, toTaskID uint) error
	ClearRecurrence(ctx context.Context, taskID uint) error
}

// TaskServiceOption configures optional parts of the task service.
type TaskServiceOption func(*taskService)

// WithRecurrence enables repeating tasks: completing one creates the next.
func WithRecurrence(store RecurrenceStore) TaskServiceOption {
	return func(s *taskService) { s.recurrence = store }
}

// recurrenceActor is the label of the automation that creates the next occurrence; clients translate it.
const recurrenceActorLabel = "recurrence"

// prepareRecurrence validates the rule of a request and fills what the server owns. previous is the rule the task has now.
func prepareRecurrence(rule *domain.Recurrence, previous *domain.Recurrence) (*domain.Recurrence, error) {
	if rule == nil {
		return nil, nil
	}
	prepared := *rule
	prepared.Weekdays = append([]int(nil), rule.Weekdays...)
	prepared.Normalize()
	prepared.Occurrence = 1
	if previous != nil {
		prepared.Occurrence = previous.Occurrence
	}
	if err := prepared.Validate(); err != nil {
		return nil, err
	}
	return &prepared, nil
}

// completedNow reports whether the change moved a repeating task into a finished status.
func (s *taskService) completedNow(ctx context.Context, before, after *domain.Task) (bool, error) {
	if s.recurrence == nil || after.Recurrence == nil || before.StatusID == after.StatusID {
		return false, nil
	}
	nowDone, err := s.recurrence.IsDoneStatus(ctx, after.StatusID)
	if err != nil || !nowDone {
		return false, err
	}
	wasDone, err := s.recurrence.IsDoneStatus(ctx, before.StatusID)
	return !wasDone, err
}

// spawnNext runs inside the transaction that completed the task. It creates the next occurrence with the shifted dates and
// hands the rule over to it, or just retires the rule when the series is over. It returns the new task, nil at the end.
func (s *taskService) spawnNext(ctx context.Context, done *domain.Task) (*domain.Task, error) {
	rule := *done.Recurrence
	base := done.DueDate
	if base == nil {
		base = done.StartDate
	}
	completedAt := time.Now()
	next, ok := rule.Next(base, completedAt)
	if err := s.recurrence.ClearRecurrence(ctx, done.ID); err != nil {
		return nil, err
	}
	done.Recurrence = nil
	if !ok {
		return nil, nil
	}

	statusID, err := s.recurrence.OpenStatus(ctx, done.ListID)
	if err != nil {
		return nil, fmt.Errorf("find the status of the next occurrence: %w", err)
	}
	list, err := s.spaceRepo.GetListByID(ctx, done.ListID)
	if err != nil {
		return nil, fmt.Errorf("get the list of the next occurrence: %w", err)
	}

	nextRule := rule.PinMonthDay(base, completedAt)
	nextRule.Occurrence = rule.Occurrence + 1
	input := CreateTaskParams{
		Title:        done.Title,
		Description:  done.Description,
		ListID:       done.ListID,
		StatusID:     statusID,
		ParentID:     done.ParentID,
		Priority:     done.Priority,
		TimeEstimate: done.TimeEstimate,
		Recurrence:   &nextRule,
	}
	for _, assignee := range done.Assignees {
		input.AssigneeIDs = append(input.AssigneeIDs, assignee.ID)
	}
	for _, tag := range done.Tags {
		input.TagIDs = append(input.TagIDs, tag.ID)
	}
	input.DueDate = &next
	if done.StartDate != nil {
		// Keep the distance between the start and the due date; with no due date the start moves to the new date.
		if done.DueDate != nil {
			start := next.Add(-done.DueDate.Sub(*done.StartDate))
			input.StartDate = &start
		} else {
			input.StartDate, input.DueDate = &next, nil
		}
	}

	created, err := s.createTaskInTx(ctx, done.UserID, input, taskOrigin{
		// source_reference_id points at a form submission, so the link to the completed task is the task.recurred
		// entry of its history instead.
		source: domain.TaskSourceAutomation,
		actor:  domain.NewActor(domain.ActorAutomation, 0, recurrenceActorLabel),
	}, list)
	if err != nil {
		return nil, fmt.Errorf("create the next occurrence: %w", err)
	}
	if err := s.recurrence.CarryOver(ctx, done.ID, created.ID); err != nil {
		return nil, fmt.Errorf("carry over to the next occurrence: %w", err)
	}
	return created, nil
}

func recurredActivity(done *domain.Task, next *domain.Task) domain.ActivityEntry {
	payload := map[string]any{"task_id": next.ID, "due_date": next.DueDate, "occurrence": next.Recurrence.Occurrence}
	raw, _ := json.Marshal(payload)
	return domain.ActivityEntry{
		TaskID: done.ID, Actor: domain.NewActor(domain.ActorAutomation, 0, recurrenceActorLabel),
		Action: domain.ActivityTaskRecurred, NewValue: raw,
	}
}
