package integration

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"rabbit-hole-server/internal/config"
)

func rgbaPNG(t *testing.T, width, height int, pix []byte) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, pix)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func workspaceAvatarPath(workspaceID uint) string {
	return "/api/v1/workspaces/" + itoa(workspaceID) + "/avatar"
}

func storedObjects(t *testing.T, tc *TestContext, prefix string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(tc.StorageDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(tc.StorageDir, path)
		if relErr != nil {
			return relErr
		}
		if strings.HasPrefix(filepath.ToSlash(rel), prefix) {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk storage: %v", err)
	}
	return found
}

func workspaceAvatarURL(t *testing.T, tc *TestContext, token string, workspaceID uint) string {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/workspaces/"+itoa(workspaceID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get workspace: %d: %s", rec.Code, rec.Body.String())
	}
	url, ok := dataObject(t, rec)["avatar_url"].(string)
	if !ok {
		t.Fatalf("workspace has no avatar_url string: %s", rec.Body.String())
	}
	return url
}

func workspaceAvatarFixture(t *testing.T, tc *TestContext) (owner workspaceOwner, admin, member, guest, outsider string) {
	t.Helper()
	owner = newWorkspaceOwner(t, tc, "wsavatar")
	adminID, adminToken := newUser(t, tc, "wsavatar-admin@example.test")
	memberID, memberToken := newUser(t, tc, "wsavatar-member@example.test")
	guestID, guestToken := newUser(t, tc, "wsavatar-guest@example.test")
	_, outsiderToken := newUser(t, tc, "wsavatar-outsider@example.test")
	for _, u := range []struct {
		id   uint
		role string
	}{{adminID, "Admin"}, {memberID, "Member"}, {guestID, "Guest"}} {
		addContact(t, tc, owner.token, u.id)
		rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
			map[string]any{"user_id": u.id, "role": u.role})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
	}
	return owner, adminToken, memberToken, guestToken, outsiderToken
}

func TestWorkspaceAvatar_OnlyWhoMayUpdateTheWorkspaceCanChangeIt(t *testing.T) {
	tc := newTestContext(t)
	owner, admin, member, guest, outsider := workspaceAvatarFixture(t, tc)
	path := workspaceAvatarPath(owner.workspaceID)
	png := solidPNG(t, 64, 64, color.NRGBA{R: 200, A: 255})

	for name, token := range map[string]string{"member": member, "guest": guest, "outsider": outsider} {
		if rec := uploadRequest(t, tc, path, token, "a.png", png); rec.Code != http.StatusForbidden {
			t.Errorf("%s upload: expected 403, got %d: %s", name, rec.Code, rec.Body.String())
		}
		if rec := request(t, tc.Router, http.MethodDelete, path, token, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s delete: expected 403, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	if got := workspaceAvatarURL(t, tc, owner.token, owner.workspaceID); got != "" {
		t.Fatalf("refused requests changed the avatar to %q", got)
	}
	if rec := uploadRequest(t, tc, path, "", "a.png", png); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous upload: expected 401, got %d", rec.Code)
	}

	rename := request(t, tc.Router, http.MethodPatch, "/api/v1/workspaces/"+itoa(owner.workspaceID), admin, map[string]any{"name": "by admin"})
	adminUpload := uploadRequest(t, tc, path, admin, "admin.png", png)
	adminDelete := request(t, tc.Router, http.MethodDelete, path, admin, nil)
	if adminUpload.Code != rename.Code {
		t.Fatalf("admin upload got %d but renaming the workspace got %d: both need workspace.update", adminUpload.Code, rename.Code)
	}
	wantDelete := http.StatusOK
	if rename.Code == http.StatusForbidden {
		wantDelete = http.StatusForbidden
	}
	if adminDelete.Code != wantDelete {
		t.Fatalf("admin delete got %d, want %d", adminDelete.Code, wantDelete)
	}

	rec := uploadRequest(t, tc, path, owner.token, "owner.png", png)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner upload: %d: %s", rec.Code, rec.Body.String())
	}
	url := dataObject(t, rec)["avatar_url"].(string)
	if !strings.HasPrefix(url, "/api/v1/images/") {
		t.Fatalf("avatar_url = %q", url)
	}
	if rec := request(t, tc.Router, http.MethodDelete, path, owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("owner delete: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceAvatar_UploadReplaceRemoveAndServe(t *testing.T) {
	tc := newTestContext(t)
	owner, _, member, guest, _ := workspaceAvatarFixture(t, tc)
	path := workspaceAvatarPath(owner.workspaceID)

	rec := uploadRequest(t, tc, path, owner.token, "logo.jpg", jpegWithOrientation(t, 6))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	first := dataObject(t, rec)["avatar_url"].(string)
	if got := workspaceAvatarURL(t, tc, owner.token, owner.workspaceID); got != first {
		t.Fatalf("workspace avatar_url = %q, upload returned %q", got, first)
	}
	if got := workspaceAvatarURL(t, tc, guest, owner.workspaceID); got != first {
		t.Fatalf("guest sees %q", got)
	}
	rotated := decodeServed(t, tc, first)
	if b := rotated.Bounds(); b.Dx() != 20 || b.Dy() != 20 {
		t.Fatalf("avatar size = %v, want a 20x20 square", b)
	}
	if !hasColor(rotated.At(10, 2), true, false, false) || !hasColor(rotated.At(10, 17), false, false, true) {
		t.Fatalf("EXIF orientation was not applied: top %v bottom %v", rotated.At(10, 2), rotated.At(10, 17))
	}
	served := request(t, tc.Router, http.MethodGet, first, "", nil)
	for header, want := range map[string]string{
		"Content-Security-Policy":      "default-src 'none'; sandbox",
		"Cross-Origin-Resource-Policy": "cross-origin",
		"X-Content-Type-Options":       "nosniff",
		"Cache-Control":                "public, max-age=31536000, immutable",
	} {
		if got := served.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 1 {
		t.Fatalf("stored avatar objects = %v", objects)
	}

	big := solidPNG(t, 1000, 500, color.NRGBA{G: 200, A: 255})
	rec = uploadRequest(t, tc, path, owner.token, "big.png", big)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d: %s", rec.Code, rec.Body.String())
	}
	second := dataObject(t, rec)["avatar_url"].(string)
	if second == first {
		t.Fatal("avatar_url did not change")
	}
	if b := decodeServed(t, tc, second).Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("avatar size = %v, want 256x256", b)
	}
	if rec := request(t, tc.Router, http.MethodGet, first, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("replaced avatar is still served: %d", rec.Code)
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 1 {
		t.Fatalf("the replaced object leaked: %v", objects)
	}

	for name, tcase := range map[string]struct {
		file    string
		content []byte
		code    string
		status  int
	}{
		"svg":        {"vector.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "UNSUPPORTED_FILE_TYPE", http.StatusUnsupportedMediaType},
		"text":       {"notes.png", []byte("not an image at all"), "UNSUPPORTED_FILE_TYPE", http.StatusUnsupportedMediaType},
		"empty":      {"empty.png", nil, "FILE_EMPTY", http.StatusBadRequest},
		"truncated":  {"cut.png", big[:12], "INVALID_IMAGE", http.StatusBadRequest},
		"no file":    {"", nil, "FILE_REQUIRED", http.StatusBadRequest},
		"json named": {"x.png", []byte(`{"a":1}`), "UNSUPPORTED_FILE_TYPE", http.StatusUnsupportedMediaType},
	} {
		t.Run(name, func(t *testing.T) {
			rec := uploadRequest(t, tc, path, owner.token, tcase.file, tcase.content)
			fcExpectError(t, rec, tcase.status, tcase.code, "")
		})
	}
	if got := workspaceAvatarURL(t, tc, member, owner.workspaceID); got != second {
		t.Fatalf("a rejected upload changed the avatar: %q", got)
	}

	rec = request(t, tc.Router, http.MethodDelete, path, owner.token, nil)
	if rec.Code != http.StatusOK || dataObject(t, rec)["avatar_url"] != "" {
		t.Fatalf("delete: %d: %s", rec.Code, rec.Body.String())
	}
	if got := workspaceAvatarURL(t, tc, owner.token, owner.workspaceID); got != "" {
		t.Fatalf("avatar_url = %q after removal", got)
	}
	if rec := request(t, tc.Router, http.MethodGet, second, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("removed avatar is still served: %d", rec.Code)
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 0 {
		t.Fatalf("the removed object leaked: %v", objects)
	}
	if rec := request(t, tc.Router, http.MethodDelete, path, owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("second delete must succeed: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceAvatar_SizeLimit(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Storage.MaxAvatarBytes = 3000 })
	owner := newWorkspaceOwner(t, tc, "wsavatar-size")
	path := workspaceAvatarPath(owner.workspaceID)

	noise := make([]byte, 5000)
	rand.New(rand.NewSource(1)).Read(noise)
	rec := uploadRequest(t, tc, path, owner.token, "huge.png", noise)
	fcExpectError(t, rec, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "")
	rec = uploadRequest(t, tc, path, owner.token, "huger.png", make([]byte, 2<<20))
	fcExpectError(t, rec, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "")

	if got := workspaceAvatarURL(t, tc, owner.token, owner.workspaceID); got != "" {
		t.Fatalf("avatar_url = %q after rejected uploads", got)
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 0 {
		t.Fatalf("rejected uploads left objects: %v", objects)
	}
}

func TestWorkspaceAvatar_BodyLimitOverrideMatchesTheRoute(t *testing.T) {
	routeKey := "POST /api/v1/workspaces/:workspace_id/avatar"
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Storage.MaxAvatarBytes = 4 << 20 })
	registered := false
	for _, route := range apiRoutes(t, tc) {
		if route.Method+" "+route.Path == routeKey {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("route %q is not registered; the body limit override key must match c.FullPath()", routeKey)
	}

	owner := newWorkspaceOwner(t, tc, "wsavatar-limit")
	noise := make([]byte, 600*600*4)
	rand.New(rand.NewSource(2)).Read(noise)
	large := rgbaPNG(t, 600, 600, noise)
	if len(large) <= 1<<20 {
		t.Fatalf("fixture is only %d bytes; it must exceed the default 1 MiB body cap", len(large))
	}
	rec := uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), owner.token, "large.png", large)
	if rec.Code != http.StatusOK {
		t.Fatalf("an avatar above the default body cap but within MAX_AVATAR_BYTES: %d: %s", rec.Code, rec.Body.String())
	}

	tooBig := make([]byte, 6<<20)
	rec = uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), owner.token, "x.png", tooBig)
	fcExpectError(t, rec, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "")
}

func TestWorkspaceAvatar_AvatarURLIsInEveryWorkspaceResponse(t *testing.T) {
	tc := newTestContext(t)
	owner, admin, _, _, _ := workspaceAvatarFixture(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", owner.token, map[string]any{"name": "fresh"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", rec.Code, rec.Body.String())
	}
	if v, ok := dataObject(t, rec)["avatar_url"]; !ok || v != "" {
		t.Fatalf("create response avatar_url = %v (present %v)", v, ok)
	}

	rec = uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), owner.token, "a.png", solidPNG(t, 32, 32, color.NRGBA{B: 255, A: 255}))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	url := dataObject(t, rec)["avatar_url"].(string)

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/workspaces/"+itoa(owner.workspaceID), owner.token, map[string]any{"name": "renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d: %s", rec.Code, rec.Body.String())
	}
	if got := dataObject(t, rec)["avatar_url"]; got != url {
		t.Fatalf("update response avatar_url = %v, want %q (a rename must not clear it)", got, url)
	}

	for _, token := range []string{owner.token, admin} {
		rec = request(t, tc.Router, http.MethodGet, "/api/v1/workspaces", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d", rec.Code)
		}
		found := false
		for _, item := range decodeJSON(t, rec)["data"].([]any) {
			ws := item.(map[string]any)
			v, ok := ws["avatar_url"]
			if !ok {
				t.Fatalf("list item without avatar_url: %v", ws)
			}
			if uint(ws["id"].(float64)) == owner.workspaceID {
				found = true
				if v != url {
					t.Fatalf("list avatar_url = %v, want %q", v, url)
				}
			}
		}
		if !found {
			t.Fatal("workspace missing from the list")
		}
	}
}

func TestWorkspaceAvatar_DeletedWorkspaceServesNothingAndLeavesNoObject(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "wsavatar-gone")

	rec := uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), owner.token, "a.png", solidPNG(t, 32, 32, color.NRGBA{R: 255, A: 255}))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	url := dataObject(t, rec)["avatar_url"].(string)
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("avatar before deletion: %d", rec.Code)
	}
	if len(storedObjects(t, tc, "workspace_avatar/")) != 1 {
		t.Fatal("expected one stored avatar object")
	}

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(owner.workspaceID), owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete workspace: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("avatar of a deleted workspace is still served: %d", rec.Code)
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 0 {
		t.Fatalf("deleting the workspace leaked objects: %v", objects)
	}
	var rows int64
	tc.DB.Table("files").Where("workspace_id = ? AND kind = ?", owner.workspaceID, "workspace_avatar").Count(&rows)
	if rows != 0 {
		t.Fatalf("file rows left behind: %d", rows)
	}
	if rec := uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), owner.token, "a.png", solidPNG(t, 8, 8, color.NRGBA{A: 255})); rec.Code == http.StatusOK {
		t.Fatal("uploading to a deleted workspace must fail")
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 0 {
		t.Fatalf("a refused upload left objects: %v", objects)
	}
}

func TestWorkspaceAvatar_UserAvatarCleanupLeavesTheWorkspaceAvatarAlone(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "wsavatar-mixed")

	rec := uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), owner.token, "ws.png", solidPNG(t, 32, 32, color.NRGBA{G: 255, A: 255}))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	url := dataObject(t, rec)["avatar_url"].(string)
	if rec := uploadRequest(t, tc, "/api/v1/profile/avatar", owner.token, "me.png", solidPNG(t, 32, 32, color.NRGBA{R: 255, A: 255})); rec.Code != http.StatusOK {
		t.Fatalf("user avatar: %d", rec.Code)
	}
	if objects := storedObjects(t, tc, "avatar/"); len(objects) != 1 {
		t.Fatalf("user avatar objects = %v", objects)
	}

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/avatar", owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove user avatar: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("the workspace avatar vanished with the user avatar: %d", rec.Code)
	}
	if got := workspaceAvatarURL(t, tc, owner.token, owner.workspaceID); got != url {
		t.Fatalf("avatar_url = %q, want %q", got, url)
	}
	if objects := storedObjects(t, tc, "avatar/"); len(objects) != 0 {
		t.Fatalf("the user avatar leaked: %v", objects)
	}
	if objects := storedObjects(t, tc, "workspace_avatar/"); len(objects) != 1 {
		t.Fatalf("workspace avatar objects = %v", objects)
	}
}

func TestWorkspaceAvatar_RoutesAreRegisteredWithPermissions(t *testing.T) {
	tc := newTestContext(t)
	want := map[string]bool{
		"POST /api/v1/workspaces/:workspace_id/avatar":   false,
		"DELETE /api/v1/workspaces/:workspace_id/avatar": false,
	}
	for _, route := range apiRoutes(t, tc) {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
			if _, public := publicRoutes[key]; public {
				t.Errorf("%s must not be public", key)
			}
			if _, self := selfScopedRoutes[key]; self {
				t.Errorf("%s must not be self scoped", key)
			}
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("route %s is missing", key)
		}
	}
}
