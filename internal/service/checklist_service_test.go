package service

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

type checklistRepositoryMock struct {
	checklist *domain.Checklist
	item      *domain.ChecklistItem
	updated   *domain.ChecklistItem
	err       error
	deleted   bool
	gotUpdate domain.ChecklistItemUpdate
	gotActor  uint
}

func (m *checklistRepositoryMock) CreateChecklist(_ context.Context, taskID uint, name string) (*domain.Checklist, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &domain.Checklist{ID: 3, TaskID: taskID, Name: name}, nil
}
func (m *checklistRepositoryMock) ListByTask(context.Context, uint) ([]domain.Checklist, error) {
	return nil, m.err
}
func (m *checklistRepositoryMock) GetChecklist(context.Context, uint, uint) (*domain.Checklist, error) {
	return m.checklist, m.err
}
func (m *checklistRepositoryMock) RenameChecklist(context.Context, uint, uint, string) (*domain.Checklist, error) {
	return m.checklist, m.err
}
func (m *checklistRepositoryMock) DeleteChecklist(context.Context, uint, uint) error {
	m.deleted = m.err == nil
	return m.err
}
func (m *checklistRepositoryMock) ReorderChecklists(context.Context, uint, []uint) error {
	return m.err
}
func (m *checklistRepositoryMock) CreateItem(context.Context, uint, uint, string, *uint) (*domain.ChecklistItem, error) {
	return m.item, m.err
}
func (m *checklistRepositoryMock) GetItem(context.Context, uint, uint, uint) (*domain.ChecklistItem, error) {
	return m.item, m.err
}
func (m *checklistRepositoryMock) UpdateItem(_ context.Context, _, _, _, actorID uint, update domain.ChecklistItemUpdate) (*domain.ChecklistItem, error) {
	m.gotActor, m.gotUpdate = actorID, update
	if m.err != nil {
		return nil, m.err
	}
	return m.updated, nil
}
func (m *checklistRepositoryMock) DeleteItem(context.Context, uint, uint, uint) error { return m.err }
func (m *checklistRepositoryMock) ReorderItems(context.Context, uint, uint, []uint) error {
	return m.err
}

func TestChecklistServiceRecordsCreationAndDeletion(t *testing.T) {
	rec := &recorderStub{}
	repo := &checklistRepositoryMock{checklist: &domain.Checklist{ID: 3, TaskID: 9, Name: "Release"}}
	svc := NewChecklistService(repo, rec)

	if _, err := svc.CreateChecklist(context.Background(), 7, 9, "Release"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteChecklist(context.Background(), 7, 9, 3); err != nil {
		t.Fatal(err)
	}
	if len(rec.entries) != 2 {
		t.Fatalf("entries = %+v", rec.entries)
	}
	created, deleted := rec.entries[0], rec.entries[1]
	if created.Action != domain.ActivityChecklistCreated || created.ActorID != 7 || created.TaskID != 9 ||
		string(created.NewValue) != `{"checklist_id":3,"name":"Release"}` {
		t.Errorf("created = %+v (%s)", created, created.NewValue)
	}
	if deleted.Action != domain.ActivityChecklistDeleted || string(deleted.OldValue) != `{"checklist_id":3,"name":"Release"}` {
		t.Errorf("deleted = %+v (%s)", deleted, deleted.OldValue)
	}
}

func TestChecklistServiceDoesNotRecordFailures(t *testing.T) {
	rec := &recorderStub{}
	boom := errors.New("boom")
	repo := &checklistRepositoryMock{err: boom, checklist: &domain.Checklist{ID: 3}}
	svc := NewChecklistService(repo, rec)

	if _, err := svc.CreateChecklist(context.Background(), 7, 9, "x"); !errors.Is(err, boom) {
		t.Fatalf("create: %v", err)
	}
	if err := svc.DeleteChecklist(context.Background(), 7, 9, 3); !errors.Is(err, boom) || repo.deleted {
		t.Fatalf("delete: %v deleted=%t", err, repo.deleted)
	}
	if len(rec.entries) != 0 {
		t.Fatalf("entries = %+v", rec.entries)
	}
}

func TestChecklistServiceRecordsOnlyResolutionChanges(t *testing.T) {
	yes, no := true, false
	text := "renamed"

	cases := []struct {
		name     string
		before   bool
		after    bool
		update   domain.ChecklistItemUpdate
		wantType string
	}{
		{"resolve", false, true, domain.ChecklistItemUpdate{Resolved: &yes}, domain.ActivityChecklistItemResolved},
		{"reopen", true, false, domain.ChecklistItemUpdate{Resolved: &no}, domain.ActivityChecklistItemReopened},
		{"resolve again", true, true, domain.ChecklistItemUpdate{Resolved: &yes}, ""},
		{"text only", false, false, domain.ChecklistItemUpdate{Text: &text}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorderStub{}
			repo := &checklistRepositoryMock{
				item:    &domain.ChecklistItem{ID: 5, Resolved: tc.before, Text: "task"},
				updated: &domain.ChecklistItem{ID: 5, Resolved: tc.after, Text: "task"},
			}
			svc := NewChecklistService(repo, rec)

			if _, err := svc.UpdateItem(context.Background(), 7, 9, 3, 5, tc.update); err != nil {
				t.Fatal(err)
			}
			if repo.gotActor != 7 {
				t.Errorf("actor passed to the repository = %d", repo.gotActor)
			}
			if tc.wantType == "" {
				if len(rec.entries) != 0 {
					t.Fatalf("entries = %+v, want none", rec.entries)
				}
				return
			}
			if len(rec.entries) != 1 || rec.entries[0].Action != tc.wantType ||
				string(rec.entries[0].NewValue) != `{"checklist_id":3,"item_id":5,"text":"task"}` {
				t.Fatalf("entries = %+v", rec.entries)
			}
		})
	}
}

func TestChecklistServiceUpdateItemStopsWhenTheItemIsMissing(t *testing.T) {
	repo := &checklistRepositoryMock{err: domain.ErrNotFound}
	svc := NewChecklistService(repo, &recorderStub{})
	if _, err := svc.UpdateItem(context.Background(), 7, 9, 3, 5, domain.ChecklistItemUpdate{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestChecklistServiceWorksWithoutAnActivityRecorder(t *testing.T) {
	repo := &checklistRepositoryMock{checklist: &domain.Checklist{ID: 3}}
	svc := NewChecklistService(repo, nil)
	if _, err := svc.CreateChecklist(context.Background(), 7, 9, "x"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteChecklist(context.Background(), 7, 9, 3); err != nil {
		t.Fatal(err)
	}
}
