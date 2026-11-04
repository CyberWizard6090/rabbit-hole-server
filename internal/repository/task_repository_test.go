package repository

import (
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
