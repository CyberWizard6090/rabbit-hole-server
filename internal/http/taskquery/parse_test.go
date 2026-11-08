package taskquery

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestParseDefaults(t *testing.T) {
	q, err := Parse(url.Values{"limit": {"10"}, "cursor": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Sort) != 0 || q.FilterSignature() != "" || q.SortSignature() != "id_desc" || q.WithTotal {
		t.Fatalf("unexpected defaults: %+v sort=%s", q, q.SortSignature())
	}
}

func TestParseSortAddsIDTieBreak(t *testing.T) {
	q, err := Parse(url.Values{"sort": {"-priority,due_date"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.SortSignature(), "priority_desc,due_date_asc,id_asc"; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
	q, _ = Parse(url.Values{"sort": {"-id"}})
	if got := q.SortSignature(); got != "id_desc" {
		t.Fatalf("explicit id must not be duplicated: %q", got)
	}
}

func TestParseFiltersAreCanonical(t *testing.T) {
	a, err := Parse(url.Values{"status_id": {"4,3"}, "priority": {"2,1,2"}, "q": {" Login "}, "due_from": {"2026-11-01"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(url.Values{"status_id": {"3", "4", "3"}, "priority": {"1,2"}, "q": {"Login"}, "due_from": {"2026-11-01"}, "with_total": {"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Filter.StatusIDs, []uint{3, 4}) || !reflect.DeepEqual(a.Filter.Priorities, []int{1, 2}) {
		t.Fatalf("lists not normalised: %+v", a.Filter)
	}
	if a.FilterSignature() == "" || a.FilterSignature() != b.FilterSignature() {
		t.Fatalf("equivalent filters must share a signature: %q vs %q", a.FilterSignature(), b.FilterSignature())
	}
	c, _ := Parse(url.Values{"status_id": {"3"}})
	if c.FilterSignature() == a.FilterSignature() {
		t.Fatal("different filters must differ")
	}
	if !b.WithTotal || a.WithTotal {
		t.Fatal("with_total parsing")
	}
}

func TestParseErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		values url.Values
		code   string
		field  string
	}{
		"unknown field": {url.Values{"sort": {"description"}}, CodeInvalidSort, "sort"},
		"four fields":   {url.Values{"sort": {"id,title,priority,status"}}, CodeInvalidSort, "sort"},
		"duplicate":     {url.Values{"sort": {"title,-title"}}, CodeInvalidSort, "sort"},
		"bad id":        {url.Values{"tag_id": {"x"}}, CodeInvalidFilter, "tag_id"},
		"too many ids":  {url.Values{"tag_id": {strings.TrimSuffix(strings.Repeat("1,", 101), ",")}}, CodeInvalidFilter, "tag_id"},
		"bad priority":  {url.Values{"priority": {"0"}}, CodeInvalidFilter, "priority"},
		"bad bool":      {url.Values{"unassigned": {"TRUE"}}, CodeInvalidFilter, "unassigned"},
		"bad date":      {url.Values{"created_to": {"2026-13-01"}}, CodeInvalidFilter, "created_to"},
		"reversed":      {url.Values{"due_from": {"2026-02-02"}, "due_to": {"2026-02-01"}}, CodeInvalidFilter, "due_from"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(tc.values)
			if err == nil || err.Code != tc.code || err.Status != 400 {
				t.Fatalf("err = %+v, want code %s", err, tc.code)
			}
			found := false
			for _, d := range err.Details {
				if d.Field == tc.field && d.Code != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("details = %+v, want an entry for %q", err.Details, tc.field)
			}
		})
	}
}

func TestEveryWhitelistedFieldParses(t *testing.T) {
	for _, f := range []domain.TaskSortField{"id", "title", "status", "priority", "time_estimate", "time_spent", "start_date", "due_date", "created_at", "updated_at", "assignee"} {
		if _, err := Parse(url.Values{"sort": {"-" + string(f)}}); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
