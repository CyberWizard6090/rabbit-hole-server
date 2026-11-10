package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/config"
)

func (f formFixtureData) patchForm(t *testing.T, tc *TestContext, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body["name"], body["slug"] = "Request", f.slug
	return request(t, tc.Router, http.MethodPatch, f.formURL+"/"+itoa(f.formID), f.owner.token, body)
}

func (f formFixtureData) setSettings(t *testing.T, tc *TestContext, settings map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return f.patchForm(t, tc, map[string]any{"settings": settings})
}

func (f formFixtureData) mustSetSettings(t *testing.T, tc *TestContext, settings map[string]any) {
	t.Helper()
	if rec := f.setSettings(t, tc, settings); rec.Code != http.StatusOK {
		t.Fatalf("set settings: %d: %s", rec.Code, rec.Body.String())
	}
}

func (f formFixtureData) publicForm(t *testing.T, tc *TestContext) (int, map[string]any, string) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/public/forms/"+f.slug, "", nil)
	if rec.Code != http.StatusOK {
		return rec.Code, nil, rec.Body.String()
	}
	return rec.Code, dataObject(t, rec), rec.Body.String()
}

func (f formFixtureData) plainAnswers() map[string]any {
	return map[string]any{itoa(f.questionID): "plain"}
}

func uploadWorkspaceImage(t *testing.T, tc *TestContext, token string, workspaceID uint) (id, url string) {
	t.Helper()
	rec := uploadRequest(t, tc, "/api/v1/workspaces/"+itoa(workspaceID)+"/images", token, "logo.png", smallPNG(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload workspace image: %d: %s", rec.Code, rec.Body.String())
	}
	url = dataObject(t, rec)["url"].(string)
	return url[strings.LastIndex(url, "/")+1:], url
}

func TestFormSettings_AreSavedPublishedAndSnapshotted(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setsnap", nil)
	logoID, logoURL := uploadWorkspaceImage(t, tc, f.owner.token, f.owner.workspaceID)
	coverID, _ := uploadWorkspaceImage(t, tc, f.owner.token, f.owner.workspaceID)
	closes := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)

	rec := f.setSettings(t, tc, map[string]any{
		"submit_button_text": "Send it", "success_message": "We will be in touch",
		"redirect_url": "https://example.test/thanks", "accent_color": "#FF8800",
		"logo_file_id": logoID, "cover_file_id": coverID,
		"notify_on_submit": []uint{f.owner.userID, f.memberID}, "closes_at": closes.Format(time.RFC3339),
		"max_submissions": 100,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set settings: %d: %s", rec.Code, rec.Body.String())
	}
	saved := dataObject(t, rec)["settings"].(map[string]any)
	if saved["submit_button_text"] != "Send it" || saved["accent_color"] != "#ff8800" || saved["logo_file_id"] != logoID ||
		saved["logo_url"] != logoURL || saved["max_submissions"] != float64(100) || len(saved["notify_on_submit"].([]any)) != 2 {
		t.Fatalf("saved settings = %v", saved)
	}

	rec = request(t, tc.Router, http.MethodGet, f.formURL+"/"+itoa(f.formID), f.member, nil)
	if got := dataObject(t, rec)["settings"].(map[string]any); got["success_message"] != "We will be in touch" ||
		got["closes_at"] != closes.Format(time.RFC3339) {
		t.Fatalf("read settings = %v", got)
	}

	rec = f.patchForm(t, tc, map[string]any{"description": "renamed only"})
	if got := dataObject(t, rec)["settings"].(map[string]any); rec.Code != http.StatusOK || got["submit_button_text"] != "Send it" {
		t.Fatalf("an update without settings must keep them: %d %v", rec.Code, got)
	}

	code, before, _ := f.publicForm(t, tc)
	if code != http.StatusOK || before["settings"].(map[string]any)["submit_button_text"] != "" {
		t.Fatalf("the public form still serves the settings of the published version: %d %v", code, before["settings"])
	}

	f.publish(t, tc)
	code, public, raw := f.publicForm(t, tc)
	if code != http.StatusOK {
		t.Fatalf("public form: %d", code)
	}
	settings := public["settings"].(map[string]any)
	if settings["submit_button_text"] != "Send it" || settings["success_message"] != "We will be in touch" ||
		settings["redirect_url"] != "https://example.test/thanks" || settings["accent_color"] != "#ff8800" ||
		settings["logo_url"] != logoURL || settings["closes_at"] != closes.Format(time.RFC3339) ||
		public["closed"] != false || public["full"] != false {
		t.Fatalf("public settings = %v (closed=%v full=%v)", settings, public["closed"], public["full"])
	}
	for _, leaked := range []string{"notify_on_submit", "max_submissions", "workspace_id", "created_by", "logo_file_id", "cover_file_id", "current_version"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("the public schema leaks %q: %s", leaked, raw)
		}
	}
	if image := request(t, tc.Router, http.MethodGet, logoURL, "", nil); image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("the logo URL must be served without authentication: %d", image.Code)
	}

	f.mustSetSettings(t, tc, map[string]any{"submit_button_text": "Changed", "accent_color": "#000000"})
	_, public, _ = f.publicForm(t, tc)
	if got := public["settings"].(map[string]any); got["submit_button_text"] != "Send it" || got["logo_url"] != logoURL {
		t.Fatalf("editing a published form must not change the live version: %v", got)
	}
	f.publish(t, tc)
	_, public, _ = f.publicForm(t, tc)
	if got := public["settings"].(map[string]any); got["submit_button_text"] != "Changed" || got["logo_url"] != "" || got["accent_color"] != "#000000" {
		t.Fatalf("after republishing: %v", got)
	}

	var texts []string
	if err := tc.DB.Raw("SELECT schema_snapshot->'settings'->>'submit_button_text' FROM form_versions WHERE form_id = ? ORDER BY version", f.formID).
		Scan(&texts).Error; err != nil {
		t.Fatal(err)
	}
	if len(texts) < 3 || texts[len(texts)-1] != "Changed" || texts[len(texts)-2] != "Send it" {
		t.Fatalf("every version keeps its own settings: %v", texts)
	}
}

func TestFormSettings_CanBeSetWhenCreatingAForm(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "setcreate")
	formURL := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/forms"
	rec := request(t, tc.Router, http.MethodPost, formURL, owner.token, map[string]any{
		"name": "With settings", "slug": fmt.Sprintf("setcreate-%d", time.Now().UnixNano()),
		"settings": map[string]any{"submit_button_text": "Go", "notify_on_submit": []uint{owner.userID}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", rec.Code, rec.Body.String())
	}
	settings := dataObject(t, rec)["settings"].(map[string]any)
	if settings["submit_button_text"] != "Go" || len(settings["notify_on_submit"].([]any)) != 1 {
		t.Fatalf("settings = %v", settings)
	}
	rec = request(t, tc.Router, http.MethodPost, formURL, owner.token, map[string]any{
		"name": "Without", "slug": fmt.Sprintf("setcreate2-%d", time.Now().UnixNano()),
	})
	settings = dataObject(t, rec)["settings"].(map[string]any)
	if settings["submit_button_text"] != "" || settings["max_submissions"] != nil || settings["closes_at"] != nil ||
		len(settings["notify_on_submit"].([]any)) != 0 {
		t.Fatalf("default settings = %v", settings)
	}
	rec = request(t, tc.Router, http.MethodPost, formURL, owner.token, map[string]any{
		"name": "Bad", "slug": fmt.Sprintf("setcreate3-%d", time.Now().UnixNano()),
		"settings": map[string]any{"accent_color": "red"},
	})
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_ACCENT_COLOR_INVALID" {
		t.Fatalf("invalid settings on create: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormSettings_ValidationRejectsBadValuesWithClearCodes(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Environment = "test" })
	f := newUploadFixture(t, tc, "setval", nil)
	other := newWorkspaceOwner(t, tc, "setval-other")
	foreignImage, _ := uploadWorkspaceImage(t, tc, other.token, other.workspaceID)
	avatar := uploadRequest(t, tc, "/api/v1/profile/avatar", f.owner.token, "me.png", smallPNG(t))
	if avatar.Code != http.StatusOK {
		t.Fatalf("avatar: %d: %s", avatar.Code, avatar.Body.String())
	}
	avatarURL := dataObject(t, avatar)["avatar_url"].(string)
	avatarID := avatarURL[strings.LastIndex(avatarURL, "/")+1:]
	outsiderID := other.userID

	cases := []struct {
		name     string
		settings map[string]any
		code     string
	}{
		{"button text too long", map[string]any{"submit_button_text": strings.Repeat("x", 81)}, "FORM_SUBMIT_BUTTON_TEXT_INVALID"},
		{"message too long", map[string]any{"success_message": strings.Repeat("x", 2001)}, "FORM_SUCCESS_MESSAGE_INVALID"},
		{"javascript redirect", map[string]any{"redirect_url": "javascript:alert(1)"}, "FORM_REDIRECT_URL_INVALID"},
		{"data redirect", map[string]any{"redirect_url": "data:text/html;base64,AAAA"}, "FORM_REDIRECT_URL_INVALID"},
		{"plain http redirect", map[string]any{"redirect_url": "http://example.test/"}, "FORM_REDIRECT_URL_INVALID"},
		{"relative redirect", map[string]any{"redirect_url": "/thanks"}, "FORM_REDIRECT_URL_INVALID"},
		{"redirect with credentials", map[string]any{"redirect_url": "https://me:pw@example.test/"}, "FORM_REDIRECT_URL_INVALID"},
		{"bad color", map[string]any{"accent_color": "#12345"}, "FORM_ACCENT_COLOR_INVALID"},
		{"color name", map[string]any{"accent_color": "orange"}, "FORM_ACCENT_COLOR_INVALID"},
		{"malformed logo", map[string]any{"logo_file_id": "12"}, "FORM_LOGO_INVALID"},
		{"unknown logo", map[string]any{"logo_file_id": strings.Repeat("a", 32)}, "FORM_LOGO_INVALID"},
		{"logo of another workspace", map[string]any{"logo_file_id": foreignImage}, "FORM_LOGO_INVALID"},
		{"avatar as cover", map[string]any{"cover_file_id": avatarID}, "FORM_COVER_INVALID"},
		{"recipient outside the workspace", map[string]any{"notify_on_submit": []uint{outsiderID}}, "FORM_NOTIFY_RECIPIENT_INVALID"},
		{"zero recipient", map[string]any{"notify_on_submit": []uint{0}}, "FORM_NOTIFY_RECIPIENT_INVALID"},
		{"zero submissions", map[string]any{"max_submissions": 0}, "FORM_MAX_SUBMISSIONS_INVALID"},
		{"negative submissions", map[string]any{"max_submissions": -3}, "FORM_MAX_SUBMISSIONS_INVALID"},
	}
	for _, tc2 := range cases {
		rec := f.setSettings(t, tc, tc2.settings)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != tc2.code {
			t.Errorf("%s: %d %s (want %s)", tc2.name, rec.Code, rec.Body.String(), tc2.code)
		}
	}
	if rec := f.setSettings(t, tc, map[string]any{"closes_at": "tomorrow"}); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed closes_at: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPatch, f.formURL+"/"+itoa(f.formID), f.member,
		map[string]any{"name": "x", "slug": f.slug, "settings": map[string]any{}}); rec.Code != http.StatusForbidden {
		t.Errorf("a member without the update permission: %d", rec.Code)
	}
	_, public, _ := f.publicForm(t, tc)
	if public["settings"].(map[string]any)["accent_color"] != "" {
		t.Fatalf("rejected settings must not be stored: %v", public["settings"])
	}
}

func TestFormSettings_HTTPRedirectIsAllowedInDevelopmentOnly(t *testing.T) {
	dev := newTestContextWith(t, func(cfg *config.Config) { cfg.Environment = "development" })
	f := newUploadFixture(t, dev, "sethttp", nil)
	if rec := f.setSettings(t, dev, map[string]any{"redirect_url": "http://localhost:3000/thanks"}); rec.Code != http.StatusOK {
		t.Fatalf("development: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := f.setSettings(t, dev, map[string]any{"redirect_url": "javascript:alert(1)"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("javascript scheme in development: %d", rec.Code)
	}
}

func TestFormSettings_ClosedFormsStillRenderButRefuseSubmissions(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setclose", nil)
	f.mustSetSettings(t, tc, map[string]any{"closes_at": time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano)})
	f.publish(t, tc)

	if _, public, _ := f.publicForm(t, tc); public["closed"] != false {
		t.Fatalf("the form is still open: %v", public["closed"])
	}
	png := f.mustUpload(t, tc, "early.png", smallPNG(t))
	rec := f.submitWith(t, tc, "early", f.answers("Early", png))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submission before the deadline: %d: %s", rec.Code, rec.Body.String())
	}

	time.Sleep(2200 * time.Millisecond)
	code, public, _ := f.publicForm(t, tc)
	if code != http.StatusOK || public["closed"] != true || public["name"] != "Request" {
		t.Fatalf("a closed form is still shown, flagged as closed: %d %v", code, public)
	}
	rec = f.submitWith(t, tc, "late", f.plainAnswers())
	if rec.Code != http.StatusGone || errorCode(t, rec) != "FORM_CLOSED" {
		t.Fatalf("submission after the deadline: %d: %s", rec.Code, rec.Body.String())
	}
	if rec = f.submitWith(t, tc, "early", f.answers("Early", png)); rec.Code != http.StatusOK {
		t.Fatalf("a replay of a submission accepted before the deadline: %d: %s", rec.Code, rec.Body.String())
	}
	if rec = f.upload(t, tc, "late.png", smallPNG(t)); rec.Code != http.StatusGone || errorCode(t, rec) != "FORM_CLOSED" {
		t.Fatalf("upload after the deadline: %d: %s", rec.Code, rec.Body.String())
	}
	var submissions int64
	_ = tc.DB.Raw("SELECT count(*) FROM form_submissions WHERE form_id = ?", f.formID).Scan(&submissions).Error
	if submissions != 1 {
		t.Fatalf("submissions = %d", submissions)
	}
}

func TestFormSettings_DeadlineInThePastClosesTheFormImmediately(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setpast", nil)
	f.mustSetSettings(t, tc, map[string]any{"closes_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)})
	f.publish(t, tc)
	if _, public, _ := f.publicForm(t, tc); public["closed"] != true {
		t.Fatalf("closed = %v", public["closed"])
	}
	if rec := f.submitWith(t, tc, "past", f.plainAnswers()); rec.Code != http.StatusGone {
		t.Fatalf("submit: %d", rec.Code)
	}
	f.mustSetSettings(t, tc, map[string]any{})
	f.publish(t, tc)
	if rec := f.submitWith(t, tc, "reopened", f.plainAnswers()); rec.Code != http.StatusCreated {
		t.Fatalf("a reopened form accepts submissions: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormSettings_SubmissionLimitIsEnforcedInsideTheTransaction(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setfull", nil)
	f.mustSetSettings(t, tc, map[string]any{"max_submissions": 2})
	f.publish(t, tc)

	for i := 1; i <= 2; i++ {
		if rec := f.submitWith(t, tc, fmt.Sprintf("full-%d", i), f.plainAnswers()); rec.Code != http.StatusCreated {
			t.Fatalf("submission %d: %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	if _, public, _ := f.publicForm(t, tc); public["full"] != true || public["closed"] != false {
		t.Fatalf("full = %v closed = %v", public["full"], public["closed"])
	}
	rec := f.submitWith(t, tc, "full-3", f.plainAnswers())
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "FORM_FULL" {
		t.Fatalf("third submission: %d: %s", rec.Code, rec.Body.String())
	}
	if rec = f.submitWith(t, tc, "full-1", f.plainAnswers()); rec.Code != http.StatusOK {
		t.Fatalf("a replay of an accepted submission still works: %d: %s", rec.Code, rec.Body.String())
	}
	if rec = f.upload(t, tc, "x.png", smallPNG(t)); rec.Code != http.StatusCreated {
		t.Fatalf("uploads are not blocked by the submission limit: %d", rec.Code)
	}
}

func TestFormSettings_SubmissionLimitHoldsUnderConcurrency(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setrace", nil)
	f.mustSetSettings(t, tc, map[string]any{"max_submissions": 3})
	f.publish(t, tc)

	const workers = 12
	codes := make([]int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = f.submitWith(t, tc, fmt.Sprintf("race-%d", i), f.plainAnswers()).Code
		}(i)
	}
	wg.Wait()
	created, full := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			full++
		default:
			t.Fatalf("unexpected status in %v", codes)
		}
	}
	var stored int64
	_ = tc.DB.Raw("SELECT count(*) FROM form_submissions WHERE form_id = ?", f.formID).Scan(&stored).Error
	if created != 3 || full != workers-3 || stored != 3 {
		t.Fatalf("created=%d full=%d stored=%d codes=%v", created, full, stored, codes)
	}
}

func TestFormSettings_LimitAndDeadlineBelongToTheirVersion(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setver", nil)
	f.mustSetSettings(t, tc, map[string]any{"max_submissions": 1})
	f.publish(t, tc)
	if rec := f.submitWith(t, tc, "ver-1", f.plainAnswers()); rec.Code != http.StatusCreated {
		t.Fatalf("first: %d", rec.Code)
	}
	f.mustSetSettings(t, tc, map[string]any{"max_submissions": 5})
	if rec := f.submitWith(t, tc, "ver-2", f.plainAnswers()); rec.Code != http.StatusConflict {
		t.Fatalf("an unpublished edit must not raise the limit: %d", rec.Code)
	}
	f.publish(t, tc)
	if rec := f.submitWith(t, tc, "ver-3", f.plainAnswers()); rec.Code != http.StatusCreated {
		t.Fatalf("the republished limit applies: %d", rec.Code)
	}
}

func TestFormSettings_NotifyOnSubmitCreatesInAppNotifications(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setnotify", nil)
	f.mustSetSettings(t, tc, map[string]any{"notify_on_submit": []uint{f.owner.userID, f.memberID, f.guestID}})
	f.publish(t, tc)

	mailBefore := len(tc.Mail.Messages())
	rec := f.submitWith(t, tc, "notify-1", f.plainAnswers())
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	tc.Notifications.Wait()

	for name, token := range map[string]string{"owner": f.owner.token, "member": f.member} {
		entries := inbox(t, tc, token, "?type=form_submission")
		if len(entries) != 1 || entries[0].Read {
			t.Fatalf("%s inbox = %v", name, entries)
		}
	}
	if entries := inbox(t, tc, f.guest, "?type=form_submission"); len(entries) != 0 {
		t.Fatalf("a listed guest must not be told about submissions: %v", entries)
	}
	if got := inbox(t, tc, f.outsider, ""); len(got) != 0 {
		t.Fatalf("a user who is not listed got %v", got)
	}

	list := request(t, tc.Router, http.MethodGet, "/api/v1/notifications?type=form_submission", f.member, nil)
	item := responseArray(t, list)[0].(map[string]any)
	payload := item["payload"].(map[string]any)
	if _, hasTask := item["task_id"]; hasTask || item["workspace_id"] != float64(f.owner.workspaceID) ||
		payload["form_id"] != float64(f.formID) || payload["submission_id"] != float64(submissionID) || payload["form_name"] != "Request" {
		t.Fatalf("notification = %v", item)
	}
	if strings.Contains(list.Body.String(), "recipients") || strings.Contains(list.Body.String(), "notify_on_submit") {
		t.Fatalf("the recipients must not be exposed: %s", list.Body.String())
	}

	counts := request(t, tc.Router, http.MethodGet, "/api/v1/notifications/unread-count", f.member, nil)
	if !strings.Contains(counts.Body.String(), `"form_submission":1`) {
		t.Fatalf("unread counts: %s", counts.Body.String())
	}
	if replay := f.submitWith(t, tc, "notify-1", f.plainAnswers()); replay.Code != http.StatusOK {
		t.Fatalf("replay: %d", replay.Code)
	}
	tc.Notifications.Wait()
	if entries := inbox(t, tc, f.member, "?type=form_submission"); len(entries) != 1 {
		t.Fatalf("a replay must not notify again: %v", entries)
	}
	if len(tc.Mail.Messages()) != mailBefore {
		t.Fatalf("e-mail is off by default: %d new messages", len(tc.Mail.Messages())-mailBefore)
	}

	read := request(t, tc.Router, http.MethodPost, "/api/v1/notifications/read", f.member, map[string]any{"all": true, "type": "form_submission"})
	if read.Code != http.StatusOK {
		t.Fatalf("mark read: %d: %s", read.Code, read.Body.String())
	}
	if entries := inbox(t, tc, f.member, "?type=form_submission"); len(entries) != 1 || !entries[0].Read {
		t.Fatalf("after marking read: %v", entries)
	}

	if rec := request(t, tc.Router, http.MethodDelete, membersPath(f.owner.workspaceID, "/"+itoa(f.memberID)), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := f.submitWith(t, tc, "notify-2", f.plainAnswers()); rec.Code != http.StatusCreated {
		t.Fatalf("second submit: %d", rec.Code)
	}
	tc.Notifications.Wait()
	if entries := inbox(t, tc, f.owner.token, "?type=form_submission"); len(entries) != 2 {
		t.Fatalf("owner inbox = %v", entries)
	}
	var memberRows int64
	_ = tc.DB.Raw("SELECT count(*) FROM notifications WHERE user_id = ?", f.memberID).Scan(&memberRows).Error
	if memberRows != 1 {
		t.Fatalf("a removed member must not be notified: %d rows", memberRows)
	}
}

func TestFormSettings_NotifyOnSubmitOnlyAcceptsWorkspaceMembers(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "setnotifybad", nil)
	strangerID, _ := newUser(t, tc, "setnotifybad-stranger@example.test")
	rec := f.setSettings(t, tc, map[string]any{"notify_on_submit": []uint{f.memberID, strangerID}})
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "FORM_NOTIFY_RECIPIENT_INVALID" {
		t.Fatalf("%d: %s", rec.Code, rec.Body.String())
	}
	rec = f.setSettings(t, tc, map[string]any{"notify_on_submit": []uint{f.memberID, f.memberID}})
	if got := dataObject(t, rec)["settings"].(map[string]any)["notify_on_submit"].([]any); rec.Code != http.StatusOK || len(got) != 1 {
		t.Fatalf("duplicates are collapsed: %d %v", rec.Code, got)
	}
}
