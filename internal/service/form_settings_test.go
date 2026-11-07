package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

const (
	testImageID = "0123456789abcdef0123456789abcdef"
	otherImage  = "fedcba9876543210fedcba9876543210"
)

type settingsRepoFake struct {
	domain.FormRepository
	images  map[string]bool
	members map[uint]bool
}

func (f *settingsRepoFake) WorkspaceImageExists(_ context.Context, _ uint, publicID string) (bool, error) {
	return f.images[publicID], nil
}

func (f *settingsRepoFake) WorkspaceMemberIDs(_ context.Context, _ uint, ids []uint) ([]uint, error) {
	var found []uint
	for _, id := range ids {
		if f.members[id] {
			found = append(found, id)
		}
	}
	return found, nil
}

func newSettingsService(insecure bool) *formService {
	repo := &settingsRepoFake{images: map[string]bool{testImageID: true}, members: map[uint]bool{3: true, 4: true}}
	return NewFormService(repo).WithSettingsPolicy(insecure, "https://api.example.test/")
}

func TestNormalizeSettingsAcceptsValidInput(t *testing.T) {
	s := newSettingsService(false)
	closes := time.Date(2030, 1, 2, 3, 4, 5, 999, time.FixedZone("x", 3600))
	limit := 50
	got, err := s.normalizeSettings(context.Background(), 1, domain.FormSettings{
		SubmitButtonText: "  Send  ", SuccessMessage: "Thanks", RedirectURL: "https://example.test/thanks?x=1",
		AccentColor: "#AABBCC", LogoFileID: testImageID, CoverFileID: "https://api.example.test/api/v1/images/" + testImageID,
		NotifyOnSubmit: []uint{3, 4, 3}, ClosesAt: &closes, MaxSubmissions: &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.SubmitButtonText != "Send" || got.AccentColor != "#aabbcc" || got.LogoFileID != testImageID ||
		got.CoverFileID != testImageID || len(got.NotifyOnSubmit) != 2 || *got.MaxSubmissions != 50 {
		t.Fatalf("settings = %+v", got)
	}
	if got.ClosesAt.Location() != time.UTC || got.ClosesAt.Nanosecond() != 0 {
		t.Fatalf("closes_at = %v", got.ClosesAt)
	}
}

func TestNormalizeSettingsDefaultsAreEmpty(t *testing.T) {
	got, err := newSettingsService(false).normalizeSettings(context.Background(), 1, domain.FormSettings{})
	if err != nil || got.NotifyOnSubmit == nil || len(got.NotifyOnSubmit) != 0 || got.MaxSubmissions != nil || got.ClosesAt != nil {
		t.Fatalf("settings = %+v, err = %v", got, err)
	}
}

func TestNormalizeSettingsRejectsInvalidInput(t *testing.T) {
	zero, tooMany := 0, 1_000_001
	cases := []struct {
		name     string
		settings domain.FormSettings
		code     string
	}{
		{"long button text", domain.FormSettings{SubmitButtonText: strings.Repeat("я", 81)}, "FORM_SUBMIT_BUTTON_TEXT_INVALID"},
		{"long success message", domain.FormSettings{SuccessMessage: strings.Repeat("a", 2001)}, "FORM_SUCCESS_MESSAGE_INVALID"},
		{"javascript redirect", domain.FormSettings{RedirectURL: "javascript:alert(1)"}, "FORM_REDIRECT_URL_INVALID"},
		{"data redirect", domain.FormSettings{RedirectURL: "data:text/html,hi"}, "FORM_REDIRECT_URL_INVALID"},
		{"http redirect outside development", domain.FormSettings{RedirectURL: "http://example.test/x"}, "FORM_REDIRECT_URL_INVALID"},
		{"relative redirect", domain.FormSettings{RedirectURL: "/thanks"}, "FORM_REDIRECT_URL_INVALID"},
		{"scheme relative redirect", domain.FormSettings{RedirectURL: "//evil.test/x"}, "FORM_REDIRECT_URL_INVALID"},
		{"redirect with credentials", domain.FormSettings{RedirectURL: "https://user:pass@example.test/"}, "FORM_REDIRECT_URL_INVALID"},
		{"redirect with a space", domain.FormSettings{RedirectURL: "https://example.test/a b"}, "FORM_REDIRECT_URL_INVALID"},
		{"redirect with a backslash", domain.FormSettings{RedirectURL: "https://example.test\\@evil.test"}, "FORM_REDIRECT_URL_INVALID"},
		{"redirect without a host", domain.FormSettings{RedirectURL: "https://"}, "FORM_REDIRECT_URL_INVALID"},
		{"long redirect", domain.FormSettings{RedirectURL: "https://example.test/" + strings.Repeat("a", 2100)}, "FORM_REDIRECT_URL_INVALID"},
		{"short color", domain.FormSettings{AccentColor: "#abc"}, "FORM_ACCENT_COLOR_INVALID"},
		{"color without hash", domain.FormSettings{AccentColor: "aabbcc"}, "FORM_ACCENT_COLOR_INVALID"},
		{"color with letters out of range", domain.FormSettings{AccentColor: "#gggggg"}, "FORM_ACCENT_COLOR_INVALID"},
		{"malformed logo", domain.FormSettings{LogoFileID: "42"}, "FORM_LOGO_INVALID"},
		{"unknown logo", domain.FormSettings{LogoFileID: otherImage}, "FORM_LOGO_INVALID"},
		{"unknown cover", domain.FormSettings{CoverFileID: otherImage}, "FORM_COVER_INVALID"},
		{"non member recipient", domain.FormSettings{NotifyOnSubmit: []uint{3, 99}}, "FORM_NOTIFY_RECIPIENT_INVALID"},
		{"zero recipient", domain.FormSettings{NotifyOnSubmit: []uint{0}}, "FORM_NOTIFY_RECIPIENT_INVALID"},
		{"too many recipients", domain.FormSettings{NotifyOnSubmit: make([]uint, 51)}, "FORM_NOTIFY_RECIPIENT_INVALID"},
		{"zero limit", domain.FormSettings{MaxSubmissions: &zero}, "FORM_MAX_SUBMISSIONS_INVALID"},
		{"huge limit", domain.FormSettings{MaxSubmissions: &tooMany}, "FORM_MAX_SUBMISSIONS_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newSettingsService(false).normalizeSettings(context.Background(), 1, tc.settings)
			if got := formErrorCode(err); got != tc.code {
				t.Fatalf("code = %q (err %v), want %q", got, err, tc.code)
			}
		})
	}
}

func TestNormalizeSettingsAllowsHTTPRedirectOnlyInDevelopment(t *testing.T) {
	settings := domain.FormSettings{RedirectURL: "http://localhost:3000/thanks"}
	if _, err := newSettingsService(true).normalizeSettings(context.Background(), 1, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := newSettingsService(false).normalizeSettings(context.Background(), 1, settings); err == nil {
		t.Fatal("http redirect must be rejected outside development")
	}
}

func TestPublicSettingsNeverExposeRecipientsOrInternalIDs(t *testing.T) {
	s := newSettingsService(false)
	public := s.publicSettings(domain.FormSettings{
		LogoFileID: testImageID, NotifyOnSubmit: []uint{3, 4}, MaxSubmissions: new(int),
	})
	if public.LogoURL != "https://api.example.test/api/v1/images/"+testImageID || public.CoverURL != "" {
		t.Fatalf("public settings = %+v", public)
	}
}

func TestFormClosed(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	if formClosed(domain.FormSettings{}, now) || formClosed(domain.FormSettings{ClosesAt: &future}, now) {
		t.Fatal("form must be open")
	}
	if !formClosed(domain.FormSettings{ClosesAt: &past}, now) || !formClosed(domain.FormSettings{ClosesAt: &now}, now) {
		t.Fatal("form must be closed")
	}
}

type notifierFake struct {
	notices []FormSubmissionNotice
}

func (n *notifierFake) NotifyFormSubmission(notice FormSubmissionNotice) {
	n.notices = append(n.notices, notice)
}

func TestNotifySubmissionSkipsEmptyRecipientList(t *testing.T) {
	notifier := &notifierFake{}
	s := newSettingsService(false).WithSubmissionNotifier(notifier)
	form := &domain.Form{WorkspaceID: 7}
	form.ID = 9
	s.notifySubmission(form, "Name", domain.FormSettings{}, 1)
	if len(notifier.notices) != 0 {
		t.Fatal("no recipients, no notice")
	}
	s.notifySubmission(form, "Name", domain.FormSettings{NotifyOnSubmit: []uint{3}}, 5)
	if len(notifier.notices) != 1 || notifier.notices[0].SubmissionID != 5 || notifier.notices[0].WorkspaceID != 7 ||
		notifier.notices[0].FormID != 9 || notifier.notices[0].Recipients[0] != 3 {
		t.Fatalf("notices = %+v", notifier.notices)
	}
}
