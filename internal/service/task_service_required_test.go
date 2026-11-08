package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"rabbit-hole-server/internal/domain"
)

type fieldListMock struct {
	CustomFieldService
	fields []domain.CustomField
	set    map[uint]any
}

func (m *fieldListMock) List(context.Context, uint) ([]domain.CustomField, error) {
	return m.fields, nil
}

func (m *fieldListMock) SetTaskValue(_ context.Context, _ uint, fieldID uint, value any) error {
	if m.set == nil {
		m.set = map[uint]any{}
	}
	m.set[fieldID] = value
	return nil
}

func requiredTaskService(fields *fieldListMock) (TaskService, *taskRepositoryMock) {
	repo := &taskRepositoryMock{}
	spaces := &spaceRepositoryMock{
		list:  &domain.List{Model: domain.Model{ID: 7}, SpaceID: 55},
		space: &domain.Space{Model: domain.Model{ID: 55}, WorkspaceID: 99},
	}
	return NewTaskService(repo, spaces, &tagServiceMock{}, nil, nil, fields, nil), repo
}

func TestTaskServiceCreateTaskEnforcesRequiredCustomFields(t *testing.T) {
	fields := &fieldListMock{fields: []domain.CustomField{
		{Model: domain.Model{ID: 1}, Name: "Customer", Required: true},
		{Model: domain.Model{ID: 2}, Name: "Region", Required: true},
		{Model: domain.Model{ID: 3}, Name: "Note"},
	}}
	svc, repo := requiredTaskService(fields)
	params := CreateTaskParams{Title: "Task", ListID: 7, StatusID: 3}

	_, err := svc.CreateTask(context.Background(), 42, params)
	var required *domain.CustomFieldRequiredError
	if !errors.As(err, &required) || !errors.Is(err, domain.ErrCustomFieldRequired) {
		t.Fatalf("error = %v, want a required-field error", err)
	}
	if !reflect.DeepEqual(required.FieldIDs, []uint{1, 2}) {
		t.Fatalf("missing fields = %v, want [1 2]", required.FieldIDs)
	}
	if repo.createdTask != nil {
		t.Fatal("task created despite the missing values")
	}

	params.CustomFields = map[uint]any{1: "ACME", 2: "   ", 3: ""}
	_, err = svc.CreateTask(context.Background(), 42, params)
	if !errors.As(err, &required) || !reflect.DeepEqual(required.FieldIDs, []uint{2}) {
		t.Fatalf("blank value: error = %v", err)
	}

	params.CustomFields = map[uint]any{1: "ACME", 2: "EU", 3: ""}
	if _, err := svc.CreateTask(context.Background(), 42, params); err != nil {
		t.Fatalf("complete values rejected: %v", err)
	}
	if !reflect.DeepEqual(fields.set, map[uint]any{1: "ACME", 2: "EU"}) {
		t.Fatalf("stored values = %v, want the two non-empty ones", fields.set)
	}
}

func TestTaskServiceCreateTaskRejectsUnknownCustomField(t *testing.T) {
	fields := &fieldListMock{fields: []domain.CustomField{{Model: domain.Model{ID: 1}, Name: "Note"}}}
	svc, repo := requiredTaskService(fields)
	_, err := svc.CreateTask(context.Background(), 42, CreateTaskParams{
		Title: "Task", ListID: 7, StatusID: 3, CustomFields: map[uint]any{9: "x"},
	})
	if !errors.Is(err, domain.ErrInvalidCustomField) {
		t.Fatalf("error = %v, want ErrInvalidCustomField", err)
	}
	if repo.createdTask != nil {
		t.Fatal("task created despite the unknown field")
	}
}

func TestTaskServiceUpdateRejectsTagAddedAndRemoved(t *testing.T) {
	repo := &taskRepositoryMock{task: &domain.Task{Model: domain.Model{ID: 5}, SpaceID: 55, ListID: 7}}
	svc := NewTaskService(repo, &spaceRepositoryMock{}, &tagServiceMock{}, nil, nil, nil, nil)
	_, err := svc.UpdateTask(context.Background(), 42, 5, UpdateTaskParams{AddTagIDs: []uint{3, 4}, RemoveTagIDs: []uint{4}})
	if !errors.Is(err, domain.ErrConflictingTagChange) {
		t.Fatalf("error = %v, want ErrConflictingTagChange", err)
	}
}
