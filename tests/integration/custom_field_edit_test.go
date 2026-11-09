package integration

import (
	"net/http"
	"testing"
)

type cfEnv struct {
	env fcEnv
	tc  *TestContext
}

func (e cfEnv) fieldURL(id uint) string {
	return "/api/v1/workspaces/" + itoa(e.env.owner.workspaceID) + "/custom-fields/" + itoa(id)
}

func (e cfEnv) newTask(t *testing.T, title string) uint {
	t.Helper()
	return mustCreate(t, e.tc, e.env.owner.token, rfTasksURL(e.env), map[string]any{"title": title, "status_id": e.env.statusID})
}

func (e cfEnv) setValue(t *testing.T, taskID, fieldID uint, value any) {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPut, "/api/v1/tasks/"+itoa(taskID)+"/custom-fields/"+itoa(fieldID),
		e.env.owner.token, map[string]any{"value": value})
	if rec.Code != http.StatusOK {
		t.Fatalf("set value %v: %d: %s", value, rec.Code, rec.Body.String())
	}
}

func (e cfEnv) edit(t *testing.T, id uint, body map[string]any) *struct {
	Code int
	Body map[string]any
} {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPatch, e.fieldURL(id), e.env.owner.token, body)
	return &struct {
		Code int
		Body map[string]any
	}{rec.Code, decodeJSON(t, rec)}
}

func (e cfEnv) expectInUse(t *testing.T, label string, id uint, body map[string]any, wantCode, wantField string) {
	t.Helper()
	res := e.edit(t, id, body)
	if res.Code != http.StatusConflict {
		t.Fatalf("%s: expected 409, got %d: %v", label, res.Code, res.Body)
	}
	errBody, _ := res.Body["error"].(map[string]any)
	if errBody["code"] != wantCode {
		t.Fatalf("%s: code = %v, want %s: %v", label, errBody["code"], wantCode, res.Body)
	}
	if wantField != "" {
		detail := errorDetail(errBody, wantField)
		if detail == nil {
			t.Fatalf("%s: details %v do not name %q", label, errBody["details"], wantField)
		}
	}
}

func TestCustomFieldEdit_RejectsIncompatibleChangesWhileValuesExist(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "cfedit")
	e := cfEnv{env: env, tc: tc}

	dropdown := rfCreateField(t, tc, env, map[string]any{
		"name": "Stage", "type": "dropdown",
		"options": []any{map[string]any{"label": "A", "value": "a"}, map[string]any{"label": "B", "value": "b"}, map[string]any{"label": "C", "value": "c"}},
	})
	multi := rfCreateField(t, tc, env, map[string]any{
		"name": "Labels", "type": "multi_select",
		"options": []any{map[string]any{"label": "X", "value": "x"}, map[string]any{"label": "Y", "value": "y"}},
	})
	number := rfCreateField(t, tc, env, map[string]any{"name": "Score", "type": "number", "config": map[string]any{"max": 100}})
	integer := rfCreateField(t, tc, env, map[string]any{"name": "Count", "type": "integer", "config": map[string]any{"min": 0, "max": 50}})
	text := rfCreateField(t, tc, env, map[string]any{"name": "Memo", "type": "text", "config": map[string]any{"max_length": 20}})

	taskID := e.newTask(t, "valued")
	e.setValue(t, taskID, dropdown, "b")
	e.setValue(t, taskID, multi, []string{"x", "y"})
	e.setValue(t, taskID, number, 80)
	e.setValue(t, taskID, integer, 5)
	e.setValue(t, taskID, text, "twelve chars!")

	opts := func(values ...string) []any {
		out := make([]any, len(values))
		for i, v := range values {
			out[i] = map[string]any{"label": v, "value": v}
		}
		return out
	}
	e.expectInUse(t, "change type", dropdown, map[string]any{"name": "Stage", "type": "text"}, "CUSTOM_FIELD_IN_USE", "type")
	e.expectInUse(t, "remove used dropdown option", dropdown,
		map[string]any{"name": "Stage", "type": "dropdown", "options": opts("a", "c")}, "CUSTOM_FIELD_VALUE_OUT_OF_RANGE", "options")
	e.expectInUse(t, "remove used multi-select option", multi,
		map[string]any{"name": "Labels", "type": "multi_select", "options": opts("x")}, "CUSTOM_FIELD_VALUE_OUT_OF_RANGE", "options")
	e.expectInUse(t, "lower number max", number,
		map[string]any{"name": "Score", "type": "number", "config": map[string]any{"max": 50}}, "CUSTOM_FIELD_VALUE_OUT_OF_RANGE", "config.max")
	e.expectInUse(t, "raise integer min", integer,
		map[string]any{"name": "Count", "type": "integer", "config": map[string]any{"min": 10, "max": 50}}, "CUSTOM_FIELD_VALUE_OUT_OF_RANGE", "config.min")
	e.expectInUse(t, "shrink text max_length", text,
		map[string]any{"name": "Memo", "type": "text", "config": map[string]any{"max_length": 5}}, "CUSTOM_FIELD_VALUE_OUT_OF_RANGE", "config.max_length")

	var kept int64
	tc.DB.Table("custom_field_options").Where("custom_field_id IN ?", []uint{dropdown, multi}).Count(&kept)
	if kept != 5 {
		t.Fatalf("rejected edits changed options: %d rows, want 5", kept)
	}

	compatible := map[uint]map[string]any{
		dropdown: {"name": "Stage renamed", "type": "dropdown", "options": []any{
			map[string]any{"label": "B relabelled", "value": "b"}, map[string]any{"label": "D", "value": "d"}}},
		multi:   {"name": "Labels", "type": "multi_select", "options": opts("x", "y", "z")},
		number:  {"name": "Score", "type": "number", "config": map[string]any{"max": 80}},
		integer: {"name": "Count", "type": "integer", "config": map[string]any{"min": 5, "max": 5}},
		text:    {"name": "Memo", "type": "text", "required": false, "config": map[string]any{"max_length": 13}},
	}
	for id, body := range compatible {
		if res := e.edit(t, id, body); res.Code != http.StatusOK {
			t.Fatalf("compatible edit of field %d: %d: %v", id, res.Code, res.Body)
		}
	}
}

func TestCustomFieldEdit_FreeWhileNoValuesExist(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "cffree")
	e := cfEnv{env: env, tc: tc}
	dropdown := rfCreateField(t, tc, env, map[string]any{
		"name": "Stage", "type": "dropdown",
		"options": []any{map[string]any{"label": "A", "value": "a"}, map[string]any{"label": "B", "value": "b"}},
	})
	number := rfCreateField(t, tc, env, map[string]any{"name": "Score", "type": "number", "config": map[string]any{"max": 100}})
	taskID := e.newTask(t, "valued")
	e.setValue(t, taskID, dropdown, "a")

	if res := e.edit(t, number, map[string]any{"name": "Score", "type": "number", "config": map[string]any{"max": 1}}); res.Code != http.StatusOK {
		t.Fatalf("tightening an unused number: %d: %v", res.Code, res.Body)
	}
	if res := e.edit(t, number, map[string]any{"name": "Score", "type": "text"}); res.Code != http.StatusOK {
		t.Fatalf("retyping an unused field: %d: %v", res.Code, res.Body)
	}
	if res := e.edit(t, dropdown, map[string]any{"name": "Stage", "type": "dropdown", "options": []any{
		map[string]any{"label": "A", "value": "a"}, map[string]any{"label": "Z", "value": "z"}}}); res.Code != http.StatusOK {
		t.Fatalf("removing an option no task uses: %d: %v", res.Code, res.Body)
	}
	if res := e.edit(t, dropdown, map[string]any{"name": "Stage", "type": "dropdown", "options": []any{
		map[string]any{"label": "Z", "value": "z"}}}); res.Code != http.StatusConflict {
		t.Fatalf("removing the option task %d uses: expected 409, got %d", taskID, res.Code)
	}
	tc.DB.Exec("DELETE FROM task_custom_field_values")
	if res := e.edit(t, dropdown, map[string]any{"name": "Stage", "type": "dropdown", "options": []any{
		map[string]any{"label": "Z", "value": "z"}}}); res.Code != http.StatusOK {
		t.Fatalf("shrinking after the values are gone: %d: %v", res.Code, res.Body)
	}
}

func TestCustomFieldEdit_IgnoresValuesOfDeletedTasks(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "cfdead")
	e := cfEnv{env: env, tc: tc}
	number := rfCreateField(t, tc, env, map[string]any{"name": "Score", "type": "number"})
	taskID := e.newTask(t, "doomed")
	e.setValue(t, taskID, number, 99)
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(taskID), env.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete task: %d", rec.Code)
	}
	if res := e.edit(t, number, map[string]any{"name": "Score", "type": "number", "config": map[string]any{"max": 10}}); res.Code != http.StatusOK {
		t.Fatalf("a deleted task's value must not block the edit: %d: %v", res.Code, res.Body)
	}
}
