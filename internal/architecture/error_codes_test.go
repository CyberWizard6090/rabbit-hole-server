package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The frontend translates API errors by code, so the set of codes is part of
// the contract. docs/error-codes.md lists every code the server can send; this
// test fails when the code and the document drift apart.
//
// After adding or removing a code run:  UPDATE_ERROR_CODES=1 go test ./internal/architecture

const (
	errorCodesDoc   = "../../docs/error-codes.md"
	errorCodesStart = "<!-- codes:start -->"
	errorCodesEnd   = "<!-- codes:end -->"
)

var errorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$`)

// errorCodeDirs are the packages that produce API errors.
var errorCodeDirs = []string{
	"../handler", "../service", "../http", "../middleware",
	"../domain", "../repository", "../realtime", "../app",
}

func collectErrorCodes(t *testing.T) []string {
	t.Helper()
	seen := map[string]struct{}{}
	for _, dir := range errorCodeDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err == nil && errorCodePattern.MatchString(value) {
					seen[value] = struct{}{}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no error codes found; the test is looking in the wrong place")
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func TestErrorCodesAreDocumented(t *testing.T) {
	codes := collectErrorCodes(t)

	var block strings.Builder
	block.WriteString(errorCodesStart + "\n")
	for _, code := range codes {
		block.WriteString("- `" + code + "`\n")
	}
	block.WriteString(errorCodesEnd)

	raw, err := os.ReadFile(errorCodesDoc)
	if err != nil {
		t.Fatalf("read %s: %v", errorCodesDoc, err)
	}
	doc := string(raw)
	start, end := strings.Index(doc, errorCodesStart), strings.Index(doc, errorCodesEnd)
	if start < 0 || end < start {
		t.Fatalf("%s has no %s ... %s block", errorCodesDoc, errorCodesStart, errorCodesEnd)
	}
	updated := doc[:start] + block.String() + doc[end+len(errorCodesEnd):]

	if updated == doc {
		return
	}
	if os.Getenv("UPDATE_ERROR_CODES") != "" {
		if err := os.WriteFile(errorCodesDoc, []byte(updated), 0o644); err != nil {
			t.Fatalf("write %s: %v", errorCodesDoc, err)
		}
		return
	}
	t.Fatalf("docs/error-codes.md is out of date with the error codes in the source.\n"+
		"Run: UPDATE_ERROR_CODES=1 go test ./internal/architecture\n(%d codes in source)", len(codes))
}
