package integration

import (
	"net/http"
	"strings"
	"testing"
)

type validationForm struct {
	env     fcEnv
	formURL string
	slug    string
	ids     map[string]uint
}

func newValidationForm(t *testing.T, tc *TestContext, label string) validationForm {
	t.Helper()
	env := newFCEnv(t, tc, label)
	_, formURL := env.newForm(t, tc, "Validation")
	abc := []map[string]any{{"label": "A", "value": "a"}, {"label": "B", "value": "b"}, {"label": "C", "value": "c"}}
	specs := []struct {
		key  string
		body map[string]any
	}{
		{"required", map[string]any{"type": "short_text", "title": "Required", "required": true}},
		{"text", map[string]any{"type": "short_text", "title": "Text", "settings": map[string]any{"min_length": 2, "max_length": 5}}},
		{"number", map[string]any{"type": "number", "title": "Number", "settings": map[string]any{"min": 1, "max": 5}}},
		{"email", map[string]any{"type": "email", "title": "Email"}},
		{"url", map[string]any{"type": "url", "title": "Url"}},
		{"date", map[string]any{"type": "date", "title": "Date"}},
		{"choice", map[string]any{"type": "single_choice", "title": "Choice", "options": abc}},
		{"multi", map[string]any{"type": "multiple_choice", "title": "Multi", "options": abc, "settings": map[string]any{"min_selected": 2, "max_selected": 2}}},
		{"rating", map[string]any{"type": "rating", "title": "Rating"}},
		{"check", map[string]any{"type": "checkbox", "title": "Check"}},
		{"section", map[string]any{"type": "section", "title": "Section"}},
		{"file", map[string]any{"type": "file", "title": "File"}},
	}
	ids := make(map[string]uint, len(specs))
	for _, spec := range specs {
		ids[spec.key] = mustCreate(t, tc, env.owner.token, formURL+"/questions", spec.body)
	}
	publishForm(t, tc, env, formURL)
	return validationForm{env: env, formURL: formURL, slug: formSlugOf(t, tc, env, formURL), ids: ids}
}

func (f validationForm) field(key string) string { return "answers." + itoa(f.ids[key]) }

func TestPublicSubmission_ReportsEveryInvalidAnswerWithDetails(t *testing.T) {
	tc := newTestContext(t)
	f := newValidationForm(t, tc, "subdetails")
	answers := map[string]any{
		itoa(f.ids["text"]):    "far too long",
		itoa(f.ids["number"]):  9,
		itoa(f.ids["email"]):   "nope",
		itoa(f.ids["url"]):     5,
		itoa(f.ids["date"]):    "2020-13-45",
		itoa(f.ids["choice"]):  "z",
		itoa(f.ids["multi"]):   []string{"a"},
		itoa(f.ids["rating"]):  9,
		itoa(f.ids["check"]):   "yes",
		itoa(f.ids["section"]): "text",
		itoa(f.ids["file"]):    []string{"not a file id"},
		"987654":               "x",
	}
	rec := fcSubmit(t, tc, f.slug, "details-1", answers)
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM", "")
	body := fcErrorBody(t, rec)

	expectDetail(t, body, f.field("required"), "REQUIRED")
	if d := expectDetail(t, body, f.field("text"), "TOO_LONG"); d["params"].(map[string]any)["max"] != float64(5) {
		t.Fatalf("text detail = %v", d)
	}
	if d := expectDetail(t, body, f.field("number"), "OUT_OF_RANGE"); d["params"].(map[string]any)["min"] != float64(1) ||
		d["params"].(map[string]any)["max"] != float64(5) {
		t.Fatalf("number detail = %v", d)
	}
	expectDetail(t, body, f.field("email"), "INVALID_FORMAT")
	if d := expectDetail(t, body, f.field("url"), "INVALID_TYPE"); d["params"].(map[string]any)["expected"] != "string" {
		t.Fatalf("url detail = %v", d)
	}
	expectDetail(t, body, f.field("date"), "INVALID_FORMAT")
	choice := expectDetail(t, body, f.field("choice"), "INVALID_CHOICE")
	if allowed, _ := choice["params"].(map[string]any)["allowed"].([]any); len(allowed) != 3 {
		t.Fatalf("choice detail = %v", choice)
	}
	if d := expectDetail(t, body, f.field("multi"), "TOO_FEW"); d["params"].(map[string]any)["min"] != float64(2) {
		t.Fatalf("multi detail = %v", d)
	}
	expectDetail(t, body, f.field("rating"), "OUT_OF_RANGE")
	if d := expectDetail(t, body, f.field("check"), "INVALID_TYPE"); d["params"].(map[string]any)["expected"] != "boolean" {
		t.Fatalf("check detail = %v", d)
	}
	expectDetail(t, body, f.field("section"), "NOT_ALLOWED")
	expectDetail(t, body, f.field("file"), "INVALID_FILE")
	expectDetail(t, body, "answers.987654", "UNKNOWN_QUESTION")
	if details := body["details"].([]any); len(details) != 13 {
		t.Fatalf("details = %v", details)
	}
}

func TestPublicSubmission_DetailCodesForShortTooManyAndEmptyValues(t *testing.T) {
	tc := newTestContext(t)
	f := newValidationForm(t, tc, "subdetails2")
	valid := func() map[string]any {
		return map[string]any{itoa(f.ids["required"]): "ok"}
	}
	answers := valid()
	answers[itoa(f.ids["text"])] = "x"
	answers[itoa(f.ids["multi"])] = []string{"a", "b", "c"}
	answers[itoa(f.ids["email"])] = ""
	answers[itoa(f.ids["number"])] = "5"
	rec := fcSubmit(t, tc, f.slug, "details-2", answers)
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM", "")
	body := fcErrorBody(t, rec)
	if d := expectDetail(t, body, f.field("text"), "TOO_SHORT"); d["params"].(map[string]any)["min"] != float64(2) {
		t.Fatalf("text detail = %v", d)
	}
	if d := expectDetail(t, body, f.field("multi"), "TOO_MANY"); d["params"].(map[string]any)["max"] != float64(2) {
		t.Fatalf("multi detail = %v", d)
	}
	expectDetail(t, body, f.field("email"), "EMPTY")
	expectDetail(t, body, f.field("number"), "INVALID_TYPE")
	if rec := fcSubmit(t, tc, f.slug, "details-3", valid()); rec.Code != http.StatusCreated {
		t.Fatalf("valid submission: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPublicSubmission_UnknownUploadIsReportedPerQuestion(t *testing.T) {
	tc := newTestContext(t)
	f := newValidationForm(t, tc, "subupload")
	answers := map[string]any{
		itoa(f.ids["required"]): "ok",
		itoa(f.ids["file"]):     []string{"0123456789abcdef0123456789abcdef"},
	}
	rec := fcSubmit(t, tc, f.slug, "upload-1", answers)
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_UPLOAD_INVALID", f.field("file"))
	expectDetail(t, fcErrorBody(t, rec), f.field("file"), "INVALID_FILE")
}

func TestPublicSubmission_ResponseIsMinimalForRespondents(t *testing.T) {
	tc := newTestContext(t)
	f := newValidationForm(t, tc, "subminimal")
	rec := request(t, tc.Router, http.MethodPatch, f.formURL, f.env.owner.token, map[string]any{
		"name": "Validation", "slug": f.slug,
		"settings": map[string]any{"success_message": "Thanks!", "redirect_url": "https://example.test/thanks"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("settings: %d: %s", rec.Code, rec.Body.String())
	}
	publishForm(t, tc, f.env, f.formURL)
	answers := map[string]any{itoa(f.ids["required"]): "ok"}

	anonymous := fcSubmit(t, tc, f.slug, "minimal-1", answers)
	if anonymous.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", anonymous.Code, anonymous.Body.String())
	}
	data := dataObject(t, anonymous)
	for _, leaked := range []string{"workspace_id", "form_version_id", "respondent_id", "form_id", "answers", "files", "task_creation_error"} {
		if _, found := data[leaked]; found {
			t.Fatalf("the respondent sees %q: %s", leaked, anonymous.Body.String())
		}
	}
	if data["id"] == nil || data["status"] != "submitted" || data["created_at"] == nil ||
		data["success_message"] != "Thanks!" || data["redirect_url"] != "https://example.test/thanks" {
		t.Fatalf("minimal object = %v", data)
	}

	replay := fcSubmit(t, tc, f.slug, "minimal-1", answers)
	if replay.Code != http.StatusOK || strings.Contains(replay.Body.String(), "workspace_id") {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}

	member := requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/"+f.slug+"/submissions", f.env.owner.token,
		"Idempotency-Key", "minimal-2", map[string]any{"answers": answers})
	if member.Code != http.StatusCreated || strings.Contains(member.Body.String(), "respondent_id") {
		t.Fatalf("a signed-in respondent gets the same minimal object: %d %s", member.Code, member.Body.String())
	}

	full := request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(f.env.owner.workspaceID)+"/submissions/"+itoa(responseID(t, anonymous)), f.env.owner.token, nil)
	if full.Code != http.StatusOK {
		t.Fatalf("workspace members keep the full object: %d: %s", full.Code, full.Body.String())
	}
	fullData := dataObject(t, full)
	if fullData["workspace_id"] != float64(f.env.owner.workspaceID) || fullData["form_version_id"] == nil || fullData["answers"] == nil {
		t.Fatalf("full object = %v", fullData)
	}
	listed := request(t, tc.Router, http.MethodGet, f.formURL+"/submissions", f.env.owner.token, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "form_version_id") ||
		!strings.Contains(listed.Body.String(), "respondent_id") {
		t.Fatalf("submission list: %d %s", listed.Code, listed.Body.String())
	}
}

func TestPublicForm_SchemaHidesOptionIdsAndAcceptsOptionValues(t *testing.T) {
	tc := newTestContext(t)
	f := newValidationForm(t, tc, "pubschema")
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/public/forms/"+f.slug, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public form: %d", rec.Code)
	}
	questions := dataObject(t, rec)["questions"].([]any)
	seenOptions := 0
	for _, raw := range questions {
		question := raw.(map[string]any)
		options, _ := question["options"].([]any)
		for _, rawOption := range options {
			option := rawOption.(map[string]any)
			seenOptions++
			if _, hasID := option["id"]; hasID {
				t.Fatalf("the public schema exposes an option id: %v", option)
			}
			if option["value"] == nil || option["label"] == nil || option["position"] == nil {
				t.Fatalf("option = %v", option)
			}
		}
	}
	if seenOptions == 0 {
		t.Fatal("the fixture has no options")
	}
	owner := request(t, tc.Router, http.MethodGet, f.formURL, f.env.owner.token, nil)
	if !strings.Contains(owner.Body.String(), `"options":[{"id":`) {
		t.Fatalf("the owner schema keeps option ids: %s", owner.Body.String())
	}
	answers := map[string]any{itoa(f.ids["required"]): "ok", itoa(f.ids["choice"]): "b"}
	if rec := fcSubmit(t, tc, f.slug, "values-1", answers); rec.Code != http.StatusCreated {
		t.Fatalf("submit by option value: %d: %s", rec.Code, rec.Body.String())
	}
}
