// Package apitypes turns docs/openapi.yaml into the TypeScript types of the frontend.
//
// The file is generated, never edited: change the schema, run `make api-types` and commit the
// result. A test fails when the committed file differs from what this package produces, and the
// contract test checks every real response against the same schemas, so the types are true.
package apitypes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Generate returns the TypeScript source for the OpenAPI document at specPath.
func Generate(specPath string) (string, error) {
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", specPath, err)
	}
	var b strings.Builder
	b.WriteString("// Generated from docs/openapi.yaml by `make api-types`. Do not edit.\n")
	b.WriteString("// Every response is checked against the same schemas by the contract test.\n\n")

	names := make([]string, 0, len(doc.Components.Schemas))
	for name := range doc.Components.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeComponent(&b, name, doc.Components.Schemas[name])
	}

	b.WriteString("export interface Endpoints {\n")
	for _, key := range operationKeys(doc) {
		op := key.op
		fmt.Fprintf(&b, "  %q: {\n", key.name)
		if params := parametersOf(doc, key.path, op, "path"); len(params) > 0 {
			fmt.Fprintf(&b, "    params: %s;\n", objectOfParameters(params))
		}
		if params := parametersOf(doc, key.path, op, "query"); len(params) > 0 {
			fmt.Fprintf(&b, "    query?: %s;\n", objectOfParameters(params))
		}
		if body := requestBody(op); body != nil {
			fmt.Fprintf(&b, "    body: %s;\n", tsType(body))
		}
		status, data, paginated := successResponse(op)
		if status != "" {
			fmt.Fprintf(&b, "    status: %s;\n", status)
		}
		if data != nil {
			fmt.Fprintf(&b, "    data: %s;\n", tsType(data))
		}
		if paginated {
			b.WriteString("    pagination: Pagination;\n")
		}
		b.WriteString("  };\n")
	}
	b.WriteString("}\n")
	return b.String(), nil
}

type namedOperation struct {
	name string
	path string
	op   *openapi3.Operation
}

func operationKeys(doc *openapi3.T) []namedOperation {
	var out []namedOperation
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			out = append(out, namedOperation{name: method + " " + path, path: path, op: op})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func parametersOf(doc *openapi3.T, path string, op *openapi3.Operation, in string) []*openapi3.Parameter {
	var params []*openapi3.Parameter
	if item := doc.Paths.Find(path); item != nil {
		for _, p := range item.Parameters {
			if p.Value != nil && p.Value.In == in {
				params = append(params, p.Value)
			}
		}
	}
	for _, p := range op.Parameters {
		if p.Value != nil && p.Value.In == in {
			params = append(params, p.Value)
		}
	}
	sort.Slice(params, func(i, j int) bool { return params[i].Name < params[j].Name })
	return params
}

func objectOfParameters(params []*openapi3.Parameter) string {
	var parts []string
	for _, p := range params {
		optional := ""
		if !p.Required {
			optional = "?"
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", propertyName(p.Name), optional, tsType(p.Schema)))
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func requestBody(op *openapi3.Operation) *openapi3.SchemaRef {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	if media := op.RequestBody.Value.Content.Get("application/json"); media != nil {
		return media.Schema
	}
	return nil
}

// successResponse returns the lowest 2xx status, the schema of its `data` and whether the
// response is paginated.
func successResponse(op *openapi3.Operation) (string, *openapi3.SchemaRef, bool) {
	if op.Responses == nil {
		return "", nil, false
	}
	var statuses []string
	for status := range op.Responses.Map() {
		if strings.HasPrefix(status, "2") {
			statuses = append(statuses, status)
		}
	}
	sort.Strings(statuses)
	if len(statuses) == 0 {
		return "", nil, false
	}
	resp := op.Responses.Value(statuses[0])
	if resp == nil || resp.Value == nil {
		return statuses[0], nil, false
	}
	media := resp.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return statuses[0], nil, false
	}
	props := media.Schema.Value.Properties
	return statuses[0], props["data"], props["pagination"] != nil
}

func writeComponent(b *strings.Builder, name string, ref *openapi3.SchemaRef) {
	s := ref.Value
	if description := firstLine(s.Description); description != "" {
		fmt.Fprintf(b, "/** %s */\n", strings.ReplaceAll(description, "*/", "* /"))
	}
	if s.Type != nil && s.Type.Is("object") && len(s.AllOf) == 0 && len(s.Properties) > 0 && !s.Nullable {
		fmt.Fprintf(b, "export interface %s %s\n\n", name, objectBody(s, "  "))
		return
	}
	fmt.Fprintf(b, "export type %s = %s;\n\n", name, tsType(ref))
}

func objectBody(s *openapi3.Schema, indent string) string {
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for _, k := range keys {
		prop := s.Properties[k]
		if description := firstLine(prop.Value.Description); description != "" && prop.Ref == "" {
			fmt.Fprintf(&b, "%s/** %s */\n", indent, strings.ReplaceAll(description, "*/", "* /"))
		}
		optional := "?"
		if required[k] {
			optional = ""
		}
		fmt.Fprintf(&b, "%s%s%s: %s;\n", indent, propertyName(k), optional, tsType(prop))
	}
	if s.AdditionalProperties.Has != nil && *s.AdditionalProperties.Has {
		fmt.Fprintf(&b, "%s[key: string]: unknown;\n", indent)
	}
	b.WriteString(strings.TrimSuffix(indent, "  ") + "}")
	return b.String()
}

func tsType(ref *openapi3.SchemaRef) string {
	if ref == nil {
		return "unknown"
	}
	if ref.Ref != "" {
		return componentName(ref.Ref)
	}
	s := ref.Value
	t := baseType(s)
	if s.Nullable && t != "unknown" {
		return t + " | null"
	}
	return t
}

func baseType(s *openapi3.Schema) string {
	if len(s.AllOf) > 0 {
		parts := make([]string, 0, len(s.AllOf))
		for _, part := range s.AllOf {
			parts = append(parts, tsType(part))
		}
		return strings.Join(parts, " & ")
	}
	if len(s.Enum) > 0 {
		lits := make([]string, 0, len(s.Enum))
		for _, v := range s.Enum {
			switch value := v.(type) {
			case string:
				lits = append(lits, fmt.Sprintf("%q", value))
			default:
				lits = append(lits, fmt.Sprint(value))
			}
		}
		return strings.Join(lits, " | ")
	}
	if s.Type == nil {
		return "unknown"
	}
	switch {
	case s.Type.Is("string"):
		return "string"
	case s.Type.Is("integer"), s.Type.Is("number"):
		return "number"
	case s.Type.Is("boolean"):
		return "boolean"
	case s.Type.Is("array"):
		item := tsType(s.Items)
		if strings.ContainsAny(item, " |&") {
			item = "(" + item + ")"
		}
		return item + "[]"
	case s.Type.Is("object"):
		if len(s.Properties) == 0 {
			if s.AdditionalProperties.Schema != nil {
				return "Record<string, " + tsType(s.AdditionalProperties.Schema) + ">"
			}
			return "Record<string, unknown>"
		}
		return objectBody(s, "    ")
	}
	return "unknown"
}

func componentName(ref string) string {
	return ref[strings.LastIndex(ref, "/")+1:]
}

func propertyName(name string) string {
	for i, r := range name {
		letter := r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return fmt.Sprintf("%q", name)
		}
	}
	return name
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
