package safehttp

import (
	"net/netip"
	"net/url"
	"strings"
)

const MaxURLLength = 2048

var blockedHostSuffixes = []string{
	".localhost", ".local", ".localdomain", ".internal", ".intranet", ".lan", ".home.arpa",
}

func isNumericLabel(label string) bool {
	if label == "" {
		return false
	}
	if len(label) > 2 && (label[:2] == "0x" || label[:2] == "0X") {
		return true
	}
	for _, r := range label {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validHostChars(host string) bool {
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '_':
		default:
			return false
		}
	}
	return true
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimRight(host, "."))
}

func (c *Client) checkHost(host string) error {
	if host == "" {
		return ErrForbiddenURL
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return ErrForbiddenURL
		}
		return nil
	}
	if strings.ContainsAny(host, ":[]%") || !validHostChars(host) {
		return ErrForbiddenURL
	}
	name := normalizeHost(host)
	if name == "" || len(name) > 253 {
		return ErrForbiddenURL
	}
	labels := strings.Split(name, ".")
	if isNumericLabel(labels[len(labels)-1]) {
		return ErrForbiddenURL
	}
	if name == "localhost" {
		return ErrForbiddenURL
	}
	for _, suffix := range blockedHostSuffixes {
		if strings.HasSuffix(name, suffix) {
			return ErrForbiddenURL
		}
	}
	return nil
}

func (c *Client) portAllowed(port string) bool {
	if c.allowAnyPort {
		return true
	}
	for _, p := range c.ports {
		if p == port {
			return true
		}
	}
	return false
}

func (c *Client) ValidateURL(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > MaxURLLength || strings.ContainsAny(raw, " \t\r\n\\") {
		return nil, ErrForbiddenURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrForbiddenURL
	}
	return c.validateParsed(u)
}

func (c *Client) validateParsed(u *url.URL) (*url.URL, error) {
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, ErrForbiddenURL
	}
	if u.User != nil || u.Opaque != "" {
		return nil, ErrForbiddenURL
	}
	if err := c.checkHost(u.Hostname()); err != nil {
		return nil, err
	}
	if !c.portAllowed(effectivePort(u)) {
		return nil, ErrForbiddenURL
	}
	clean := *u
	clean.Fragment = ""
	clean.RawFragment = ""
	return &clean, nil
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
