package pagination

import (
	"errors"
	"strconv"
	"testing"
)

func TestParseOffset(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got, err := ParseOffset("", "")
		if err != nil {
			t.Fatalf("ParseOffset() unexpected error: %v", err)
		}
		if got.Page != DefaultPage || got.Limit != DefaultLimit || got.Offset != 0 {
			t.Fatalf("ParseOffset() = %#v, want page=%d limit=%d offset=0", got, DefaultPage, DefaultLimit)
		}
	})

	t.Run("valid custom values", func(t *testing.T) {
		got, err := ParseOffset("2", "25")
		if err != nil {
			t.Fatalf("ParseOffset() unexpected error: %v", err)
		}
		if got.Page != 2 || got.Limit != 25 || got.Offset != 25 {
			t.Fatalf("ParseOffset() = %#v, want page=2 limit=25 offset=25", got)
		}
	})

	for _, tc := range []struct {
		name     string
		rawPage  string
		rawLimit string
		wantErr  error
	}{
		{name: "negative page", rawPage: "-1", rawLimit: "10", wantErr: ErrInvalidPage},
		{name: "zero page", rawPage: "0", rawLimit: "10", wantErr: ErrInvalidPage},
		{name: "page beyond max", rawPage: strconv.Itoa(MaxPage + 1), rawLimit: "10", wantErr: ErrInvalidPage},
		{name: "page with trailing text", rawPage: "2abc", rawLimit: "10", wantErr: ErrInvalidPage},
		{name: "page with decimal", rawPage: "2.0", rawLimit: "10", wantErr: ErrInvalidPage},
		{name: "page with internal whitespace", rawPage: "2 3", rawLimit: "10", wantErr: ErrInvalidPage},
		{name: "negative limit", rawPage: "1", rawLimit: "-1", wantErr: ErrInvalidLimit},
		{name: "zero limit", rawPage: "1", rawLimit: "0", wantErr: ErrInvalidLimit},
		{name: "limit beyond max", rawPage: "1", rawLimit: strconv.Itoa(MaxLimit + 1), wantErr: ErrInvalidLimit},
		{name: "limit with trailing text", rawPage: "1", rawLimit: "10abc", wantErr: ErrInvalidLimit},
		{name: "limit with decimal", rawPage: "1", rawLimit: "10.5", wantErr: ErrInvalidLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOffset(tc.rawPage, tc.rawLimit)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParseOffset(%q, %q) error = %v, want %v", tc.rawPage, tc.rawLimit, err, tc.wantErr)
			}
		})
	}
}
