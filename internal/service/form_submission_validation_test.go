package service

import (
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func submissionProblems(t *testing.T, err error) map[string]domain.FieldProblem {
	t.Helper()
	var fieldErr *domain.FieldError
	if !errors.As(err, &fieldErr) || !errors.Is(err, domain.ErrInvalidSubmission) {
		t.Fatalf("err = %v, want a field error wrapping ErrInvalidSubmission", err)
	}
	problems := make(map[string]domain.FieldProblem, len(fieldErr.Problems))
	for _, problem := range fieldErr.Problems {
		problems[problem.Field] = problem
	}
	if len(problems) != len(fieldErr.Problems) {
		t.Fatalf("duplicate fields in %v", fieldErr.Problems)
	}
	return problems
}

func TestValidateSubmissionCollectsEveryProblem(t *testing.T) {
	abc := []domain.QuestionOption{{Value: "a"}, {Value: "b"}, {Value: "c"}}
	questions := []domain.FormQuestion{
		{ID: 1, Type: domain.QuestionShortText, Title: "t", Position: 0, Required: true},
		{ID: 2, Type: domain.QuestionShortText, Title: "t", Position: 1, Settings: []byte(`{"min_length":2,"max_length":4}`)},
		{ID: 3, Type: domain.QuestionNumber, Title: "n", Position: 2, Settings: []byte(`{"min":1}`)},
		{ID: 4, Type: domain.QuestionEmail, Title: "e", Position: 3},
		{ID: 5, Type: domain.QuestionSingleChoice, Title: "c", Position: 4, Options: abc},
		{ID: 6, Type: domain.QuestionMultipleChoice, Title: "m", Position: 5, Options: abc, Settings: []byte(`{"max_selected":1}`)},
		{ID: 7, Type: domain.QuestionRating, Title: "r", Position: 6},
		{ID: 8, Type: domain.QuestionSection, Title: "s", Position: 7},
		{ID: 9, Type: domain.QuestionFile, Title: "f", Position: 8, Settings: []byte(`{"max_files":1}`)},
		{ID: 10, Type: domain.QuestionDate, Title: "d", Position: 9},
		{ID: 11, Type: domain.QuestionDateTime, Title: "dt", Position: 10},
		{ID: 12, Type: domain.QuestionURL, Title: "u", Position: 11},
		{ID: 13, Type: domain.QuestionCheckbox, Title: "cb", Position: 12},
		{ID: 14, Type: domain.QuestionPhone, Title: "p", Position: 13},
	}
	_, err := validateSubmission(questions, answersInput(map[uint]any{
		2: "x", 3: 0.5, 4: "nope", 5: "z", 6: []string{"a", "b"}, 7: 5.5, 8: "x", 9: []string{"a", "b"},
		10: "2020-02-31", 11: "yesterday", 12: "ftp://x", 13: 1, 14: 5, 99: "x",
	}))
	problems := submissionProblems(t, err)
	want := map[string]string{
		"answers.1": "REQUIRED", "answers.2": "TOO_SHORT", "answers.3": "OUT_OF_RANGE", "answers.4": "INVALID_FORMAT",
		"answers.5": "INVALID_CHOICE", "answers.6": "TOO_MANY", "answers.7": "INVALID_TYPE", "answers.8": "NOT_ALLOWED",
		"answers.9": "TOO_MANY", "answers.10": "INVALID_FORMAT", "answers.11": "INVALID_FORMAT", "answers.12": "INVALID_FORMAT",
		"answers.13": "INVALID_TYPE", "answers.14": "INVALID_TYPE", "answers.99": "UNKNOWN_QUESTION",
	}
	if len(problems) != len(want) {
		t.Fatalf("problems = %v", problems)
	}
	for field, reason := range want {
		if problems[field].Reason != reason {
			t.Fatalf("%s = %+v, want %s", field, problems[field], reason)
		}
	}
	if problems["answers.2"].Params["min"] != 2 || problems["answers.3"].Params["min"] != float64(1) ||
		problems["answers.6"].Params["max"] != 1 || problems["answers.7"].Params["expected"] != "integer" {
		t.Fatalf("params = %v", problems)
	}
	if allowed, _ := problems["answers.5"].Params["allowed"].([]string); len(allowed) != 3 {
		t.Fatalf("allowed = %v", problems["answers.5"].Params)
	}
}

func TestValidateSubmissionEmptyAndRequiredValues(t *testing.T) {
	questions := []domain.FormQuestion{
		{ID: 1, Type: domain.QuestionShortText, Title: "t", Position: 0, Required: true},
		{ID: 2, Type: domain.QuestionShortText, Title: "t", Position: 1},
	}
	_, err := validateSubmission(questions, answersInput(map[uint]any{1: "  ", 2: ""}))
	problems := submissionProblems(t, err)
	if problems["answers.1"].Reason != "REQUIRED" || problems["answers.2"].Reason != "EMPTY" {
		t.Fatalf("problems = %v", problems)
	}
}

func TestValidateSubmissionSkipsQuestionsThatDependOnInvalidAnswers(t *testing.T) {
	gate := choiceQuestion(1, 0, "yes", "no")
	notYes := textQuestion(2, 1, true, visibleIf(1, "not_equals", "yes"))
	_, err := validateSubmission([]domain.FormQuestion{gate, notYes}, answersInput(map[uint]any{1: "maybe"}))
	problems := submissionProblems(t, err)
	if len(problems) != 1 || problems["answers.1"].Reason != "INVALID_CHOICE" {
		t.Fatalf("a question behind an invalid answer must not add noise: %v", problems)
	}
}

func TestValidateSubmissionAcceptsValidAnswers(t *testing.T) {
	questions := []domain.FormQuestion{
		{ID: 1, Type: domain.QuestionShortText, Title: "t", Position: 0, Required: true},
		{ID: 2, Type: domain.QuestionMultipleChoice, Title: "m", Position: 1, Options: []domain.QuestionOption{{Value: "a"}, {Value: "b"}}},
		{ID: 3, Type: domain.QuestionFile, Title: "f", Position: 2},
	}
	answers, err := validateSubmission(questions, answersInput(map[uint]any{
		1: "ok", 2: []string{"a", "b"}, 3: []string{"0123456789abcdef0123456789abcdef"},
	}))
	if err != nil || len(answers) != 3 {
		t.Fatalf("answers = %v err = %v", answers, err)
	}
	if _, err := validateSubmission(questions, answersInput(map[uint]any{
		1: "ok", 3: []string{"0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef"},
	})); submissionProblems(t, err)["answers.3"].Reason != "DUPLICATE" {
		t.Fatalf("duplicate upload ids: %v", err)
	}
}

func TestReorderSetError(t *testing.T) {
	questions := []domain.FormQuestion{{ID: 1, Position: 0}, {ID: 2, Position: 1}, {ID: 3, Position: 2}}
	if err := reorderSetError(questions, []uint{3, 1, 2}); err != nil {
		t.Fatal(err)
	}
	err := reorderSetError(questions, []uint{3, 3, 9})
	var formErr *domain.FormError
	if !errors.As(err, &formErr) || formErr.Code != reorderInvalidCode {
		t.Fatalf("err = %v", err)
	}
	got := map[string]string{}
	for _, problem := range formErr.Problems {
		got[problem.Field+"/"+problem.Reason] = ""
	}
	for _, key := range []string{"ids[1]/DUPLICATE", "ids[2]/UNKNOWN_QUESTION", "ids/MISSING"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %s in %v", key, formErr.Problems)
		}
	}
	missing := 0
	for _, problem := range formErr.Problems {
		if problem.Reason == "MISSING" {
			missing++
		}
	}
	if missing != 2 {
		t.Fatalf("missing problems = %d, want questions 1 and 2", missing)
	}
}

func TestReorderVisibilityError(t *testing.T) {
	gate := choiceQuestion(1, 0, "a", "b")
	dependent := textQuestion(2, 1, false, visibleIf(1, "equals", "a"))
	other := textQuestion(3, 2, false, nil)
	questions := []domain.FormQuestion{gate, dependent, other}
	if err := reorderVisibilityError(questions, []uint{3, 1, 2}); err != nil {
		t.Fatal(err)
	}
	err := reorderVisibilityError(questions, []uint{2, 1, 3})
	var formErr *domain.FormError
	if !errors.As(err, &formErr) || formErr.Code != visibilityInvalidCode || len(formErr.Problems) != 1 {
		t.Fatalf("err = %v", err)
	}
	problem := formErr.Problems[0]
	if problem.Field != "ids" || problem.Reason != "RULE_NOT_EARLIER" ||
		problem.Params["question_id"] != uint(2) || problem.Params["depends_on"] != uint(1) {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestPlaceNewQuestionMirrorsTheRepository(t *testing.T) {
	existing := []domain.FormQuestion{{ID: 1, Position: 0}, {ID: 2, Position: 1}, {ID: 3, Position: 5}}
	placed, saved := placeNewQuestion(existing, domain.FormQuestion{Position: domain.AppendQuestionPosition})
	if saved.Position != 6 || placed[0].Position != 0 {
		t.Fatalf("append: saved = %d placed = %v", saved.Position, placed)
	}
	placed, saved = placeNewQuestion(existing, domain.FormQuestion{Position: 1})
	if saved.Position != 1 || placed[1].Position != 2 || placed[2].Position != 6 || placed[0].Position != 0 {
		t.Fatalf("insert at a taken position: saved = %d placed = %v", saved.Position, placed)
	}
	placed, saved = placeNewQuestion(existing, domain.FormQuestion{Position: 3})
	if saved.Position != 3 || placed[1].Position != 1 || placed[2].Position != 5 {
		t.Fatalf("insert at a free position: saved = %d placed = %v", saved.Position, placed)
	}
	if existing[1].Position != 1 {
		t.Fatal("the input slice must not be modified")
	}
}

func TestPublishVisibilityErrorNamesTheQuestion(t *testing.T) {
	gate := choiceQuestion(1, 5, "a")
	dependent := textQuestion(2, 1, false, visibleIf(1, "equals", "a"))
	err := publishVisibilityError([]domain.FormQuestion{gate, dependent})
	var formErr *domain.FormError
	if !errors.As(err, &formErr) || len(formErr.Problems) != 1 {
		t.Fatalf("err = %v", err)
	}
	problem := formErr.Problems[0]
	if problem.Field != "questions[0].settings.visible_if.question_id" || problem.Reason != "NOT_EARLIER" ||
		problem.Params["question_id"] != uint(2) {
		t.Fatalf("problem = %+v", problem)
	}
}
