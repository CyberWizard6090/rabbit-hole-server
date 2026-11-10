package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"rabbit-hole-server/internal/domain"
)

type answerUsageFake struct {
	domain.FormRepository
	answers int64
	used    map[string]bool
}

func (f *answerUsageFake) QuestionAnswerUsage(_ context.Context, _ uint, values []string) (int64, []string, error) {
	used := []string{}
	for _, value := range values {
		if f.used[value] {
			used = append(used, value)
		}
	}
	return f.answers, used, nil
}

func colourQuestion() domain.FormQuestion {
	question := choiceQuestion(7, 0, "red", "blue")
	question.Title = "Color"
	return question
}

func TestCheckAnsweredQuestionChange(t *testing.T) {
	service := NewFormService(&answerUsageFake{answers: 3, used: map[string]bool{"red": true}})
	existing := []domain.FormQuestion{colourQuestion()}

	t.Run("type change is refused", func(t *testing.T) {
		updated := colourQuestion()
		updated.Type = domain.QuestionDropdown
		err := service.checkAnsweredQuestionChange(context.Background(), existing, updated)
		var formErr *domain.FormError
		if !errors.As(err, &formErr) || formErr.Code != questionInUseCode || formErr.Status != http.StatusConflict ||
			len(formErr.Problems) != 1 || formErr.Problems[0].Field != "type" || formErr.Problems[0].Reason != "HAS_ANSWERS" ||
			formErr.Problems[0].Params["answers"] != int64(3) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("removing a used option is refused", func(t *testing.T) {
		updated := colourQuestion()
		updated.Options = updated.Options[1:]
		err := service.checkAnsweredQuestionChange(context.Background(), existing, updated)
		var formErr *domain.FormError
		if !errors.As(err, &formErr) || len(formErr.Problems) != 1 || formErr.Problems[0].Reason != "OPTION_IN_USE" ||
			formErr.Problems[0].Params["value"] != "red" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("renaming the value of a used option is refused", func(t *testing.T) {
		updated := colourQuestion()
		updated.Options[0].Value = "crimson"
		if err := service.checkAnsweredQuestionChange(context.Background(), existing, updated); err == nil {
			t.Fatal("expected a conflict")
		}
	})
	t.Run("removing an unused option and relabelling are allowed", func(t *testing.T) {
		updated := colourQuestion()
		updated.Title = "Favorite color"
		updated.Options = []domain.QuestionOption{{Label: "Dark red", Value: "red"}}
		if err := service.checkAnsweredQuestionChange(context.Background(), existing, updated); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a question without answers may change", func(t *testing.T) {
		unanswered := NewFormService(&answerUsageFake{})
		updated := colourQuestion()
		updated.Type = domain.QuestionDropdown
		if err := unanswered.checkAnsweredQuestionChange(context.Background(), existing, updated); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("an unknown question is left to the repository", func(t *testing.T) {
		updated := colourQuestion()
		updated.ID = 99
		if err := service.checkAnsweredQuestionChange(context.Background(), existing, updated); err != nil {
			t.Fatal(err)
		}
	})
}
