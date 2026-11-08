package logging

import (
	"log/slog"
	"regexp"
	"strings"
)

const redacted = "[redacted]"

var (
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	secretPattern = regexp.MustCompile(`(?i)\b(token|ticket|secret|password|passwd|api[_-]?key|authorization|signature|hash)(["']?\s*[=:]\s*["']?)([^\s&"',;)]+)`)
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]+`)
)

func MaskEmail(address string) string {
	at := strings.LastIndex(address, "@")
	if at <= 0 {
		return "***"
	}
	return address[:1] + "***" + address[at:]
}

func Scrub(value string) string {
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	if !strings.ContainsAny(value, "@=:") && !strings.Contains(lower, "bearer") && !strings.Contains(lower, "basic ") {
		return value
	}
	value = emailPattern.ReplaceAllStringFunc(value, MaskEmail)
	value = bearerPattern.ReplaceAllString(value, "${1} "+redacted)
	return secretPattern.ReplaceAllString(value, "${1}${2}"+redacted)
}

func scrubAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(Scrub(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok && err != nil {
			a.Value = slog.StringValue(Scrub(err.Error()))
		}
	}
	return a
}
