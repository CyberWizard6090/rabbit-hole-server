package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

var (
	integrationDB     *gorm.DB
	integrationConfig *config.Config
)

type TestContext struct {
	DB     *gorm.DB
	Router http.Handler
	Token  string
	UserID uint
}

func TestMain(m *testing.M) {
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load(".env")

	testDBURL := os.Getenv("TEST_DB_URL")
	if testDBURL == "" {
		if integrationDBRequiredInCI(os.Getenv("CI"), testDBURL) {
			fmt.Fprintln(os.Stderr, "TEST_DB_URL is required in CI for integration tests")
			os.Exit(1)
		}
		os.Exit(m.Run())
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load integration config: %v\n", err)
		os.Exit(1)
	}
	if cfg.Environment == "production" {
		fmt.Fprintln(os.Stderr, "integration tests cannot reset the database in production")
		os.Exit(1)
	}
	if err := ensureSeparateDatabaseURLs(cfg.DB.URL, testDBURL); err != nil {
		fmt.Fprintf(os.Stderr, "unsafe integration database configuration: %v\n", err)
		os.Exit(1)
	}

	bootstrapDB, err := gorm.Open(postgres.Open(testDBURL), &gorm.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open integration database: %v\n", err)
		os.Exit(1)
	}
	if err := bootstrapDB.Exec("DROP SCHEMA IF EXISTS public CASCADE").Error; err != nil {
		fmt.Fprintf(os.Stderr, "drop integration schema: %v\n", err)
		os.Exit(1)
	}
	if err := bootstrapDB.Exec("CREATE SCHEMA public").Error; err != nil {
		fmt.Fprintf(os.Stderr, "create integration schema: %v\n", err)
		os.Exit(1)
	}
	bootstrapSQLDB, err := bootstrapDB.DB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "get bootstrap connection pool: %v\n", err)
		os.Exit(1)
	}
	if err := bootstrapSQLDB.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close bootstrap connection pool: %v\n", err)
		os.Exit(1)
	}

	for _, action := range []string{"up", "up", "down", "up"} {
		if _, _, err := database.RunMigrations(testDBURL, action, 0, 0); err != nil {
			fmt.Fprintf(os.Stderr, "run integration database migrations (%s): %v\n", action, err)
			os.Exit(1)
		}
	}

	cfg.DB.URL = testDBURL
	integrationDB, err = database.ConnectDB(
		testDBURL,
		cfg.Environment,
		cfg.DB.MaxIdleConnections,
		cfg.DB.MaxOpenConnections,
		cfg.DB.ConnMaxLifetime,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect integration database: %v\n", err)
		os.Exit(1)
	}
	integrationConfig = cfg

	exitCode := m.Run()
	if sqlDB, err := integrationDB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	os.Exit(exitCode)
}

func integrationDBRequiredInCI(ci, testDBURL string) bool {
	return strings.EqualFold(ci, "true") && testDBURL == ""
}

func newTestContext(t *testing.T) *TestContext {
	t.Helper()

	if integrationDB == nil || integrationConfig == nil {
		t.Skip("TEST_DB_URL is not set; integration tests require a dedicated PostgreSQL test database")
	}

	t.Cleanup(func() {
		if err := resetIntegrationDatabase(); err != nil {
			t.Errorf("reset integration database: %v", err)
		}
	})

	return &TestContext{
		DB:     integrationDB,
		Router: app.SetupRouter(app.NewContainer(integrationDB, integrationConfig), integrationConfig),
	}
}

func ensureSeparateDatabaseURLs(applicationURL, testURL string) error {
	applicationConfig, err := pgx.ParseConfig(applicationURL)
	if err != nil {
		return fmt.Errorf("parse DB_URL: %w", err)
	}
	testConfig, err := pgx.ParseConfig(testURL)
	if err != nil {
		return fmt.Errorf("parse TEST_DB_URL: %w", err)
	}
	if strings.EqualFold(applicationConfig.Database, testConfig.Database) {
		return fmt.Errorf("TEST_DB_URL must use a different PostgreSQL database name than DB_URL")
	}
	return nil
}

func resetIntegrationDatabase() error {
	return integrationDB.Exec(`
		TRUNCATE TABLE
			task_assignees,
			task_tags,
			tasks,
			tags,
			task_statuses,
			lists,
			folders,
			spaces,
			workspace_members,
			role_permissions,
			roles,
			workspaces,
			user_contacts,
			user_sessions,
			users
		RESTART IDENTITY CASCADE
	`).Error
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
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("login response does not contain data object: %s", rec.Body.String())
	}
	token, ok := data["access_token"].(string)
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
