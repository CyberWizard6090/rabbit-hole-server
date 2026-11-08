package taskquery

import (
	"net/url"
	"testing"
)

func TestParseHasOpenChecklistItems(t *testing.T) {
	open, err := Parse(url.Values{"has_open_checklist_items": {"true"}})
	if err != nil || open.Filter.HasOpenChecklistItems == nil || !*open.Filter.HasOpenChecklistItems {
		t.Fatalf("true: %+v %v", open.Filter, err)
	}
	closed, _ := Parse(url.Values{"has_open_checklist_items": {"false"}})
	if closed.Filter.HasOpenChecklistItems == nil || *closed.Filter.HasOpenChecklistItems {
		t.Fatalf("false: %+v", closed.Filter)
	}
	none, _ := Parse(url.Values{"has_open_checklist_items": {""}})
	if none.Filter.HasOpenChecklistItems != nil || none.FilterSignature() != "" {
		t.Fatalf("empty must be ignored: %+v", none.Filter)
	}
	if open.FilterSignature() == "" || open.FilterSignature() == closed.FilterSignature() {
		t.Fatal("the filter must be part of the cursor signature")
	}
	if _, err := Parse(url.Values{"has_open_checklist_items": {"maybe"}}); err == nil || len(err.Details) != 1 || err.Details[0].Field != "has_open_checklist_items" || err.Details[0].Code != "INVALID_BOOLEAN" {
		t.Fatalf("invalid value accepted: %v", err)
	}
}
