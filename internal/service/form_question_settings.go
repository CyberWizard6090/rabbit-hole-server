package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

const (
	defaultRatingMin = 1
	defaultRatingMax = 5
	defaultScaleMin  = 1
	defaultScaleMax  = 10
	maxRatingValue   = 10
	maxScaleValue    = 100
	maxPlaceholder   = 255
	maxScaleLabel    = 100
)

type settingKind int

const (
	settingString settingKind = iota
	settingInteger
	settingNumber
)

type settingSpec struct {
	kind settingKind
	min  float64
	max  float64
}

var placeholderSetting = settingSpec{kind: settingString, max: maxPlaceholder}

const visibleIfSettingKey = "visible_if"

var questionSettingSpecs = map[domain.QuestionType]map[string]settingSpec{
	domain.QuestionShortText: {
		"placeholder": placeholderSetting,
		"min_length":  {kind: settingInteger, min: 0, max: 1000},
		"max_length":  {kind: settingInteger, min: 1, max: 1000},
	},
	domain.QuestionLongText: {
		"placeholder": placeholderSetting,
		"min_length":  {kind: settingInteger, min: 0, max: 50000},
		"max_length":  {kind: settingInteger, min: 1, max: 50000},
	},
	domain.QuestionNumber: {
		"placeholder": placeholderSetting,
		"min":         {kind: settingNumber, min: -1e15, max: 1e15},
		"max":         {kind: settingNumber, min: -1e15, max: 1e15},
	},
	domain.QuestionEmail:    {"placeholder": placeholderSetting},
	domain.QuestionPhone:    {"placeholder": placeholderSetting},
	domain.QuestionURL:      {"placeholder": placeholderSetting},
	domain.QuestionDropdown: {"placeholder": placeholderSetting},
	domain.QuestionMultipleChoice: {
		"min_selected": {kind: settingInteger, min: 0, max: 100},
		"max_selected": {kind: settingInteger, min: 1, max: 100},
	},
	domain.QuestionRating: {
		"min": {kind: settingInteger, min: 0, max: maxRatingValue},
		"max": {kind: settingInteger, min: 1, max: maxRatingValue},
	},
	domain.QuestionScale: {
		"min":       {kind: settingInteger, min: 0, max: maxScaleValue},
		"max":       {kind: settingInteger, min: 1, max: maxScaleValue},
		"min_label": {kind: settingString, max: maxScaleLabel},
		"max_label": {kind: settingString, max: maxScaleLabel},
	},
}

func validateQuestionSettings(
	questionType domain.QuestionType,
	raw json.RawMessage,
	optionCount int,
) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var settings map[string]any
	if err := decoder.Decode(&settings); err != nil || settings == nil {
		return domain.NewFormValidationError("settings", "must be a JSON object")
	}
	if questionType == domain.QuestionFile {
		return nil
	}
	delete(settings, visibleIfSettingKey)
	specs := questionSettingSpecs[questionType]
	numbers := make(map[string]float64, len(settings))
	for key, value := range settings {
		spec, allowed := specs[key]
		if !allowed {
			return domain.NewFormValidationError("settings."+key,
				fmt.Sprintf("is not supported for %s questions", questionType))
		}
		number, err := validateSettingValue(spec, value)
		if err != nil {
			return domain.NewFormValidationError("settings."+key, err.Error())
		}
		if spec.kind != settingString {
			numbers[key] = number
		}
	}
	return validateSettingRelations(questionType, numbers, optionCount)
}

func validateSettingValue(spec settingSpec, value any) (float64, error) {
	if spec.kind == settingString {
		text, ok := value.(string)
		if !ok {
			return 0, fmt.Errorf("must be a string")
		}
		if utf8.RuneCountInString(text) > int(spec.max) {
			return 0, fmt.Errorf("must be at most %d characters", int(spec.max))
		}
		return 0, nil
	}
	number, ok := questionNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("must be a number")
	}
	if spec.kind == settingInteger && number != math.Trunc(number) {
		return 0, fmt.Errorf("must be an integer")
	}
	if number < spec.min || number > spec.max {
		return 0, fmt.Errorf("must be between %v and %v", spec.min, spec.max)
	}
	return number, nil
}

func validateSettingRelations(
	questionType domain.QuestionType,
	numbers map[string]float64,
	optionCount int,
) error {
	switch questionType {
	case domain.QuestionShortText, domain.QuestionLongText:
		low, hasLow := numbers["min_length"]
		high, hasHigh := numbers["max_length"]
		if hasLow && hasHigh && low > high {
			return domain.NewFormValidationError("settings.min_length", "must not exceed max_length")
		}
	case domain.QuestionNumber:
		low, hasLow := numbers["min"]
		high, hasHigh := numbers["max"]
		if hasLow && hasHigh && low > high {
			return domain.NewFormValidationError("settings.min", "must not exceed max")
		}
	case domain.QuestionMultipleChoice:
		low, hasLow := numbers["min_selected"]
		high, hasHigh := numbers["max_selected"]
		if hasLow && hasHigh && low > high {
			return domain.NewFormValidationError("settings.min_selected", "must not exceed max_selected")
		}
		if hasHigh && int(high) > optionCount {
			return domain.NewFormValidationError("settings.max_selected", "must not exceed the number of options")
		}
		if hasLow && int(low) > optionCount {
			return domain.NewFormValidationError("settings.min_selected", "must not exceed the number of options")
		}
	case domain.QuestionRating, domain.QuestionScale:
		low, high := integerBounds(questionType, numbers)
		if low >= high {
			return domain.NewFormValidationError("settings.min", "must be less than max")
		}
	}
	return nil
}

func integerBounds(questionType domain.QuestionType, numbers map[string]float64) (float64, float64) {
	low, high := float64(defaultRatingMin), float64(defaultRatingMax)
	if questionType == domain.QuestionScale {
		low, high = defaultScaleMin, defaultScaleMax
	}
	if value, ok := numbers["min"]; ok {
		low = value
	}
	if value, ok := numbers["max"]; ok {
		high = value
	}
	return low, high
}

func questionSettingsMap(raw json.RawMessage) map[string]any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var settings map[string]any
	if err := decoder.Decode(&settings); err != nil {
		return map[string]any{}
	}
	return settings
}

func settingNumberValue(settings map[string]any, key string) (float64, bool) {
	value, exists := settings[key]
	if !exists {
		return 0, false
	}
	return questionNumber(value)
}

func validTextLength(question domain.FormQuestion, text string) bool {
	settings := questionSettingsMap(question.Settings)
	length := float64(utf8.RuneCountInString(text))
	if low, ok := settingNumberValue(settings, "min_length"); ok && length < low {
		return false
	}
	if high, ok := settingNumberValue(settings, "max_length"); ok && length > high {
		return false
	}
	return true
}

func validNumericAnswer(question domain.FormQuestion, value any) bool {
	number, ok := questionNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return false
	}
	settings := questionSettingsMap(question.Settings)
	low, high := math.Inf(-1), math.Inf(1)
	switch question.Type {
	case domain.QuestionRating, domain.QuestionScale:
		if number != math.Trunc(number) {
			return false
		}
		low, high = integerBounds(question.Type, map[string]float64{})
	}
	if bound, ok := settingNumberValue(settings, "min"); ok {
		low = bound
	}
	if bound, ok := settingNumberValue(settings, "max"); ok {
		high = bound
	}
	return number >= low && number <= high
}

func validSelectionCount(question domain.FormQuestion, count int) bool {
	settings := questionSettingsMap(question.Settings)
	if low, ok := settingNumberValue(settings, "min_selected"); ok && float64(count) < low {
		return false
	}
	if high, ok := settingNumberValue(settings, "max_selected"); ok && float64(count) > high {
		return false
	}
	return true
}
