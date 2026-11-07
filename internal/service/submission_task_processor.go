package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
)

type submissionTaskProcessor struct {
	forms     domain.FormRepository
	tasks     FormTaskCreator
	templates domain.TaskTemplateRepository
	fields    CustomFieldService
	uploads   domain.FormUploadRepository
	activity  ActivityRecorder
}

func NewSubmissionTaskProcessor(
	forms domain.FormRepository,
	tasks FormTaskCreator,
	templates domain.TaskTemplateRepository,
	fields CustomFieldService,
	uploads domain.FormUploadRepository,
	activity ActivityRecorder,
) domain.FormTaskProcessor {
	return &submissionTaskProcessor{
		forms: forms, tasks: tasks, templates: templates, fields: fields, uploads: uploads, activity: activity,
	}
}

func (s *submissionTaskProcessor) attachFiles(ctx context.Context, submission domain.FormSubmission, taskID uint) error {
	if s.uploads == nil {
		return nil
	}
	files, err := s.uploads.AttachSubmissionFiles(ctx, submission.ID, taskID, submission.CreatedBy)
	if err != nil {
		return fmt.Errorf("attach submission files: %w", err)
	}
	for _, file := range files {
		recordActivity(ctx, s.activity, domain.ActivityEntry{
			TaskID: taskID, ActorID: submission.CreatedBy, Action: domain.ActivityAttachmentCreated,
			NewValue: mustJSON(map[string]any{"id": file.ID, "name": file.Name, "size": file.Size}),
		})
	}
	return nil
}

func (s *submissionTaskProcessor) ProcessSubmission(
	ctx context.Context,
	submissionID uint,
) (domain.TaskCreationStatus, error) {
	return s.process(ctx, submissionID)
}

func (s *submissionTaskProcessor) RetrySubmission(
	ctx context.Context,
	submissionID uint,
) (domain.TaskCreationStatus, error) {
	status, _, err := s.forms.GetSubmissionTaskStatus(ctx, submissionID)
	if err != nil {
		return "", err
	}
	if status == domain.TaskCreationSucceeded || status == domain.TaskCreationSkipped {
		return status, nil
	}
	return s.process(ctx, submissionID)
}

func (s *submissionTaskProcessor) process(
	ctx context.Context,
	submissionID uint,
) (domain.TaskCreationStatus, error) {
	status, err := s.forms.ProcessSubmission(ctx, submissionID, func(
		txCtx context.Context,
		submission domain.FormSubmission,
		config domain.FormTaskConfiguration,
	) (uint, json.RawMessage, error) {
		templateConfig := TaskTemplateConfiguration{}
		if config.Template != nil {
			template, err := s.templates.Get(txCtx, config.Template.TemplateID)
			if err != nil || template.WorkspaceID != submission.WorkspaceID {
				return 0, nil, domain.ErrInvalidTaskTemplate
			}
			version, err := s.templates.GetVersion(txCtx, config.Template.TemplateID, config.Template.VersionID)
			if err != nil {
				return 0, nil, domain.ErrInvalidTaskTemplate
			}
			if err := json.Unmarshal(version.Configuration, &templateConfig); err != nil {
				return 0, nil, fmt.Errorf("decode task template configuration: %w", err)
			}
		}

		taskInput, customValues, mappingSnapshot, err := resolveSubmissionTask(
			submission, config, templateConfig,
		)
		if err != nil {
			return 0, nil, err
		}
		task, err := s.tasks.CreateTaskFromSubmission(txCtx, submission.CreatedBy,
			CreateTaskFromSubmissionParams{
				WorkspaceID: submission.WorkspaceID, SubmissionID: submission.ID, Task: taskInput,
			})
		if err != nil {
			return 0, nil, err
		}
		for fieldID, value := range customValues {
			if err := s.fields.SetTaskValue(txCtx, task.ID, fieldID, value); err != nil {
				return 0, nil, fmt.Errorf("set mapped custom field %d: %w", fieldID, err)
			}
		}
		if err := s.attachFiles(txCtx, submission, task.ID); err != nil {
			return 0, nil, err
		}
		return task.ID, mappingSnapshot, nil
	})
	if err != nil {
		return status, err
	}
	if status == domain.TaskCreationFailed {
		_, cause, statusErr := s.forms.GetSubmissionTaskStatus(ctx, submissionID)
		if statusErr != nil {
			return status, statusErr
		}
		slog.Warn("form submission task creation failed", "submission_id", submissionID, "error", cause)
	}
	return status, nil
}

func resolveSubmissionTask(
	submission domain.FormSubmission,
	config domain.FormTaskConfiguration,
	template TaskTemplateConfiguration,
) (CreateTaskParams, map[uint]any, json.RawMessage, error) {
	input := CreateTaskParams{
		ListID:   config.Target.ListID,
		Priority: taskPriorityValue(config.TaskDefaults.Priority),
		TagIDs:   append([]uint(nil), template.Tags...),
	}
	if template.Task.Priority != "" {
		input.Priority = taskPriorityValue(template.Task.Priority)
	}
	if template.Task.Title != nil {
		input.Title = strings.TrimSpace(*template.Task.Title)
	}
	if template.Task.Description != nil {
		input.Description = strings.TrimSpace(*template.Task.Description)
	}
	if template.Task.DueDate != nil {
		date, err := time.Parse("2006-01-02", *template.Task.DueDate)
		if err != nil {
			return CreateTaskParams{}, nil, nil, domain.ErrInvalidTaskTemplate
		}
		input.DueDate = &date
	}
	if config.TaskDefaults.Priority != "" {
		if template.Task.Priority == "" {
			input.Priority = taskPriorityValue(config.TaskDefaults.Priority)
		}
	}
	if config.TaskDefaults.StatusID != nil {
		input.StatusID = *config.TaskDefaults.StatusID
	}
	if config.TaskDefaults.AssigneeID != nil {
		input.AssigneeIDs = []uint{*config.TaskDefaults.AssigneeID}
	}
	if template.Task.StatusID != nil {
		input.StatusID = *template.Task.StatusID
	}
	if template.Task.AssigneeID != nil {
		input.AssigneeIDs = []uint{*template.Task.AssigneeID}
	}
	customValues := make(map[uint]any, len(template.CustomFields))
	for _, field := range template.CustomFields {
		var value any
		if err := json.Unmarshal(field.Value, &value); err != nil {
			return CreateTaskParams{}, nil, nil, domain.ErrInvalidTaskTemplate
		}
		customValues[field.FieldID] = value
	}
	answers := make(map[uint]json.RawMessage, len(submission.Answers))
	for _, answer := range submission.Answers {
		answers[answer.QuestionID] = answer.Value
	}
	for _, mapping := range config.Mappings {
		raw, exists := answers[mapping.QuestionID]
		if !exists {
			continue
		}
		value, err := resolveMappedValue(raw, mapping)
		if err != nil {
			return CreateTaskParams{}, nil, nil, err
		}
		switch mapping.Target.Type {
		case "task_field":
			switch mapping.Target.Field {
			case "title":
				input.Title, _ = value.(string)
			case "description":
				input.Description, _ = value.(string)
			case "priority":
				priority, ok := value.(string)
				if !ok || !validTaskPriority(priority) {
					return CreateTaskParams{}, nil, nil, domain.ErrInvalidSubmission
				}
				input.Priority = taskPriorityValue(priority)
			case "status_id":
				id, ok := value.(uint)
				if !ok || id == 0 {
					return CreateTaskParams{}, nil, nil, domain.ErrInvalidSubmission
				}
				input.StatusID = id
			case "assignee_id":
				id, ok := value.(uint)
				if !ok || id == 0 {
					return CreateTaskParams{}, nil, nil, domain.ErrInvalidSubmission
				}
				input.AssigneeIDs = []uint{id}
			case "due_date":
				dateText, ok := value.(string)
				if !ok {
					return CreateTaskParams{}, nil, nil, domain.ErrInvalidSubmission
				}
				date, err := time.Parse("2006-01-02", dateText)
				if err != nil {
					return CreateTaskParams{}, nil, nil, domain.ErrInvalidSubmission
				}
				input.DueDate = &date
			}
		case "custom_field":
			customValues[mapping.Target.FieldID] = value
		}
	}
	if strings.TrimSpace(input.Title) == "" {
		input.Title = fmt.Sprintf("Form submission %d", submission.ID)
	}
	if input.StatusID == 0 {
		return CreateTaskParams{}, nil, nil, domain.ErrInvalidSubmission
	}
	snapshot, err := json.Marshal(config.Mappings)
	if err != nil {
		return CreateTaskParams{}, nil, nil, fmt.Errorf("encode form mapping snapshot: %w", err)
	}
	return input, customValues, snapshot, nil
}

func resolveMappedValue(raw json.RawMessage, mapping domain.FormTaskMapping) (any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, domain.ErrInvalidSubmission
	}
	if mapping.Transform != nil {
		key, ok := value.(string)
		if !ok {
			return nil, domain.ErrInvalidSubmission
		}
		mapped, exists := mapping.Transform.Values[key]
		if !exists {
			return nil, domain.ErrInvalidSubmission
		}
		value = mapped
	}
	if mapping.Target.Type == "task_field" &&
		(mapping.Target.Field == "status_id" || mapping.Target.Field == "assignee_id") {
		text, ok := value.(string)
		if !ok {
			return nil, domain.ErrInvalidSubmission
		}
		id, err := strconv.ParseUint(text, 10, 64)
		if err != nil || id == 0 {
			return nil, domain.ErrInvalidSubmission
		}
		return uint(id), nil
	}
	return value, nil
}

func taskPriorityValue(priority string) int {
	switch priority {
	case "low":
		return 1
	case "high":
		return 3
	case "urgent":
		return 4
	default:
		return 2
	}
}
