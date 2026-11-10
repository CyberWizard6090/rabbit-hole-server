package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func detailOf(t *testing.T, body map[string]any, field string) map[string]any {
	t.Helper()
	detail := errorDetail(body, field)
	if detail == nil {
		t.Fatalf("error %v has no detail for %q", body, field)
	}
	return detail
}

func expectDetail(t *testing.T, body map[string]any, field, code string) map[string]any {
	t.Helper()
	detail := detailOf(t, body, field)
	if detail["code"] != code {
		t.Fatalf("detail for %q = %v, want code %s (error %v)", field, detail, code, body)
	}
	return detail
}

func publishForm(t *testing.T, tc *TestContext, env fcEnv, formURL string) {
	t.Helper()
	if rec := request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormQuestions_ConditionalQuestionCanBeAddedWithoutPosition(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "ruleappend")
	_, formURL := env.newForm(t, tc, "Conditional")
	gate := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "single_choice", "title": "Gate", "options": yesNo(),
	})
	rec := request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, map[string]any{
		"type": "short_text", "title": "Follow-up", "settings": visibleRule(gate, "equals", "yes"),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("append a conditional question: %d: %s", rec.Code, rec.Body.String())
	}
	if position := dataObject(t, rec)["position"]; position != float64(1) {
		t.Fatalf("position = %v, want 1", position)
	}
	rec = request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, map[string]any{
		"type": "short_text", "title": "Still invalid", "position": 0, "settings": visibleRule(gate, "equals", "yes"),
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_VISIBILITY_INVALID", "settings.visible_if.question_id")
	rec = request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, map[string]any{
		"type": "short_text", "title": "Refers to nothing", "settings": visibleRule(99999999, "any", nil),
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_VISIBILITY_INVALID", "settings.visible_if.question_id")
}

func TestFormQuestions_ReorderRequiresEveryLiveQuestion(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "reorderall")
	_, formURL := env.newForm(t, tc, "Reorder")
	a, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "A", nil)
	b, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "B", nil)
	c, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "C", nil)
	reorder := func(ids ...uint) *httpResult {
		rec := request(t, tc.Router, http.MethodPut, formURL+"/questions/reorder", env.owner.token, map[string]any{"ids": ids})
		return &httpResult{code: rec.Code, body: rec.Body.String()}
	}
	titles := func() string {
		names, _ := fcOrder(fcQuestions(t, tc, env.owner.token, formURL))
		return strings.Join(names, ",")
	}

	partial := request(t, tc.Router, http.MethodPut, formURL+"/questions/reorder", env.owner.token, map[string]any{"ids": []uint{c, a}})
	fcExpectError(t, partial, http.StatusBadRequest, "FORM_QUESTION_REORDER_INVALID", "ids")
	missing := detailOf(t, fcErrorBody(t, partial), "ids")
	if missing["code"] != "MISSING" || missing["params"].(map[string]any)["question_id"] != float64(b) {
		t.Fatalf("missing detail = %v", missing)
	}
	if got := titles(); got != "A,B,C" {
		t.Fatalf("a rejected reorder changed the order: %s", got)
	}

	mixed := request(t, tc.Router, http.MethodPut, formURL+"/questions/reorder", env.owner.token,
		map[string]any{"ids": []uint{c, a, a, 987654, b}})
	fcExpectError(t, mixed, http.StatusBadRequest, "FORM_QUESTION_REORDER_INVALID", "ids[2]")
	body := fcErrorBody(t, mixed)
	expectDetail(t, body, "ids[2]", "DUPLICATE")
	unknown := expectDetail(t, body, "ids[3]", "UNKNOWN_QUESTION")
	if unknown["params"].(map[string]any)["question_id"] != float64(987654) {
		t.Fatalf("unknown detail = %v", unknown)
	}

	if res := reorder(c, a, b); res.code != http.StatusOK {
		t.Fatalf("full reorder: %d: %s", res.code, res.body)
	}
	if got := titles(); got != "C,A,B" {
		t.Fatalf("order = %s", got)
	}
	if res := request(t, tc.Router, http.MethodDelete, formURL+"/questions/"+itoa(b), env.owner.token, nil); res.Code != http.StatusOK {
		t.Fatalf("delete: %d", res.Code)
	}
	if res := reorder(a, b, c); res.code != http.StatusBadRequest || !strings.Contains(res.body, "UNKNOWN_QUESTION") {
		t.Fatalf("a deleted question id must be unknown: %d %s", res.code, res.body)
	}
	if res := reorder(a, c); res.code != http.StatusOK {
		t.Fatalf("reorder of the live questions: %d: %s", res.code, res.body)
	}
}

func TestFormQuestions_ReorderCannotBreakVisibilityRules(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "reorderrule")
	_, formURL := env.newForm(t, tc, "Reorder rules")
	gate := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "single_choice", "title": "Gate", "options": yesNo(),
	})
	detail := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "short_text", "title": "Detail", "settings": visibleRule(gate, "equals", "yes"),
	})
	other := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{"type": "short_text", "title": "Other"})

	rec := request(t, tc.Router, http.MethodPut, formURL+"/questions/reorder", env.owner.token,
		map[string]any{"ids": []uint{detail, gate, other}})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_VISIBILITY_INVALID", "ids")
	d := expectDetail(t, fcErrorBody(t, rec), "ids", "RULE_NOT_EARLIER")
	params := d["params"].(map[string]any)
	if params["question_id"] != float64(detail) || params["depends_on"] != float64(gate) {
		t.Fatalf("params = %v", params)
	}
	if names, _ := fcOrder(fcQuestions(t, tc, env.owner.token, formURL)); strings.Join(names, ",") != "Gate,Detail,Other" {
		t.Fatalf("a rejected reorder changed the order: %v", names)
	}
	if rec := request(t, tc.Router, http.MethodPut, formURL+"/questions/reorder", env.owner.token,
		map[string]any{"ids": []uint{other, gate, detail}}); rec.Code != http.StatusOK {
		t.Fatalf("valid reorder: %d: %s", rec.Code, rec.Body.String())
	}
}

func answeredChoiceForm(t *testing.T, tc *TestContext, env fcEnv) (string, string, uint, uint) {
	t.Helper()
	formID, formURL := env.newForm(t, tc, "Answered")
	_ = formID
	choice := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "single_choice", "title": "Color", "required": true,
		"options": []map[string]any{{"label": "Red", "value": "red"}, {"label": "Blue", "value": "blue"}},
	})
	text := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "short_text", "title": "Name", "required": true,
	})
	publishForm(t, tc, env, formURL)
	var slug string
	form := request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil)
	slug, _ = dataObject(t, form)["slug"].(string)
	rec := fcSubmit(t, tc, slug, "answered-1", map[string]any{itoa(choice): "red", itoa(text): "Ann"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	return formURL, slug, choice, text
}

func TestFormQuestions_AnsweredQuestionKeepsItsTypeAndUsedOptions(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "answeredq")
	formURL, _, choice, text := answeredChoiceForm(t, tc, env)
	questionURL := func(id uint) string { return formURL + "/questions/" + itoa(id) }
	patch := func(id uint, body map[string]any) *httpResult {
		rec := request(t, tc.Router, http.MethodPatch, questionURL(id), env.owner.token, body)
		return &httpResult{code: rec.Code, body: rec.Body.String()}
	}

	res := patch(text, map[string]any{"type": "long_text", "title": "Name", "required": true})
	if res.code != http.StatusConflict || !strings.Contains(res.body, "FORM_QUESTION_IN_USE") || !strings.Contains(res.body, `"field":"type"`) ||
		!strings.Contains(res.body, "HAS_ANSWERS") {
		t.Fatalf("type change of an answered question: %d %s", res.code, res.body)
	}
	res = patch(choice, map[string]any{
		"type": "single_choice", "title": "Color", "required": true,
		"options": []map[string]any{{"label": "Blue", "value": "blue"}},
	})
	if res.code != http.StatusConflict || !strings.Contains(res.body, "OPTION_IN_USE") || !strings.Contains(res.body, `"value":"red"`) {
		t.Fatalf("removing a used option: %d %s", res.code, res.body)
	}
	res = patch(choice, map[string]any{
		"type": "single_choice", "title": "Color", "required": true,
		"options": []map[string]any{{"label": "Crimson", "value": "crimson"}, {"label": "Blue", "value": "blue"}},
	})
	if res.code != http.StatusConflict || !strings.Contains(res.body, "OPTION_IN_USE") {
		t.Fatalf("renaming the value of a used option: %d %s", res.code, res.body)
	}

	allowed := []map[string]any{
		{"type": "short_text", "title": "Your name", "description": "Full name", "required": true, "settings": map[string]any{"placeholder": "Ann"}},
	}
	for _, body := range allowed {
		if res := patch(text, body); res.code != http.StatusOK {
			t.Fatalf("title and placeholder edits stay allowed: %d %s", res.code, res.body)
		}
	}
	res = patch(choice, map[string]any{
		"type": "single_choice", "title": "Favorite color", "required": false,
		"options": []map[string]any{{"label": "Dark red", "value": "red"}, {"label": "Blue", "value": "blue"}, {"label": "Green", "value": "green"}},
	})
	if res.code != http.StatusOK {
		t.Fatalf("relabelling and adding options stays allowed: %d %s", res.code, res.body)
	}
	res = patch(choice, map[string]any{
		"type": "single_choice", "title": "Favorite color",
		"options": []map[string]any{{"label": "Red", "value": "red"}, {"label": "Blue", "value": "blue"}},
	})
	if res.code != http.StatusOK {
		t.Fatalf("removing an option no answer uses stays allowed: %d %s", res.code, res.body)
	}
}

func TestFormQuestions_UnansweredQuestionOfPublishedFormCanChangeType(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "unansweredq")
	_, formURL := env.newForm(t, tc, "Unanswered")
	id := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{"type": "short_text", "title": "Name"})
	publishForm(t, tc, env, formURL)
	rec := request(t, tc.Router, http.MethodPatch, formURL+"/questions/"+itoa(id), env.owner.token,
		map[string]any{"type": "long_text", "title": "Name"})
	if rec.Code != http.StatusOK {
		t.Fatalf("a question without answers may change type: %d: %s", rec.Code, rec.Body.String())
	}
}

func formSlugOf(t *testing.T, tc *TestContext, env fcEnv, formURL string) string {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil)
	slug, _ := dataObject(t, rec)["slug"].(string)
	return slug
}

func renameForm(t *testing.T, tc *TestContext, env fcEnv, formURL, slug string) *httpResult {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPatch, formURL, env.owner.token, map[string]any{"name": "Renamed", "slug": slug})
	return &httpResult{code: rec.Code, body: rec.Body.String()}
}

func publicStatus(t *testing.T, tc *TestContext, slug string) (int, map[string]any) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/public/forms/"+slug, "", nil)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	return rec.Code, dataObject(t, rec)
}

func TestFormSlug_PreviousSlugKeepsResolvingAfterRename(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "slugalias")
	formID, formURL := env.newForm(t, tc, "Aliased")
	question := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{"type": "short_text", "title": "Name", "required": true})
	publishForm(t, tc, env, formURL)
	oldSlug := formSlugOf(t, tc, env, formURL)
	newSlug := env.newSlug("renamed")

	if res := renameForm(t, tc, env, formURL, newSlug); res.code != http.StatusOK {
		t.Fatalf("rename: %d %s", res.code, res.body)
	}
	for _, slug := range []string{oldSlug, strings.ToUpper(oldSlug), newSlug} {
		code, data := publicStatus(t, tc, slug)
		if code != http.StatusOK || data["slug"] != newSlug {
			t.Fatalf("GET %s = %d %v, want the form with slug %s", slug, code, data, newSlug)
		}
	}
	if rec := fcSubmit(t, tc, oldSlug, "alias-1", map[string]any{itoa(question): "Via the old link"}); rec.Code != http.StatusCreated {
		t.Fatalf("submission to the old slug: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := fcSubmit(t, tc, "missing-"+oldSlug, "alias-2", map[string]any{itoa(question): "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slug: %d", rec.Code)
	}

	form := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))
	previous, _ := form["previous_slugs"].([]any)
	if len(previous) != 1 || previous[0] != oldSlug {
		t.Fatalf("previous_slugs = %v", form["previous_slugs"])
	}

	other, otherURL := env.newForm(t, tc, "Other")
	_ = other
	if res := renameForm(t, tc, env, otherURL, oldSlug); res.code != http.StatusConflict || !strings.Contains(res.body, "FORM_SLUG_TAKEN") {
		t.Fatalf("taking an alias by rename: %d %s", res.code, res.body)
	}
	rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{"name": "Dup", "slug": strings.ToUpper(oldSlug)})
	if rec.Code != http.StatusConflict {
		t.Fatalf("creating a form with an alias slug: %d: %s", rec.Code, rec.Body.String())
	}

	if res := renameForm(t, tc, env, formURL, oldSlug); res.code != http.StatusOK {
		t.Fatalf("switching back to a previous slug: %d %s", res.code, res.body)
	}
	if code, data := publicStatus(t, tc, newSlug); code != http.StatusOK || data["slug"] != oldSlug {
		t.Fatalf("the slug we left behind must now be the alias: %d %v", code, data)
	}
	_ = formID
}

func TestFormSlug_AliasesAreBoundedAndDieWithTheForm(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "slugbound")
	_, formURL := env.newForm(t, tc, "Bounded")
	mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{"type": "short_text", "title": "Name"})
	publishForm(t, tc, env, formURL)
	first := formSlugOf(t, tc, env, formURL)
	slugs := []string{first}
	for i := 0; i < 12; i++ {
		next := env.newSlug(fmt.Sprintf("hop%d", i))
		if res := renameForm(t, tc, env, formURL, next); res.code != http.StatusOK {
			t.Fatalf("rename %d: %d %s", i, res.code, res.body)
		}
		slugs = append(slugs, next)
	}
	form := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))
	previous, _ := form["previous_slugs"].([]any)
	if len(previous) != 10 {
		t.Fatalf("previous_slugs has %d entries, want the newest 10: %v", len(previous), previous)
	}
	if code, _ := publicStatus(t, tc, first); code != http.StatusNotFound {
		t.Fatalf("the oldest alias must have been dropped: %d", code)
	}
	if code, _ := publicStatus(t, tc, slugs[len(slugs)-2]); code != http.StatusOK {
		t.Fatalf("a recent alias must resolve: %d", code)
	}
	if res := request(t, tc.Router, http.MethodPost, formURL+"/archive", env.owner.token, nil); res.Code != http.StatusOK {
		t.Fatalf("archive: %d", res.Code)
	}
	if code, _ := publicStatus(t, tc, slugs[len(slugs)-2]); code != http.StatusNotFound {
		t.Fatalf("an archived form must not resolve through an alias: %d", code)
	}
	released := slugs[len(slugs)-2]
	if rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{"name": "Reuse", "slug": released}); rec.Code != http.StatusCreated {
		t.Fatalf("an alias of an archived form is free again: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormSlug_AliasDiesWhenTheFormIsDeletedAndDraftsKeepNone(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "slugdel")
	_, draftURL := env.newForm(t, tc, "Draft")
	draftSlug := formSlugOf(t, tc, env, draftURL)
	if res := renameForm(t, tc, env, draftURL, env.newSlug("draft2")); res.code != http.StatusOK {
		t.Fatalf("rename draft: %d %s", res.code, res.body)
	}
	if rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{"name": "Taker", "slug": draftSlug}); rec.Code != http.StatusCreated {
		t.Fatalf("a never published form leaves no alias behind: %d: %s", rec.Code, rec.Body.String())
	}

	_, liveURL := env.newForm(t, tc, "Live")
	mustCreate(t, tc, env.owner.token, liveURL+"/questions", map[string]any{"type": "short_text", "title": "Name"})
	publishForm(t, tc, env, liveURL)
	liveSlug := formSlugOf(t, tc, env, liveURL)
	if res := renameForm(t, tc, env, liveURL, env.newSlug("live2")); res.code != http.StatusOK {
		t.Fatalf("rename live: %d %s", res.code, res.body)
	}
	if rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{"name": "Blocked", "slug": liveSlug}); rec.Code != http.StatusConflict {
		t.Fatalf("alias must block the slug: %d", rec.Code)
	}
	if res := request(t, tc.Router, http.MethodDelete, liveURL, env.owner.token, nil); res.Code != http.StatusOK {
		t.Fatalf("delete: %d: %s", res.Code, res.Body.String())
	}
	if code, _ := publicStatus(t, tc, liveSlug); code != http.StatusNotFound {
		t.Fatalf("deleted form resolves: %d", code)
	}
	if rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{"name": "Free", "slug": liveSlug}); rec.Code != http.StatusCreated {
		t.Fatalf("alias of a deleted form is free: %d: %s", rec.Code, rec.Body.String())
	}
}
