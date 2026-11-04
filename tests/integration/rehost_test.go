package integration

import (
	"bytes"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/safehttp"
)

func rehostPath(workspaceID uint) string {
	return "/api/v1/workspaces/" + itoa(workspaceID) + "/images/rehost"
}

func TestRehostImage_ThroughTheAPI(t *testing.T) {
	remote := solidPNG(t, 12, 9, color.NRGBA{R: 200, A: 255})
	mux := http.NewServeMux()
	mux.HandleFunc("/ok.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(remote)
	})
	mux.HandleFunc("/octet", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(remote)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok.png", http.StatusFound) })
	mux.HandleFunc("/to-metadata", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	mux.HandleFunc("/html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("<html><script>alert(1)</script></html>"))
	})
	mux.HandleFunc("/svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	})
	mux.HandleFunc("/png-as-html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(remote)
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "image/png") })
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal database password is hunter2", http.StatusInternalServerError)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(append(append([]byte{}, remote...), bytes.Repeat([]byte{0}, 5000)...))
	})
	remoteServer := httptest.NewServer(mux)
	t.Cleanup(remoteServer.Close)

	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Storage.MaxRehostBytes = 2000 },
		app.WithRemoteFetcher(safehttp.New(safehttp.AllowLoopbackForTests())))
	owner, _, _, guestToken, outsiderToken := filesFixture(t, tc)
	path := rehostPath(owner.workspaceID)

	for _, name := range []string{"/ok.png", "/octet", "/redirect"} {
		rec := request(t, tc.Router, http.MethodPost, path, owner.token, map[string]any{"url": remoteServer.URL + name})
		if rec.Code != http.StatusCreated {
			t.Fatalf("rehost %s: %d: %s", name, rec.Code, rec.Body.String())
		}
		data := dataObject(t, rec)
		url, _ := data["url"].(string)
		if !strings.HasPrefix(url, "/api/v1/images/") || data["content_type"] != "image/png" || data["size"] != float64(len(remote)) {
			t.Fatalf("rehost %s result = %v", name, data)
		}
		served := request(t, tc.Router, http.MethodGet, url, "", nil)
		if served.Code != http.StatusOK || !bytes.Equal(served.Body.Bytes(), remote) {
			t.Fatalf("serve %s: %d", name, served.Code)
		}
	}

	var stored int64
	tc.DB.Table("files").Where("kind = ?", "image").Count(&stored)
	if stored != 3 {
		t.Fatalf("stored images = %d, want 3", stored)
	}

	for name, target := range map[string]string{
		"metadata redirect": remoteServer.URL + "/to-metadata",
		"html as image":     remoteServer.URL + "/html",
		"svg":               remoteServer.URL + "/svg",
		"png as html":       remoteServer.URL + "/png-as-html",
		"empty":             remoteServer.URL + "/empty",
		"remote error":      remoteServer.URL + "/missing",
		"too big":           remoteServer.URL + "/big",
		"not found":         remoteServer.URL + "/nothing",
		"file scheme":       "file:///etc/passwd",
		"userinfo":          "http://user:pw@example.com/a.png",
		"localhost":         "http://localhost/a.png",
		"link local":        "http://169.254.169.254/latest/meta-data/",
		"private literal":   "http://10.0.0.1/a.png",
		"numeric host":      "http://2130706433/a.png",
		"mapped ipv6":       "http://[::ffff:127.0.0.1]/a.png",
		"odd port":          "http://example.com:22/",
	} {
		rec := request(t, tc.Router, http.MethodPost, path, owner.token, map[string]any{"url": target})
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "REHOST_REFUSED") {
			t.Errorf("%s: %d: %s", name, rec.Code, rec.Body.String())
			continue
		}
		for _, leak := range []string{"hunter2", "169.254", "127.0.0.1", "example.com", "10.0.0.1"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("%s: the answer leaks %q: %s", name, leak, rec.Body.String())
			}
		}
	}
	tc.DB.Table("files").Where("kind = ?", "image").Count(&stored)
	if stored != 3 {
		t.Fatalf("refused fetches stored files: %d", stored)
	}

	for name, body := range map[string]map[string]any{
		"missing url": {},
		"empty url":   {"url": ""},
		"long url":    {"url": "https://example.com/" + strings.Repeat("a", 3000)},
	} {
		if rec := request(t, tc.Router, http.MethodPost, path, owner.token, body); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}

	body := map[string]any{"url": remoteServer.URL + "/ok.png"}
	if rec := request(t, tc.Router, http.MethodPost, path, guestToken, body); rec.Code != http.StatusForbidden {
		t.Fatalf("guest: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, path, outsiderToken, body); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, path, "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: expected 401, got %d", rec.Code)
	}
}

func TestRehostImage_DefaultServerRefusesLoopback(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(t, 4, 4, color.NRGBA{A: 255}))
	}))
	t.Cleanup(remote.Close)

	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "rehost-default")
	rec := request(t, tc.Router, http.MethodPost, rehostPath(owner.workspaceID), owner.token, map[string]any{"url": remote.URL + "/a.png"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("loopback remote: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}
