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
	if _, wrapped := binding.Validator.(*rootedValidator); !wrapped {
		binding.Validator = &rootedValidator{inner: binding.Validator}
	}
}

type rootedValidator struct {
	inner binding.StructValidator
}

func (v *rootedValidator) ValidateStruct(obj any) error {
	err := v.inner.ValidateStruct(obj)
	if err == nil {
		return nil
	}
	return &rootedError{err: err, root: reflect.TypeOf(obj)}
}

func (v *rootedValidator) Engine() any { return v.inner.Engine() }

type rootedError struct {
	err  error
	root reflect.Type
}

func (e *rootedError) Error() string { return e.err.Error() }

func (e *rootedError) Unwrap() error { return e.err }

func structType(t reflect.Type) reflect.Type {
	for t != nil {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		case reflect.Struct:
			return t
		default:
			return nil
		}
	}
	return nil
}

func parentStruct(root reflect.Type, fe validator.FieldError) reflect.Type {
	current := structType(root)
	segments := strings.Split(fe.StructNamespace(), ".")
	if len(segments) < 2 {
		return current
	}
	for _, segment := range segments[1 : len(segments)-1] {
		if current == nil {
			return nil
		}
		name, _, _ := strings.Cut(segment, "[")
		field, ok := current.FieldByName(name)
		if !ok {
			return nil
		}
		current = structType(field.Type)
	}
	return current
}

func referencedWireNames(root reflect.Type, fe validator.FieldError) string {
	names := strings.Fields(fe.Param())
	parent := parentStruct(root, fe)
	if parent == nil {
		return strings.Join(names, " ")
	}
	for i, name := range names {
		if field, ok := parent.FieldByName(name); ok {
			if wire := wireName(field); wire != "" {
				names[i] = wire
			}
		}
	}
	return strings.Join(names, " ")
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

func fieldDetail(fe validator.FieldError, root reflect.Type) httperrors.Detail {
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
		detail.Params = map[string]any{"with": referencedWireNames(root, fe)}
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
	segments := strings.Split(ns, ".")
	kept := segments[:0]
	for _, segment := range segments {
		if segment != "" && segment[0] >= 'A' && segment[0] <= 'Z' {
			continue
		}
		kept = append(kept, segment)
	}
	ns = strings.Join(kept, ".")
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
