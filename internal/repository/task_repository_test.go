package repository

import "testing"

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
