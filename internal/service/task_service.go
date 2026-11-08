package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
)

var tagColorPalette = []string{
	"#F87171", "#FB923C", "#FBBF24", "#A3E635",
	"#34D399", "#22D3EE", "#60A5FA", "#A78BFA",
	"#F472B6", "#94A3B8",
}

func randomTagColor() string {
	return tagColorPalette[rand.Intn(len(tagColorPalette))]
}

type TaskService interface {
	CreateTask(ctx context.Context, userID uint, input CreateTaskParams) (*domain.Task, error)
	ListTasks(ctx context.Context, listID uint, params pagination.Params, query domain.TaskQuery) (pagination.Response[domain.Task], error)
	GetTaskByID(ctx context.Context, taskID uint) (*domain.Task, error)
	UpdateTask(ctx context.Context, userID, id uint, params UpdateTaskParams) (*domain.Task, error)
	DeleteTask(ctx context.Context, taskID uint) error
	MoveTask(ctx context.Context, userID, taskID uint, params MoveTaskParams) (*domain.Task, error)
}

type FormTaskCreator interface {
	CreateTaskFromSubmission(ctx context.Context, userID uint, input CreateTaskFromSubmissionParams) (*domain.Task, error)
}

type TaskModule interface {
	TaskService
	FormTaskCreator
}

type CreateTaskFromSubmissionParams struct {
	WorkspaceID  uint
	SubmissionID uint
	Task         CreateTaskParams
	Actor        domain.Actor
	Requester    *domain.Requester
}

type MoveTaskParams struct {
	ListID   *uint
	StatusID *uint
	AfterID  *uint
	BeforeID *uint
}

type CreateTaskParams struct {
	Title        string
	Description  string
	ListID       uint
	StatusID     uint
	ParentID     *uint
	Priority     int
	AssigneeIDs  []uint
	TagIDs       []uint
	NewTagNames  []string
	StartDate    *time.Time
	DueDate      *time.Time
	TimeEstimate int
	CustomFields map[uint]any
}

type UpdateTaskParams struct {
	Title        *string
	Description  *string
	StatusID     *uint
	Priority     *int
	ParentID     domain.Nullable[uint]
	StartDate    domain.Nullable[time.Time]
	DueDate      domain.Nullable[time.Time]
	TimeEstimate *int
	AssigneeIDs  *[]uint
	AddTagIDs    []uint
	AddTagNames  []string
	RemoveTagIDs []uint
}

func checkDateRange(start, due *time.Time) error {
	if start != nil && due != nil && due.Before(*start) {
		return domain.ErrInvalidDateRange
	}
	return nil
}

type taskService struct {
	repo        domain.TaskRepository
	spaceRepo   domain.SpaceRepository
	tagService  TagService
	cursorCodec *pagination.Codec
	activity    ActivityRecorder
	fields      CustomFieldService
	tx          TransactionRunner
}

func NewTaskService(
	repo domain.TaskRepository,
	spaceRepo domain.SpaceRepository,
	tagService TagService,
	cursorCodec *pagination.Codec,
	activity ActivityRecorder,
	fields CustomFieldService,
	tx TransactionRunner,
) TaskModule {
	return &taskService{
		repo:        repo,
		spaceRepo:   spaceRepo,
		tagService:  tagService,
		cursorCodec: cursorCodec,
		activity:    activity,
		fields:      fields,
		tx:          tx,
	}
}

func (s *taskService) resolveNewTags(ctx context.Context, spaceID uint, names []string) ([]uint, error) {
	ids := make([]uint, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		tag, err := s.tagService.GetOrCreateByName(ctx, spaceID, name, randomTagColor())
		if err != nil {
			return nil, fmt.Errorf("get or create tag %q: %w", name, err)
		}
		ids = append(ids, tag.ID)
	}
	return ids, nil
}

func (s *taskService) CreateTask(ctx context.Context, userID uint, input CreateTaskParams) (*domain.Task, error) {
	return s.createTask(ctx, userID, input, taskOrigin{source: domain.TaskSourceManual, actor: domain.UserActor(userID)})
}

func (s *taskService) CreateTaskFromSubmission(
	ctx context.Context,
	userID uint,
	input CreateTaskFromSubmissionParams,
) (*domain.Task, error) {
	if input.WorkspaceID == 0 || input.SubmissionID == 0 {
		return nil, domain.ErrInvalidTaskSource
	}
	list, err := s.spaceRepo.GetListByID(ctx, input.Task.ListID)
	if err != nil {
		return nil, fmt.Errorf("get target list: %w", err)
	}
	if list == nil {
		return nil, domain.ErrNotFound
	}
	space, err := s.spaceRepo.GetByID(ctx, list.SpaceID)
	if err != nil {
		return nil, fmt.Errorf("get target workspace: %w", err)
	}
	if space == nil {
		return nil, domain.ErrNotFound
	}
	if space.WorkspaceID != input.WorkspaceID {
		return nil, domain.ErrInvalidListReference
	}
	return s.createTask(ctx, userID, input.Task, taskOrigin{
		source: domain.TaskSourceForm, sourceReferenceID: &input.SubmissionID,
		actor: input.Actor, requester: input.Requester,
	})
}

func (s *taskService) createTask(
	ctx context.Context,
	userID uint,
	input CreateTaskParams,
	origin taskOrigin,
) (*domain.Task, error) {
	if err := checkDateRange(input.StartDate, input.DueDate); err != nil {
		return nil, err
	}
	list, err := s.spaceRepo.GetListByID(ctx, input.ListID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}
	newTagIDs, err := s.resolveNewTags(ctx, list.SpaceID, input.NewTagNames)
	if err != nil {
		return nil, err
	}
	input.TagIDs = append(append([]uint{}, input.TagIDs...), newTagIDs...)
	input.NewTagNames = nil
	if s.tx == nil {
		return s.createTaskInTx(ctx, userID, input, origin, list)
	}
	var created *domain.Task
	err = s.tx.Run(ctx, func(txCtx context.Context) error {
		var err error
		created, err = s.createTaskInTx(txCtx, userID, input, origin, list)
		return err
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *taskService) createTaskInTx(
	ctx context.Context,
	userID uint,
	input CreateTaskParams,
	origin taskOrigin,
	list *domain.List,
) (*domain.Task, error) {
	if err := s.checkCustomFields(ctx, list.SpaceID, input.CustomFields); err != nil {
		return nil, err
	}

	if input.Priority == 0 {
		input.Priority = 2
	}

	allTagIDs := input.TagIDs

	taskTags := make([]domain.Tag, len(allTagIDs))
	for i, tagID := range allTagIDs {
		taskTags[i] = domain.Tag{ID: tagID}
	}

	task := &domain.Task{
		UserID:            userID,
		SpaceID:           list.SpaceID,
		ListID:            input.ListID,
		StatusID:          input.StatusID,
		ParentID:          input.ParentID,
		Title:             input.Title,
		Description:       input.Description,
		Priority:          input.Priority,
		StartDate:         input.StartDate,
		DueDate:           input.DueDate,
		TimeEstimate:      input.TimeEstimate,
		Tags:              taskTags,
		Source:            origin.source,
		SourceReferenceID: origin.sourceReferenceID,
		Actor:             origin.actor.Normalized(),
		RequesterInfo:     origin.requester,
	}

	if err := s.repo.Create(ctx, task, input.AssigneeIDs, allTagIDs); err != nil {
		return nil, err
	}
	for fieldID, value := range input.CustomFields {
		if isEmptyCustomFieldValue(value) {
			continue
		}
		if err := s.fields.SetTaskValue(ctx, task.ID, fieldID, value); err != nil {
			return nil, err
		}
	}
	recordActivity(ctx, s.activity, domain.ActivityEntry{
		TaskID: task.ID, Actor: origin.actor.Normalized(), Action: domain.ActivityTaskCreated,
		NewValue: createdTaskActivity(task.Title, input.AssigneeIDs, ownerForActivity(origin.actor, userID)),
	})
	return task, nil
}

func (s *taskService) checkCustomFields(ctx context.Context, spaceID uint, values map[uint]any) error {
	if s.fields == nil {
		return nil
	}
	space, err := s.spaceRepo.GetByID(ctx, spaceID)
	if err != nil {
		return fmt.Errorf("get task space: %w", err)
	}
	fields, err := s.fields.List(ctx, space.WorkspaceID)
	if err != nil {
		return err
	}
	known := make(map[uint]struct{}, len(fields))
	var missing []uint
	for _, field := range fields {
		known[field.ID] = struct{}{}
		if field.Required && isEmptyCustomFieldValue(values[field.ID]) {
			missing = append(missing, field.ID)
		}
	}
	for fieldID := range values {
		if _, ok := known[fieldID]; !ok {
			return fmt.Errorf("%w: unknown custom field %d", domain.ErrInvalidCustomField, fieldID)
		}
	}
	if len(missing) > 0 {
		return &domain.CustomFieldRequiredError{FieldIDs: missing}
	}
	return nil
}

func (s *taskService) ListTasks(ctx context.Context, listID uint, params pagination.Params, query domain.TaskQuery) (pagination.Response[domain.Task], error) {
	context := query.CursorContext(listID)
	items, hasNext, hasPrev, err := s.repo.List(ctx, listID, query, params)
	if err != nil {
		return pagination.Response[domain.Task]{}, err
	}
	var nextCursor *string
	var prevCursor *string

	if len(items) > 0 {
		keys := query.SortKeys()
		if hasNext {
			cursor, err := s.encodeTaskCursor(context, pagination.DirectionNext, keys, items[len(items)-1])
			if err != nil {
				return pagination.Response[domain.Task]{}, err
			}
			nextCursor = &cursor
		}

		if hasPrev {
			cursor, err := s.encodeTaskCursor(context, pagination.DirectionPrev, keys, items[0])
			if err != nil {
				return pagination.Response[domain.Task]{}, err
			}
			prevCursor = &cursor
		}
	}

	response := pagination.NewResponse(items, hasNext, hasPrev, nextCursor, prevCursor)
	if query.WithTotal {
		total, err := s.repo.Count(ctx, listID, query.Filter)
		if err != nil {
			return pagination.Response[domain.Task]{}, err
		}
		response.TotalCount = &total
	}
	return response, nil
}

func (s *taskService) encodeTaskCursor(
	context pagination.CursorContext,
	direction pagination.Direction,
	keys []domain.TaskSortKey,
	task domain.Task,
) (string, error) {
	position := pagination.TaskCursorPosition{Keys: make([]json.RawMessage, len(keys))}
	for i, key := range keys {
		raw, err := json.Marshal(task.SortValue(key.Field))
		if err != nil {
			return "", err
		}
		position.Keys[i] = raw
	}
	return pagination.EncodePosition(s.cursorCodec, context, direction, position)
}

func (s *taskService) GetTaskByID(ctx context.Context, taskID uint) (*domain.Task, error) {
	return s.repo.GetByID(ctx, taskID)
}

func (s *taskService) UpdateTask(ctx context.Context, userID, id uint, params UpdateTaskParams) (*domain.Task, error) {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	newTagIDs, err := s.resolveNewTags(ctx, current.SpaceID, params.AddTagNames)
	if err != nil {
		return nil, err
	}
	tagIDsToAdd := append(append([]uint{}, params.AddTagIDs...), newTagIDs...)
	if overlapsIDs(tagIDsToAdd, params.RemoveTagIDs) {
		return nil, domain.ErrConflictingTagChange
	}

	apply := func(task *domain.Task) error {
		if params.Title != nil {
			task.Title = *params.Title
		}
		if params.Description != nil {
			task.Description = *params.Description
		}
		if params.StatusID != nil {
			task.StatusID = *params.StatusID
		}
		if params.Priority != nil {
			task.Priority = *params.Priority
		}
		if params.ParentID.Set {
			if params.ParentID.Value != nil && *params.ParentID.Value == task.ID {
				return domain.ErrInvalidParentReference
			}
			task.ParentID = params.ParentID.Value
		}
		if params.StartDate.Set {
			task.StartDate = params.StartDate.Value
		}
		if params.DueDate.Set {
			task.DueDate = params.DueDate.Value
		}
		if err := checkDateRange(task.StartDate, task.DueDate); err != nil {
			return err
		}
		if params.TimeEstimate != nil {
			task.TimeEstimate = *params.TimeEstimate
		}
		return nil
	}

	before, updated, err := s.repo.Update(ctx, id, apply, tagIDsToAdd, params.RemoveTagIDs, params.AssigneeIDs)
	if err != nil {
		return nil, err
	}
	recordActivity(ctx, s.activity, diffTask(updated.ID, userID, snapshotTask(before), snapshotTask(updated))...)
	return updated, nil
}

func (s *taskService) DeleteTask(ctx context.Context, taskID uint) error {
	return s.repo.Delete(ctx, taskID)
}

func (s *taskService) MoveTask(ctx context.Context, userID, taskID uint, params MoveTaskParams) (*domain.Task, error) {
	if params.AfterID != nil && params.BeforeID != nil {
		return nil, domain.ErrInvalidMoveAnchor
	}
	for _, anchor := range []*uint{params.AfterID, params.BeforeID} {
		if anchor != nil && *anchor == taskID {
			return nil, domain.ErrInvalidMoveAnchor
		}
	}

	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}

	target := domain.TaskMove{
		ListID:   task.ListID,
		StatusID: task.StatusID,
		AfterID:  params.AfterID,
		BeforeID: params.BeforeID,
	}
	if params.ListID != nil && *params.ListID != task.ListID {
		if params.StatusID == nil {
			return nil, domain.ErrInvalidStatusReference
		}
		target.ListID = *params.ListID
	}
	if params.StatusID != nil {
		target.StatusID = *params.StatusID
	}
	before := snapshotTask(task)
	moved, err := s.repo.Move(ctx, taskID, target)
	if err != nil {
		return nil, err
	}
	recordActivity(ctx, s.activity, diffTask(moved.ID, userID, before, snapshotTask(moved))...)
	return moved, nil
}

func overlapsIDs(a, b []uint) bool {
	seen := make(map[uint]struct{}, len(a))
	for _, id := range a {
		seen[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := seen[id]; ok {
			return true
		}
	}
	return false
}

type taskOrigin struct {
	source            domain.TaskSource
	sourceReferenceID *uint
	actor             domain.Actor
	requester         *domain.Requester
}

func ownerForActivity(actor domain.Actor, ownerID uint) uint {
	if actor.IsUser() {
		return 0
	}
	return ownerID
}

func createdTaskActivity(title string, assigneeIDs []uint, ownerID uint) json.RawMessage {
	value := map[string]any{"title": title}
	if ownerID != 0 {
		value["owner_id"] = ownerID
	}
	if len(assigneeIDs) > 0 {
		value["assignee_ids"] = sortedIDs(assigneeIDs)
	}
	return mustJSON(value)
}
