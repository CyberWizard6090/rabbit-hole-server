package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func importsOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	result := map[string][]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			result[path] = append(result[path], strings.Trim(spec.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", dir, err)
	}
	if len(result) == 0 {
		t.Fatalf("no Go files found below %s; the test is looking in the wrong place", dir)
	}
	return result
}

func assertNoImports(t *testing.T, dir string, forbiddenPrefixes ...string) {
	t.Helper()
	for file, imports := range importsOf(t, dir) {
		for _, imp := range imports {
			for _, prefix := range forbiddenPrefixes {
				if imp == prefix || strings.HasPrefix(imp, prefix+"/") {
					t.Errorf("%s imports %q, which this layer must not depend on", file, imp)
				}
			}
		}
	}
}

const module = "rabbit-hole-server/internal/"

func TestStorageDetailsStayInsideTheRepositoryLayer(t *testing.T) {
	orm := []string{"gorm.io", "github.com/jackc/pgx", "github.com/golang-migrate"}
	storage := append([]string{module + "persistence", module + "repository"}, orm...)

	t.Run("domain", func(t *testing.T) {
		assertNoImports(t, "../domain", append(storage, module+"service", module+"handler", module+"middleware")...)
	})
	t.Run("handler", func(t *testing.T) { assertNoImports(t, "../handler", append(storage, module+"database")...) })
	t.Run("middleware", func(t *testing.T) { assertNoImports(t, "../middleware", append(storage, module+"database")...) })
	t.Run("http helpers", func(t *testing.T) { assertNoImports(t, "../http", append(storage, module+"database")...) })
	t.Run("dto", func(t *testing.T) { assertNoImports(t, "../dto", storage...) })

	t.Run("service", func(t *testing.T) {
		assertNoImports(t, "../service", append(orm, module+"persistence")...)
	})
}

func TestPersistenceModelsAreIndependentOfBusinessLayers(t *testing.T) {
	assertNoImports(t, "../persistence",
		module+"domain", module+"service", module+"handler", module+"repository", module+"middleware")
}
