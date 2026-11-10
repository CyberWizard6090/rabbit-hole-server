package integration

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rabbit-hole-server/internal/config"
)

func uploadRequest(t *testing.T, tc *TestContext, path, token, filename string, content []byte) *httptest.ResponseRecorder {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec
}

func uploadFixtureFile(t *testing.T, tc *TestContext, token string, taskID uint) uint {
	t.Helper()
	rec := uploadRequest(t, tc, "/api/v1/tasks/"+itoa(taskID)+"/attachments", token, "fixture.txt", []byte("fixture"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload fixture attachment: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func solidPNG(t *testing.T, width, height int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func jpegWithOrientation(t *testing.T, orientation uint16) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			if x < 20 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}

	exif := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01")
	exif = binary.BigEndian.AppendUint16(exif, orientation)
	exif = append(exif, 0, 0, 0, 0, 0, 0)
	segment := []byte{0xFF, 0xE1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(exif)+2))
	segment = append(segment, exif...)

	data := encoded.Bytes()
	return append(append(append([]byte{}, data[:2]...), segment...), data[2:]...)
}

func filesFixture(t *testing.T, tc *TestContext) (owner workspaceOwner, taskID uint, member, guest, outsider string) {
	t.Helper()
	owner = newWorkspaceOwner(t, tc, "files")
	memberID, memberToken := newUser(t, tc, "files-member@example.test")
	guestID, guestToken := newUser(t, tc, "files-guest@example.test")
	_, outsiderToken := newUser(t, tc, "files-outsider@example.test")
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

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", owner.token, map[string]any{"name": "list"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: %d: %s", rec.Code, rec.Body.String())
	}
	listID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", owner.token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", owner.token,
		map[string]any{"title": "task with files", "status_id": responseID(t, rec)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	return owner, responseID(t, rec), memberToken, guestToken, outsiderToken
}

func TestAttachments_UploadListDownloadAndDelete(t *testing.T) {
	tc := newTestContext(t)
	owner, taskID, memberToken, guestToken, outsiderToken := filesFixture(t, tc)
	base := "/api/v1/tasks/" + itoa(taskID) + "/attachments"

	rec := uploadRequest(t, tc, base, owner.token, `C:\Users\evil\report.txt`, []byte("quarterly numbers"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	created := dataObject(t, rec)
	attachmentID := responseID(t, rec)
	if created["name"] != "report.txt" || created["size"] != float64(17) || created["content_type"] != "text/plain; charset=utf-8" {
		t.Fatalf("attachment = %v", created)
	}
	if _, leaked := created["uploader"].(map[string]any)["email"]; leaked {
		t.Fatalf("uploader exposes the email address: %v", created["uploader"])
	}
	if _, leaked := created["storage_key"]; leaked {
		t.Fatalf("storage key is exposed: %v", created)
	}

	rec = uploadRequest(t, tc, base, owner.token, "page.html", []byte("<html><script>alert(1)</script></html>"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload html: %d: %s", rec.Code, rec.Body.String())
	}
	htmlID := responseID(t, rec)

	if rec := uploadRequest(t, tc, base, owner.token, "empty.txt", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty file: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := uploadRequest(t, tc, base, owner.token, "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("no file field: expected 400, got %d", rec.Code)
	}
	if rec := uploadRequest(t, tc, base, guestToken, "guest.txt", []byte("x")); rec.Code != http.StatusForbidden {
		t.Fatalf("guest upload: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, base, outsiderToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list: expected 403, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodGet, base, guestToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("guest list: %d: %s", rec.Code, rec.Body.String())
	}
	if items := decodeJSON(t, rec)["data"].([]any); len(items) != 2 {
		t.Fatalf("attachments = %v, want 2", items)
	}

	rec = request(t, tc.Router, http.MethodGet, base+"/"+itoa(attachmentID)+"/download", guestToken, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "quarterly numbers" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename=report.txt` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}

	rec = request(t, tc.Router, http.MethodGet, base+"/"+itoa(htmlID)+"/download", owner.token, nil)
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("an uploaded HTML page is served as %q, want application/octet-stream", got)
	}

	otherTask := "/api/v1/tasks/" + itoa(taskID+1000) + "/attachments/" + itoa(attachmentID) + "/download"
	if rec := request(t, tc.Router, http.MethodGet, otherTask, owner.token, nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("attachment through another task: expected 403/404, got %d", rec.Code)
	}

	if rec := request(t, tc.Router, http.MethodDelete, base+"/"+itoa(attachmentID), guestToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("guest delete: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, base+"/"+itoa(attachmentID), memberToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("member (task.delete) delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, base+"/"+itoa(attachmentID), owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete twice: expected 404, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, base+"/"+itoa(attachmentID)+"/download", owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("download after delete: expected 404, got %d", rec.Code)
	}
}

func TestAttachments_SizeLimitsAndCount(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Storage.MaxAttachmentBytes = 1000 })
	owner, taskID, _, _, _ := filesFixture(t, tc)
	base := "/api/v1/tasks/" + itoa(taskID) + "/attachments"

	if rec := uploadRequest(t, tc, base, owner.token, "exact.bin", bytes.Repeat([]byte("a"), 1000)); rec.Code != http.StatusCreated {
		t.Fatalf("file of exactly the limit: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := uploadRequest(t, tc, base, owner.token, "over.bin", bytes.Repeat([]byte("a"), 1001)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("one byte over: expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := uploadRequest(t, tc, base, owner.token, "huge.bin", bytes.Repeat([]byte("a"), 2<<20)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body past the request cap: expected 413, got %d: %s", rec.Code, rec.Body.String())
	}

	var stored int64
	tc.DB.Table("files").Where("task_id = ?", taskID).Count(&stored)
	if stored != 1 {
		t.Fatalf("stored files = %d, want only the accepted one", stored)
	}
}

func TestEditorImages_UploadServeAndReject(t *testing.T) {
	tc := newTestContext(t)
	owner, _, _, guestToken, outsiderToken := filesFixture(t, tc)
	path := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/images"
	original := solidPNG(t, 30, 20, color.NRGBA{G: 200, A: 255})

	rec := uploadRequest(t, tc, path, owner.token, "screenshot.png", original)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload image: %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	url, _ := data["url"].(string)
	if !strings.HasPrefix(url, "/api/v1/images/") || len(url) != len("/api/v1/images/")+32 || data["content_type"] != "image/png" {
		t.Fatalf("upload result = %v", data)
	}

	served := request(t, tc.Router, http.MethodGet, url, "", nil)
	if served.Code != http.StatusOK || !bytes.Equal(served.Body.Bytes(), original) {
		t.Fatalf("serve: %d, %d bytes", served.Code, served.Body.Len())
	}
	for header, want := range map[string]string{
		"Content-Type":                 "image/png",
		"Cache-Control":                "public, max-age=31536000, immutable",
		"Cross-Origin-Resource-Policy": "cross-origin",
		"X-Content-Type-Options":       "nosniff",
		"Content-Security-Policy":      "default-src 'none'; sandbox",
	} {
		if got := served.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	etag := served.Header().Get("ETag")
	conditional := httptest.NewRequest(http.MethodGet, url, nil)
	conditional.Header.Set("If-None-Match", etag)
	notModified := httptest.NewRecorder()
	tc.Router.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("conditional request: expected 304, got %d", notModified.Code)
	}

	if rec := uploadRequest(t, tc, path, guestToken, "g.png", original); rec.Code != http.StatusForbidden {
		t.Fatalf("guest upload: expected 403, got %d", rec.Code)
	}
	if rec := uploadRequest(t, tc, path, outsiderToken, "o.png", original); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider upload: expected 403, got %d", rec.Code)
	}

	bomb := append([]byte{}, original[:16]...)
	bomb = binary.BigEndian.AppendUint32(bomb, 30000)
	bomb = binary.BigEndian.AppendUint32(bomb, 30000)
	bomb = append(bomb, original[24:]...)
	for name, tcase := range map[string]struct {
		file    string
		content []byte
		want    int
	}{
		"svg":                {"vector.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), http.StatusUnsupportedMediaType},
		"html named png":     {"photo.png", []byte("<html><body>hi</body></html>"), http.StatusUnsupportedMediaType},
		"random bytes":       {"noise.png", bytes.Repeat([]byte{0x13, 0x37}, 400), http.StatusUnsupportedMediaType},
		"truncated header":   {"cut.png", original[:12], http.StatusBadRequest},
		"oversized pixels":   {"bomb.png", bomb, http.StatusBadRequest},
		"empty":              {"empty.png", nil, http.StatusBadRequest},
		"plain text as jpeg": {"a.jpg", []byte("just text"), http.StatusUnsupportedMediaType},
	} {
		if rec := uploadRequest(t, tc, path, owner.token, tcase.file, tcase.content); rec.Code != tcase.want {
			t.Errorf("%s: expected %d, got %d: %s", name, tcase.want, rec.Code, rec.Body.String())
		}
	}

	for _, id := range []string{"0123456789abcdef0123456789abcdef", "not-an-id", "../../etc/passwd"} {
		if rec := request(t, tc.Router, http.MethodGet, "/api/v1/images/"+id, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET /images/%s: expected 404, got %d", id, rec.Code)
		}
	}
}

func decodeServed(t *testing.T, tc *TestContext, url string) image.Image {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, url, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", url, rec.Code)
	}
	img, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return img
}

func hasColor(c color.Color, r, g, b bool) bool {
	cr, cg, cb, _ := c.RGBA()
	high := func(v uint32) bool { return v > 0xA000 }
	return high(cr) == r && high(cg) == g && high(cb) == b
}

func TestAvatar_UploadRotateReplaceAndRemove(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, "avatar-user@example.test")

	profileAvatar := func() string {
		t.Helper()
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("profile: %d", rec.Code)
		}
		return dataObject(t, rec)["avatar_url"].(string)
	}

	rec := uploadRequest(t, tc, "/api/v1/profile/avatar", token, "me.jpg", jpegWithOrientation(t, 6))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload avatar: %d: %s", rec.Code, rec.Body.String())
	}
	first := dataObject(t, rec)["avatar_url"].(string)
	if first == "" || profileAvatar() != first {
		t.Fatalf("avatar_url = %q, profile has %q", first, profileAvatar())
	}
	rotated := decodeServed(t, tc, first)
	if b := rotated.Bounds(); b.Dx() != 20 || b.Dy() != 20 {
		t.Fatalf("avatar size = %v, want a 20x20 square cropped from the rotated photo", b)
	}
	if !hasColor(rotated.At(10, 2), true, false, false) || !hasColor(rotated.At(10, 17), false, false, true) {
		t.Fatalf("EXIF orientation 6 was not applied: top %v bottom %v", rotated.At(10, 2), rotated.At(10, 17))
	}

	big := solidPNG(t, 1000, 500, color.NRGBA{R: 10, G: 200, B: 30, A: 120})
	rec = uploadRequest(t, tc, "/api/v1/profile/avatar", token, "big.png", big)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace avatar: %d: %s", rec.Code, rec.Body.String())
	}
	second := dataObject(t, rec)["avatar_url"].(string)
	if second == first || profileAvatar() != second {
		t.Fatalf("avatar was not replaced: %q then %q", first, second)
	}
	scaled := decodeServed(t, tc, second)
	if b := scaled.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("avatar size = %v, want 256x256", b)
	}
	if _, _, a := func() (uint32, uint32, uint32) { _, _, _, a := scaled.At(100, 100).RGBA(); return 0, 0, a }(); a == 0xFFFF {
		t.Fatal("transparency was lost")
	}
	if rec := request(t, tc.Router, http.MethodGet, first, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("replaced avatar is still served: %d", rec.Code)
	}

	for name, content := range map[string][]byte{
		"vector.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"fake.png":   []byte("not an image at all"),
	} {
		if rec := uploadRequest(t, tc, "/api/v1/profile/avatar", token, name, content); rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: expected 415, got %d", name, rec.Code)
		}
	}
	if profileAvatar() != second {
		t.Fatal("a rejected upload changed the avatar")
	}

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/avatar", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove avatar: %d", rec.Code)
	}
	if profileAvatar() != "" {
		t.Fatalf("avatar_url = %q after removal", profileAvatar())
	}
	if rec := request(t, tc.Router, http.MethodGet, second, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("removed avatar is still served: %d", rec.Code)
	}
}

func TestAvatar_DeletingTheAccountRemovesTheFile(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, "avatar-gone@example.test")

	rec := uploadRequest(t, tc, "/api/v1/profile/avatar", token, "me.png", solidPNG(t, 64, 64, color.NRGBA{R: 200, A: 255}))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload avatar: %d: %s", rec.Code, rec.Body.String())
	}
	url := dataObject(t, rec)["avatar_url"].(string)
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("avatar before deletion: %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/profile", token, map[string]any{"password": "password-123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("avatar of a deleted account is still served: %d", rec.Code)
	}
}
