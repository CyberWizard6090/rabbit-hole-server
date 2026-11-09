package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

const (
	maxSubmitButtonRunes  = 80
	maxSuccessMessageRune = 2000
	maxRedirectURLLength  = 2048
	maxNotifyRecipients   = 50
	maxFormSubmissionsCap = 1_000_000
	imagePathSegment      = "/images/"
)

var accentColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type FormSubmissionNotifier interface {
	NotifyFormSubmission(ctx context.Context, notice FormSubmissionNotice) error
}

type PublicFormSettings struct {
	SubmitButtonText string     `json:"submit_button_text"`
	SuccessMessage   string     `json:"success_message"`
	RedirectURL      string     `json:"redirect_url"`
	AccentColor      string     `json:"accent_color"`
	LogoURL          string     `json:"logo_url"`
	CoverURL         string     `json:"cover_url"`
	ClosesAt         *time.Time `json:"closes_at"`
}

func (s *formService) WithSettingsPolicy(allowInsecureRedirect bool, publicBaseURL string) *formService {
	s.allowInsecureRedirect = allowInsecureRedirect
	s.imageBaseURL = strings.TrimRight(publicBaseURL, "/")
	return s
}

func (s *formService) WithSubmissionNotifier(notifier FormSubmissionNotifier) *formService {
	s.notifier = notifier
	return s
}

func settingsError(code, message, field, reason string, params map[string]any) error {
	return domain.NewFormFieldError(code, message, "settings."+field, reason, params)
}

func (s *formService) imageURL(publicID string) string {
	if publicID == "" {
		return ""
	}
	return s.imageBaseURL + "/api/v1" + imagePathSegment + publicID
}

func (s *formService) decorateForm(form *domain.Form) {
	form.Settings.LogoURL = s.imageURL(form.Settings.LogoFileID)
	form.Settings.CoverURL = s.imageURL(form.Settings.CoverFileID)
	if form.Settings.NotifyOnSubmit == nil {
		form.Settings.NotifyOnSubmit = []uint{}
	}
}

func (s *formService) publicSettings(settings domain.FormSettings) PublicFormSettings {
	return PublicFormSettings{
		SubmitButtonText: settings.SubmitButtonText, SuccessMessage: settings.SuccessMessage,
		RedirectURL: settings.RedirectURL, AccentColor: settings.AccentColor,
		LogoURL: s.imageURL(settings.LogoFileID), CoverURL: s.imageURL(settings.CoverFileID),
		ClosesAt: settings.ClosesAt,
	}
}

func (s *formService) normalizeSettings(
	ctx context.Context,
	workspaceID uint,
	in domain.FormSettings,
) (domain.FormSettings, error) {
	out := domain.FormSettings{NotifyOnSubmit: []uint{}}
	out.SubmitButtonText = strings.TrimSpace(in.SubmitButtonText)
	if utf8.RuneCountInString(out.SubmitButtonText) > maxSubmitButtonRunes {
		return out, settingsError("FORM_SUBMIT_BUTTON_TEXT_INVALID", "submit_button_text must be at most 80 characters",
			"submit_button_text", "TOO_LONG", map[string]any{"max": maxSubmitButtonRunes})
	}
	out.SuccessMessage = strings.TrimSpace(in.SuccessMessage)
	if utf8.RuneCountInString(out.SuccessMessage) > maxSuccessMessageRune {
		return out, settingsError("FORM_SUCCESS_MESSAGE_INVALID", "success_message must be at most 2000 characters",
			"success_message", "TOO_LONG", map[string]any{"max": maxSuccessMessageRune})
	}
	redirect, err := s.normalizeRedirectURL(in.RedirectURL)
	if err != nil {
		return out, err
	}
	out.RedirectURL = redirect
	if color := strings.TrimSpace(in.AccentColor); color != "" {
		if !accentColorPattern.MatchString(color) {
			return out, settingsError("FORM_ACCENT_COLOR_INVALID", "accent_color must look like #RRGGBB",
				"accent_color", "INVALID_COLOR", nil)
		}
		out.AccentColor = strings.ToLower(color)
	}
	if out.LogoFileID, err = s.normalizeImage(ctx, workspaceID, in.LogoFileID, "FORM_LOGO_INVALID", "logo_file_id"); err != nil {
		return out, err
	}
	if out.CoverFileID, err = s.normalizeImage(ctx, workspaceID, in.CoverFileID, "FORM_COVER_INVALID", "cover_file_id"); err != nil {
		return out, err
	}
	if out.NotifyOnSubmit, err = s.normalizeRecipients(ctx, workspaceID, in.NotifyOnSubmit); err != nil {
		return out, err
	}
	if in.ClosesAt != nil {
		closesAt := in.ClosesAt.UTC().Truncate(time.Second)
		out.ClosesAt = &closesAt
	}
	if in.MaxSubmissions != nil {
		if *in.MaxSubmissions < 1 || *in.MaxSubmissions > maxFormSubmissionsCap {
			return out, settingsError("FORM_MAX_SUBMISSIONS_INVALID", "max_submissions must be between 1 and 1000000",
				"max_submissions", "OUT_OF_RANGE", map[string]any{"min": 1, "max": maxFormSubmissionsCap})
		}
		limit := *in.MaxSubmissions
		out.MaxSubmissions = &limit
	}
	return out, nil
}

func (s *formService) normalizeRedirectURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	invalid := func(message, reason string, params map[string]any) error {
		return settingsError("FORM_REDIRECT_URL_INVALID", message, "redirect_url", reason, params)
	}
	if len(raw) > maxRedirectURLLength {
		return "", invalid("redirect_url must be a valid URL of at most 2048 characters", "TOO_LONG",
			map[string]any{"max": maxRedirectURLLength})
	}
	if strings.ContainsAny(raw, " \t\r\n\\") {
		return "", invalid("redirect_url must be a valid URL of at most 2048 characters", "INVALID_FORMAT",
			map[string]any{"format": "url"})
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", invalid("redirect_url must be an absolute https URL", "INVALID_FORMAT", map[string]any{"format": "url"})
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !s.allowInsecureRedirect {
			return "", invalid("redirect_url must use https", "INVALID_SCHEME", map[string]any{"allowed": []string{"https"}})
		}
	default:
		return "", invalid("redirect_url must use https", "INVALID_SCHEME", map[string]any{"allowed": []string{"https"}})
	}
	if parsed.User != nil {
		return "", invalid("redirect_url must not contain credentials", "NOT_ALLOWED", nil)
	}
	return parsed.String(), nil
}

func (s *formService) normalizeImage(
	ctx context.Context,
	workspaceID uint,
	raw, code, field string,
) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	publicID := raw
	if strings.Contains(raw, imagePathSegment) {
		publicID = raw[strings.LastIndex(raw, imagePathSegment)+len(imagePathSegment):]
	}
	publicID = strings.ToLower(publicID)
	if !publicIDPattern.MatchString(publicID) {
		return "", settingsError(code, field+" must be the id of an image uploaded to this workspace", field, "INVALID_REFERENCE", nil)
	}
	exists, err := s.repo.WorkspaceImageExists(ctx, workspaceID, publicID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", settingsError(code, field+" must be the id of an image uploaded to this workspace", field, "NOT_FOUND", nil)
	}
	return publicID, nil
}

func (s *formService) normalizeRecipients(ctx context.Context, workspaceID uint, ids []uint) ([]uint, error) {
	invalid := func(message, field, reason string, params map[string]any) error {
		return settingsError("FORM_NOTIFY_RECIPIENT_INVALID", message, field, reason, params)
	}
	const field = "notify_on_submit"
	if len(ids) > maxNotifyRecipients {
		return nil, invalid("notify_on_submit accepts at most 50 users", field, "TOO_MANY", map[string]any{"max": maxNotifyRecipients})
	}
	unique := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for i, id := range ids {
		if id == 0 {
			return nil, invalid("notify_on_submit must contain user ids", fmt.Sprintf("%s[%d]", field, i), "INVALID_VALUE", nil)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	members, err := s.repo.WorkspaceMemberIDs(ctx, workspaceID, unique)
	if err != nil {
		return nil, err
	}
	if len(members) != len(unique) {
		isMember := make(map[uint]struct{}, len(members))
		for _, id := range members {
			isMember[id] = struct{}{}
		}
		for i, id := range ids {
			if _, ok := isMember[id]; !ok {
				return nil, invalid("notify_on_submit may only list members of this workspace",
					fmt.Sprintf("%s[%d]", field, i), "NOT_A_MEMBER", map[string]any{"user_id": id})
			}
		}
	}
	return unique, nil
}

func formClosed(settings domain.FormSettings, now time.Time) bool {
	return settings.ClosesAt != nil && !now.Before(*settings.ClosesAt)
}

func (s *formService) notifySubmission(
	ctx context.Context, form *domain.Form, name string, settings domain.FormSettings, submissionID uint,
) error {
	if s.notifier == nil || len(settings.NotifyOnSubmit) == 0 {
		return nil
	}
	return s.notifier.NotifyFormSubmission(ctx, FormSubmissionNotice{
		WorkspaceID: form.WorkspaceID, FormID: form.ID, FormName: name,
		SubmissionID: submissionID, Recipients: settings.NotifyOnSubmit,
	})
}
