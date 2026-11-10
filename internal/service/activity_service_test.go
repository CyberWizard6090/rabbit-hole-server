package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
)

func newTestCodec(t *testing.T) *pagination.Codec {
	t.Helper()
	codec, err := pagination.NewCodec([]byte("test-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

type recorderStub struct {
	entries []domain.ActivityEntry
}

func (r *recorderStub) Record(_ context.Context, entries ...domain.ActivityEntry) error {
	r.entries = append(r.entries, entries...)
	return nil
}

type activityRepositoryMock struct {
	recorded []domain.ActivityEntry
	err      error
}

func (m *activityRepositoryMock) Record(_ context.Context, entries []domain.ActivityEntry) error {
	m.recorded = append(m.recorded, entries...)
	return m.err
}

func (m *activityRepositoryMock) ListByTask(context.Context, uint, int, int) ([]domain.Activity, int64, error) {
	return nil, 0, nil
}

func entryByField(entries []domain.ActivityEntry, field string) *domain.ActivityEntry {
	for i := range entries {
		if entries[i].Field == field {
			return &entries[i]
		}
	}
	return nil
}

func TestDiffTaskReportsOnlyChangedFields(t *testing.T) {
	due := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	parent := uint(4)
	before := &domain.Task{
		Model: domain.Model{ID: 9}, Title: "old", Description: "secret old", StatusID: 1, Priority: 2, ListID: 7,
		Assignees: []domain.CommentUser{{ID: 3}, {ID: 1}}, Tags: []domain.Tag{{ID: 5}},
	}
	after := &domain.Task{
		Model: domain.Model{ID: 9}, Title: "new", Description: "secret new", StatusID: 1, Priority: 2, ListID: 8,
		DueDate: &due, ParentID: &parent, Assignees: []domain.CommentUser{{ID: 1}, {ID: 3}, {ID: 6}}, Tags: []domain.Tag{{ID: 5}},
	}

	entries := diffTask(9, 42, snapshotTask(before), snapshotTask(after))

	for _, field := range []string{"status_id", "priority", "time_estimate", "tag_ids", "start_date"} {
		if entryByField(entries, field) != nil {
			t.Errorf("unchanged field %s was reported", field)
		}
	}
	title := entryByField(entries, "title")
	if title == nil || string(title.OldValue) != `"old"` || string(title.NewValue) != `"new"` || title.Actor.ID != 42 || title.TaskID != 9 {
		t.Fatalf("title entry = %+v", title)
	}
	if title.Action != domain.ActivityTaskUpdated {
		t.Errorf("title action = %s", title.Action)
	}
	if d := entryByField(entries, "description"); d == nil || d.OldValue != nil || d.NewValue != nil {
		t.Fatalf("description entry must carry no text: %+v", d)
	}
	if a := entryByField(entries, "assignee_ids"); a == nil || string(a.OldValue) != `[1,3]` || string(a.NewValue) != `[1,3,6]` {
		t.Fatalf("assignee entry = %+v", a)
	}
	if m := entryByField(entries, "list_id"); m == nil || m.Action != domain.ActivityTaskMoved {
		t.Fatalf("list entry = %+v", m)
	}
	if p := entryByField(entries, "parent_id"); p == nil || string(p.OldValue) != `null` || string(p.NewValue) != `4` {
		t.Fatalf("parent entry = %+v", p)
	}
	if d := entryByField(entries, "due_date"); d == nil || string(d.OldValue) != `null` {
		t.Fatalf("due entry = %+v", d)
	}
}

func TestDiffTaskIsEmptyWhenNothingChanged(t *testing.T) {
	task := &domain.Task{Model: domain.Model{ID: 1}, Title: "same", Assignees: []domain.CommentUser{{ID: 2}}}
	if entries := diffTask(1, 7, snapshotTask(task), snapshotTask(task)); len(entries) != 0 {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestTaskServiceRecordsCreationUpdateAndMove(t *testing.T) {
	rec := &recorderStub{}
	repo := &taskRepositoryMock{task: &domain.Task{Model: domain.Model{ID: 9}, SpaceID: 55, ListID: 7, StatusID: 3, Title: "task"}}
	spaces := &spaceRepositoryMock{list: &domain.List{Model: domain.Model{ID: 7}, SpaceID: 55}}
	codec := newTestCodec(t)
	svc := NewTaskService(repo, spaces, &tagServiceMock{}, codec, rec, nil, nil)

	if _, err := svc.CreateTask(context.Background(), 42, CreateTaskParams{Title: "Build it", ListID: 7, StatusID: 3}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(rec.entries) != 1 || rec.entries[0].Action != domain.ActivityTaskCreated || rec.entries[0].Actor.ID != 42 ||
		string(rec.entries[0].NewValue) != `{"title":"Build it"}` {
		t.Fatalf("created entries = %+v", rec.entries)
	}

	rec.entries = nil
	title := "renamed"
	repo.task = &domain.Task{Model: domain.Model{ID: 9}, SpaceID: 55, ListID: 7, StatusID: 3, Title: "task"}
	if _, err := svc.UpdateTask(context.Background(), 42, 9, UpdateTaskParams{Title: &title}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if len(rec.entries) != 1 || rec.entries[0].Field != "title" || string(rec.entries[0].NewValue) != `"renamed"` {
		t.Fatalf("update entries = %+v", rec.entries)
	}
}

func TestTaskServiceRecordsNothingForAFailedUpdateOrANoOp(t *testing.T) {
	rec := &recorderStub{}
	repo := &taskRepositoryMock{task: &domain.Task{Model: domain.Model{ID: 9}, ListID: 7, StatusID: 3, Title: "task"}}
	svc := NewTaskService(repo, &spaceRepositoryMock{}, &tagServiceMock{}, newTestCodec(t), rec, nil, nil)

	if _, err := svc.UpdateTask(context.Background(), 42, 9, UpdateTaskParams{}); err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	repo.updateErr = errors.New("boom")
	title := "x"
	if _, err := svc.UpdateTask(context.Background(), 42, 9, UpdateTaskParams{Title: &title}); err == nil {
		t.Fatal("expected the update error")
	}
	if len(rec.entries) != 0 {
		t.Fatalf("entries = %+v", rec.entries)
	}
}

func TestCommentServiceRecordsActivity(t *testing.T) {
	rec := &recorderStub{}
	repo := &commentRepositoryMock{existing: commentBy(7)}
	svc := NewCommentService(repo, &taskPermissionStub{}, rec)
	body := "edited"

	if _, err := svc.CreateComment(context.Background(), 7, 9, "hi", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateComment(context.Background(), 7, 9, 4, &body, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteComment(context.Background(), 7, 9, 4); err != nil {
		t.Fatal(err)
	}

	want := []string{domain.ActivityCommentCreated, domain.ActivityCommentUpdated, domain.ActivityCommentDeleted}
	if len(rec.entries) != 3 {
		t.Fatalf("entries = %+v", rec.entries)
	}
	for i, action := range want {
		e := rec.entries[i]
		if e.Action != action || e.TaskID != 9 || e.Actor.ID != 7 {
			t.Errorf("entry %d = %+v", i, e)
		}
		var payload map[string]uint
		if err := json.Unmarshal(e.NewValue, &payload); err != nil || payload["comment_id"] == 0 {
			t.Errorf("entry %d payload = %s", i, e.NewValue)
		}
	}
}

func TestActivityServiceRecordSwallowsRepositoryErrors(t *testing.T) {
	repo := &activityRepositoryMock{err: errors.New("db down")}
	svc := NewActivityService(repo)

	svc.Record(context.Background(), domain.ActivityEntry{TaskID: 1, Actor: domain.UserActor(2), Action: domain.ActivityTaskCreated})
	svc.Record(context.Background())

	if len(repo.recorded) != 1 {
		t.Fatalf("recorded = %+v", repo.recorded)
	}
}
