package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func jsonBodyRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimitByRoute(64, map[string]int64{"POST /big": 1 << 10}), PathIDs(), JSONBody(map[string]bool{"POST /upload/:item_id": true}))
	echo := func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, string(body))
	}
	r.POST("/echo", echo)
	r.PUT("/echo", echo)
	r.PATCH("/echo", echo)
	r.GET("/echo", echo)
	r.DELETE("/echo", echo)
	r.POST("/big", echo)
	r.POST("/upload/:item_id", echo)
	r.GET("/items/:item_id", echo)
	r.GET("/files/:slug", echo)
	return r
}

func send(r http.Handler, method, path, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
		t.Fatalf("no error envelope: %v %s", err, rec.Body.String())
	}
	return body.Error
}

func TestJSONBodyPassesCleanBodiesThroughUnchanged(t *testing.T) {
	r := jsonBodyRouter()
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "APPLICATION/JSON"} {
		rec := send(r, http.MethodPost, "/echo", ct, `{"name":"é 😀"}`)
		if rec.Code != http.StatusOK || rec.Body.String() != `{"name":"é 😀"}` {
			t.Fatalf("%s: %d %s", ct, rec.Code, rec.Body.String())
		}
	}
}

func TestJSONBodyRejectsOtherContentTypes(t *testing.T) {
	r := jsonBodyRouter()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/jsonx", "garbage;;"} {
			rec := send(r, method, "/echo", ct, `{"a":1}`)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("%s %q: status %d", method, ct, rec.Code)
			}
			payload := errorOf(t, rec)
			params, _ := payload["params"].(map[string]any)
			if payload["code"] != "UNSUPPORTED_MEDIA_TYPE" || params["expected"] != "application/json" {
				t.Fatalf("payload = %v", payload)
			}
		}
	}
}

func TestJSONBodyIgnoresRequestsWithoutBodyOrOtherMethods(t *testing.T) {
	r := jsonBodyRouter()
	if rec := send(r, http.MethodPost, "/echo", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("empty POST: %d", rec.Code)
	}
	if rec := send(r, http.MethodGet, "/echo", "text/plain", "x"); rec.Code != http.StatusOK {
		t.Fatalf("GET: %d", rec.Code)
	}
	if rec := send(r, http.MethodDelete, "/echo", "text/plain", "x"); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d", rec.Code)
	}
	if rec := send(r, http.MethodPost, "/upload/5", "multipart/form-data; boundary=x", "raw"); rec.Code != http.StatusOK {
		t.Fatalf("skipped route: %d", rec.Code)
	}
	if rec := send(r, http.MethodPost, "/missing", "text/plain", "x"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown route: %d", rec.Code)
	}
}

func TestJSONBodyRejectsNulInvalidEncodingAndHugeIntegers(t *testing.T) {
	r := jsonBodyRouter()
	cases := []struct {
		name, body, field, code string
	}{
		{"nul", `{"name":"a\u0000"}`, "name", "INVALID_VALUE"},
		{"nested", `{"tags":["x","y\u0000"]}`, "tags[1]", "INVALID_VALUE"},
		{"lone surrogate", `{"name":"\ud800"}`, "name", "INVALID_ENCODING"},
		{"invalid utf8", "{\"name\":\"\xff\"}", "name", "INVALID_ENCODING"},
		{"huge integer", `{"id":18446744073709551615}`, "id", "TOO_LARGE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(r, http.MethodPost, "/echo", "application/json", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", rec.Code)
			}
			payload := errorOf(t, rec)
			details, _ := payload["details"].([]any)
			if payload["code"] != "VALIDATION_FAILED" || len(details) != 1 {
				t.Fatalf("payload = %v", payload)
			}
			detail := details[0].(map[string]any)
			if detail["field"] != tc.field || detail["code"] != tc.code {
				t.Fatalf("detail = %v", detail)
			}
		})
	}
}

func TestJSONBodyReportsOversizedBodiesAs413(t *testing.T) {
	r := jsonBodyRouter()
	rec := send(r, http.MethodPost, "/echo", "application/json", `{"pad":"`+strings.Repeat("a", 100)+`"}`)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	payload := errorOf(t, rec)
	params, _ := payload["params"].(map[string]any)
	if payload["code"] != "PAYLOAD_TOO_LARGE" || params["max_bytes"] != float64(64) {
		t.Fatalf("payload = %v", payload)
	}
	if rec := send(r, http.MethodPost, "/big", "application/json", `{"pad":"`+strings.Repeat("a", 100)+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("route override ignored: %d", rec.Code)
	}
}

func TestPathIDsRejectsIdentifiersAboveInt64(t *testing.T) {
	r := jsonBodyRouter()
	for _, id := range []string{"9223372036854775808", "18446744073709551615", "99999999999999999999999"} {
		rec := send(r, http.MethodGet, "/items/"+id, "", "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d", id, rec.Code)
		}
		payload := errorOf(t, rec)
		params, _ := payload["params"].(map[string]any)
		if payload["code"] != "INVALID_ID" || params["param"] != "item_id" {
			t.Fatalf("payload = %v", payload)
		}
	}
	for _, id := range []string{"1", "9223372036854775807", "abc", "-5"} {
		if rec := send(r, http.MethodGet, "/items/"+id, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", id, rec.Code)
		}
	}
	if rec := send(r, http.MethodGet, "/files/99999999999999999999999", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("non id param must pass: %d", rec.Code)
	}
}
