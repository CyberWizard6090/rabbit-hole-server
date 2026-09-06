package service

import (
	"fmt"
	"math/rand"
	"strings"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/repository"
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
	GetAllTasks(userID uint, limit int, offset int) ([]domain.Task, int64, error)
	GetTaskByID(taskID uint, userID uint) (*domain.Task, error)
	UpdateTask(id uint, uid uint, params UpdateTaskParams) (*domain.Task, error)
	DeleteTask(taskID uint, userID uint) error
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
	repo       repository.TaskRepository
	spaceRepo  domain.SpaceRepository
	tagService TagService
}

func NewTaskService(
	repo repository.TaskRepository,
	spaceRepo domain.SpaceRepository,
	tagService TagService,
) TaskService {
	return &taskService{
		repo:       repo,
		spaceRepo:  spaceRepo,
		tagService: tagService,
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

func (s *taskService) GetAllTasks(userID uint, limit int, offset int) ([]domain.Task, int64, error) {
	return s.repo.GetAll(userID, limit, offset)
}

func (s *taskService) GetTaskByID(taskID uint, userID uint) (*domain.Task, error) {
	return s.repo.GetByID(taskID, userID)
}

func (s *taskService) UpdateTask(id uint, uid uint, params UpdateTaskParams) (*domain.Task, error) {
	task, err := s.repo.GetByID(id, uid)
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

	return s.repo.GetByID(task.ID, uid)
}

func (s *taskService) DeleteTask(taskID uint, userID uint) error {
	return s.repo.Delete(taskID, userID)
}
