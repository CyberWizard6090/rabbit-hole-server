package domain

import (
	"fmt"
	"strings"
)

type FieldProblem struct {
	Field  string
	Reason string
	Params map[string]any
}

type FieldError struct {
	Err      error
	Problems []FieldProblem
}

func InvalidField(err error, field, reason string) *FieldError {
	return &FieldError{Err: err, Problems: []FieldProblem{{Field: field, Reason: reason}}}
}

func InvalidFields(err error, problems []FieldProblem) *FieldError {
	return &FieldError{Err: err, Problems: problems}
}

func (e *FieldError) WithParams(params map[string]any) *FieldError {
	copied := &FieldError{Err: e.Err, Problems: append([]FieldProblem(nil), e.Problems...)}
	for i := range copied.Problems {
		copied.Problems[i].Params = params
	}
	return copied
}

func (e *FieldError) WithPrefix(prefix string) *FieldError {
	copied := &FieldError{Err: e.Err, Problems: append([]FieldProblem(nil), e.Problems...)}
	for i := range copied.Problems {
		copied.Problems[i].Field = prefix + copied.Problems[i].Field
	}
	return copied
}

func (e *FieldError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, problem := range e.Problems {
		parts[i] = problem.Field + ": " + problem.Reason
	}
	return fmt.Sprintf("%s: %s", e.Err, strings.Join(parts, "; "))
}

func (e *FieldError) Unwrap() error { return e.Err }
