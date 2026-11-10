package service

import (
	"bytes"
	"encoding/json"
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
		return domain.NewFormValidationError("settings", "INVALID_TYPE").WithParams(map[string]any{"expected": "object"})
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
			return domain.NewFormValidationError("settings."+key, "NOT_SUPPORTED").WithParams(map[string]any{"question_type": string(questionType)})
		}
		number, problem := validateSettingValue(spec, value)
		if problem != nil {
			return problem.WithPrefix("settings." + key)
		}
		if spec.kind != settingString {
			numbers[key] = number
		}
	}
	return validateSettingRelations(questionType, numbers, optionCount)
}

func validateSettingValue(spec settingSpec, value any) (float64, *domain.FormValidationError) {
	problem := func(reason string, params map[string]any) (float64, *domain.FormValidationError) {
		return 0, domain.NewFormValidationError("", reason).WithParams(params)
	}
	if spec.kind == settingString {
		text, ok := value.(string)
		if !ok {
			return problem("INVALID_TYPE", map[string]any{"expected": "string"})
		}
		if utf8.RuneCountInString(text) > int(spec.max) {
			return problem("TOO_LONG", map[string]any{"max": int(spec.max)})
		}
		return 0, nil
	}
	number, ok := questionNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return problem("INVALID_TYPE", map[string]any{"expected": "number"})
	}
	if spec.kind == settingInteger && number != math.Trunc(number) {
		return problem("INVALID_TYPE", map[string]any{"expected": "integer"})
	}
	if number < spec.min || number > spec.max {
		return problem("OUT_OF_RANGE", map[string]any{"min": spec.min, "max": spec.max})
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
			return domain.NewFormValidationError("settings.min_length", "EXCEEDS_FIELD").WithParams(map[string]any{"other": "max_length"})
		}
	case domain.QuestionNumber:
		low, hasLow := numbers["min"]
		high, hasHigh := numbers["max"]
		if hasLow && hasHigh && low > high {
			return domain.NewFormValidationError("settings.min", "EXCEEDS_FIELD").WithParams(map[string]any{"other": "max"})
		}
	case domain.QuestionMultipleChoice:
		low, hasLow := numbers["min_selected"]
		high, hasHigh := numbers["max_selected"]
		if hasLow && hasHigh && low > high {
			return domain.NewFormValidationError("settings.min_selected", "EXCEEDS_FIELD").WithParams(map[string]any{"other": "max_selected"})
		}
		if hasHigh && int(high) > optionCount {
			return domain.NewFormValidationError("settings.max_selected", "EXCEEDS_OPTIONS").WithParams(map[string]any{"max": optionCount})
		}
		if hasLow && int(low) > optionCount {
			return domain.NewFormValidationError("settings.min_selected", "EXCEEDS_OPTIONS").WithParams(map[string]any{"max": optionCount})
		}
	case domain.QuestionRating, domain.QuestionScale:
		low, high := integerBounds(questionType, numbers)
		if low >= high {
			return domain.NewFormValidationError("settings.min", "NOT_LESS_THAN_FIELD").WithParams(map[string]any{"other": "max"})
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
