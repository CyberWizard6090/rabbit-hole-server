package integration

import (
	"net/http"
	"sort"
	"testing"
	"time"
)

func TestLogin_UnknownEmailCostsAsMuchAsWrongPassword(t *testing.T) {
	tc := newTestContext(t)
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": "timing@example.test", "password": "correct-password-1",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("register: %d: %s", rec.Code, rec.Body.String())
	}

	measure := func(email string) time.Duration {
		samples := make([]time.Duration, 0, 4)
		for i := 0; i < 4; i++ {
			start := time.Now()
			rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": "wrong-password-1"})
			samples = append(samples, time.Since(start))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("login %s: expected 401, got %d", email, rec.Code)
			}
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		return samples[len(samples)/2]
	}

	known, unknown := measure("timing@example.test"), measure("nobody@example.test")
	t.Logf("median wrong-password=%v unknown-email=%v", known, unknown)
	if unknown < known/3 {
		t.Fatalf("unknown e-mail answered in %v vs %v for a wrong password: timing leaks account existence", unknown, known)
	}
}
