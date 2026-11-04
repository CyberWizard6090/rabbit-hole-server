package service

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
	"time"
)

type localeKey struct{}

const (
	localeEnglish = "en"
	localeRussian = "ru"
)

func WithLocale(ctx context.Context, acceptLanguage string) context.Context {
	return context.WithValue(ctx, localeKey{}, localeFromHeader(acceptLanguage))
}

func localeFrom(ctx context.Context) string {
	if value, ok := ctx.Value(localeKey{}).(string); ok {
		return value
	}
	return localeEnglish
}

func localeFromHeader(header string) string {
	first := strings.ToLower(strings.TrimSpace(strings.SplitN(strings.SplitN(header, ",", 2)[0], ";", 2)[0]))
	if first == localeRussian || strings.HasPrefix(first, localeRussian+"-") {
		return localeRussian
	}
	return localeEnglish
}

type mailTemplate struct {
	subject string
	text    *texttemplate.Template
	html    *htmltemplate.Template
}

type mailView struct {
	Link    string
	TTL     string
	Address string
}

func newMailTemplate(subject, text, html string) mailTemplate {
	return mailTemplate{
		subject: subject,
		text:    texttemplate.Must(texttemplate.New("text").Parse(text)),
		html:    htmltemplate.Must(htmltemplate.New("html").Parse(html)),
	}
}

var mailTemplates = map[string]map[string]mailTemplate{
	localeEnglish: {
		TokenKindVerify: newMailTemplate(
			"Confirm your email address",
			"Hello,\n\nPlease confirm your email address to finish setting up your Rabbit Hole account:\n\n{{.Link}}\n\nThe link works for {{.TTL}}. If you did not create this account, just ignore this email.\n",
			`<p>Hello,</p><p>Please confirm your email address to finish setting up your Rabbit Hole account:</p><p><a href="{{.Link}}">Confirm email</a></p><p>The link works for {{.TTL}}. If you did not create this account, just ignore this email.</p>`,
		),
		TokenKindReset: newMailTemplate(
			"Reset your password",
			"Hello,\n\nWe received a request to reset the password of your Rabbit Hole account:\n\n{{.Link}}\n\nThe link works for {{.TTL}} and can be used once. If you did not ask for it, ignore this email: your password stays the same.\n",
			`<p>Hello,</p><p>We received a request to reset the password of your Rabbit Hole account:</p><p><a href="{{.Link}}">Reset password</a></p><p>The link works for {{.TTL}} and can be used once. If you did not ask for it, ignore this email: your password stays the same.</p>`,
		),
		TokenKindChangeEmail: newMailTemplate(
			"Confirm your new email address",
			"Hello,\n\nSomeone asked to use this address as the login email of a Rabbit Hole account. Confirm the change:\n\n{{.Link}}\n\nThe link works for {{.TTL}} and can be used once. If you did not ask for it, ignore this email: nothing will change.\n",
			`<p>Hello,</p><p>Someone asked to use this address as the login email of a Rabbit Hole account. Confirm the change:</p><p><a href="{{.Link}}">Confirm new email</a></p><p>The link works for {{.TTL}} and can be used once. If you did not ask for it, ignore this email: nothing will change.</p>`,
		),
		noticeKindEmailChanged: newMailTemplate(
			"Your Rabbit Hole email address was changed",
			"Hello,\n\nThe login email of your Rabbit Hole account was changed to {{.Address}}. You were signed out everywhere.\n\nIf this was not you, reset your password right away and contact support: the new address now controls the account.\n",
			`<p>Hello,</p><p>The login email of your Rabbit Hole account was changed to {{.Address}}. You were signed out everywhere.</p><p>If this was not you, reset your password right away and contact support: the new address now controls the account.</p>`,
		),
	},
	localeRussian: {
		TokenKindVerify: newMailTemplate(
			"Подтвердите адрес электронной почты",
			"Здравствуйте!\n\nПодтвердите адрес электронной почты, чтобы завершить регистрацию в Rabbit Hole:\n\n{{.Link}}\n\nСсылка действует {{.TTL}}. Если вы не регистрировались, просто проигнорируйте это письмо.\n",
			`<p>Здравствуйте!</p><p>Подтвердите адрес электронной почты, чтобы завершить регистрацию в Rabbit Hole:</p><p><a href="{{.Link}}">Подтвердить почту</a></p><p>Ссылка действует {{.TTL}}. Если вы не регистрировались, просто проигнорируйте это письмо.</p>`,
		),
		TokenKindReset: newMailTemplate(
			"Сброс пароля",
			"Здравствуйте!\n\nМы получили запрос на сброс пароля вашего аккаунта в Rabbit Hole:\n\n{{.Link}}\n\nСсылка действует {{.TTL}} и срабатывает один раз. Если вы не запрашивали сброс, проигнорируйте это письмо: пароль останется прежним.\n",
			`<p>Здравствуйте!</p><p>Мы получили запрос на сброс пароля вашего аккаунта в Rabbit Hole:</p><p><a href="{{.Link}}">Сбросить пароль</a></p><p>Ссылка действует {{.TTL}} и срабатывает один раз. Если вы не запрашивали сброс, проигнорируйте это письмо: пароль останется прежним.</p>`,
		),
		TokenKindChangeEmail: newMailTemplate(
			"Подтвердите новый адрес электронной почты",
			"Здравствуйте!\n\nКто-то хочет использовать этот адрес как почту для входа в аккаунт Rabbit Hole. Подтвердите смену:\n\n{{.Link}}\n\nСсылка действует {{.TTL}} и срабатывает один раз. Если вы этого не запрашивали, проигнорируйте письмо: ничего не изменится.\n",
			`<p>Здравствуйте!</p><p>Кто-то хочет использовать этот адрес как почту для входа в аккаунт Rabbit Hole. Подтвердите смену:</p><p><a href="{{.Link}}">Подтвердить новую почту</a></p><p>Ссылка действует {{.TTL}} и срабатывает один раз. Если вы этого не запрашивали, проигнорируйте письмо: ничего не изменится.</p>`,
		),
		noticeKindEmailChanged: newMailTemplate(
			"Адрес электронной почты в Rabbit Hole изменён",
			"Здравствуйте!\n\nПочта для входа в ваш аккаунт Rabbit Hole изменена на {{.Address}}. Вы вышли из аккаунта на всех устройствах.\n\nЕсли это были не вы, немедленно сбросьте пароль и свяжитесь с поддержкой: новый адрес теперь управляет аккаунтом.\n",
			`<p>Здравствуйте!</p><p>Почта для входа в ваш аккаунт Rabbit Hole изменена на {{.Address}}. Вы вышли из аккаунта на всех устройствах.</p><p>Если это были не вы, немедленно сбросьте пароль и свяжитесь с поддержкой: новый адрес теперь управляет аккаунтом.</p>`,
		),
	},
}

func ttlText(locale string, d time.Duration) string {
	hours := int(d.Round(time.Hour) / time.Hour)
	if d >= time.Hour && hours >= 1 {
		if locale == localeRussian {
			return fmt.Sprintf("%d ч", hours)
		}
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	if locale == localeRussian {
		return fmt.Sprintf("%d мин", minutes)
	}
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

func renderMail(locale, kind string, view mailView) (subject, text, html string, err error) {
	tpl, ok := mailTemplates[locale][kind]
	if !ok {
		tpl = mailTemplates[localeEnglish][kind]
	}
	var textBuf, htmlBuf bytes.Buffer
	if err := tpl.text.Execute(&textBuf, view); err != nil {
		return "", "", "", err
	}
	if err := tpl.html.Execute(&htmlBuf, view); err != nil {
		return "", "", "", err
	}
	return tpl.subject, textBuf.String(), htmlBuf.String(), nil
}
