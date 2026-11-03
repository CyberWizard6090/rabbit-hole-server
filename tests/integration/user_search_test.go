package integration

import (
	"net/http"
	"net/url"
	"testing"
)

func searchUsers(t *testing.T, tc *TestContext, token, q string) (int, []any) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/users/search?q="+url.QueryEscape(q), token, nil)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	data, _ := decodeJSON(t, rec)["data"].([]any)
	return rec.Code, data
}

func TestUserSearch_DoesNotLeakEmailsOrAllowEnumeration(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, "searcher@example.test")
	for _, email := range []string{"alice.secret@example.test", "bob.secret@example.test"} {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": email, "password": "password-123", "username": email[:5] + "user",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("register %s: %d: %s", email, rec.Code, rec.Body.String())
		}
	}

	t.Run("short query is rejected", func(t *testing.T) {
		if code, _ := searchUsers(t, tc, token, "al"); code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("wildcards are matched literally", func(t *testing.T) {
		for _, q := range []string{"%%%", "___", "%.%"} {
			if code, res := searchUsers(t, tc, token, q); code != http.StatusOK || len(res) != 0 {
				t.Fatalf("q=%q: code=%d results=%v, want 200 and none", q, code, res)
			}
		}
	})

	t.Run("email substring does not match", func(t *testing.T) {
		if code, res := searchUsers(t, tc, token, "secret@example"); code != http.StatusOK || len(res) != 0 {
			t.Fatalf("code=%d results=%v, want 200 and none", code, res)
		}
	})

	t.Run("exact email matches without exposing it", func(t *testing.T) {
		code, res := searchUsers(t, tc, token, "Alice.Secret@example.test")
		if code != http.StatusOK || len(res) != 1 {
			t.Fatalf("code=%d results=%v, want exactly one", code, res)
		}
		user := res[0].(map[string]any)
		if _, leaked := user["email"]; leaked {
			t.Fatalf("search result exposes email: %v", user)
		}
		if user["id"] == nil || user["username"] == nil {
			t.Fatalf("search result misses public fields: %v", user)
		}
	})

	t.Run("username prefix matches and excludes self", func(t *testing.T) {
		code, res := searchUsers(t, tc, token, "bob.s")
		if code != http.StatusOK || len(res) != 1 {
			t.Fatalf("code=%d results=%v, want exactly one", code, res)
		}
	})
}
