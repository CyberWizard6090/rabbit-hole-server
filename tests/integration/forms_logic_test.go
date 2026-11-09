package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type logicFixture struct {
	formFixtureData
	gate   uint
	detail uint
	third  uint
}

func visibleRule(questionID uint, operator string, value any) map[string]any {
	rule := map[string]any{"question_id": questionID, "operator": operator}
	if value != nil {
		rule["value"] = value
	}
	return map[string]any{"visible_if": rule}
}

func (f formFixtureData) questionsURL() string {
	return f.formURL + "/" + itoa(f.formID) + "/questions"
}

func (f formFixtureData) addQuestion(t *testing.T, tc *TestContext, body map[string]any) *httpResult {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, f.questionsURL(), f.owner.token, body)
	return &httpResult{code: rec.Code, body: rec.Body.String(), id: func() uint {
		if rec.Code != http.StatusCreated {
			return 0
		}
		return responseID(t, rec)
	}()}
}

type httpResult struct {
	code int
	body string
	id   uint
}

func yesNo() []map[string]any {
	return []map[string]any{{"label": "Yes", "value": "yes"}, {"label": "No", "value": "no"}}
}

func newLogicFixture(t *testing.T, tc *TestContext, label string) logicFixture {
	t.Helper()
	f := buildFormFixture(t, tc, label, false)
	gate := f.addQuestion(t, tc, map[string]any{
		"type": "single_choice", "title": "Do you have an order?", "required": true, "position": 1, "options": yesNo(),
	})
	detail := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Order number", "required": true, "position": 2,
		"settings": visibleRule(gate.id, "equals", "yes"),
	})
	third := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Anything else about the order", "required": true, "position": 3,
		"settings": visibleRule(detail.id, "any", nil),
	})
	for name, created := range map[string]*httpResult{"gate": gate, "detail": detail, "third": third} {
		if created.code != http.StatusCreated {
			t.Fatalf("create %s: %d: %s", name, created.code, created.body)
		}
	}
	f.publish(t, tc)
	return logicFixture{formFixtureData: f, gate: gate.id, detail: detail.id, third: third.id}
}

func (f logicFixture) answers(gate, detail, third any) map[string]any {
	answers := f.plainAnswers()
	for id, value := range map[uint]any{f.gate: gate, f.detail: detail, f.third: third} {
		if value != nil {
			answers[itoa(id)] = value
		}
	}
	return answers
}

func storedQuestionIDs(t *testing.T, tc *TestContext, submissionID uint) map[uint]bool {
	t.Helper()
	var ids []uint
	if err := tc.DB.Raw("SELECT question_id FROM submission_answers WHERE submission_id = ?", submissionID).Scan(&ids).Error; err != nil {
		t.Fatal(err)
	}
	out := map[uint]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestFormLogic_HiddenQuestionsAreNotRequiredAndTheirAnswersAreDropped(t *testing.T) {
	tc := newTestContext(t)
	f := newLogicFixture(t, tc, "logicsubmit")

	rec := f.submitWith(t, tc, "k1", f.answers("no", nil, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("hidden required questions must not block the submission: %d: %s", rec.Code, rec.Body.String())
	}
	stored := storedQuestionIDs(t, tc, responseID(t, rec))
	if !stored[f.gate] || stored[f.detail] || stored[f.third] {
		t.Fatalf("stored answers = %v", stored)
	}

	rec = f.submitWith(t, tc, "k2", f.answers("no", "stray order number", "stray note"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("answers to hidden questions are dropped, not rejected: %d: %s", rec.Code, rec.Body.String())
	}
	submission := dataObject(t, rec)
	stored = storedQuestionIDs(t, tc, responseID(t, rec))
	if stored[f.detail] || stored[f.third] || strings.Contains(rec.Body.String(), "stray") {
		t.Fatalf("hidden answers leaked into storage or the response: %v %v", stored, submission["answers"])
	}

	rec = f.submitWith(t, tc, "k3", f.answers("no", 12345, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("even an invalid hidden answer is ignored: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormLogic_VisibleChainsAreRequiredStepByStep(t *testing.T) {
	tc := newTestContext(t)
	f := newLogicFixture(t, tc, "logicchain")

	if rec := f.submitWith(t, tc, "c1", f.answers("yes", nil, nil)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a visible required question must be answered: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := f.submitWith(t, tc, "c2", f.answers("yes", "A-17", nil)); rec.Code != http.StatusBadRequest {
		t.Fatalf("the third question became visible with the second answer: %d: %s", rec.Code, rec.Body.String())
	}
	rec := f.submitWith(t, tc, "c3", f.answers("yes", "A-17", "please hurry"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("complete chain: %d: %s", rec.Code, rec.Body.String())
	}
	stored := storedQuestionIDs(t, tc, responseID(t, rec))
	if !stored[f.gate] || !stored[f.detail] || !stored[f.third] {
		t.Fatalf("stored answers = %v", stored)
	}
	if rec = f.submitWith(t, tc, "c4", f.answers("maybe", nil, nil)); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown option is still rejected: %d", rec.Code)
	}
}

func TestFormLogic_SupportedOperatorsDriveVisibility(t *testing.T) {
	tc := newTestContext(t)
	f := buildFormFixture(t, tc, "logicops", false)
	multi := f.addQuestion(t, tc, map[string]any{
		"type": "multiple_choice", "title": "Channels", "position": 1,
		"options": []map[string]any{{"label": "Mail", "value": "mail"}, {"label": "Phone", "value": "phone"}},
	})
	notes := f.addQuestion(t, tc, map[string]any{"type": "long_text", "title": "Notes", "position": 2})
	phone := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Phone number", "required": true, "position": 3,
		"settings": visibleRule(multi.id, "contains", "phone"),
	})
	urgent := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Why urgent", "required": true, "position": 4,
		"settings": visibleRule(notes.id, "contains", "URGENT"),
	})
	notBy := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Not by mail only", "required": true, "position": 5,
		"settings": visibleRule(multi.id, "not_equals", "mail"),
	})
	for _, created := range []*httpResult{multi, notes, phone, urgent, notBy} {
		if created.code != http.StatusCreated {
			t.Fatalf("create question: %d: %s", created.code, created.body)
		}
	}
	f.publish(t, tc)
	with := func(extra map[uint]any) map[string]any {
		answers := f.plainAnswers()
		for id, value := range extra {
			answers[itoa(id)] = value
		}
		return answers
	}
	cases := []struct {
		name    string
		answers map[string]any
		want    int
	}{
		{"nothing selected shows only the not_equals follow-up", with(map[uint]any{}), http.StatusBadRequest},
		{"mail only hides everything", with(map[uint]any{multi.id: []string{"mail"}}), http.StatusCreated},
		{"phone needs the number and the not_equals follow-up", with(map[uint]any{multi.id: []string{"phone"}}), http.StatusBadRequest},
		{"phone with both answers", with(map[uint]any{multi.id: []string{"phone"}, phone.id: "123", notBy.id: "x"}), http.StatusCreated},
		{"substring match ignores case", with(map[uint]any{multi.id: []string{"mail"}, notes.id: "this is Urgent!"}), http.StatusBadRequest},
		{"substring match answered", with(map[uint]any{multi.id: []string{"mail"}, notes.id: "this is Urgent!", urgent.id: "deadline"}), http.StatusCreated},
	}
	for i, tc2 := range cases {
		if rec := f.submitWith(t, tc, "ops-"+string(rune('a'+i)), tc2.answers); rec.Code != tc2.want {
			t.Errorf("%s: %d (want %d): %s", tc2.name, rec.Code, tc2.want, rec.Body.String())
		}
	}
}

func TestFormLogic_RejectsInvalidRulesWhenSavingQuestions(t *testing.T) {
	tc := newTestContext(t)
	f := newLogicFixture(t, tc, "logicbad")
	other := buildFormFixture(t, tc, "logicbad2", false)
	foreign := other.addQuestion(t, tc, map[string]any{"type": "short_text", "title": "Elsewhere", "position": 1})

	invalid := []struct {
		name     string
		settings map[string]any
		code     string
	}{
		{"unknown question", visibleRule(99999, "any", nil), "FORM_QUESTION_VISIBILITY_INVALID"},
		{"question of another form", visibleRule(foreign.id, "any", nil), "FORM_QUESTION_VISIBILITY_INVALID"},
		{"unknown operator", visibleRule(f.gate, "starts_with", "y"), "FORM_QUESTION_VISIBILITY_INVALID"},
		{"missing value", visibleRule(f.gate, "equals", nil), "FORM_QUESTION_VISIBILITY_INVALID"},
		{"value that is not an option", visibleRule(f.gate, "equals", "maybe"), "FORM_QUESTION_VISIBILITY_INVALID"},
		{"object value", visibleRule(f.gate, "equals", map[string]any{"a": 1}), "FORM_QUESTION_VISIBILITY_INVALID"},
		{"rule without a question", map[string]any{"visible_if": map[string]any{"operator": "any"}}, "FORM_QUESTION_VISIBILITY_INVALID"},
		{"rule that is not an object", map[string]any{"visible_if": "yes"}, "FORM_QUESTION_VISIBILITY_INVALID"},
	}
	for _, tc2 := range invalid {
		created := f.addQuestion(t, tc, map[string]any{"type": "short_text", "title": "Bad", "position": 9, "settings": tc2.settings})
		if created.code != http.StatusBadRequest || !strings.Contains(created.body, tc2.code) {
			t.Errorf("%s: %d: %s", tc2.name, created.code, created.body)
		}
	}

	later := f.addQuestion(t, tc, map[string]any{"type": "short_text", "title": "Later", "position": 20})
	earlier := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Depends on a later question", "position": 10, "settings": visibleRule(later.id, "any", nil),
	})
	if earlier.code != http.StatusBadRequest || !strings.Contains(earlier.body, "FORM_QUESTION_VISIBILITY_INVALID") {
		t.Errorf("a rule that points forward: %d: %s", earlier.code, earlier.body)
	}
	section := f.addQuestion(t, tc, map[string]any{"type": "section", "title": "Section", "position": 0})
	onSection := f.addQuestion(t, tc, map[string]any{
		"type": "short_text", "title": "Depends on a section", "position": 30, "settings": visibleRule(section.id, "any", nil),
	})
	if onSection.code != http.StatusBadRequest {
		t.Errorf("a static question cannot be a source: %d: %s", onSection.code, onSection.body)
	}
}

func TestFormLogic_RejectsSelfReferencesAndCycles(t *testing.T) {
	tc := newTestContext(t)
	f := newLogicFixture(t, tc, "logiccycle")
	questionURL := func(id uint) string { return f.questionsURL() + "/" + itoa(id) }
	update := func(id uint, body map[string]any) (int, string) {
		rec := request(t, tc.Router, http.MethodPatch, questionURL(id), f.owner.token, body)
		return rec.Code, rec.Body.String()
	}

	code, body := update(f.detail, map[string]any{
		"type": "short_text", "title": "Order number", "required": true, "position": 2, "settings": visibleRule(f.detail, "any", nil),
	})
	if code != http.StatusBadRequest || !strings.Contains(body, "FORM_QUESTION_VISIBILITY_CYCLE") {
		t.Fatalf("self reference: %d: %s", code, body)
	}
	code, body = update(f.gate, map[string]any{
		"type": "single_choice", "title": "Do you have an order?", "required": true, "position": 1, "options": yesNo(),
		"settings": visibleRule(f.third, "any", nil),
	})
	if code != http.StatusBadRequest || !strings.Contains(body, "FORM_QUESTION_VISIBILITY_CYCLE") {
		t.Fatalf("cycle through a chain: %d: %s", code, body)
	}
	code, body = update(f.gate, map[string]any{
		"type": "single_choice", "title": "Do you have an order?", "required": true, "position": 9, "options": yesNo(),
	})
	if code != http.StatusBadRequest || !strings.Contains(body, "FORM_QUESTION_VISIBILITY_INVALID") {
		t.Fatalf("moving a source behind its dependants: %d: %s", code, body)
	}
	code, body = update(f.detail, map[string]any{
		"type": "short_text", "title": "Order number renamed", "required": true, "position": 2, "settings": visibleRule(f.gate, "equals", "yes"),
	})
	if code != http.StatusOK {
		t.Fatalf("an unrelated valid update: %d: %s", code, body)
	}
	code, body = update(f.detail, map[string]any{
		"type": "short_text", "title": "Order number", "required": true, "position": 2, "settings": map[string]any{"visible_if": nil},
	})
	if code != http.StatusOK {
		t.Fatalf("clearing a rule: %d: %s", code, body)
	}

	var allowed int64
	_ = tc.DB.Raw("SELECT count(*) FROM form_questions WHERE form_id = ? AND settings::text LIKE '%visible_if%'", f.formID).Scan(&allowed).Error
	if allowed != 2 {
		t.Fatalf("rejected rules must not be stored: %d questions with rules", allowed)
	}
}

func TestFormLogic_ReferencedQuestionsCannotBeDeletedAndReorderCannotBreakTheOrder(t *testing.T) {
	tc := newTestContext(t)
	f := newLogicFixture(t, tc, "logicdelete")

	rec := request(t, tc.Router, http.MethodDelete, f.questionsURL()+"/"+itoa(f.gate), f.owner.token, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "FORM_QUESTION_REFERENCED" {
		t.Fatalf("delete a referenced question: %d: %s", rec.Code, rec.Body.String())
	}
	if rec = request(t, tc.Router, http.MethodDelete, f.questionsURL()+"/"+itoa(f.third), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete an unreferenced question: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPut, f.questionsURL()+"/reorder", f.owner.token,
		map[string]any{"ids": []uint{f.detail, f.gate, f.questionID}})
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_QUESTION_VISIBILITY_INVALID" {
		t.Fatalf("a reorder that puts a rule before its source: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPut, f.questionsURL()+"/reorder", f.owner.token,
		map[string]any{"ids": []uint{f.questionID, f.gate, f.detail}})
	if rec.Code != http.StatusOK {
		t.Fatalf("a reorder that keeps every rule valid: %d: %s", rec.Code, rec.Body.String())
	}
	if rec = request(t, tc.Router, http.MethodPost, f.formURL+"/"+itoa(f.formID)+"/publish", f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("publishing after the reorder: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormLogic_PublishedVersionIsStableWhenTheLiveQuestionChanges(t *testing.T) {
	tc := newTestContext(t)
	f := newLogicFixture(t, tc, "logicsnap")

	_, public, _ := f.publicForm(t, tc)
	ruleOf := func(public map[string]any, id uint) any {
		for _, raw := range public["questions"].([]any) {
			question := raw.(map[string]any)
			if uint(question["id"].(float64)) == id {
				if settings, ok := question["settings"].(map[string]any); ok {
					return settings["visible_if"]
				}
			}
		}
		return "missing"
	}
	before, _ := json.Marshal(ruleOf(public, f.detail))
	if !strings.Contains(string(before), `"equals"`) {
		t.Fatalf("the public schema carries the rule for the client to evaluate: %s", before)
	}

	rec := request(t, tc.Router, http.MethodPatch, f.questionsURL()+"/"+itoa(f.detail), f.owner.token, map[string]any{
		"type": "short_text", "title": "Order number", "required": true, "position": 2, "settings": map[string]any{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("make the live question unconditional: %d: %s", rec.Code, rec.Body.String())
	}
	_, public, _ = f.publicForm(t, tc)
	after, _ := json.Marshal(ruleOf(public, f.detail))
	if string(after) != string(before) {
		t.Fatalf("the published rule changed with the live question: %s -> %s", before, after)
	}
	if rec := f.submitWith(t, tc, "snap-1", f.answers("no", nil, nil)); rec.Code != http.StatusCreated {
		t.Fatalf("the published version still hides the question: %d: %s", rec.Code, rec.Body.String())
	}

	f.publish(t, tc)
	if rec := f.submitWith(t, tc, "snap-2", f.answers("no", nil, nil)); rec.Code != http.StatusBadRequest {
		t.Fatalf("the republished question is always visible and required: %d: %s", rec.Code, rec.Body.String())
	}
	_, public, _ = f.publicForm(t, tc)
	if ruleOf(public, f.detail) != nil {
		t.Fatalf("the new version has no rule: %v", ruleOf(public, f.detail))
	}
}
