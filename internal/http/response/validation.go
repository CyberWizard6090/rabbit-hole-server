package response

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	httperrors "rabbit-hole-server/internal/http/errors"
)

func RegisterValidatorFieldNames() {
	engine, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	engine.RegisterTagNameFunc(wireName)
}

func wireName(f reflect.StructField) string {
	for _, key := range []string{"json", "form", "uri"} {
		name := strings.SplitN(f.Tag.Get(key), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	return ""
}

func fieldDetail(fe validator.FieldError) httperrors.Detail {
	detail := httperrors.Detail{Field: fieldPath(fe)}
	lengthLike := false
	switch fe.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		lengthLike = true
	}
	collection := fe.Kind() == reflect.Slice || fe.Kind() == reflect.Array || fe.Kind() == reflect.Map

	switch fe.Tag() {
	case "required", "required_with", "required_without", "required_if", "required_unless":
		detail.Code = "REQUIRED"
	case "email":
		detail.Code = "INVALID_EMAIL"
	case "min", "gte":
		detail.Code = lowerBoundCode(lengthLike, collection)
		detail.Params = map[string]any{"min": number(fe.Param())}
	case "gt":
		detail.Code = lowerBoundCode(lengthLike, collection)
		if lengthLike {
			if n, err := strconv.ParseInt(fe.Param(), 10, 64); err == nil {
				detail.Params = map[string]any{"min": n + 1}
				break
			}
		}
		detail.Params = map[string]any{"min": number(fe.Param()), "exclusive": true}
	case "max", "lte":
		detail.Code = upperBoundCode(lengthLike, collection)
		detail.Params = map[string]any{"max": number(fe.Param())}
	case "len":
		detail.Code = "INVALID_LENGTH"
		detail.Params = map[string]any{"length": number(fe.Param())}
	case "oneof":
		detail.Code = "INVALID_CHOICE"
		detail.Params = map[string]any{"allowed": strings.Fields(fe.Param())}
	case "hexcolor":
		detail.Code = "INVALID_COLOR"
	case "timezone":
		detail.Code = "INVALID_TIMEZONE"
	case "excluded_with":
		detail.Code = "CONFLICTING_FIELD"
		detail.Params = map[string]any{"with": strings.TrimSpace(fe.Param())}
	case "hostname", "hostname|ip", "ip", "alpha":
		detail.Code = "INVALID_FORMAT"
	default:
		detail.Code = "INVALID_VALUE"
	}
	return detail
}

func lowerBoundCode(lengthLike, collection bool) string {
	switch {
	case collection:
		return "TOO_FEW"
	case lengthLike:
		return "TOO_SHORT"
	default:
		return "TOO_SMALL"
	}
}

func upperBoundCode(lengthLike, collection bool) string {
	switch {
	case collection:
		return "TOO_MANY"
	case lengthLike:
		return "TOO_LONG"
	default:
		return "TOO_LARGE"
	}
}

func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if i := strings.Index(ns, "."); i >= 0 {
		ns = ns[i+1:]
	}
	if ns == "" {
		return "request"
	}
	return ns
}

func number(raw string) any {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	return raw
}

func jsonTypeName(e *json.UnmarshalTypeError) string {
	if e.Type == nil {
		return "unknown"
	}
	switch e.Type.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return "unknown"
	}
}
