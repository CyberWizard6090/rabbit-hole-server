package integration

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// Every response the integration tests receive from the router is checked against
// docs/openapi.yaml, so the contract the frontend generates its types from cannot
// drift from what the server really sends. Operations whose success response is still
// the generic envelope accept any data and are listed by TestContract_UntypedOperations.

var (
	contractOnce   sync.Once
	contractRouter routers.Router
	contractDoc    *openapi3.T
	contractErr    error
)

func loadContract() (*openapi3.T, routers.Router, error) {
	contractOnce.Do(func() {
		raw, err := os.ReadFile("../../docs/openapi.yaml")
		if err != nil {
			contractErr = err
			return
		}
		doc, err := openapi3.NewLoader().LoadFromData(raw)
		if err != nil {
			contractErr = err
			return
		}
		if err := doc.Validate(openapi3.NewLoader().Context); err != nil {
			contractErr = err
			return
		}
		doc.Servers = openapi3.Servers{{URL: "/api/v1"}}
		router, err := gorillamux.NewRouter(doc)
		if err != nil {
			contractErr = err
			return
		}
		contractDoc, contractRouter = doc, router
	})
	return contractDoc, contractRouter, contractErr
}

type contractHandler struct {
	inner http.Handler
	t     *testing.T
}

type teeWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *teeWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *teeWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(p)
	return w.ResponseWriter.Write(p)
}

func (w *teeWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *teeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

func (h contractHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		h.inner.ServeHTTP(w, r)
		return
	}
	tee := &teeWriter{ResponseWriter: w}
	h.inner.ServeHTTP(tee, r)
	h.check(r, tee)
}

func (h contractHandler) check(r *http.Request, w *teeWriter) {
	if w.body.Len() == 0 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		return
	}
	_, router, err := loadContract()
	if err != nil {
		h.t.Errorf("load docs/openapi.yaml: %v", err)
		return
	}
	route, params, err := router.FindRoute(r)
	if err != nil {
		return // not an API route (health checks, unknown paths)
	}
	recordContractSample(route.Method, route.Path, w.status, w.body.Bytes())
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route},
		Status:                 w.status,
		Header:                 w.Header(),
		Options:                &openapi3filter.Options{IncludeResponseStatus: false},
	}
	input.SetBodyBytes(w.body.Bytes())
	if err := openapi3filter.ValidateResponse(r.Context(), input); err != nil {
		h.t.Errorf("response of %s %s (%d) does not match docs/openapi.yaml: %v\nbody: %.600s", r.Method, r.URL.Path, w.status, err, w.body.String())
	}
}

var _ io.Writer = (*teeWriter)(nil)

func TestContract_SpecIsAValidOpenAPIDocument(t *testing.T) {
	if _, _, err := loadContract(); err != nil {
		t.Fatalf("docs/openapi.yaml: %v", err)
	}
}

// Unwrap lets http.ResponseController reach the real connection (read deadlines, flushing).
func (w *teeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

var (
	sampleMu   sync.Mutex
	sampleSeen = map[string]bool{}
)

// recordContractSample appends one real response per operation and status to the file named by
// CONTRACT_SAMPLES. It is how a response schema is written from facts: run the suite with the
// variable set, then read the samples of the operation being described.
func recordContractSample(method, path string, status int, body []byte) {
	file := os.Getenv("CONTRACT_SAMPLES")
	if file == "" {
		return
	}
	key := method + " " + path + " " + http.StatusText(status)
	sampleMu.Lock()
	defer sampleMu.Unlock()
	if sampleSeen[key] {
		return
	}
	sampleSeen[key] = true
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("### " + method + " " + path + " " + http.StatusText(status) + "\n" + string(body) + "\n")
}

// responsesWithoutJSON are the success responses that are not a JSON envelope: files, images and
// WebSocket upgrades. Everything else must describe its `data`, so no endpoint falls back to "any".
var responsesWithoutJSON = map[string]string{
	"GET /images/{public_id}":                                                    "image bytes",
	"GET /tasks/{task_id}/attachments/{attachment_id}/download":                  "file bytes",
	"GET /workspaces/{workspace_id}/submissions/{submission_id}/files/{file_id}": "file bytes",
}

func TestContract_EverySuccessResponseDescribesItsData(t *testing.T) {
	doc, _, err := loadContract()
	if err != nil {
		t.Fatal(err)
	}
	seenExempt := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			key := method + " " + path
			for status, resp := range op.Responses.Map() {
				if !strings.HasPrefix(status, "2") || resp.Value == nil {
					continue
				}
				media := resp.Value.Content.Get("application/json")
				if media == nil {
					if _, ok := responsesWithoutJSON[key]; ok {
						seenExempt[key] = true
						continue
					}
					t.Errorf("%s %s has no JSON body schema; describe it or add it to responsesWithoutJSON with a reason", key, status)
					continue
				}
				if media.Schema == nil || media.Schema.Value == nil || media.Schema.Value.Properties["data"] == nil {
					t.Errorf("%s %s does not describe `data`; reference a typed response from components/responses", key, status)
				}
			}
		}
	}
	for key := range responsesWithoutJSON {
		if !seenExempt[key] {
			t.Errorf("responsesWithoutJSON lists %q but the spec has a JSON response there (or no such operation); remove the entry", key)
		}
	}
}
