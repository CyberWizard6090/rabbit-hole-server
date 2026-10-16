package service

import (
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
	CreateTask(userID uint, input CreateTaskParams) (*domain.Task, error)
	ListTasks(listID uint, params pagination.Params) (pagination.Response[domain.Task], error)
	GetTaskByID(taskID uint) (*domain.Task, error)
	UpdateTask(id uint, params UpdateTaskParams) (*domain.Task, error)
	DeleteTask(taskID uint) error
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

func (s *taskService) resolveNewTags(spaceID uint, names []string) ([]uint, error) {
	ids := make([]uint, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		tag, err := s.tagService.GetOrCreateByName(spaceID, name, randomTagColor())
		if err != nil {
			return nil, fmt.Errorf("get or create tag %q: %w", name, err)
		}
		ids = append(ids, tag.ID)
	}
	return ids, nil
}

func (s *taskService) CreateTask(userID uint, input CreateTaskParams) (*domain.Task, error) {
	list, err := s.spaceRepo.GetListByID(input.ListID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}

	if input.Priority == 0 {
		input.Priority = 2
	}

	newTagIDs, err := s.resolveNewTags(list.SpaceID, input.NewTagNames)
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

	if err := s.repo.Create(task, input.AssigneeIDs, allTagIDs); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *taskService) ListTasks(listID uint, params pagination.Params) (pagination.Response[domain.Task], error) {
	context := pagination.TaskCursorContext(listID)
	items, hasNext, hasPrev, err := s.repo.List(listID, params)
	if err != nil {
		return pagination.Response[domain.Task]{}, err
	}
	var nextCursor *string
	var prevCursor *string

	if len(items) > 0 {
		if hasNext {
			cursor, err := pagination.EncodePosition(s.cursorCodec, context, pagination.DirectionNext, pagination.TaskCursorPosition{
				CreatedAt: items[len(items)-1].CreatedAt,
				ID:        uint64(items[len(items)-1].ID),
			})
			if err != nil {
				return pagination.Response[domain.Task]{}, err
			}
			nextCursor = &cursor
		}

		if hasPrev {
			cursor, err := pagination.EncodePosition(s.cursorCodec, context, pagination.DirectionPrev, pagination.TaskCursorPosition{
				CreatedAt: items[0].CreatedAt,
				ID:        uint64(items[0].ID),
			})
			if err != nil {
				return pagination.Response[domain.Task]{}, err
			}
			prevCursor = &cursor
		}
	}

	return pagination.NewResponse(items, hasNext, hasPrev, nextCursor, prevCursor), nil
}

func (s *taskService) GetTaskByID(taskID uint) (*domain.Task, error) {
	return s.repo.GetByID(taskID)
}

func (s *taskService) UpdateTask(id uint, params UpdateTaskParams) (*domain.Task, error) {
	task, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

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

	if err := s.repo.Update(task); err != nil {
		return nil, err
	}

	newTagIDs, err := s.resolveNewTags(task.SpaceID, params.AddTagNames)
	if err != nil {
		return nil, err
	}
	tagIDsToAdd := append(append([]uint{}, params.AddTagIDs...), newTagIDs...)
	if len(tagIDsToAdd) > 0 {
		if err := s.repo.AddTags(task.ID, tagIDsToAdd); err != nil {
			return nil, fmt.Errorf("add tags to task: %w", err)
		}
	}

	return s.repo.GetByID(task.ID)
}

func (s *taskService) DeleteTask(taskID uint) error {
	return s.repo.Delete(taskID)
}
