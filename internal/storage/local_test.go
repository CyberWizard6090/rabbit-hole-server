package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "attachment/0123456789abcdef0123456789abcdef"

func readAll(t *testing.T, store Store, key string) string {
	t.Helper()
	body, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLocalPutGetDelete(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := store.Put(ctx, testKey, strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := readAll(t, store, testKey); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if err := store.Put(ctx, testKey, strings.NewReader("replaced"), 8, ""); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := readAll(t, store, testKey); got != "replaced" {
		t.Fatalf("got %q after overwrite", got)
	}

	if err := store.Delete(ctx, testKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, testKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
	if err := store.Delete(ctx, testKey); err != nil {
		t.Fatalf("deleting a missing object must succeed: %v", err)
	}
}

func TestLocalRejectsUnsafeKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"", "../escape", "attachment/../../escape", "/etc/passwd", "attachment/short",
		"attachment/0123456789ABCDEF0123456789abcdef", "a/b/0123456789abcdef0123456789abcdef", "attachment\\0123456789abcdef0123456789abcdef",
	} {
		if err := store.Put(context.Background(), key, strings.NewReader("x"), 1, ""); err == nil {
			t.Errorf("Put accepted key %q", key)
		}
		if _, err := store.Get(context.Background(), key); err == nil {
			t.Errorf("Get accepted key %q", key)
		}
		if err := store.Delete(context.Background(), key); err == nil {
			t.Errorf("Delete accepted key %q", key)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("something was written outside the store: %v", entries)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection lost") }

func TestLocalFailedPutLeavesNoPartialFile(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), testKey, failingReader{}, 10, ""); err == nil {
		t.Fatal("expected the read error")
	}
	if _, err := store.Get(context.Background(), testKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("object exists after a failed upload: %v", err)
	}
	var leftovers []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			leftovers = append(leftovers, path)
		}
		return nil
	})
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestLocalPing(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := os.RemoveAll(store.root); err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(context.Background()); err == nil {
		t.Fatal("Ping must fail when the directory is gone")
	}
}

func TestLocalAcceptsUnderscoreInTheKeyKind(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "workspace_avatar/0123456789abcdef0123456789abcdef"
	if err := store.Put(context.Background(), key, strings.NewReader("x"), 1, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := readAll(t, store, key); got != "x" {
		t.Fatalf("got %q", got)
	}
}
