package integration

import (
	"net/url"
	"strings"
	"testing"
)

func TestSearch_FindsEmailAddressesTypedIntoTextButNeverAccountEmails(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Call the supplier", "")
	e.comment(t, task, "write to jane.doe@supplier.example about the invoice")

	results := e.results(t, "q="+url.QueryEscape("jane.doe@supplier.example"))
	if len(results) != 1 || results[0]["type"] != "comment" {
		t.Fatalf("results = %v, want the comment that contains the address", results)
	}
	snippet, _ := results[0]["snippet"].(map[string]any)
	if text, _ := snippet["text"].(string); !strings.Contains(text, "jane.doe@supplier.example") {
		t.Fatalf("snippet %v: free text written by users is returned as written", snippet)
	}

	guestCode, guestBody := e.search(t, e.guest, e.owner.workspaceID, "q="+url.QueryEscape("jane.doe@supplier.example"))
	if guestCode != 200 || len(guestBody["data"].([]any)) != 1 {
		t.Fatalf("a guest can read the same text, so the same hit is fine: %d %v", guestCode, guestBody)
	}

	for _, accountEmail := range []string{"srch-owner@example.test", "srch-guest@example.test", "srch-outsider@example.test"} {
		if got := e.results(t, "q="+url.QueryEscape(accountEmail)); len(got) != 0 {
			t.Fatalf("account e-mail %s matched: %v", accountEmail, got)
		}
	}
}
