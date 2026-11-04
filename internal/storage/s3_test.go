package storage

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}, types: map[string]string{}}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != "files" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if key == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	switch r.Method {
	case http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		f.objects[key] = data
		f.types[key] = r.Header.Get("Content-Type")
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		data, ok := f.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
			}
			return
		}
		w.Header().Set("Content-Length", itoa(len(data)))
		w.Header().Set("Content-Type", f.types[key])
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func newTestS3(t *testing.T, bucket string) (*S3, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	server := httptest.NewTLSServer(fake)
	t.Cleanup(server.Close)

	store, err := NewS3(context.Background(), S3Options{
		Endpoint:  strings.TrimPrefix(server.URL, "https://"),
		Bucket:    bucket,
		AccessKey: "access", SecretKey: "secret", Region: "us-east-1", UseSSL: true,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return store, fake
}

func TestS3PutGetDelete(t *testing.T) {
	store, fake := newTestS3(t, "files")
	ctx := context.Background()

	if err := store.Put(ctx, testKey, strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := string(fake.objects[testKey]); got != "hello" || fake.types[testKey] != "text/plain" {
		t.Fatalf("stored %q as %q", got, fake.types[testKey])
	}
	if got := readAll(t, store, testKey); got != "hello" {
		t.Fatalf("Get = %q", got)
	}
	if err := store.Delete(ctx, testKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, testKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}

func TestS3FailsWhenTheBucketIsMissing(t *testing.T) {
	fake := newFakeS3()
	server := httptest.NewTLSServer(fake)
	defer server.Close()

	_, err := NewS3(context.Background(), S3Options{
		Endpoint: strings.TrimPrefix(server.URL, "https://"), Bucket: "other",
		AccessKey: "a", SecretKey: "s", Region: "us-east-1", UseSSL: true,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	})
	if err == nil {
		t.Fatal("NewS3 must fail for a bucket that does not exist")
	}
}
