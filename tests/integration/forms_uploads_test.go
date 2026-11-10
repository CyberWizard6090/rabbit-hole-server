package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rabbit-hole-server/internal/config"
)

var testIPCounter atomic.Uint32

func freshIP() string {
	n := testIPCounter.Add(1)
	return fmt.Sprintf("198.51.%d.%d:4000", (n>>8)&255, n&255)
}

func uploadFrom(t *testing.T, tc *TestContext, path, filename string, content []byte, remote string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec
}

type uploadFixture struct {
	formFixtureData
	memberID uint
	guestID  uint
	fileQ    uint
}

func newUploadFixture(t *testing.T, tc *TestContext, label string, fileSettings map[string]any) uploadFixture {
	t.Helper()
	return newUploadFixtureWith(t, tc, label, fileSettings, true)
}

func newUploadFixtureWith(t *testing.T, tc *TestContext, label string, fileSettings map[string]any, withUsers bool) uploadFixture {
	t.Helper()
	owner := newWorkspaceOwner(t, tc, label)
	var memberID, guestID uint
	var memberToken, guestToken, outsiderToken string
	if withUsers {
		memberID, memberToken = newUser(t, tc, label+"-member@example.test")
		guestID, guestToken = newUser(t, tc, label+"-guest@example.test")
		_, outsiderToken = newUser(t, tc, label+"-outsider@example.test")
		for _, u := range []struct {
			id   uint
			role string
		}{{memberID, "Member"}, {guestID, "Guest"}} {
			addContact(t, tc, owner.token, u.id)
			rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
				map[string]any{"user_id": u.id, "role": u.role})
			if rec.Code != http.StatusCreated {
				t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
			}
		}
	}
	f := finishFormFixture(t, tc, owner, memberToken, guestToken, outsiderToken, label)
	body := map[string]any{"type": "file", "title": "Attachments", "position": 1}
	if fileSettings != nil {
		body["settings"] = fileSettings
	}
	fileQ := mustCreate(t, tc, owner.token, f.formURL+"/"+itoa(f.formID)+"/questions", body)
	fx := uploadFixture{formFixtureData: f, memberID: memberID, guestID: guestID, fileQ: fileQ}
	fx.publish(t, tc)
	return fx
}

func (f formFixtureData) publish(t *testing.T, tc *TestContext) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, f.formURL+"/"+itoa(f.formID)+"/publish", f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
}

func (f formFixtureData) upload(t *testing.T, tc *TestContext, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	return uploadFrom(t, tc, "/api/v1/public/forms/"+f.slug+"/uploads", name, content, freshIP())
}

func (f formFixtureData) mustUpload(t *testing.T, tc *TestContext, name string, content []byte) string {
	t.Helper()
	rec := f.upload(t, tc, name, content)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %s: %d: %s", name, rec.Code, rec.Body.String())
	}
	id, _ := dataObject(t, rec)["id"].(string)
	if len(id) != 32 {
		t.Fatalf("upload id = %q", id)
	}
	return id
}

func (f formFixtureData) submitWith(t *testing.T, tc *TestContext, key string, answers map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"answers": answers})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+f.slug+"/submissions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req.RemoteAddr = freshIP()
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := decodeJSON(t, rec)
	errObject, _ := body["error"].(map[string]any)
	code, _ := errObject["code"].(string)
	return code
}

func (f uploadFixture) answers(title string, ids ...string) map[string]any {
	files := make([]any, len(ids))
	for i, id := range ids {
		files[i] = id
	}
	answers := map[string]any{itoa(f.questionID): title}
	if len(ids) > 0 {
		answers[itoa(f.fileQ)] = files
	}
	return answers
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	return solidPNG(t, 8, 8, color.NRGBA{R: 10, G: 120, B: 200, A: 255})
}

func submissionTaskID(t *testing.T, tc *TestContext, token string, submissionID uint) uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/submissions/"+itoa(submissionID)+"/tasks", token, nil)
	tasks := responseArray(t, rec)
	if rec.Code != http.StatusOK || len(tasks) != 1 {
		t.Fatalf("submission tasks: %d %s", rec.Code, rec.Body.String())
	}
	return uint(tasks[0].(map[string]any)["id"].(float64))
}

func TestFormUploads_FilesAreAttachedToTheCreatedTask(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "upok", nil)
	image := smallPNG(t)
	text := []byte("plain notes for the request")

	rec := f.upload(t, tc, `..\evil/../report final.png`, image)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	uploaded := dataObject(t, rec)
	if uploaded["name"] != "report final.png" || uploaded["content_type"] != "image/png" ||
		uploaded["size"] != float64(len(image)) || uploaded["expires_at"] == "" {
		t.Fatalf("upload result = %v", uploaded)
	}
	first := uploaded["id"].(string)
	second := f.mustUpload(t, tc, "notes.txt", text)

	rec = f.submitWith(t, tc, "files-1", f.answers("With files", first, second))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	if status := dataObject(t, rec)["task_creation_status"]; status != "succeeded" {
		t.Fatalf("task creation status = %v", status)
	}
	taskID := submissionTaskID(t, tc, f.owner.token, submissionID)

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/attachments", f.owner.token, nil)
	attachments := responseArray(t, rec)
	if rec.Code != http.StatusOK || len(attachments) != 2 {
		t.Fatalf("attachments: %d %s", rec.Code, rec.Body.String())
	}
	firstAttachment := attachments[0].(map[string]any)
	if firstAttachment["name"] != "report final.png" || firstAttachment["content_type"] != "image/png" ||
		uint(firstAttachment["uploader"].(map[string]any)["id"].(float64)) != f.owner.userID {
		t.Fatalf("attachment = %v", firstAttachment)
	}
	download := request(t, tc.Router, http.MethodGet,
		"/api/v1/tasks/"+itoa(taskID)+"/attachments/"+itoa(uint(firstAttachment["id"].(float64)))+"/download", f.owner.token, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), image) {
		t.Fatalf("attachment download: %d (%d bytes)", download.Code, download.Body.Len())
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/activity", f.owner.token, nil)
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), "attachment.created") != 2 ||
		!strings.Contains(rec.Body.String(), "notes.txt") {
		t.Fatalf("activity is missing attachment entries: %s", rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(f.owner.workspaceID)+"/submissions/"+itoa(submissionID), f.member, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get submission: %d: %s", rec.Code, rec.Body.String())
	}
	files, _ := dataObject(t, rec)["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("submission files = %v", files)
	}
	file := files[0].(map[string]any)
	if uint(file["question_id"].(float64)) != f.fileQ || file["task_attachment_id"] == nil || file["name"] != "report final.png" {
		t.Fatalf("submission file = %v", file)
	}
}

func TestFormUploads_SubmissionFilesAreReadableByWorkspaceMembersOnly(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "updl", nil)
	image := smallPNG(t)
	id := f.mustUpload(t, tc, "photo.png", image)
	rec := f.submitWith(t, tc, "dl-1", f.answers("Download", id))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(f.owner.workspaceID)+"/submissions/"+itoa(submissionID), f.owner.token, nil)
	fileID := uint(dataObject(t, rec)["files"].([]any)[0].(map[string]any)["id"].(float64))
	path := "/api/v1/workspaces/" + itoa(f.owner.workspaceID) + "/submissions/" + itoa(submissionID) + "/files/" + itoa(fileID)

	if rec = request(t, tc.Router, http.MethodGet, path, f.guest, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("guest download: %d, want 403", rec.Code)
	}
	for name, token := range map[string]string{"owner": f.owner.token, "member": f.member} {
		rec = request(t, tc.Router, http.MethodGet, path, token, nil)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), image) {
			t.Fatalf("%s download: %d", name, rec.Code)
		}
		if rec.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") ||
			!strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("%s download headers: %v", name, rec.Header())
		}
	}
	if rec = request(t, tc.Router, http.MethodGet, path, f.outsider, nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("outsider download: %d", rec.Code)
	}
	if rec = request(t, tc.Router, http.MethodGet, path, "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous download: %d", rec.Code)
	}

	other := newWorkspaceOwner(t, tc, "updl-other")
	otherPath := "/api/v1/workspaces/" + itoa(other.workspaceID) + "/submissions/" + itoa(submissionID) + "/files/" + itoa(fileID)
	if rec = request(t, tc.Router, http.MethodGet, otherPath, other.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("another workspace owner must not reach the file through his own workspace: %d", rec.Code)
	}
	wrongFile := "/api/v1/workspaces/" + itoa(f.owner.workspaceID) + "/submissions/" + itoa(submissionID) + "/files/" + itoa(fileID+500)
	if rec = request(t, tc.Router, http.MethodGet, wrongFile, f.owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown file: %d", rec.Code)
	}
	if rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(f.owner.workspaceID)+"/submissions/"+itoa(submissionID+500)+"/files/"+itoa(fileID), f.owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("file of another submission: %d", rec.Code)
	}

	if guestList := request(t, tc.Router, http.MethodGet, f.formURL+"/"+itoa(f.formID)+"/submissions", f.guest, nil); guestList.Code != http.StatusForbidden {
		t.Fatalf("guest submission list: %d, want 403", guestList.Code)
	}
	list := request(t, tc.Router, http.MethodGet, f.formURL+"/"+itoa(f.formID)+"/submissions", f.member, nil)
	items := responseArray(t, list)
	if list.Code != http.StatusOK || len(items) != 1 || len(items[0].(map[string]any)["files"].([]any)) != 1 {
		t.Fatalf("submission list: %d %s", list.Code, list.Body.String())
	}
}

func TestFormUploads_SubmissionWithoutTaskKeepsFilesLinkedToTheSubmission(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "upnotask")
	formURL := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/forms"
	slug := fmt.Sprintf("notask-%d", time.Now().UnixNano())
	formID := mustCreate(t, tc, owner.token, formURL, map[string]any{"name": "Plain", "slug": slug})
	fileQ := mustCreate(t, tc, owner.token, formURL+"/"+itoa(formID)+"/questions",
		map[string]any{"type": "file", "title": "Proof", "required": true, "position": 0})
	if rec := request(t, tc.Router, http.MethodPost, formURL+"/"+itoa(formID)+"/publish", owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	uploaded := uploadFrom(t, tc, "/api/v1/public/forms/"+slug+"/uploads", "proof.pdf", []byte("%PDF-1.4\nbody"), freshIP())
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload: %d: %s", uploaded.Code, uploaded.Body.String())
	}
	id := dataObject(t, uploaded)["id"].(string)
	rec := requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/"+slug+"/submissions", "",
		"Idempotency-Key", "notask-1", map[string]any{"answers": map[string]any{itoa(fileQ): []string{id}}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	if dataObject(t, rec)["task_creation_status"] != "skipped" {
		t.Fatalf("submission = %v", dataObject(t, rec))
	}

	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(owner.workspaceID)+"/submissions/"+itoa(submissionID), owner.token, nil)
	file := dataObject(t, rec)["files"].([]any)[0].(map[string]any)
	if file["task_attachment_id"] != nil || file["name"] != "proof.pdf" {
		t.Fatalf("file = %v", file)
	}
	path := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/submissions/" + itoa(submissionID) + "/files/" + itoa(uint(file["id"].(float64)))
	download := request(t, tc.Router, http.MethodGet, path, owner.token, nil)
	if download.Code != http.StatusOK || download.Body.String() != "%PDF-1.4\nbody" {
		t.Fatalf("download: %d %q", download.Code, download.Body.String())
	}
	var attachments int64
	if err := tc.DB.Raw("SELECT count(*) FROM files WHERE kind = 'attachment'").Scan(&attachments).Error; err != nil || attachments != 0 {
		t.Fatalf("attachments = %d, err = %v", attachments, err)
	}
}

func TestFormUploads_RejectsOversizedAndWrongTypeFiles(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Forms.UploadMaxBytes = 2048 })
	f := newUploadFixture(t, tc, "uplimit", nil)

	rec := f.upload(t, tc, "big.txt", bytes.Repeat([]byte("a"), 2049))
	if rec.Code != http.StatusRequestEntityTooLarge || errorCode(t, rec) != "FILE_TOO_LARGE" {
		t.Fatalf("oversize: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.upload(t, tc, "huge.txt", bytes.Repeat([]byte("a"), 2048+(2<<20)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body past the route limit: %d", rec.Code)
	}
	if rec = f.upload(t, tc, "edge.txt", bytes.Repeat([]byte("a"), 2048)); rec.Code != http.StatusCreated {
		t.Fatalf("a file of exactly the limit: %d: %s", rec.Code, rec.Body.String())
	}

	for name, upload := range map[string]struct {
		file    string
		content []byte
	}{
		"html disguised as png":  {"avatar.png", []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")},
		"svg with a script":      {"logo.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>1</script></svg>`)},
		"svg with an xml prolog": {"logo.png", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"windows executable":     {"photo.png", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff\x00\x00")},
		"elf executable":         {"photo.jpg", []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")},
		"script with exe name":   {"setup.exe", []byte("plain text")},
		"html file":              {"page.html", []byte("plain text")},
		"html inside text":       {"notes.txt", []byte("hello <script>alert(1)</script>")},
	} {
		rec = f.upload(t, tc, upload.file, upload.content)
		if rec.Code != http.StatusUnsupportedMediaType || errorCode(t, rec) != "UNSUPPORTED_FILE_TYPE" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec = f.upload(t, tc, "empty.txt", []byte{}); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FILE_EMPTY" {
		t.Errorf("empty file: %d %s", rec.Code, rec.Body.String())
	}
	if rec = f.upload(t, tc, "", nil); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FILE_REQUIRED" {
		t.Errorf("missing file: %d %s", rec.Code, rec.Body.String())
	}
	var stored int64
	if err := tc.DB.Raw("SELECT count(*) FROM form_uploads").Scan(&stored).Error; err != nil || stored != 1 {
		t.Fatalf("only the valid file may be recorded: %d, err %v", stored, err)
	}
}

func TestFormUploads_OnlyPublishedFormsWithFileQuestionsAcceptUploads(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "upstate", nil)
	png := smallPNG(t)

	if rec := uploadFrom(t, tc, "/api/v1/public/forms/no-such-form/uploads", "a.png", png, freshIP()); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown form: %d", rec.Code)
	}

	draftSlug := fmt.Sprintf("draft-%d", time.Now().UnixNano())
	draftID := mustCreate(t, tc, f.owner.token, f.formURL, map[string]any{"name": "Draft", "slug": draftSlug})
	mustCreate(t, tc, f.owner.token, f.formURL+"/"+itoa(draftID)+"/questions", map[string]any{"type": "file", "title": "F", "position": 0})
	if rec := uploadFrom(t, tc, "/api/v1/public/forms/"+draftSlug+"/uploads", "a.png", png, freshIP()); rec.Code != http.StatusNotFound {
		t.Fatalf("draft form: %d: %s", rec.Code, rec.Body.String())
	}

	textOnlySlug := fmt.Sprintf("textonly-%d", time.Now().UnixNano())
	textOnlyID := mustCreate(t, tc, f.owner.token, f.formURL, map[string]any{"name": "Text", "slug": textOnlySlug})
	mustCreate(t, tc, f.owner.token, f.formURL+"/"+itoa(textOnlyID)+"/questions", map[string]any{"type": "short_text", "title": "T", "position": 0})
	request(t, tc.Router, http.MethodPost, f.formURL+"/"+itoa(textOnlyID)+"/publish", f.owner.token, nil)
	rec := uploadFrom(t, tc, "/api/v1/public/forms/"+textOnlySlug+"/uploads", "a.png", png, freshIP())
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_UPLOADS_NOT_ACCEPTED" {
		t.Fatalf("form without a file question: %d: %s", rec.Code, rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodPost, f.formURL+"/"+itoa(f.formID)+"/archive", f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("archive: %d", rec.Code)
	}
	if rec := f.upload(t, tc, "a.png", png); rec.Code != http.StatusNotFound {
		t.Fatalf("archived form: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormUploads_FileIdsAreScopedToTheirFormAndSingleUse(t *testing.T) {
	tc := newTestContext(t)
	a := newUploadFixture(t, tc, "upscopea", nil)
	b := newUploadFixtureWith(t, tc, "upscopeb", nil, false)
	png := smallPNG(t)
	foreign := a.mustUpload(t, tc, "a.png", png)

	rec := b.submitWith(t, tc, "foreign-1", b.answers("Reuse", foreign))
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_UPLOAD_INVALID" {
		t.Fatalf("an id of another form: %d: %s", rec.Code, rec.Body.String())
	}
	for _, bogus := range []string{strings.Repeat("0", 32), "not-an-id", ""} {
		rec = a.submitWith(t, tc, "bogus-"+bogus, a.answers("Bogus", bogus))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bogus id %q: %d: %s", bogus, rec.Code, rec.Body.String())
		}
	}
	var failedSubmissions int64
	if err := tc.DB.Raw("SELECT count(*) FROM form_submissions").Scan(&failedSubmissions).Error; err != nil || failedSubmissions != 0 {
		t.Fatalf("rejected submissions must leave nothing behind: %d, err %v", failedSubmissions, err)
	}

	if rec = a.submitWith(t, tc, "claim-1", a.answers("First", foreign)); rec.Code != http.StatusCreated {
		t.Fatalf("first claim: %d: %s", rec.Code, rec.Body.String())
	}
	rec = a.submitWith(t, tc, "claim-2", a.answers("Second", foreign))
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_UPLOAD_INVALID" {
		t.Fatalf("an already claimed id: %d: %s", rec.Code, rec.Body.String())
	}
	var submissions, tasks int64
	_ = tc.DB.Raw("SELECT count(*) FROM form_submissions").Scan(&submissions).Error
	_ = tc.DB.Raw("SELECT count(*) FROM tasks").Scan(&tasks).Error
	if submissions != 1 || tasks != 1 {
		t.Fatalf("the failed claim left submissions=%d tasks=%d", submissions, tasks)
	}

	same := a.mustUpload(t, tc, "same.png", png)
	twice := a.answers("Twice", same)
	twice[itoa(a.fileQ)] = []string{same, same}
	if rec = a.submitWith(t, tc, "twice-1", twice); rec.Code != http.StatusBadRequest {
		t.Fatalf("the same id twice in one answer: %d", rec.Code)
	}
}

func TestFormUploads_ExpiredUploadsCannotBeSubmitted(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "upexp", nil)
	id := f.mustUpload(t, tc, "late.png", smallPNG(t))
	if err := tc.DB.Exec("UPDATE form_uploads SET expires_at = now() - interval '1 minute' WHERE public_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	rec := f.submitWith(t, tc, "expired-1", f.answers("Late", id))
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_UPLOAD_INVALID" {
		t.Fatalf("expired upload: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormUploads_UploadTTLComesFromConfiguration(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Forms.UploadTTL = 90 * time.Minute })
	f := newUploadFixture(t, tc, "upttl", nil)
	f.mustUpload(t, tc, "ttl.png", smallPNG(t))
	var minutes float64
	if err := tc.DB.Raw("SELECT extract(epoch FROM (expires_at - created_at)) / 60 FROM form_uploads").Scan(&minutes).Error; err != nil {
		t.Fatal(err)
	}
	if minutes < 89 || minutes > 91 {
		t.Fatalf("expiry is %.1f minutes after upload, want 90", minutes)
	}
}

func TestFormUploads_ReplayDoesNotClaimTwiceAndConcurrentClaimsHaveOneWinner(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "uprace", nil)
	id := f.mustUpload(t, tc, "once.png", smallPNG(t))

	rec := f.submitWith(t, tc, "same-key", f.answers("Once", id))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	replay := f.submitWith(t, tc, "same-key", f.answers("Once", id))
	if replay.Code != http.StatusOK || responseID(t, replay) != submissionID {
		t.Fatalf("replay: %d: %s", replay.Code, replay.Body.String())
	}
	taskID := submissionTaskID(t, tc, f.owner.token, submissionID)
	var attachments int64
	if err := tc.DB.Raw("SELECT count(*) FROM files WHERE kind = 'attachment' AND task_id = ?", taskID).Scan(&attachments).Error; err != nil || attachments != 1 {
		t.Fatalf("attachments after a replay = %d, err %v", attachments, err)
	}

	contested := f.mustUpload(t, tc, "contested.png", smallPNG(t))
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = f.submitWith(t, tc, fmt.Sprintf("race-%d", i), f.answers("Race", contested)).Code
		}(i)
	}
	wg.Wait()
	created := 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusBadRequest:
		default:
			t.Fatalf("unexpected status %d in %v", code, codes)
		}
	}
	if created != 1 {
		t.Fatalf("%d submissions claimed one file: %v", created, codes)
	}
	var claimed int64
	_ = tc.DB.Raw("SELECT count(*) FROM form_uploads WHERE public_id = ? AND claimed_at IS NOT NULL", contested).Scan(&claimed).Error
	if claimed != 1 {
		t.Fatalf("claimed rows = %d", claimed)
	}
}

func TestFormUploads_QuestionLimitsAndRequiredFiles(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "uplim", map[string]any{"max_files": 2})
	png := smallPNG(t)
	ids := []string{f.mustUpload(t, tc, "1.png", png), f.mustUpload(t, tc, "2.png", png), f.mustUpload(t, tc, "3.png", png)}

	if rec := f.submitWith(t, tc, "limit-1", f.answers("Too many", ids...)); rec.Code != http.StatusBadRequest {
		t.Fatalf("three files with max_files=2: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := f.submitWith(t, tc, "limit-2", f.answers("Two", ids[:2]...)); rec.Code != http.StatusCreated {
		t.Fatalf("two files with max_files=2: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := f.submitWith(t, tc, "limit-3", f.answers("None")); rec.Code != http.StatusCreated {
		t.Fatalf("an optional file question may stay empty: %d: %s", rec.Code, rec.Body.String())
	}

	for _, invalid := range []int{0, 11, -1} {
		rec := request(t, tc.Router, http.MethodPost, f.formURL+"/"+itoa(f.formID)+"/questions", f.owner.token,
			map[string]any{"type": "file", "title": "Bad", "position": 5, "settings": map[string]any{"max_files": invalid}})
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_QUESTION_SETTINGS_INVALID" {
			t.Fatalf("max_files=%d: %d: %s", invalid, rec.Code, rec.Body.String())
		}
	}

	required := mustCreate(t, tc, f.owner.token, f.formURL+"/"+itoa(f.formID)+"/questions",
		map[string]any{"type": "file", "title": "Must", "required": true, "position": 9})
	f.publish(t, tc)
	answers := f.answers("No required file")
	if rec := f.submitWith(t, tc, "req-1", answers); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing required file question: %d", rec.Code)
	}
	answers[itoa(required)] = []string{f.mustUpload(t, tc, "r.png", png)}
	if rec := f.submitWith(t, tc, "req-2", answers); rec.Code != http.StatusCreated {
		t.Fatalf("required file supplied: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormUploads_UploadsAreRateLimitedPerClient(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "uprate", nil)
	png := smallPNG(t)
	path := "/api/v1/public/forms/" + f.slug + "/uploads"
	const client = "203.0.113.7:5000"
	for i := 0; i < 30; i++ {
		if rec := uploadFrom(t, tc, path, "a.png", png, client); rec.Code != http.StatusCreated {
			t.Fatalf("upload %d: %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	if rec := uploadFrom(t, tc, path, "a.png", png, client); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the 31st upload in a minute: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := uploadFrom(t, tc, path, "a.png", png, freshIP()); rec.Code != http.StatusCreated {
		t.Fatalf("another client keeps its own budget: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormUploads_PendingUploadsPerFormAreCapped(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "upcap", nil)
	err := tc.DB.Exec(`INSERT INTO form_uploads (public_id, form_id, storage_key, name, content_type, size, expires_at)
		SELECT md5(random()::text || g::text), ?, 'formupload/cap-' || g, 'x.txt', 'text/plain', 1, now() + interval '1 hour'
		FROM generate_series(1, 500) g`, f.formID).Error
	if err != nil {
		t.Fatal(err)
	}
	rec := f.upload(t, tc, "a.png", smallPNG(t))
	if rec.Code != http.StatusTooManyRequests || errorCode(t, rec) != "FORM_UPLOADS_LIMITED" {
		t.Fatalf("pending cap: %d: %s", rec.Code, rec.Body.String())
	}
}

func objectCount(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(filepath.Join(dir, "formupload"), func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestFormUploads_CleanupRemovesOnlyExpiredUnclaimedUploads(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "upgc", nil)
	png := smallPNG(t)
	orphan := f.mustUpload(t, tc, "orphan.png", png)
	fresh := f.mustUpload(t, tc, "fresh.png", png)
	claimed := f.mustUpload(t, tc, "claimed.png", png)
	if rec := f.submitWith(t, tc, "gc-1", f.answers("Claim", claimed)); rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	if objectCount(t, tc.StorageDir) != 3 {
		t.Fatalf("stored objects = %d, want 3", objectCount(t, tc.StorageDir))
	}
	if err := tc.DB.Exec("UPDATE form_uploads SET expires_at = now() - interval '2 hours' WHERE public_id IN (?, ?)", orphan, claimed).Error; err != nil {
		t.Fatal(err)
	}

	removed, err := tc.FormUploads.CleanupExpired(context.Background())
	if err != nil || removed != 1 {
		t.Fatalf("removed = %d, err = %v", removed, err)
	}
	var remaining []string
	if err := tc.DB.Raw("SELECT public_id FROM form_uploads ORDER BY id").Scan(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || remaining[0] != fresh || remaining[1] != claimed {
		t.Fatalf("remaining uploads = %v", remaining)
	}
	if objectCount(t, tc.StorageDir) != 2 {
		t.Fatalf("stored objects after cleanup = %d, want 2", objectCount(t, tc.StorageDir))
	}
	if removed, err = tc.FormUploads.CleanupExpired(context.Background()); err != nil || removed != 0 {
		t.Fatalf("second run removed = %d, err = %v", removed, err)
	}
	if rec := f.submitWith(t, tc, "gc-2", f.answers("Fresh", fresh)); rec.Code != http.StatusCreated {
		t.Fatalf("a fresh upload survives the cleanup: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormUploads_DeletingTheAttachmentLeavesTheSubmissionRecord(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "updel", nil)
	id := f.mustUpload(t, tc, "gone.png", smallPNG(t))
	rec := f.submitWith(t, tc, "del-1", f.answers("Delete", id))
	submissionID := responseID(t, rec)
	taskID := submissionTaskID(t, tc, f.owner.token, submissionID)
	attachments := responseArray(t, request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/attachments", f.owner.token, nil))
	attachmentID := uint(attachments[0].(map[string]any)["id"].(float64))
	if rec = request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(taskID)+"/attachments/"+itoa(attachmentID), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete attachment: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(f.owner.workspaceID)+"/submissions/"+itoa(submissionID), f.owner.token, nil)
	file := dataObject(t, rec)["files"].([]any)[0].(map[string]any)
	if rec.Code != http.StatusOK || file["task_attachment_id"] != nil {
		t.Fatalf("submission after deleting the attachment: %d %v", rec.Code, file)
	}
}
