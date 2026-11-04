package integration

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const slowUploadBoundary = "slowupload"

func slowPost(t *testing.T, url, contentType, token string, body func(w io.Writer, pause func())) (*http.Response, error) {
	t.Helper()
	reader, writer := io.Pipe()
	go func() {
		body(writer, func() { time.Sleep(900 * time.Millisecond) })
		_ = writer.Close()
	}()
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func TestUploadsOutliveTheServerReadTimeout(t *testing.T) {
	tc := newTestContext(t)
	owner, taskID, _, _, _ := filesFixture(t, tc)

	server := httptest.NewUnstartedServer(tc.Router)
	server.Config.ReadTimeout = 400 * time.Millisecond
	server.Config.WriteTimeout = 400 * time.Millisecond
	server.Start()
	defer server.Close()

	slowFile := func(w io.Writer, pause func()) {
		mw := multipart.NewWriter(w)
		_ = mw.SetBoundary(slowUploadBoundary)
		part, _ := mw.CreateFormFile("file", "slow.txt")
		_, _ = part.Write([]byte("first half "))
		pause()
		_, _ = part.Write([]byte("second half"))
		_ = mw.Close()
	}
	resp, err := slowPost(t, server.URL+"/api/v1/tasks/"+itoa(taskID)+"/attachments",
		"multipart/form-data; boundary="+slowUploadBoundary, owner.token, slowFile)
	if err != nil {
		t.Fatalf("a slow upload was cut off by the server read timeout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("slow upload: %d: %s", resp.StatusCode, body)
	}

	var stored int64
	tc.DB.Table("files").Where("task_id = ? AND size = ?", taskID, len("first half second half")).Count(&stored)
	if stored != 1 {
		t.Fatalf("the slow upload was stored %d times, want exactly once and complete", stored)
	}

	slowComment := func(w io.Writer, pause func()) {
		_, _ = io.WriteString(w, `{"body":"slow`)
		pause()
		_, _ = io.WriteString(w, ` comment"}`)
	}
	control, err := slowPost(t, server.URL+"/api/v1/tasks/"+itoa(taskID)+"/comments", "application/json", owner.token, slowComment)
	if err == nil {
		defer control.Body.Close()
		if control.StatusCode == http.StatusCreated {
			t.Fatal("a slow ordinary request survived the read timeout, so this test proves nothing about uploads")
		}
	}
}
