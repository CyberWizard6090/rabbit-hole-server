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
		if err := recordActivity(ctx, s.activity, domain.ActivityEntry{
			TaskID: taskID, Actor: domain.FormActor(submission.FormID, submission.FormName),
			Action:   domain.ActivityAttachmentCreated,
			NewValue: mustJSON(map[string]any{"id": file.ID, "name": file.Name, "size": file.Size}),
		}); err != nil {
			return err
		}
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

		members, err := s.forms.WorkspaceMemberIDs(txCtx, submission.WorkspaceID, submissionUserCandidates(submission, config))
		if err != nil {
			return 0, nil, fmt.Errorf("load workspace members: %w", err)
		}
		resolved, err := resolveSubmissionTask(submission, config, templateConfig, memberSet(members))
		if err != nil {
			return 0, nil, err
		}
		taskInput, customValues, mappingSnapshot := resolved.input, resolved.customValues, resolved.snapshot
		ownerID := submission.CreatedBy
		if config.OwnerID != nil {
			if resolved.ownerIsMember {
				ownerID = *config.OwnerID
			} else {
				slog.Info("form task owner is no longer a workspace member, using the form creator",
					"submission_id", submission.ID, "owner_id", *config.OwnerID)
			}
		}
		taskInput.CustomFields = customValues
		task, err := s.tasks.CreateTaskFromSubmission(txCtx, ownerID,
			CreateTaskFromSubmissionParams{
				WorkspaceID: submission.WorkspaceID, SubmissionID: submission.ID, Task: taskInput,
				Actor:     domain.FormActor(submission.FormID, submission.FormName),
				Requester: resolved.requester,
			})
		if err != nil {
			return 0, nil, err
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
	members map[uint]bool,
) (resolvedSubmissionTask, error) {
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
	reference := submission.SubmittedAt
	if reference.IsZero() {
		reference = time.Now()
	}
	dueDate, err := templateDueDate(template, reference)
	if err != nil {
		return resolvedSubmissionTask{}, err
	}
	input.DueDate = dueDate
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
			return resolvedSubmissionTask{}, domain.ErrInvalidTaskTemplate
		}
		customValues[field.FieldID] = value
	}
	answers := make(map[uint]json.RawMessage, len(submission.Answers))
	for _, answer := range submission.Answers {
		answers[answer.QuestionID] = answer.Value
	}
	failures := make(map[int]string)
	requester := domain.Requester{}
	for index, mapping := range config.Mappings {
		raw, exists := answers[mapping.QuestionID]
		if !exists {
			continue
		}
		value, err := resolveMappedValue(raw, mapping)
		if err != nil {
			return resolvedSubmissionTask{}, err
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
				if !ok || !domain.ValidTaskPriorityName(priority) {
					return resolvedSubmissionTask{}, domain.ErrInvalidSubmission
				}
				input.Priority = taskPriorityValue(priority)
			case "status_id":
				id, ok := value.(uint)
				if !ok || id == 0 {
					return resolvedSubmissionTask{}, domain.ErrInvalidSubmission
				}
				input.StatusID = id
			case "assignee_id":
				id, ok := value.(uint)
				if !ok || id == 0 {
					return resolvedSubmissionTask{}, domain.ErrInvalidSubmission
				}
				input.AssigneeIDs = []uint{id}
			case "assignee":
				id, failure := memberFromAnswer(value, members)
				if failure != "" {
					failures[index] = failure
					break
				}
				input.AssigneeIDs = []uint{id}
			case "due_date":
				dateText, ok := value.(string)
				if !ok {
					return resolvedSubmissionTask{}, domain.ErrInvalidSubmission
				}
				date, err := time.Parse("2006-01-02", dateText)
				if err != nil {
					return resolvedSubmissionTask{}, domain.ErrInvalidSubmission
				}
				input.DueDate = &date
			}
		case "custom_field":
			customValues[mapping.Target.FieldID] = value
		case "requester":
			text, ok := value.(string)
			if !ok {
				failures[index] = "INVALID_VALUE"
				break
			}
			text = strings.TrimSpace(text)
			switch mapping.Target.Field {
			case "name":
				requester.Name = truncateRunes(text, maxRequesterNameRunes)
			case "email":
				requester.Email = truncateRunes(text, maxRequesterEmailRunes)
			}
		}
	}
	if strings.TrimSpace(input.Title) == "" {
		input.Title = fmt.Sprintf("Form submission %d", submission.ID)
	}
	if input.StatusID == 0 {
		return resolvedSubmissionTask{}, domain.ErrInvalidSubmission
	}
	if submission.RespondentID != nil {
		requester = domain.Requester{UserID: submission.RespondentID}
	}
	var requesterResult *domain.Requester
	if !requester.Empty() {
		requesterResult = &requester
	}
	snapshotItems := make([]mappingSnapshotEntry, len(config.Mappings))
	for i, mapping := range config.Mappings {
		snapshotItems[i] = mappingSnapshotEntry{FormTaskMapping: mapping, Failure: failures[i]}
	}
	snapshot, err := json.Marshal(snapshotItems)
	if err != nil {
		return resolvedSubmissionTask{}, fmt.Errorf("encode form mapping snapshot: %w", err)
	}
	ownerIsMember := config.OwnerID != nil && members[*config.OwnerID]
	return resolvedSubmissionTask{
		input: input, customValues: customValues, snapshot: snapshot,
		requester: requesterResult, ownerIsMember: ownerIsMember,
	}, nil
}

type resolvedSubmissionTask struct {
	input         CreateTaskParams
	customValues  map[uint]any
	snapshot      json.RawMessage
	requester     *domain.Requester
	ownerIsMember bool
}

type mappingSnapshotEntry struct {
	domain.FormTaskMapping
	Failure string `json:"failure,omitempty"`
}

const (
	maxRequesterNameRunes  = 200
	maxRequesterEmailRunes = 254
)

func memberSet(ids []uint) map[uint]bool {
	set := make(map[uint]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func parseUserIDText(value any) (uint, bool) {
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func memberFromAnswer(value any, members map[uint]bool) (uint, string) {
	id, ok := parseUserIDText(value)
	if !ok {
		return 0, "INVALID_ID"
	}
	if !members[id] {
		return 0, "NOT_A_MEMBER"
	}
	return id, ""
}

func submissionUserCandidates(submission domain.FormSubmission, config domain.FormTaskConfiguration) []uint {
	var ids []uint
	if config.OwnerID != nil {
		ids = append(ids, *config.OwnerID)
	}
	answers := make(map[uint]json.RawMessage, len(submission.Answers))
	for _, answer := range submission.Answers {
		answers[answer.QuestionID] = answer.Value
	}
	for _, mapping := range config.Mappings {
		if mapping.Target.Type != "task_field" || mapping.Target.Field != "assignee" {
			continue
		}
		var value any
		if raw, ok := answers[mapping.QuestionID]; ok && json.Unmarshal(raw, &value) == nil {
			if id, ok := parseUserIDText(value); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
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
	if value, ok := domain.TaskPriorityFromName(priority); ok {
		return value
	}
	return domain.TaskPriorityNormal
}
