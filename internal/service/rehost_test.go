package service

import "testing"

func TestDeclaredImageType(t *testing.T) {
	for header, want := range map[string]bool{
		"":                           true,
		"image/png":                  true,
		"image/jpeg; charset=binary": true,
		"IMAGE/WebP":                 true,
		"application/octet-stream":   true,
		"binary/octet-stream":        true,
		"image/svg+xml":              false,
		"text/html":                  false,
		"text/html; charset=utf-8":   false,
		"application/json":           false,
		"application/pdf":            false,
	} {
		if got := declaredImageType(header); got != want {
			t.Errorf("declaredImageType(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestRehostFilename(t *testing.T) {
	for raw, want := range map[string]string{
		"https://example.com/a/b/cat.png?x=1": "cat.png",
		"https://example.com":                 "remote-image",
		"https://example.com/a%2F..%2Fb.png":  "b.png",
	} {
		if got := rehostFilename(raw); got != want {
			t.Errorf("rehostFilename(%q) = %q, want %q", raw, got, want)
		}
	}
}
