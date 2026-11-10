package safehttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedResolver struct {
	mu      sync.Mutex
	answers map[string][][]netip.Addr
	calls   map[string]int
}

func (r *scriptedResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	seq, ok := r.answers[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	i := r.calls[host]
	r.calls[host]++
	if i >= len(seq) {
		i = len(seq) - 1
	}
	return seq[i], nil
}

func addrs(values ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(values))
	for _, v := range values {
		out = append(out, netip.MustParseAddr(v))
	}
	return out
}

type dialRecorder struct {
	mu     sync.Mutex
	target string
	dialed []string
}

func (d *dialRecorder) dial(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.dialed = append(d.dialed, address)
	d.mu.Unlock()
	var nd net.Dialer
	return nd.DialContext(ctx, network, d.target)
}

func loopbackClient(opts ...Option) *Client {
	return New(append([]Option{AllowLoopbackForTests()}, opts...)...)
}

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchReturnsBodyAndContentType(t *testing.T) {
	var gotUA, gotEnc, gotCookie string
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotEnc, gotCookie = r.UserAgent(), r.Header.Get("Accept-Encoding"), r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("hello"))
	})
	res, err := loopbackClient().Fetch(t.Context(), url+"/a.png", 100, "image/*")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != "hello" || res.ContentType != "image/png" {
		t.Fatalf("result = %q %q", res.Body, res.ContentType)
	}
	if gotUA != DefaultUserAgent || gotEnc != "identity" || gotCookie != "" {
		t.Fatalf("headers ua=%q enc=%q cookie=%q", gotUA, gotEnc, gotCookie)
	}
}

func TestFetchBodyExactlyAtTheCap(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bytes.Repeat([]byte("a"), 100)) })
	if _, err := loopbackClient().Fetch(t.Context(), url, 100, ""); err != nil {
		t.Fatalf("exactly at the cap: %v", err)
	}
	if _, err := loopbackClient().Fetch(t.Context(), url, 99, ""); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("one over: %v", err)
	}
}

func TestFetchCapIsEnforcedWhileStreaming(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("a"), 4096)
		for i := 0; i < 1000; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	})
	if _, err := loopbackClient().Fetch(t.Context(), url, 10_000, ""); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("chunked oversize: %v", err)
	}
}

func TestFetchRefusesDeclaredOversize(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		_, _ = w.Write([]byte("tiny"))
	})
	if _, err := loopbackClient().Fetch(t.Context(), url, 1000, ""); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("declared oversize: %v", err)
	}
}

func TestFetchSlowBodyTimesOut(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("a"))
		w.(http.Flusher).Flush()
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	start := time.Now()
	_, err := loopbackClient(WithTimeout(300*time.Millisecond)).Fetch(t.Context(), url, 1000, "")
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("slow body: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestFetchSlowHeadersTimeOut(t *testing.T) {
	url := serve(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	if _, err := loopbackClient(WithTimeout(300*time.Millisecond)).Fetch(t.Context(), url, 1000, ""); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("slow headers: %v", err)
	}
}

func TestFetchRefusesCompressedResponses(t *testing.T) {
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, _ = zw.Write(bytes.Repeat([]byte("\x00"), 8<<20))
	_ = zw.Close()
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(packed.Bytes())
	})
	if _, err := loopbackClient().Fetch(t.Context(), url, 1<<20, ""); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("gzip bomb: %v", err)
	}
}

func TestFetchDoesNotDecompressUnannouncedGzip(t *testing.T) {
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, _ = zw.Write(bytes.Repeat([]byte("\x00"), 5<<20))
	_ = zw.Close()
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(packed.Bytes()) })
	res, err := loopbackClient().Fetch(t.Context(), url, 1<<20, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Body, packed.Bytes()) {
		t.Fatal("the body must be returned exactly as received")
	}
}

func TestFetchRejectsNon200(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusNoContent, http.StatusPartialContent, http.StatusForbidden} {
		url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("secret internal error text"))
		})
		_, err := loopbackClient().Fetch(t.Context(), url, 1000, "")
		if !errors.Is(err, ErrBadResponse) {
			t.Errorf("status %d: %v", code, err)
		} else if strings.Contains(err.Error(), "secret") {
			t.Errorf("status %d leaks the remote text", code)
		}
	}
}

func TestFetchRedirectToForbiddenTargets(t *testing.T) {
	for _, target := range []string{
		"http://10.0.0.1/a.png",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fd00::1]/",
		"http://[::ffff:10.0.0.1]/",
		"http://0.0.0.0/",
		"http://localhost/a.png",
		"http://0x7f.1/",
		"http://2130706433/",
		"http://user:pw@example.com/",
		"ftp://example.com/a.png",
		"file:///etc/passwd",
	} {
		url := serve(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusFound)
		})
		_, err := loopbackClient(WithAllowedPorts(80, 443)).Fetch(t.Context(), url, 1000, "")
		if !errors.Is(err, ErrForbiddenAddress) && !errors.Is(err, ErrForbiddenURL) {
			t.Errorf("redirect to %q: %v", target, err)
		}
	}
}

func TestFetchRedirectLoopAndLimit(t *testing.T) {
	var hits int
	var mu sync.Mutex
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	})
	if _, err := loopbackClient().Fetch(t.Context(), url+"/a", 1000, ""); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("loop: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != DefaultMaxRedirects+1 {
		t.Fatalf("hits = %d, want %d", hits, DefaultMaxRedirects+1)
	}
}

func TestFetchFollowsUpToThreeRedirects(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1":
			http.Redirect(w, r, "/2", http.StatusFound)
		case "/2":
			http.Redirect(w, r, "/3", http.StatusMovedPermanently)
		case "/3":
			http.Redirect(w, r, "/4", http.StatusTemporaryRedirect)
		default:
			_, _ = w.Write([]byte("done"))
		}
	})
	res, err := loopbackClient().Fetch(t.Context(), url+"/1", 100, "")
	if err != nil || string(res.Body) != "done" {
		t.Fatalf("three redirects: %v", err)
	}
	if _, err := loopbackClient(WithMaxRedirects(2)).Fetch(t.Context(), url+"/1", 100, ""); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("limit of two: %v", err)
	}
}

func TestFetchRefusesHTTPSDowngrade(t *testing.T) {
	plain := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("plain")) })
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			_, _ = w.Write([]byte("secure"))
			return
		}
		http.Redirect(w, r, plain, http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	tlsCfg := &tls.Config{InsecureSkipVerify: true}
	c := loopbackClient(WithTLSConfig(tlsCfg))
	if _, err := c.Fetch(t.Context(), secure.URL+"/ok", 100, ""); err != nil {
		t.Fatalf("https fetch: %v", err)
	}
	if _, err := c.Fetch(t.Context(), secure.URL+"/down", 100, ""); !errors.Is(err, ErrForbiddenURL) {
		t.Fatalf("downgrade: %v", err)
	}
}

func TestFetchDoesNotKeepCookies(t *testing.T) {
	var second string
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		second = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("ok"))
	})
	if _, err := loopbackClient().Fetch(t.Context(), url+"/start", 100, ""); err != nil {
		t.Fatal(err)
	}
	if second != "" {
		t.Fatalf("cookie sent on the redirect hop: %q", second)
	}
}

func TestFetchIgnoresEnvironmentProxy(t *testing.T) {
	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHit = true
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("direct")) })
	res, err := loopbackClient().Fetch(t.Context(), url, 100, "")
	if err != nil || string(res.Body) != "direct" || proxyHit {
		t.Fatalf("err=%v proxyHit=%v", err, proxyHit)
	}
}

func TestDefaultClientRefusesLoopbackServer(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	if _, err := New().Fetch(t.Context(), url, 100, ""); err == nil {
		t.Fatal("the default client must not reach a loopback server")
	}
}

func publicClient(t *testing.T, res *scriptedResolver, target string) (*Client, *dialRecorder) {
	t.Helper()
	rec := &dialRecorder{target: target}
	return New(WithResolver(res), WithDialer(rec.dial)), rec
}

func serverAddr(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestDialsTheValidatedAddressDirectly(t *testing.T) {
	target := serverAddr(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "images.example.com" {
			t.Errorf("Host = %q", r.Host)
		}
		_, _ = w.Write([]byte("ok"))
	})
	res := &scriptedResolver{answers: map[string][][]netip.Addr{"images.example.com": {addrs("93.184.216.34")}}}
	c, rec := publicClient(t, res, target)
	if _, err := c.Fetch(t.Context(), "http://images.example.com/a.png", 100, ""); err != nil {
		t.Fatal(err)
	}
	if len(rec.dialed) != 1 || rec.dialed[0] != "93.184.216.34:80" {
		t.Fatalf("dialed %v", rec.dialed)
	}
	if res.calls["images.example.com"] != 1 {
		t.Fatalf("resolver called %d times, want exactly one per connection", res.calls["images.example.com"])
	}
}

func TestDNSRebindingPublicThenPrivate(t *testing.T) {
	target := serverAddr(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	res := &scriptedResolver{answers: map[string][][]netip.Addr{
		"rebind.example.com": {addrs("93.184.216.34"), addrs("127.0.0.1"), addrs("127.0.0.1")},
	}}
	c, rec := publicClient(t, res, target)

	if _, err := c.Fetch(t.Context(), "http://rebind.example.com/a.png", 100, ""); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if len(rec.dialed) != 1 || rec.dialed[0] != "93.184.216.34:80" {
		t.Fatalf("first fetch dialed %v", rec.dialed)
	}
	if _, err := c.Fetch(t.Context(), "http://rebind.example.com/a.png", 100, ""); !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("second fetch after the rebind: %v", err)
	}
	if len(rec.dialed) != 1 {
		t.Fatalf("a private address was dialed: %v", rec.dialed)
	}
}

func TestDNSRebindingOnRedirectHop(t *testing.T) {
	target := serverAddr(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "first.example.com" {
			http.Redirect(w, r, "http://second.example.com/b.png", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	res := &scriptedResolver{answers: map[string][][]netip.Addr{
		"first.example.com":  {addrs("93.184.216.34")},
		"second.example.com": {addrs("169.254.169.254")},
	}}
	c, rec := publicClient(t, res, target)
	if _, err := c.Fetch(t.Context(), "http://first.example.com/a.png", 100, ""); !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("redirect to a name resolving to the metadata address: %v", err)
	}
	if len(rec.dialed) != 1 {
		t.Fatalf("dialed %v", rec.dialed)
	}
}

func TestMixedAnswersAreRefusedEntirely(t *testing.T) {
	target := serverAddr(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	for _, mixed := range [][]netip.Addr{
		addrs("93.184.216.34", "10.0.0.1"),
		addrs("10.0.0.1", "93.184.216.34"),
		addrs("2606:4700:4700::1111", "::1"),
		addrs("93.184.216.34", "::ffff:127.0.0.1"),
	} {
		res := &scriptedResolver{answers: map[string][][]netip.Addr{"mixed.example.com": {mixed}}}
		c, rec := publicClient(t, res, target)
		if _, err := c.Fetch(t.Context(), "http://mixed.example.com/", 100, ""); !errors.Is(err, ErrForbiddenAddress) {
			t.Errorf("%v: %v", mixed, err)
		}
		if len(rec.dialed) != 0 {
			t.Errorf("%v: dialed %v", mixed, rec.dialed)
		}
	}
}

func TestResolverFailureIsUnreachable(t *testing.T) {
	c := New(WithResolver(&scriptedResolver{answers: map[string][][]netip.Addr{}}))
	if _, err := c.Fetch(t.Context(), "http://nowhere.example.com/", 100, ""); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("unresolvable: %v", err)
	}
}

func TestDefaultPortsAndCustomAllowlist(t *testing.T) {
	c := New()
	for _, raw := range []string{"http://example.com:8080/", "https://example.com:8443/", "http://example.com:25/"} {
		if _, err := c.ValidateURL(raw); err == nil {
			t.Errorf("%s accepted with the default ports", raw)
		}
	}
	custom := New(WithAllowedPorts(8443))
	if _, err := custom.ValidateURL("https://example.com:8443/"); err != nil {
		t.Errorf("custom port: %v", err)
	}
	if _, err := custom.ValidateURL("https://example.com/"); err == nil {
		t.Error("443 should not be allowed when the list is replaced")
	}
}
