package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var ownProfileTypes = map[string]bool{"User": true}

func isRequestShape(name string) bool {
	for _, suffix := range []string{"Request", "Params", "Input"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func mentionsUser(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if v.Name == "User" {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "User" {
				found = true
			}
		}
		return true
	})
	return found
}

func TestOnlyTheOwnProfileTypeCarriesEmailAddresses(t *testing.T) {
	roots := []string{"../domain", "../dto", "../handler", "../service"}
	checked := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				structType, ok := spec.Type.(*ast.StructType)
				if !ok {
					return true
				}
				checked++
				for _, field := range structType.Fields.List {
					if !ownProfileTypes[spec.Name.Name] && mentionsUser(field.Type) {
						t.Errorf("%s: %s embeds the full user type; use domain.CommentUser", path, spec.Name.Name)
					}
					if field.Tag == nil || ownProfileTypes[spec.Name.Name] || isRequestShape(spec.Name.Name) {
						continue
					}
					tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
					if strings.Contains(strings.ToLower(tag), "email") {
						t.Errorf("%s: %s exposes %q in JSON; only the own profile type may carry e-mail addresses", path, spec.Name.Name, tag)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
	if checked == 0 {
		t.Fatal("no struct types were inspected; the test is looking in the wrong place")
	}
}
