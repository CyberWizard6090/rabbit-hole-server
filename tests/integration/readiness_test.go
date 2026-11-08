package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/database"
)

func TestHealthEndpoints_LivenessIsCheapReadinessChecksTheDatabase(t *testing.T) {
	tc := newTestContext(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := request(t, tc.Router, http.MethodGet, path, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s with a healthy database: expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	db, err := database.ConnectDB(integrationConfig.DB.URL, database.Options{Environment: integrationConfig.Environment, MaxIdleConnections: 1, MaxOpenConnections: 1, ConnMaxLifetime: integrationConfig.DB.ConnMaxLifetime})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	container, err := app.NewContainer(db, integrationConfig)
	if err != nil {
		t.Fatal(err)
	}
	broken := app.SetupRouter(container, integrationConfig)
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		broken.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/healthz"); rec.Code != http.StatusOK {
		t.Errorf("liveness must not depend on the database: got %d", rec.Code)
	}
	rec := get("/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness with a dead database: expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); len(body) > 40 {
		t.Errorf("readiness body should not carry error details: %s", body)
	}
}
