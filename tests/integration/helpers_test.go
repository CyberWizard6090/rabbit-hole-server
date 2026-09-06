package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/domain"
)

type TestContext struct {
	DB     *gorm.DB
	Router http.Handler
	Token  string
	UserID uint
}

func newTestContext(t *testing.T) *TestContext {
	t.Helper()

	_ = godotenv.Load("../../.env")
	_ = godotenv.Load(".env")
	dbURL := os.Getenv("TEST_DB_URL")
	if dbURL == "" {
		t.Skip("TEST_DB_URL is not set; integration tests require a PostgreSQL test database")
	}

	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", "integration-test-secret")
	}
	if os.Getenv("APP_PEPPER") == "" {
		os.Setenv("APP_PEPPER", "integration-test-pepper")
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	_ = db.Migrator().DropIndex(&domain.Tag{}, "idx_space_tag_name")

	err = db.AutoMigrate(
		&domain.User{},
		&domain.UserSession{},
		&domain.Workspace{},
		&domain.Space{},
		&domain.Folder{},
		&domain.List{},
		&domain.TaskStatus{},
		&domain.Tag{},
		&domain.Task{},
		&domain.Role{},
		&domain.Permission{},
		&domain.RolePermission{},
		&domain.Member{},
	)
	if err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	if err := config.SeedPermissions(db); err != nil {
		t.Fatalf("seed permissions: %v", err)
	}

	return &TestContext{
		DB:     db,
		Router: app.SetupRouter(app.NewContainer(db)),
	}
}

func request(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		payload = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, payload)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response JSON: %v; body=%s", err, rec.Body.String())
	}
	return result
}

func dataObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	body := decodeJSON(t, rec)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("response.data is not an object: %v", body["data"])
	}
	return data
}

func registerAndLogin(t *testing.T, tc *TestContext) {
	t.Helper()

	username := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	email := username + "@example.test"
	password := "test-password-123"

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    email,
		"password": password,
		"username": username,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email":    email,
		"password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)

	token, ok := body["access_token"].(string)
	if !ok || token == "" {
		t.Fatalf("login response does not contain access_token: %s", rec.Body.String())
	}

	var user domain.User
	if err := tc.DB.Where("email = ?", email).First(&user).Error; err != nil {
		t.Fatalf("find registered user: %v", err)
	}

	tc.UserID = user.ID
	tc.Token = token
}
