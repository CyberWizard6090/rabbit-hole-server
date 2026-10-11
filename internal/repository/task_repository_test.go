package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"rabbit-hole-server/internal/http/pagination"
)

func TestHasPreviousPage(t *testing.T) {
	tests := []struct {
		name   string
		cursor *pagination.Cursor
		more   bool
		want   bool
	}{
		{name: "first page", want: false},
		{name: "first page reached with previous cursor", cursor: &pagination.Cursor{Direction: pagination.DirectionPrev}, want: false},
		{name: "previous page exists", cursor: &pagination.Cursor{Direction: pagination.DirectionPrev}, more: true, want: true},
		{name: "next page has previous page", cursor: &pagination.Cursor{Direction: pagination.DirectionNext}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasPreviousPage(test.cursor, test.more); got != test.want {
				t.Fatalf("hasPreviousPage() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestUniqueIDsRemovesDuplicatesAndPreservesOrder(t *testing.T) {
	got := uniqueIDs([]uint{4, 2, 4, 9, 2, 9, 1})
	want := []uint{4, 2, 9, 1}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestUniqueIDsHandlesEmptyInput(t *testing.T) {
	got := uniqueIDs(nil)
	if len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestDecodeTaskCursorPositionRejectsInvalidIDs(t *testing.T) {
	for _, raw := range []string{
		`{"created_at":"2026-10-09T12:00:00Z","id":0}`,
		`{"created_at":"2026-10-09T12:00:00Z","id":-1}`,
		`{"created_at":"2026-10-09T12:00:00Z","id":"42"}`,
		`{"created_at":"2026-10-09T12:00:00Z","id":9223372036854775808}`,
		`{"created_at":"2026-10-09T12:00:00Z","id":18446744073709551616}`,
		`not-json`,
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := decodeTaskCursorPosition(&pagination.Cursor{Position: json.RawMessage(raw)})
			if !errors.Is(err, pagination.ErrInvalidCursor) {
				t.Fatalf("expected ErrInvalidCursor for position %s, got %v", raw, err)
			}
		})
	}
}

func TestDecodeTaskCursorPositionAcceptsPositiveID(t *testing.T) {
	got, err := decodeTaskCursorPosition(&pagination.Cursor{
		Position: json.RawMessage(`{"created_at":"2026-10-09T12:00:00Z","id":42}`),
	})
	if err != nil {
		t.Fatalf("decodeTaskCursorPosition returned error: %v", err)
	}
	if got.ID != 42 {
		t.Fatalf("decodeTaskCursorPosition() = %+v, want ID 42", got)
	}
}
