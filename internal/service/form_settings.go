package service

import (
	"context"
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
	NotifyFormSubmission(notice FormSubmissionNotice)
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

func settingsError(code, message string) error {
	return domain.NewFormError(code, message)
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
		return out, settingsError("FORM_SUBMIT_BUTTON_TEXT_INVALID", "submit_button_text must be at most 80 characters")
	}
	out.SuccessMessage = strings.TrimSpace(in.SuccessMessage)
	if utf8.RuneCountInString(out.SuccessMessage) > maxSuccessMessageRune {
		return out, settingsError("FORM_SUCCESS_MESSAGE_INVALID", "success_message must be at most 2000 characters")
	}
	redirect, err := s.normalizeRedirectURL(in.RedirectURL)
	if err != nil {
		return out, err
	}
	out.RedirectURL = redirect
	if color := strings.TrimSpace(in.AccentColor); color != "" {
		if !accentColorPattern.MatchString(color) {
			return out, settingsError("FORM_ACCENT_COLOR_INVALID", "accent_color must look like #RRGGBB")
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
			return out, settingsError("FORM_MAX_SUBMISSIONS_INVALID", "max_submissions must be between 1 and 1000000")
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
	invalid := func(message string) error { return settingsError("FORM_REDIRECT_URL_INVALID", message) }
	if len(raw) > maxRedirectURLLength || strings.ContainsAny(raw, " \t\r\n\\") {
		return "", invalid("redirect_url must be a valid URL of at most 2048 characters")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", invalid("redirect_url must be an absolute https URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !s.allowInsecureRedirect {
			return "", invalid("redirect_url must use https")
		}
	default:
		return "", invalid("redirect_url must use https")
	}
	if parsed.User != nil {
		return "", invalid("redirect_url must not contain credentials")
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
		return "", settingsError(code, field+" must be the id of an image uploaded to this workspace")
	}
	exists, err := s.repo.WorkspaceImageExists(ctx, workspaceID, publicID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", settingsError(code, field+" must be the id of an image uploaded to this workspace")
	}
	return publicID, nil
}

func (s *formService) normalizeRecipients(ctx context.Context, workspaceID uint, ids []uint) ([]uint, error) {
	invalid := func(message string) error { return settingsError("FORM_NOTIFY_RECIPIENT_INVALID", message) }
	if len(ids) > maxNotifyRecipients {
		return nil, invalid("notify_on_submit accepts at most 50 users")
	}
	unique := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, invalid("notify_on_submit must contain user ids")
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
		return nil, invalid("notify_on_submit may only list members of this workspace")
	}
	return unique, nil
}

func formClosed(settings domain.FormSettings, now time.Time) bool {
	return settings.ClosesAt != nil && !now.Before(*settings.ClosesAt)
}

func (s *formService) notifySubmission(
	form *domain.Form, name string, settings domain.FormSettings, submissionID uint,
) {
	if s.notifier == nil || len(settings.NotifyOnSubmit) == 0 {
		return
	}
	s.notifier.NotifyFormSubmission(FormSubmissionNotice{
		WorkspaceID: form.WorkspaceID, FormID: form.ID, FormName: name,
		SubmissionID: submissionID, Recipients: settings.NotifyOnSubmit,
	})
}
