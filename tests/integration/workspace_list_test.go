package integration

import (
	"net/http"
	"testing"
)

func listWorkspaces(t *testing.T, tc *TestContext, token, query string) (int, map[string]any) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/workspaces"+query, token, nil)
	return rec.Code, decodeJSON(t, rec)
}

func TestWorkspaceList_CarriesTheCallersRoleInEachWorkspace(t *testing.T) {
	tc := newTestContext(t)
	_, ownerToken := newUser(t, tc, "list-owner@example.test")
	memberID, memberToken := newUser(t, tc, "list-member@example.test")

	roles := map[string]string{"First": "Admin", "Second": "Guest", "Third": "Member"}
	ids := map[string]uint{}
	for _, name := range []string{"First", "Second", "Third"} {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", ownerToken, map[string]any{"name": name})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d: %s", name, rec.Code, rec.Body.String())
		}
		ids[name] = responseID(t, rec)
		addMemberWithRole(t, tc, ids[name], memberID, roles[name])
	}

	code, body := listWorkspaces(t, tc, ownerToken, "")
	if code != http.StatusOK {
		t.Fatalf("owner list: %d", code)
	}
	for _, item := range body["data"].([]any) {
		if got := item.(map[string]any)["role"]; got != "owner" {
			t.Fatalf("owner sees role %v, want owner: %v", got, item)
		}
	}

	code, body = listWorkspaces(t, tc, memberToken, "")
	if code != http.StatusOK {
		t.Fatalf("member list: %d", code)
	}
	items := body["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("member sees %d workspaces, want 3", len(items))
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		want := map[string]string{"Admin": "admin", "Guest": "guest", "Member": "member"}[roles[item["name"].(string)]]
		if item["role"] != want {
			t.Fatalf("workspace %v role = %v, want %s", item["name"], item["role"], want)
		}
		if _, leaked := item["email"]; leaked {
			t.Fatalf("workspace item must not carry e-mail addresses: %v", item)
		}
	}
}

func TestWorkspaceList_IsPaginatedWithTheStandardEnvelope(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, "list-pages@example.test")
	for _, name := range []string{"One", "Two", "Three", "Four", "Five"} {
		if rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{"name": name}); rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d", name, rec.Code)
		}
	}

	t.Run("defaults", func(t *testing.T) {
		code, body := listWorkspaces(t, tc, token, "")
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		pagination := body["pagination"].(map[string]any)
		if pagination["total"] != float64(5) || pagination["page"] != float64(1) || pagination["limit"] != float64(50) || pagination["pages"] != float64(1) {
			t.Fatalf("pagination = %v, want total 5 page 1 limit 50 pages 1", pagination)
		}
		if len(body["data"].([]any)) != 5 {
			t.Fatalf("items = %d", len(body["data"].([]any)))
		}
	})
	t.Run("pages are stable and complete", func(t *testing.T) {
		seen := map[string]bool{}
		for page := 1; page <= 3; page++ {
			code, body := listWorkspaces(t, tc, token, "?limit=2&page="+itoa(uint(page)))
			if code != http.StatusOK {
				t.Fatalf("page %d status %d", page, code)
			}
			pagination := body["pagination"].(map[string]any)
			if pagination["total"] != float64(5) || pagination["pages"] != float64(3) || pagination["limit"] != float64(2) {
				t.Fatalf("pagination = %v", pagination)
			}
			for _, item := range body["data"].([]any) {
				name := item.(map[string]any)["name"].(string)
				if seen[name] {
					t.Fatalf("workspace %s repeated across pages", name)
				}
				seen[name] = true
			}
		}
		if len(seen) != 5 {
			t.Fatalf("pages together returned %d workspaces, want 5", len(seen))
		}
	})
	t.Run("page past the end is empty", func(t *testing.T) {
		code, body := listWorkspaces(t, tc, token, "?limit=2&page=9")
		if code != http.StatusOK || len(body["data"].([]any)) != 0 {
			t.Fatalf("status %d body %v", code, body)
		}
	})
	t.Run("invalid parameters are rejected", func(t *testing.T) {
		for _, query := range []string{"?limit=0", "?limit=101", "?limit=abc", "?page=0", "?page=-1"} {
			code, body := listWorkspaces(t, tc, token, query)
			if code != http.StatusBadRequest {
				t.Fatalf("%s: status %d, want 400", query, code)
			}
			if got := body["error"].(map[string]any)["code"]; got != "INVALID_PAGINATION" {
				t.Fatalf("%s: code %v", query, got)
			}
		}
	})
	t.Run("maximum limit is accepted", func(t *testing.T) {
		if code, _ := listWorkspaces(t, tc, token, "?limit=100"); code != http.StatusOK {
			t.Fatalf("limit=100: %d", code)
		}
	})
}

func TestWorkspaceList_HidesDeletedWorkspacesAndForeignOnes(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, "list-hidden@example.test")
	_, other := newUser(t, tc, "list-hidden-other@example.test")

	kept := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{"name": "Kept"})
	gone := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{"name": "Gone"})
	request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", other, map[string]any{"name": "Foreign"})
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(responseID(t, gone)), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}

	code, body := listWorkspaces(t, tc, token, "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	items := body["data"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != float64(responseID(t, kept)) {
		t.Fatalf("items = %v, want only the kept workspace", items)
	}
	if body["pagination"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("total = %v, want 1", body["pagination"])
	}
}
