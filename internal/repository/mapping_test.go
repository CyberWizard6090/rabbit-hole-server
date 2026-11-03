package repository

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

func TestStatusTypeEnumsStayAligned(t *testing.T) {
	pairs := []struct {
		name string
		d    domain.StatusType
		p    persistence.StatusType
	}{
		{"undefined", domain.StatusTypeUndefined, persistence.StatusTypeUndefined},
		{"todo", domain.StatusTodo, persistence.StatusTodo},
		{"in progress", domain.StatusInProgress, persistence.StatusInProgress},
		{"done", domain.StatusDone, persistence.StatusDone},
	}
	for _, pair := range pairs {
		if int(pair.d) != int(pair.p) {
			t.Errorf("%s: domain=%d persistence=%d", pair.name, pair.d, pair.p)
		}
	}
}

func TestMapErrTranslatesRecordNotFoundOnly(t *testing.T) {
	if got := mapErr(nil); got != nil {
		t.Fatalf("mapErr(nil) = %v", got)
	}
	boom := errors.New("boom")
	if got := mapErr(boom); got != boom {
		t.Fatalf("other errors must pass through untouched, got %v", got)
	}
	wrapped := fmt.Errorf("lookup: %w", gorm.ErrRecordNotFound)
	if got := mapErr(wrapped); !errors.Is(got, domain.ErrNotFound) {
		t.Fatalf("a wrapped record-not-found must become domain.ErrNotFound, got %v", got)
	}
}

func TestMapSliceKeepsNilAndEmptyApart(t *testing.T) {
	double := func(i int) int { return i * 2 }
	if got := mapSlice[int, int](nil, double); got != nil {
		t.Fatalf("nil input must stay nil (it serialises as null), got %#v", got)
	}
	if got := mapSlice([]int{}, double); got == nil || len(got) != 0 {
		t.Fatalf("empty input must stay a non-nil empty slice (it serialises as []), got %#v", got)
	}
}

func TestTaskMappingRoundTripsEveryField(t *testing.T) {
	parent := uint(3)
	original := domain.Task{
		Model: domain.Model{ID: 9}, SpaceID: 1, ListID: 2, StatusID: 4, ParentID: &parent,
		Title: "t", Description: "d", Priority: 5, UserID: 6, TimeEstimate: 7, TimeSpent: 8,
		Assignees: []domain.User{{ID: 10, Email: "a@b.c"}},
		Tags:      []domain.Tag{{ID: 11, SpaceID: 1, Name: "x", Color: "#000000"}},
	}
	back := taskToDomain(taskFromDomain(original))
	if back.ID != 9 || back.SpaceID != 1 || back.ListID != 2 || back.StatusID != 4 || *back.ParentID != 3 ||
		back.Title != "t" || back.Description != "d" || back.Priority != 5 || back.UserID != 6 ||
		back.TimeEstimate != 7 || back.TimeSpent != 8 ||
		len(back.Assignees) != 1 || back.Assignees[0].ID != 10 || len(back.Tags) != 1 || back.Tags[0].Name != "x" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}
