package service

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
)

const (
	confirmChannelPath    = "/confirm-channel"
	taskPathPrefix        = "/tasks/"
	notificationPrefsPath = "/settings/notifications"
	maxDetailRunes        = 300
)

type channelMailView struct {
	Link string
	TTL  string
}

var channelConfirmTemplates = map[string]mailTemplate{
	localeEnglish: newMailTemplate(
		"Confirm this address for Rabbit Hole notifications",
		"Hello,\n\nSomeone asked to receive Rabbit Hole notifications at this address. If it was you, confirm it:\n\n{{.Link}}\n\nThe link works for {{.TTL}} and can be used once. If you did not ask for it, ignore this email: nothing will be sent to this address.\n",
		`<p>Hello,</p><p>Someone asked to receive Rabbit Hole notifications at this address. If it was you, confirm it:</p><p><a href="{{.Link}}">Confirm address</a></p><p>The link works for {{.TTL}} and can be used once. If you did not ask for it, ignore this email: nothing will be sent to this address.</p>`,
	),
	localeRussian: newMailTemplate(
		"Подтвердите адрес для уведомлений Rabbit Hole",
		"Здравствуйте!\n\nКто-то попросил присылать уведомления Rabbit Hole на этот адрес. Если это были вы, подтвердите его:\n\n{{.Link}}\n\nСсылка действует {{.TTL}} и срабатывает один раз. Если вы этого не делали, проигнорируйте письмо: на этот адрес ничего не будет отправляться.\n",
		`<p>Здравствуйте!</p><p>Кто-то попросил присылать уведомления Rabbit Hole на этот адрес. Если это были вы, подтвердите его:</p><p><a href="{{.Link}}">Подтвердить адрес</a></p><p>Ссылка действует {{.TTL}} и срабатывает один раз. Если вы этого не делали, проигнорируйте письмо: на этот адрес ничего не будет отправляться.</p>`,
	),
}

func renderChannelConfirmation(locale string, view channelMailView) (subject, text, html string, err error) {
	tpl, ok := channelConfirmTemplates[locale]
	if !ok {
		tpl = channelConfirmTemplates[localeEnglish]
	}
	return renderTemplate(tpl, view)
}

func renderTemplate(tpl mailTemplate, view any) (subject, text, html string, err error) {
	var textBuf, htmlBuf bytes.Buffer
	if err := tpl.text.Execute(&textBuf, view); err != nil {
		return "", "", "", err
	}
	if err := tpl.html.Execute(&htmlBuf, view); err != nil {
		return "", "", "", err
	}
	return tpl.subject, textBuf.String(), htmlBuf.String(), nil
}

type notificationMailStrings struct {
	Open        string
	OpenForm    string
	Submission  string
	Footer      string
	PrefsLabel  string
	NoDueDate   string
	SomeoneName string
}

var notificationMailLocales = map[string]notificationMailStrings{
	localeEnglish: {
		Open: "Open the task", OpenForm: "Open the submission", Submission: "Submission #%d",
		Footer:     "You get this e-mail because of your notification settings.",
		PrefsLabel: "Change them", NoDueDate: "no due date", SomeoneName: "Someone",
	},
	localeRussian: {
		Open: "Открыть задачу", OpenForm: "Открыть ответ", Submission: "Ответ №%d",
		Footer:     "Вы получили это письмо в соответствии с настройками уведомлений.",
		PrefsLabel: "Изменить настройки", NoDueDate: "срок снят", SomeoneName: "Кто-то",
	},
}

type notificationMailView struct {
	Summary         string
	Detail          string
	Link            string
	OpenLabel       string
	Footer          string
	PreferencesURL  string
	PreferencesText string
}

var notificationMailTemplate = newMailTemplate(
	"",
	"{{.Summary}}\n{{if .Detail}}\n{{.Detail}}\n{{end}}\n{{.OpenLabel}}: {{.Link}}\n\n--\n{{.Footer}} {{.PreferencesText}}: {{.PreferencesURL}}\n",
	`<p>{{.Summary}}</p>{{if .Detail}}<blockquote>{{.Detail}}</blockquote>{{end}}<p><a href="{{.Link}}">{{.OpenLabel}}</a></p><hr><p>{{.Footer}} <a href="{{.PreferencesURL}}">{{.PreferencesText}}</a></p>`,
)

func singleLine(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "\x00", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func actorDisplayName(u domain.CommentUser, fallback string) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	name = singleLine(name)
	if name == "" {
		return fallback
	}
	return name
}

func payloadString(payload map[string]any, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func notificationDetail(locale, typeKey string, payload map[string]any) string {
	strs := notificationMailLocales[locale]
	switch typeKey {
	case domain.NotificationMention, domain.NotificationComment:
		return truncateRunes(payloadString(payload, "comment_excerpt"), maxDetailRunes)
	case domain.NotificationStatusChanged:
		return payloadString(payload, "status_name")
	case domain.NotificationDueDateChange:
		due := payloadString(payload, "due_date")
		if due == "" {
			return strs.NoDueDate
		}
		if parsed, err := time.Parse(time.RFC3339, due); err == nil {
			return parsed.UTC().Format("2006-01-02 15:04 UTC")
		}
		return due
	case domain.NotificationAssigned:
		return truncateRunes(payloadString(payload, "item_text"), maxDetailRunes)
	case domain.NotificationFormSubmitted:
		return fmt.Sprintf(strs.Submission, payloadID(payload, "submission_id"))
	}
	return ""
}

func renderNotificationMail(locale, frontendURL string, c NotificationContent) (subject, text, html string, err error) {
	strs, ok := notificationMailLocales[locale]
	if !ok {
		locale = localeEnglish
		strs = notificationMailLocales[localeEnglish]
	}
	wording := c.Type.WordingFor(locale)
	actor := actorDisplayName(c.Actor, strs.SomeoneName)
	summary := singleLine(fmt.Sprintf(wording.Subject, actor, singleLine(c.Title())))
	openLabel := strs.Open
	if c.Type.Key == domain.NotificationFormSubmitted {
		openLabel = strs.OpenForm
	}
	view := notificationMailView{
		Summary:         summary,
		Detail:          notificationDetail(locale, c.Type.Key, c.Payload),
		Link:            notificationLink(frontendURL, c),
		OpenLabel:       openLabel,
		Footer:          strs.Footer,
		PreferencesURL:  frontendURL + notificationPrefsPath,
		PreferencesText: strs.PrefsLabel,
	}
	_, text, html, err = renderTemplate(notificationMailTemplate, view)
	return summary, text, html, err
}

func LocaleFromAcceptLanguage(header string) string { return localeFromHeader(header) }
