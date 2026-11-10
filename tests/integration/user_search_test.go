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

	t.Run("an e-mail address never matches, registered or not", func(t *testing.T) {
		for _, q := range []string{"Alice.Secret@example.test", "alice.secret@example.test", "nobody.here@example.test"} {
			if code, res := searchUsers(t, tc, token, q); code != http.StatusOK || len(res) != 0 {
				t.Fatalf("q=%q: code=%d results=%v, want 200 and none", q, code, res)
			}
		}
	})

	t.Run("results carry only the public projection", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/users/search?q=bob.s", token, nil)
		requireNoEmail(t, "search", rec.Body.String())
		user := dataList(t, rec)[0].(map[string]any)
		for _, key := range []string{"id", "username", "first_name", "last_name", "avatar_url"} {
			if _, ok := user[key]; !ok {
				t.Fatalf("missing %s in %v", key, user)
			}
		}
		if len(user) != 5 {
			t.Fatalf("unexpected fields in the public projection: %v", user)
		}
	})

	t.Run("display name prefix matches", func(t *testing.T) {
		if err := tc.DB.Exec("UPDATE users SET first_name = 'Zebulon', last_name = 'Quimby' WHERE username = 'aliceuser'").Error; err != nil {
			t.Fatalf("set name: %v", err)
		}
		if code, res := searchUsers(t, tc, token, "zebul"); code != http.StatusOK || len(res) != 1 {
			t.Fatalf("first name: code=%d results=%v", code, res)
		}
		if code, res := searchUsers(t, tc, token, "QUIMB"); code != http.StatusOK || len(res) != 1 {
			t.Fatalf("last name: code=%d results=%v", code, res)
		}
	})

	t.Run("username prefix matches and excludes self", func(t *testing.T) {
		code, res := searchUsers(t, tc, token, "bob.s")
		if code != http.StatusOK || len(res) != 1 {
			t.Fatalf("code=%d results=%v, want exactly one", code, res)
		}
	})
}
