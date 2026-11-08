package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

type Problem struct {
	Var string
	Msg string
}

type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	noun := "problems"
	if len(e.Problems) == 1 {
		noun = "problem"
	}
	fmt.Fprintf(&b, "invalid configuration, %d %s found:", len(e.Problems), noun)
	for _, p := range e.Problems {
		fmt.Fprintf(&b, "\n  - %s: %s", p.Var, p.Msg)
	}
	return b.String()
}

type problemSet struct {
	items  []Problem
	failed map[string]bool
}

func newProblemSet() *problemSet {
	return &problemSet{failed: map[string]bool{}}
}

func (s *problemSet) add(name, format string, args ...any) {
	if s.failed[name] {
		return
	}
	s.failed[name] = true
	s.items = append(s.items, Problem{Var: name, Msg: fmt.Sprintf(format, args...)})
}

func (s *problemSet) err() error {
	if len(s.items) == 0 {
		return nil
	}
	items := append([]Problem(nil), s.items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Var < items[j].Var })
	return &ValidationError{Problems: items}
}

func envName(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return name
}

func checkEnvTypes(target any, prefix string, set *problemSet) {
	walkEnvFields(reflect.ValueOf(target).Elem(), prefix, func(name string, field reflect.StructField, _ reflect.Value) {
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" {
			return
		}
		switch {
		case field.Type == reflect.TypeOf(time.Duration(0)):
			if _, err := time.ParseDuration(raw); err != nil {
				set.add(name, "%q is not a valid duration; use a value such as 30s, 15m or 168h", raw)
			}
		case field.Type.Kind() == reflect.Int, field.Type.Kind() == reflect.Int64:
			if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
				set.add(name, "%q is not a whole number; use digits only, for example 25", raw)
			}
		case field.Type.Kind() == reflect.Bool:
			if _, err := strconv.ParseBool(raw); err != nil {
				set.add(name, "%q is not a boolean; use true or false", raw)
			}
		}
	})
}

func walkEnvFields(v reflect.Value, prefix string, visit func(name string, field reflect.StructField, value reflect.Value)) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)
		if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
			walkEnvFields(value, prefix+field.Tag.Get("envPrefix"), visit)
			continue
		}
		tag := field.Tag.Get("env")
		if tag == "" {
			continue
		}
		visit(prefix+envName(tag), field, value)
	}
}

func envNames(target any, prefix string) map[string]string {
	names := map[string]string{}
	var walk func(v reflect.Value, prefix, path string)
	walk = func(v reflect.Value, prefix, path string) {
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			p := path + "." + field.Name
			if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
				walk(v.Field(i), prefix+field.Tag.Get("envPrefix"), p)
				continue
			}
			if tag := field.Tag.Get("env"); tag != "" {
				names[p] = prefix + envName(tag)
			}
		}
	}
	root := reflect.ValueOf(target).Elem()
	walk(root, prefix, root.Type().Name())
	return names
}

func checkStruct(target any, set *problemSet) {
	names := envNames(target, "")
	err := validator.New().Struct(target)
	if err == nil {
		return
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		set.add("configuration", "%v", err)
		return
	}
	for _, fe := range verrs {
		name := names[fe.Namespace()]
		if name == "" {
			name = strings.TrimPrefix(fe.Namespace(), reflect.TypeOf(target).Elem().Name()+".")
		}
		set.add(name, "%s", describeTag(fe))
	}
}

func describeTag(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required but empty; set it in the environment or in .env"
	case "min", "max", "gt", "gte":
		return describeBound(fe)
	case "oneof":
		return fmt.Sprintf("has the invalid value %q; allowed values: %s", fmt.Sprint(fe.Value()), strings.ReplaceAll(fe.Param(), " ", ", "))
	case "url":
		return fmt.Sprintf("%q is not a valid URL; use a full URL such as https://app.example.com", fmt.Sprint(fe.Value()))
	case "hostname|ip":
		return fmt.Sprintf("%q is not a valid host name or IP address", fmt.Sprint(fe.Value()))
	}
	return fmt.Sprintf("failed the %q rule", fe.Tag())
}

func describeBound(fe validator.FieldError) string {
	kind := fe.Kind()
	value := fmt.Sprint(fe.Value())
	if kind == reflect.String {
		return fmt.Sprintf("must have at least %s characters (it has %d)", fe.Param(), len(value))
	}
	if kind == reflect.Slice {
		return fmt.Sprintf("needs at least %s entries", fe.Param())
	}
	bound := map[string]string{"min": "at least", "gte": "at least", "max": "at most", "gt": "greater than"}[fe.Tag()]
	param := fe.Param()
	if fe.Type() == reflect.TypeOf(time.Duration(0)) && param == "0" {
		param = "0s"
	}
	return fmt.Sprintf("is %s but must be %s %s", value, bound, param)
}

func validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func validHTTPURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	return u, true
}

func checkProxies(cfg *Config, set *problemSet) {
	for _, entry := range cfg.Server.Proxies() {
		if ip := net.ParseIP(entry); ip != nil {
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			set.add("TRUSTED_PROXIES", "entry %q is neither an IP address nor a CIDR range; use a comma separated list such as 127.0.0.1,10.0.0.0/8, or leave it empty to trust no proxy", entry)
			return
		}
		if ones, _ := network.Mask.Size(); ones == 0 {
			set.add("TRUSTED_PROXIES", "entry %q would trust every client, which lets anyone forge X-Forwarded-For and dodge the rate limits; list only the addresses of your reverse proxies", entry)
			return
		}
	}
}

func checkDatabase(cfg *Config, set *problemSet) {
	db := cfg.DB
	if db.MaxIdleConnections > db.MaxOpenConnections {
		set.add("DB_MAX_IDLE_CONNS", "is %d but must not exceed DB_MAX_OPEN_CONNS (%d)", db.MaxIdleConnections, db.MaxOpenConnections)
	}
	if db.StatementTimeout > 0 && db.LockTimeout > db.StatementTimeout {
		set.add("DB_LOCK_TIMEOUT", "is %s but must not exceed DB_STATEMENT_TIMEOUT (%s); use 0 to disable a timeout", db.LockTimeout, db.StatementTimeout)
	}
}

func checkAuth(cfg *Config, set *problemSet) {
	a := cfg.Auth
	if a.LoginFailureLimit == 0 {
		return
	}
	if a.LoginLockBase == 0 {
		set.add("AUTH_LOGIN_LOCK_BASE", "must be greater than 0s while AUTH_LOGIN_FAILURE_LIMIT is set; set AUTH_LOGIN_FAILURE_LIMIT=0 to disable the lock")
		return
	}
	if a.LoginLockMax < a.LoginLockBase {
		set.add("AUTH_LOGIN_LOCK_MAX", "is %s but must be at least AUTH_LOGIN_LOCK_BASE (%s)", a.LoginLockMax, a.LoginLockBase)
	}
}

func checkOrigins(cfg *Config, set *problemSet) {
	for _, origin := range cfg.AllowedOrigins {
		if !validOrigin(origin) {
			set.add("ALLOWED_ORIGINS", "entry %q is not an origin; use scheme and host only without a path or trailing slash, for example https://app.example.com", origin)
			return
		}
		if cfg.IsDeployed() && !strings.HasPrefix(origin, "https://") {
			set.add("ALLOWED_ORIGINS", "entry %q must use https:// in %s", origin, cfg.Environment)
			return
		}
	}
}

func checkSecurity(cfg *Config, set *problemSet) {
	secret := cfg.JWT.Secret
	if len(secret) < 32 {
		return
	}
	if !cfg.IsDeployed() {
		return
	}
	lower := strings.ToLower(secret)
	for _, marker := range knownWeakSecretMarkers {
		if strings.Contains(lower, marker) {
			set.add("JWT_SECRET", "contains the placeholder fragment %q; generate a random secret, for example `openssl rand -base64 48`", marker)
			return
		}
	}
	distinct := map[rune]struct{}{}
	for _, r := range secret {
		distinct[r] = struct{}{}
	}
	if len(distinct) < minDistinctSecretChars {
		set.add("JWT_SECRET", "uses only %d distinct characters (need at least %d), so it does not look random; generate one with `openssl rand -base64 48`", len(distinct), minDistinctSecretChars)
	}
}

func checkMail(cfg *Config, set *problemSet) {
	m := cfg.Mail
	if parsed, err := mail.ParseAddress(m.From); err != nil || parsed.Address == "" || strings.ContainsAny(m.From, "\r\n") {
		set.add("MAIL_FROM", "must be an address such as \"Rabbit Hole <no-reply@example.com>\"")
	}
	if m.Driver == "smtp" {
		if strings.TrimSpace(m.SMTPHost) == "" {
			set.add("SMTP_HOST", "is required when MAIL_DRIVER=smtp; set the mail server host name")
		}
		if (m.SMTPUsername == "") != (m.SMTPPassword == "") {
			set.add("SMTP_PASSWORD", "SMTP_USERNAME and SMTP_PASSWORD must be set together")
		}
	}
	if _, ok := validHTTPURL(m.FrontendURL); !ok {
		set.add("FRONTEND_URL", "%q is not a valid http(s) URL; mail links are built from it, for example https://app.example.com", m.FrontendURL)
	} else if cfg.IsDeployed() && !strings.HasPrefix(m.FrontendURL, "https://") {
		set.add("FRONTEND_URL", "%q must use https:// in %s because mail links point to it", m.FrontendURL, cfg.Environment)
	}
	if cfg.IsDeployed() {
		if m.Driver == "log" {
			set.add("MAIL_DRIVER", "MAIL_DRIVER=log only writes mail to the server log, so users would never get verification or reset links; set MAIL_DRIVER=smtp in %s", cfg.Environment)
		}
		if m.Driver == "smtp" && m.SMTPSecurity == "none" && m.SMTPHost != "" && m.SMTPHost != "localhost" && m.SMTPHost != "127.0.0.1" {
			set.add("SMTP_SECURITY", "SMTP_SECURITY=none sends mail and the SMTP password in clear text; use starttls or tls")
		}
	}
}

func checkStorage(cfg *Config, set *problemSet) {
	s := cfg.Storage
	if s.PublicBaseURL != "" {
		if _, ok := validHTTPURL(s.PublicBaseURL); !ok {
			set.add("PUBLIC_BASE_URL", "%q is not a valid http(s) URL; image links are built from it, for example https://api.example.com", s.PublicBaseURL)
		} else if cfg.IsDeployed() && !strings.HasPrefix(s.PublicBaseURL, "https://") {
			set.add("PUBLIC_BASE_URL", "%q must use https:// in %s", s.PublicBaseURL, cfg.Environment)
		}
	}
	if s.Driver != "s3" {
		return
	}
	for _, item := range []struct{ name, value string }{
		{"S3_ENDPOINT", s.S3.Endpoint}, {"S3_BUCKET", s.S3.Bucket},
		{"S3_ACCESS_KEY", s.S3.AccessKey}, {"S3_SECRET_KEY", s.S3.SecretKey},
	} {
		if strings.TrimSpace(item.value) == "" {
			set.add(item.name, "is required when STORAGE_DRIVER=s3")
		}
	}
	if strings.Contains(s.S3.Endpoint, "://") {
		set.add("S3_ENDPOINT", "must be host[:port] without a scheme; use S3_USE_SSL=true for https")
	}
}

func checkDeployment(cfg *Config, set *problemSet) {
	checkSecurity(cfg, set)
	checkMail(cfg, set)
	checkStorage(cfg, set)
	checkOrigins(cfg, set)
	checkProxies(cfg, set)
	checkDatabase(cfg, set)
	checkAuth(cfg, set)
}
