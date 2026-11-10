package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/mail"
)

type emailTokenStoreFake struct {
	created      []createdToken
	last         *time.Time
	verifyHashes []string
	resetHashes  []string
	resetPwHash  string
	invalid      bool
	discarded    []string
}

type createdToken struct {
	userID    uint
	kind      string
	hash      string
	expiresAt time.Time
}

func (f *emailTokenStoreFake) Create(_ context.Context, userID uint, kind, hash string, expiresAt time.Time) error {
	f.created = append(f.created, createdToken{userID, kind, hash, expiresAt})
	return nil
}

func (f *emailTokenStoreFake) LastCreatedAt(context.Context, uint, string) (*time.Time, error) {
	return f.last, nil
}

func (f *emailTokenStoreFake) Discard(_ context.Context, hash string) error {
	f.discarded = append(f.discarded, hash)
	return nil
}

func (f *emailTokenStoreFake) VerifyEmail(_ context.Context, hash string) (uint, error) {
	f.verifyHashes = append(f.verifyHashes, hash)
	if f.invalid {
		return 0, domain.ErrInvalidToken
	}
	return 1, nil
}

func (f *emailTokenStoreFake) ResetPassword(_ context.Context, hash, passwordHash string) (uint, error) {
	f.resetHashes = append(f.resetHashes, hash)
	f.resetPwHash = passwordHash
	if f.invalid {
		return 0, domain.ErrInvalidToken
	}
	return 1, nil
}

type emailUserStoreFake struct {
	users map[string]*domain.User
}

func (f *emailUserStoreFake) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if u, ok := f.users[email]; ok {
		return u, nil
	}
	return nil, domain.ErrNotFound
}

func (f *emailUserStoreFake) GetByID(_ context.Context, id uint) (*domain.User, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}

var testNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func newAccountEmailFixture(t *testing.T) (*AccountEmailService, *emailTokenStoreFake, *mail.Memory) {
	t.Helper()
	users := &emailUserStoreFake{users: map[string]*domain.User{
		"ann@example.test":      {ID: 1, Email: "ann@example.test"},
		"verified@example.test": {ID: 2, Email: "verified@example.test", EmailVerifiedAt: &testNow},
	}}
	tokens := &emailTokenStoreFake{}
	sender := mail.NewMemory()
	svc := NewAccountEmailService(users, tokens, sender, AccountEmailConfig{
		FrontendURL: "https://app.example.test/", VerificationTTL: 48 * time.Hour, ResetTTL: time.Hour,
	})
	svc.now = func() time.Time { return testNow }
	return svc, tokens, sender
}

func tokenFromMessage(t *testing.T, msg mail.Message, path string) string {
	t.Helper()
	for _, field := range strings.Fields(msg.Text) {
		if strings.HasPrefix(field, "https://") {
			u, err := url.Parse(field)
			if err != nil {
				t.Fatalf("parse link %q: %v", field, err)
			}
			if u.Host != "app.example.test" || u.Path != path {
				t.Fatalf("link = %q, want host app.example.test and path %s", field, path)
			}
			return u.Query().Get("token")
		}
	}
	t.Fatalf("no link in %q", msg.Text)
	return ""
}

func TestSendVerificationStoresOnlyTheHashOfTheLinkToken(t *testing.T) {
	svc, tokens, sender := newAccountEmailFixture(t)

	if err := svc.SendVerification(context.Background(), &domain.User{ID: 1, Email: "ann@example.test"}); err != nil {
		t.Fatal(err)
	}
	msgs := sender.To("ann@example.test")
	if len(msgs) != 1 || len(tokens.created) != 1 {
		t.Fatalf("messages=%d tokens=%d", len(msgs), len(tokens.created))
	}
	token := tokenFromMessage(t, msgs[0], "/verify-email")
	stored := tokens.created[0]
	if len(token) < 40 {
		t.Fatalf("token %q is too short", token)
	}
	if stored.hash == token || stored.hash != hashSecretToken(token) {
		t.Fatalf("stored %q for token %q: only the sha256 of the token may be stored", stored.hash, token)
	}
	if stored.userID != 1 || stored.kind != TokenKindVerify || !stored.expiresAt.Equal(testNow.Add(48*time.Hour)) {
		t.Fatalf("stored token = %+v", stored)
	}
	if strings.Contains(msgs[0].Subject, token) || !strings.Contains(msgs[0].Text, "48 hours") {
		t.Fatalf("message = %+v", msgs[0])
	}
}

func TestTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		token, hash, err := newSecretToken()
		if err != nil || seen[token] || seen[hash] {
			t.Fatalf("token %q hash %q err %v", token, hash, err)
		}
		seen[token], seen[hash] = true, true
	}
}

func TestForgotPasswordSendsOnlyToKnownAccountsAndSaysNothingElse(t *testing.T) {
	svc, tokens, sender := newAccountEmailFixture(t)

	svc.ForgotPassword(context.Background(), "nobody@example.test")
	if len(sender.Messages()) != 0 || len(tokens.created) != 0 {
		t.Fatal("an unknown address caused a token or a mail")
	}

	svc.ForgotPassword(context.Background(), "  ANN@example.test ")
	msgs := sender.To("ann@example.test")
	if len(msgs) != 1 || msgs[0].Subject != "Reset your password" {
		t.Fatalf("messages = %+v", sender.Messages())
	}
	token := tokenFromMessage(t, msgs[0], "/reset-password")
	if tokens.created[0].kind != TokenKindReset || tokens.created[0].hash != hashSecretToken(token) ||
		!tokens.created[0].expiresAt.Equal(testNow.Add(time.Hour)) || !strings.Contains(msgs[0].Text, "1 hour") {
		t.Fatalf("stored = %+v", tokens.created[0])
	}
}

func TestMailsAreRateLimitedPerAccount(t *testing.T) {
	svc, tokens, sender := newAccountEmailFixture(t)

	recent := testNow.Add(-30 * time.Second)
	tokens.last = &recent
	svc.ForgotPassword(context.Background(), "ann@example.test")
	svc.ResendVerification(context.Background(), "ann@example.test")
	if len(sender.Messages()) != 0 || len(tokens.created) != 0 {
		t.Fatalf("mail was sent %d times inside the cool-down", len(sender.Messages()))
	}

	older := testNow.Add(-ResendInterval - time.Second)
	tokens.last = &older
	svc.ForgotPassword(context.Background(), "ann@example.test")
	svc.ResendVerification(context.Background(), "ann@example.test")
	if len(sender.Messages()) != 2 {
		t.Fatalf("messages after the cool-down = %d, want 2", len(sender.Messages()))
	}
}

func TestResendVerificationIgnoresVerifiedAndUnknownAddresses(t *testing.T) {
	svc, tokens, sender := newAccountEmailFixture(t)
	svc.ResendVerification(context.Background(), "verified@example.test")
	svc.ResendVerification(context.Background(), "nobody@example.test")
	if len(sender.Messages()) != 0 || len(tokens.created) != 0 {
		t.Fatalf("unexpected mail: %+v", sender.Messages())
	}
}

func TestAMailFailureDoesNotLeakThroughTheSilentFlows(t *testing.T) {
	svc, tokens, sender := newAccountEmailFixture(t)
	sender.FailWith(errors.New("smtp down"))

	svc.ForgotPassword(context.Background(), "ann@example.test")
	svc.ResendVerification(context.Background(), "ann@example.test")

	if err := svc.SendVerification(context.Background(), &domain.User{ID: 1, Email: "ann@example.test"}); err == nil {
		t.Fatal("SendVerification must report a delivery failure to its caller")
	}
	if len(tokens.created) != 3 || len(tokens.discarded) != 3 {
		t.Fatalf("created %d tokens and discarded %d: a token for a mail that was never sent must not stay behind", len(tokens.created), len(tokens.discarded))
	}
	for i, token := range tokens.created {
		if tokens.discarded[i] != token.hash {
			t.Errorf("discarded %q, want the token %q that was created", tokens.discarded[i], token.hash)
		}
	}
}

func TestVerifyEmailHashesTheTrimmedToken(t *testing.T) {
	svc, tokens, _ := newAccountEmailFixture(t)
	if err := svc.VerifyEmail(context.Background(), "  abc123  "); err != nil {
		t.Fatal(err)
	}
	if len(tokens.verifyHashes) != 1 || tokens.verifyHashes[0] != hashSecretToken("abc123") {
		t.Fatalf("hashes = %v", tokens.verifyHashes)
	}
	tokens.invalid = true
	if err := svc.VerifyEmail(context.Background(), "abc123"); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestResetPasswordChecksThePolicyBeforeSpendingTheToken(t *testing.T) {
	svc, tokens, _ := newAccountEmailFixture(t)

	if err := svc.ResetPassword(context.Background(), "tok", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("err = %v", err)
	}
	if err := svc.ResetPassword(context.Background(), "tok", strings.Repeat("x", MaxPasswordLength+1)); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("err = %v", err)
	}
	if len(tokens.resetHashes) != 0 {
		t.Fatal("a rejected password must not consume the token")
	}

	if err := svc.ResetPassword(context.Background(), " tok ", "a-new-good-password"); err != nil {
		t.Fatal(err)
	}
	if tokens.resetHashes[0] != hashSecretToken("tok") {
		t.Fatalf("hash = %q", tokens.resetHashes[0])
	}
	if !verifyPassword("a-new-good-password", tokens.resetPwHash) || strings.Contains(tokens.resetPwHash, "a-new-good-password") {
		t.Fatalf("stored password hash %q is not an argon2 hash of the new password", tokens.resetPwHash)
	}

	tokens.invalid = true
	if err := svc.ResetPassword(context.Background(), "tok", "another-good-password"); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestMailLanguageFollowsTheRequest(t *testing.T) {
	cases := map[string]string{
		"":                  localeEnglish,
		"en-US,en;q=0.9":    localeEnglish,
		"ru":                localeRussian,
		"ru-RU,ru;q=0.9,en": localeRussian,
		"RU-ru":             localeRussian,
		"de,ru;q=0.5":       localeEnglish,
		"russian":           localeEnglish,
		"  ru ; q=1":        localeRussian,
		"fr-CH, fr;q=0.9":   localeEnglish,
	}
	for header, want := range cases {
		if got := localeFromHeader(header); got != want {
			t.Errorf("Accept-Language %q -> %q, want %q", header, got, want)
		}
	}

	svc, _, sender := newAccountEmailFixture(t)
	ctx := WithLocale(context.Background(), "ru-RU,ru;q=0.9")
	svc.ForgotPassword(ctx, "ann@example.test")
	msgs := sender.To("ann@example.test")
	if len(msgs) != 1 || msgs[0].Subject != "Сброс пароля" || !strings.Contains(msgs[0].Text, "действует 1 ч и срабатывает") || !strings.Contains(msgs[0].HTML, "Сбросить пароль") {
		t.Fatalf("message = %+v", msgs)
	}
	if got := localeFrom(context.Background()); got != localeEnglish {
		t.Fatalf("default locale = %q", got)
	}
}

func TestTTLText(t *testing.T) {
	cases := []struct {
		d      time.Duration
		locale string
		want   string
	}{
		{48 * time.Hour, localeEnglish, "48 hours"},
		{time.Hour, localeEnglish, "1 hour"},
		{90 * time.Minute, localeEnglish, "2 hours"},
		{30 * time.Minute, localeEnglish, "30 minutes"},
		{time.Minute, localeEnglish, "1 minute"},
		{10 * time.Second, localeEnglish, "1 minute"},
		{24 * time.Hour, localeRussian, "24 ч"},
		{15 * time.Minute, localeRussian, "15 мин"},
	}
	for _, tc := range cases {
		if got := ttlText(tc.locale, tc.d); got != tc.want {
			t.Errorf("ttlText(%s, %v) = %q, want %q", tc.locale, tc.d, got, tc.want)
		}
	}
}

func TestMailHTMLEscapesTheLink(t *testing.T) {
	_, text, html, err := renderMail(localeEnglish, TokenKindReset, mailView{
		Link: `https://app.example.test/reset?token=a"><script>alert(1)</script>`, TTL: "1 hour",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("HTML part carries a raw script tag: %s", html)
	}
	if !strings.Contains(text, `token=a"><script>`) {
		t.Fatalf("the plain text part must keep the link as it is: %s", text)
	}
	if _, _, _, err := renderMail("xx", TokenKindVerify, mailView{Link: "https://x.test", TTL: "1 hour"}); err != nil {
		t.Fatalf("an unknown locale must fall back to English: %v", err)
	}
}

type authStoreFake struct {
	AuthUserStore
	user      *domain.User
	created   *domain.User
	createErr error
	sessions  int
}

func (f *authStoreFake) CreateUser(_ context.Context, user *domain.User) error {
	if f.createErr != nil {
		return f.createErr
	}
	user.ID = 77
	f.created = user
	return nil
}

func (f *authStoreFake) GetByEmail(context.Context, string) (*domain.User, error) {
	if f.user == nil {
		return nil, domain.ErrNotFound
	}
	return f.user, nil
}

func (f *authStoreFake) CreateSession(context.Context, *domain.UserSession) error {
	f.sessions++
	return nil
}

func (f *authStoreFake) TouchLoginDevice(context.Context, uint, string, string) (bool, bool, error) {
	return false, false, nil
}

type verificationSenderFake struct {
	sent []*domain.User
	err  error
}

func (v *verificationSenderFake) SendVerification(_ context.Context, user *domain.User) error {
	v.sent = append(v.sent, user)
	return v.err
}

func TestRegisterSendsTheVerificationMailAndSurvivesItsFailure(t *testing.T) {
	store := &authStoreFake{}
	sender := &verificationSenderFake{}
	svc := NewAuthService(store, "12345678901234567890123456789012", time.Hour, time.Hour).WithVerificationSender(sender)

	if err := svc.Register(context.Background(), " Ann@Example.test ", "good-password-1", "ann"); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || sender.sent[0].ID != 77 || sender.sent[0].Email != "ann@example.test" {
		t.Fatalf("verification requests = %+v", sender.sent)
	}

	sender.err = errors.New("smtp down")
	if err := svc.Register(context.Background(), "bob@example.test", "good-password-1", "bob"); err != nil {
		t.Fatalf("a mail failure must not fail the registration: %v", err)
	}

	store.createErr = domain.ErrEmailTaken
	before := len(sender.sent)
	if err := svc.Register(context.Background(), "ann@example.test", "good-password-1", "ann2"); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v", err)
	}
	if len(sender.sent) != before {
		t.Fatal("a failed registration sent a verification mail")
	}
}

func TestLoginCanRequireAVerifiedEmail(t *testing.T) {
	hash, err := hashPassword("good-password-1")
	if err != nil {
		t.Fatal(err)
	}
	verified := testNow
	secret := "12345678901234567890123456789012"

	open := NewAuthService(&authStoreFake{user: &domain.User{ID: 1, PasswordHash: hash}}, secret, time.Hour, time.Hour)
	if _, err := open.Login(context.Background(), "ann@example.test", "good-password-1"); err != nil {
		t.Fatalf("login must work for an unverified user when verification is not required: %v", err)
	}

	for name, tc := range map[string]struct {
		user     *domain.User
		password string
		want     error
	}{
		"unverified":     {&domain.User{ID: 1, PasswordHash: hash}, "good-password-1", ErrEmailNotVerified},
		"wrong password": {&domain.User{ID: 1, PasswordHash: hash}, "nope-nope-nope", ErrInvalidCredentials},
		"verified":       {&domain.User{ID: 1, PasswordHash: hash, EmailVerifiedAt: &verified}, "good-password-1", nil},
	} {
		t.Run(name, func(t *testing.T) {
			strict := NewAuthService(&authStoreFake{user: tc.user}, secret, time.Hour, time.Hour).WithRequireVerifiedEmail(true)
			_, err := strict.Login(context.Background(), "ann@example.test", tc.password)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
