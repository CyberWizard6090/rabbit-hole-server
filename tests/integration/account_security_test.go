package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type session struct {
	access  string
	refresh *http.Cookie
}

func loginSession(t *testing.T, tc *TestContext, email, password string) session {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: %d: %s", email, rec.Code, rec.Body.String())
	}
	s := session{access: dataObject(t, rec)["access_token"].(string)}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "refresh_token" {
			s.refresh = ck
		}
	}
	if s.refresh == nil {
		t.Fatal("login did not set a refresh cookie")
	}
	return s
}

func refreshStatus(tc *TestContext, ck *http.Cookie) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec.Code
}

func registerUser(t *testing.T, tc *TestContext, email, password string) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": email, "password": password})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s: %d: %s", email, rec.Code, rec.Body.String())
	}
}

func TestAccount_PasswordPolicyOnRegister(t *testing.T) {
	tc := newTestContext(t)
	for name, password := range map[string]string{"too short": "1234567", "too long": strings.Repeat("a", 129)} {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": "policy@example.test", "password": password,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	registerUser(t, tc, "policy-ok@example.test", "12345678")
}

func TestAccount_ChangePasswordRevokesOldTokens(t *testing.T) {
	tc := newTestContext(t)
	email := "pw-change@example.test"
	registerUser(t, tc, email, "old-password-1")
	old := loginSession(t, tc, email, "old-password-1")

	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", old.access, nil); rec.Code != http.StatusOK {
		t.Fatalf("profile before change: %d", rec.Code)
	}

	t.Run("wrong current password", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/profile/password", old.access,
			map[string]any{"current_password": "nope-nope-1", "new_password": "new-password-1"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("weak new password", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/profile/password", old.access,
			map[string]any{"current_password": "old-password-1", "new_password": "short"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", old.access, nil); rec.Code != http.StatusOK {
		t.Fatalf("token revoked by failed attempts: %d", rec.Code)
	}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/profile/password", old.access,
		map[string]any{"current_password": "old-password-1", "new_password": "new-password-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d: %s", rec.Code, rec.Body.String())
	}
	fresh := dataObject(t, rec)["access_token"].(string)

	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", old.access, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old access token after password change: expected 401, got %d", rec.Code)
	}
	if code := refreshStatus(tc, old.refresh); code != http.StatusUnauthorized {
		t.Fatalf("old refresh token after password change: expected 401, got %d", code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", fresh, nil); rec.Code != http.StatusOK {
		t.Fatalf("fresh access token: expected 200, got %d", rec.Code)
	}

	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": "old-password-1"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password: expected 401, got %d", rec.Code)
	}
	loginSession(t, tc, email, "new-password-1")
}

func TestAccount_LogoutAllRevokesEverySession(t *testing.T) {
	tc := newTestContext(t)
	email := "logout-all@example.test"
	registerUser(t, tc, email, "password-123")
	first := loginSession(t, tc, email, "password-123")
	second := loginSession(t, tc, email, "password-123")

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/logout-all", first.access, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout-all: %d: %s", rec.Code, rec.Body.String())
	}
	for name, s := range map[string]session{"first": first, "second": second} {
		if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", s.access, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s access token: expected 401, got %d", name, rec.Code)
		}
		if code := refreshStatus(tc, s.refresh); code != http.StatusUnauthorized {
			t.Errorf("%s refresh token: expected 401, got %d", name, code)
		}
	}
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/logout-all", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous logout-all: expected 401, got %d", rec.Code)
	}
	loginSession(t, tc, email, "password-123")
}

func TestAccount_DeleteAccount(t *testing.T) {
	tc := newTestContext(t)
	email := "doomed@example.test"
	registerUser(t, tc, email, "password-123")
	s := loginSession(t, tc, email, "password-123")
	friendID, friendToken := newUser(t, tc, "friend@example.test")
	var selfID uint
	tc.DB.Table("users").Select("id").Where("email = ?", email).Scan(&selfID)
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/contacts", friendToken, map[string]any{"contact_id": selfID}); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("add contact: %d: %s", rec.Code, rec.Body.String())
	}
	_ = friendID

	workspace := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", s.access, map[string]any{"name": "mine"})
	if workspace.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d", workspace.Code)
	}
	workspaceID := responseID(t, workspace)

	t.Run("wrong password", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", s.access, map[string]any{"password": "wrong-password"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("blocked while owning a workspace", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", s.access, map[string]any{"password": "password-123"})
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(workspaceID), s.access, nil); rec.Code != http.StatusOK && rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete workspace: %d", rec.Code)
	}
	tc.DB.Exec("UPDATE workspaces SET deleted_at = now() WHERE id = ?", workspaceID)

	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", s.access, map[string]any{"password": "password-123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", s.access, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("access token after deletion: expected 401, got %d", rec.Code)
	}
	if code := refreshStatus(tc, s.refresh); code != http.StatusUnauthorized {
		t.Errorf("refresh after deletion: expected 401, got %d", code)
	}
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": "password-123"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("login after deletion: expected 401, got %d", rec.Code)
	}

	var stored struct{ Email, Username string }
	tc.DB.Table("users").Where("id = ?", selfID).Scan(&stored)
	if strings.Contains(stored.Email, "doomed") || !strings.HasSuffix(stored.Email, "@deleted.invalid") {
		t.Errorf("email not anonymised: %q", stored.Email)
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/contacts", friendToken, nil)
	if data, _ := decodeJSON(t, rec)["data"].([]any); len(data) != 0 {
		t.Errorf("deleted account still in friend's contacts: %v", data)
	}

	registerUser(t, tc, email, "password-456")
}
