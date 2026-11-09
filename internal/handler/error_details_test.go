package handler

import (
	"errors"
	"net/http"
	"testing"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
)

func TestTaskReferenceErrorNamesTheField(t *testing.T) {
	status, parent, list, after := uint(4), uint(5), uint(6), uint(7)
	cases := []struct {
		name   string
		err    error
		input  taskReferenceInput
		field  string
		code   string
		params map[string]any
	}{
		{"status", domain.ErrInvalidStatusReference, taskReferenceInput{Status: &status}, "status_id", "NOT_IN_LIST", map[string]any{"id": status}},
		{"missing status", domain.ErrInvalidStatusReference, taskReferenceInput{}, "status_id", "REQUIRED", nil},
		{"parent", domain.ErrInvalidParentReference, taskReferenceInput{Parent: &parent}, "parent_id", "NOT_IN_LIST", map[string]any{"id": parent}},
		{"list", domain.ErrInvalidListReference, taskReferenceInput{List: &list}, "list_id", "NOT_IN_WORKSPACE", map[string]any{"id": list}},
		{"after anchor", domain.ErrInvalidMoveAnchor, taskReferenceInput{After: &after}, "after_id", "NOT_IN_LIST", map[string]any{"id": after}},
		{"before anchor", domain.ErrInvalidMoveAnchor, taskReferenceInput{Before: &after}, "before_id", "NOT_IN_LIST", map[string]any{"id": after}},
		{
			"tags",
			domain.InvalidFields(domain.ErrInvalidTagReference, []domain.FieldProblem{{Params: map[string]any{"ids": []uint{9}}}}),
			taskReferenceInput{TagField: "add_tag_ids"}, "add_tag_ids", "NOT_IN_SPACE", map[string]any{"ids": []uint{9}},
		},
		{
			"assignees",
			domain.InvalidFields(domain.ErrInvalidAssigneeReference, []domain.FieldProblem{{Params: map[string]any{"ids": []uint{8}}}}),
			taskReferenceInput{}, "assignee_ids", "NOT_A_MEMBER", map[string]any{"ids": []uint{8}},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			appErr := taskReferenceError(test.err, test.input)
			if appErr.Code != "INVALID_TASK_REFERENCE" || appErr.Status != http.StatusBadRequest || len(appErr.Details) != 1 {
				t.Fatalf("appErr = %+v", appErr)
			}
			detail := appErr.Details[0]
			if detail.Field != test.field || detail.Code != test.code {
				t.Fatalf("detail = %+v", detail)
			}
			if len(detail.Params) != len(test.params) {
				t.Fatalf("params = %v, want %v", detail.Params, test.params)
			}
			if !errors.Is(appErr, test.err) {
				t.Fatalf("the cause must stay reachable: %v", appErr)
			}
		})
	}
}

func TestWithFieldDetailsUsesTheDefaultFieldForBareProblems(t *testing.T) {
	err := domain.InvalidFields(domain.ErrInvalidMentionReference, []domain.FieldProblem{
		{Reason: "NOT_A_MEMBER", Params: map[string]any{"user_id": uint(3)}},
		{Field: "explicit", Reason: "X"},
	})
	appErr := withFieldDetails(httperrors.BadRequest("INVALID_MENTION", "m", err), err, "mention_user_ids")
	if len(appErr.Details) != 2 || appErr.Details[0].Field != "mention_user_ids" || appErr.Details[1].Field != "explicit" ||
		appErr.Details[0].Params["user_id"] != uint(3) {
		t.Fatalf("details = %+v", appErr.Details)
	}
	plain := withFieldDetails(httperrors.BadRequest("X", "m", domain.ErrNotFound), domain.ErrNotFound, "f")
	if plain.Details != nil {
		t.Fatalf("details = %+v", plain.Details)
	}
}

func TestTemplateApplicationErrorNamesTheField(t *testing.T) {
	cases := []struct {
		err   error
		field string
		code  string
	}{
		{domain.ErrInvalidListReference, "list_id", "NOT_IN_WORKSPACE"},
		{domain.ErrInvalidStatusReference, "status_id", "NOT_IN_LIST"},
		{domain.ErrInvalidAssigneeReference, "configuration.task.assignee_id", "NOT_A_MEMBER"},
		{domain.InvalidFields(domain.ErrInvalidTagReference, []domain.FieldProblem{{Params: map[string]any{"ids": []uint{1}}}}), "configuration.tags", "NOT_IN_SPACE"},
		{domain.InvalidField(domain.ErrInvalidCustomField, "value", "OUT_OF_RANGE"), "configuration.custom_fields", "OUT_OF_RANGE"},
	}
	for _, test := range cases {
		appErr := templateApplicationError(test.err)
		if appErr.Code != "INVALID_TASK_TEMPLATE_APPLICATION" || len(appErr.Details) != 1 ||
			appErr.Details[0].Field != test.field || appErr.Details[0].Code != test.code {
			t.Fatalf("appErr = %+v for %v", appErr, test.err)
		}
	}
}
