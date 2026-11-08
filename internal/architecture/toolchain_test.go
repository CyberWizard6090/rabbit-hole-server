package architecture

import (
	"os"
	"regexp"
	"testing"
)

func firstMatch(t *testing.T, path, pattern string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	match := regexp.MustCompile(pattern).FindSubmatch(raw)
	if match == nil {
		t.Fatalf("%s has nothing matching %s", path, pattern)
	}
	return string(match[1])
}

func TestGoVersionComesFromGoMod(t *testing.T) {
	want := firstMatch(t, "../../go.mod", `(?m)^go (\S+)$`)

	if got := firstMatch(t, "../../Dockerfile", `(?m)^ARG GO_VERSION=(\S+)$`); got != want {
		t.Errorf("Dockerfile defaults GO_VERSION to %s but go.mod says %s", got, want)
	}
	if got := firstMatch(t, "../../.github/workflows/go.yml", `(?m)^\s+GO_VERSION: "?([^"\s]+)"?$`); got != want {
		t.Errorf("go.yml GO_VERSION is %s but go.mod says %s", got, want)
	}
}

func TestDockerImageRunsAsNonRootWithAHealthcheck(t *testing.T) {
	raw, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{`(?m)^USER app$`, `(?m)^HEALTHCHECK `} {
		if !regexp.MustCompile(pattern).Match(raw) {
			t.Errorf("Dockerfile lacks %s", pattern)
		}
	}
}

func TestComposeGivesTheServerTimeToShutDown(t *testing.T) {
	raw, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s+stop_grace_period: \d+s$`).Match(raw) {
		t.Error("docker-compose.yml must set stop_grace_period for the app")
	}
	prod, err := os.ReadFile("../../docker-compose.prod.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`APP_ENV: production`).Match(prod) {
		t.Error("docker-compose.prod.yml must set APP_ENV=production")
	}
	if regexp.MustCompile(`TRUSTED_PROXIES:.*172\.16`).Match(raw) {
		t.Error("docker-compose.yml must not trust the whole docker network by default")
	}
}
