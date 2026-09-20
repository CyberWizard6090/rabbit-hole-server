package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

func TestContact_DuplicateAddIsRejected(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	secondEmail := "contact-target-" + suffix + "@example.test"
	secondUsername := "contact-target-" + suffix
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    secondEmail,
		"password": "test-password-123",
		"username": secondUsername,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register second user: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var otherUser domain.User
	if err := tc.DB.Where("email = ?", secondEmail).First(&otherUser).Error; err != nil {
		t.Fatalf("find second user: %v", err)
	}

	body := map[string]any{"contact_id": otherUser.ID}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/contacts/", tc.Token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first add: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/contacts/", tc.Token, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate add: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	errorBody := decodeJSON(t, rec)
	errorPayload, ok := errorBody["error"].(map[string]any)
	if !ok || errorPayload["code"] != "CONTACT_ALREADY_ADDED" {
		t.Fatalf("duplicate add: unexpected error payload: %s", rec.Body.String())
	}
}
