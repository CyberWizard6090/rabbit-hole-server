package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestAuth_RegisterLoginAndProfile(t *testing.T) {
	tc := newTestContext(t)

	email := fmt.Sprintf("auth-%s-%d@example.test", t.Name(), time.Now().UnixNano())
	password := "password-123"

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("login response does not contain data object: %s", rec.Body.String())
	}
	token, ok := data["access_token"].(string)
	if !ok || token == "" {
		t.Fatalf("missing access_token: %s", rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/profile/", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	profile := dataObject(t, rec)
	if profile["email"] != email {
		t.Fatalf("profile email = %v, want %s", profile["email"], email)
	}
}

func TestAuth_DuplicateEmailIsRejectedAsConflict(t *testing.T) {
	tc := newTestContext(t)

	email := fmt.Sprintf("duplicate-%s-%d@example.test", t.Name(), time.Now().UnixNano())
	password := "password-123"
	body := map[string]any{"email": email, "password": password}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first registration: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate registration: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	errorBody := decodeJSON(t, rec)
	errorPayload, ok := errorBody["error"].(map[string]any)
	if !ok || errorPayload["code"] != "EMAIL_TAKEN" {
		t.Fatalf("duplicate registration: unexpected error payload: %s", rec.Body.String())
	}
}

func TestAuth_DuplicateUsernameIsRejectedAsConflict(t *testing.T) {
	tc := newTestContext(t)

	suffix := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	username := "user-" + suffix
	first := map[string]any{
		"email":    "first-" + suffix + "@example.test",
		"password": "password-123",
		"username": username,
	}
	second := map[string]any{
		"email":    "second-" + suffix + "@example.test",
		"password": "password-123",
		"username": username,
	}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", first)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first registration: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", second)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate username: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	errorBody := decodeJSON(t, rec)
	errorPayload, ok := errorBody["error"].(map[string]any)
	if !ok || errorPayload["code"] != "USERNAME_TAKEN" {
		t.Fatalf("duplicate username: unexpected error payload: %s", rec.Body.String())
	}
}

func TestAuth_ProtectedRouteRequiresToken(t *testing.T) {
	tc := newTestContext(t)

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile/", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuth_InvalidTokenIsRejected(t *testing.T) {
	tc := newTestContext(t)

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile/", "invalid-token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuth_LogoutInvalidatesRefreshSession(t *testing.T) {
	tc := newTestContext(t)

	email := fmt.Sprintf("logout-%s-%d@example.test", t.Name(), time.Now().UnixNano())
	password := "password-123"

	server := httptest.NewServer(tc.Router)
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	client := &http.Client{Jar: jar}
	accessToken := ""

	postJSON := func(path string, body map[string]any) *http.Response {
		t.Helper()

		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}

		req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if accessToken != "" {
			req.Header.Set("Authorization", "Bearer "+accessToken)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		return resp
	}

	resp := postJSON("/api/v1/auth/register", map[string]any{
		"email": email, "password": password,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON("/api/v1/auth/login", map[string]any{
		"email": email, "password": password,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	var loginBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	data, ok := loginBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("login response does not contain data object: %s", resp.Body)
	}
	accessToken = data["access_token"].(string)
	resp.Body.Close()

	if len(jar.Cookies(mustURL(t, server.URL+"/api/v1/auth/"))) == 0 {
		t.Fatal("login did not create cookies")
	}

	resp = postJSON("/api/v1/auth/logout", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON("/api/v1/auth/refresh", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_LoginTwiceRapidlyDoesNotDuplicateSession(t *testing.T) {
	tc := newTestContext(t)

	email := fmt.Sprintf("rapid-%s-%d@example.test", t.Name(), time.Now().UnixNano())
	password := "password-123"

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	for i := 0; i < 2; i++ {
		rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": email, "password": password,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("login #%d: expected 200, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return u
}
