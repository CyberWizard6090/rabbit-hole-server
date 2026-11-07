package integration

import (
	"net/http"
	"testing"
)

func TestCustomFields_TaskValuesAreTypedAndWorkspaceScoped(t *testing.T) {
	tc := newTestContext(t)
	owner := newTenant(t, tc, "custom-fields")
	other := newTenant(t, tc, "other-fields")

	fieldURL := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/custom-fields"
	rec := request(t, tc.Router, http.MethodPost, fieldURL, owner.token, map[string]any{
		"name": "Priority", "type": "dropdown", "required": true,
		"options": []map[string]string{
			{"label": "High", "value": "high"},
			{"label": "Low", "value": "low"},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create custom field: %d: %s", rec.Code, rec.Body.String())
	}
	fieldID := responseID(t, rec)

	taskFieldsURL := "/api/v1/tasks/" + itoa(owner.taskID) + "/custom-fields"
	rec = request(t, tc.Router, http.MethodPut, taskFieldsURL+"/"+itoa(fieldID), owner.token, map[string]any{
		"value": "high",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set task custom field: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet, taskFieldsURL, owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task custom fields: %d: %s", rec.Code, rec.Body.String())
	}
	values := dataObject(t, rec)
	if values[itoa(fieldID)] != "high" {
		t.Fatalf("task custom field values = %v, want field %d = high", values, fieldID)
	}

	rec = request(t, tc.Router, http.MethodPut, taskFieldsURL+"/"+itoa(fieldID), owner.token, map[string]any{
		"value": "unknown",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid option: got %d: %s, want 400", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPut,
		"/api/v1/tasks/"+itoa(other.taskID)+"/custom-fields/"+itoa(fieldID),
		other.token, map[string]any{"value": "high"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace custom field: got %d: %s, want 404", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, fieldURL+"/"+itoa(fieldID), owner.token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete custom field with stored value: got %d: %s, want 409", rec.Code, rec.Body.String())
	}
}
