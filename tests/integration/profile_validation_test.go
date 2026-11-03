package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestProfileUpdate_ValidatesFields(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	bad := map[string]map[string]any{
		"username too short":       {"username": "ab"},
		"username blank":           {"username": "   "},
		"username too long":        {"username": strings.Repeat("a", 31)},
		"time zone not IANA":       {"time_zone": "Mars/Olympus"},
		"time zone empty":          {"time_zone": ""},
		"time zone Local":          {"time_zone": "Local"},
		"first name too long":      {"first_name": strings.Repeat("a", 101)},
		"bio too long":             {"bio": strings.Repeat("a", 2001)},
		"time zone has wrong type": {"time_zone": 5},
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			rec := request(t, tc.Router, http.MethodPatch, "/api/v1/profile", tc.Token, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// Nothing from the rejected requests may have been stored.
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", tc.Token, nil)
	profile := dataObject(t, rec)
	if profile["time_zone"] != "UTC" || profile["bio"] != "" {
		t.Fatalf("rejected requests changed the profile: %v", profile)
	}

	good := map[string]any{"username": "new-name", "time_zone": "Europe/Berlin", "first_name": "", "bio": "hello"}
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/profile", tc.Token, good)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid update: %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	if data["username"] != "new-name" || data["time_zone"] != "Europe/Berlin" || data["bio"] != "hello" {
		t.Fatalf("profile = %v", data)
	}

	// An absent field is left alone; clearing optional text fields with "" stays possible.
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/profile", tc.Token, map[string]any{"bio": ""})
	if rec.Code != http.StatusOK || dataObject(t, rec)["time_zone"] != "Europe/Berlin" {
		t.Fatalf("partial update changed other fields: %d %s", rec.Code, rec.Body.String())
	}
}
