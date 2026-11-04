package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

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
	UpdateTask(ctx context.Context, id uint, params UpdateTaskParams) (*domain.Task, error)
	DeleteTask(ctx context.Context, taskID uint) error
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
	TimeEstimate int
}

type UpdateTaskParams struct {
	Title        *string
	Description  *string
	StatusID     *uint
	Priority     *int
	TimeEstimate *int
	AddTagIDs    []uint
	AddTagNames  []string
}

type taskService struct {
	repo        domain.TaskRepository
	spaceRepo   domain.SpaceRepository
	tagService  TagService
	cursorCodec *pagination.Codec
}

func NewTaskService(
	repo domain.TaskRepository,
	spaceRepo domain.SpaceRepository,
	tagService TagService,
	cursorCodec *pagination.Codec,
) TaskService {
	return &taskService{
		repo:        repo,
		spaceRepo:   spaceRepo,
		tagService:  tagService,
		cursorCodec: cursorCodec,
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
	list, err := s.spaceRepo.GetListByID(ctx, input.ListID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}

	if input.Priority == 0 {
		input.Priority = 2
	}

	newTagIDs, err := s.resolveNewTags(ctx, list.SpaceID, input.NewTagNames)
	if err != nil {
		return nil, err
	}
	allTagIDs := append(append([]uint{}, input.TagIDs...), newTagIDs...)

	taskTags := make([]domain.Tag, len(allTagIDs))
	for i, tagID := range allTagIDs {
		taskTags[i] = domain.Tag{ID: tagID}
	}

	task := &domain.Task{
		UserID:       userID,
		SpaceID:      list.SpaceID,
		ListID:       input.ListID,
		StatusID:     input.StatusID,
		ParentID:     input.ParentID,
		Title:        input.Title,
		Description:  input.Description,
		Priority:     input.Priority,
		TimeEstimate: input.TimeEstimate,
		Tags:         taskTags,
	}

	if err := s.repo.Create(ctx, task, input.AssigneeIDs, allTagIDs); err != nil {
		return nil, err
	}
	return task, nil
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

func (s *taskService) UpdateTask(ctx context.Context, id uint, params UpdateTaskParams) (*domain.Task, error) {
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	newTagIDs, err := s.resolveNewTags(ctx, task.SpaceID, params.AddTagNames)
	if err != nil {
		return nil, err
	}
	tagIDsToAdd := append(append([]uint{}, params.AddTagIDs...), newTagIDs...)

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
	if params.TimeEstimate != nil {
		task.TimeEstimate = *params.TimeEstimate
	}

	if err := s.repo.Update(ctx, task, tagIDsToAdd); err != nil {
		return nil, err
	}

	return s.repo.GetByID(ctx, task.ID)
}

func (s *taskService) DeleteTask(ctx context.Context, taskID uint) error {
	return s.repo.Delete(ctx, taskID)
}
