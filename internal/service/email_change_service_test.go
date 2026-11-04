package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/mail"
)

type changeStoreFake struct {
	created    []string
	hashes     []string
	expires    time.Time
	last       *time.Time
	discarded  []string
	confirmed  domain.EmailChange
	confirmErr error
	canceled   bool
	pending    string
}

func (f *changeStoreFake) Create(_ context.Context, _ uint, email, hash string, expiresAt time.Time) error {
	f.created = append(f.created, email)
	f.hashes = append(f.hashes, hash)
	f.expires = expiresAt
	return nil
}
func (f *changeStoreFake) Discard(_ context.Context, hash string) error {
	f.discarded = append(f.discarded, hash)
	return nil
}
func (f *changeStoreFake) LastCreatedAt(context.Context, uint) (*time.Time, error) {
	return f.last, nil
}
func (f *changeStoreFake) Pending(context.Context, uint) (string, error) { return f.pending, nil }
func (f *changeStoreFake) Cancel(context.Context, uint) error {
	f.canceled = true
	return nil
}
func (f *changeStoreFake) Confirm(_ context.Context, hash string) (domain.EmailChange, error) {
	f.hashes = append(f.hashes, hash)
	return f.confirmed, f.confirmErr
}

type changeUsersFake struct {
	byID  *domain.User
	taken map[string]bool
}

func (f *changeUsersFake) GetByID(context.Context, uint) (*domain.User, error) { return f.byID, nil }
func (f *changeUsersFake) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if f.taken[email] {
		return &domain.User{ID: 99, Email: email}, nil
	}
	return nil, domain.ErrNotFound
}

func newChangeFixture(t *testing.T) (*EmailChangeService, *changeStoreFake, *mail.Memory) {
	t.Helper()
	hash, err := hashPassword("good-password-1")
	if err != nil {
		t.Fatal(err)
	}
	users := &changeUsersFake{
		byID:  &domain.User{ID: 1, Email: "Ann@example.test", PasswordHash: hash},
		taken: map[string]bool{"bob@example.test": true},
	}
	store := &changeStoreFake{}
	sender := mail.NewMemory()
	svc := NewEmailChangeService(users, store, sender, "https://app.example.test/")
	svc.now = func() time.Time { return testNow }
	return svc, store, sender
}

func TestEmailChangeRequestSendsALinkToTheNewAddressOnly(t *testing.T) {
	svc, store, sender := newChangeFixture(t)
	ctx := WithLocale(context.Background(), "ru")
	if err := svc.Request(ctx, 1, " New@Example.test ", "good-password-1"); err != nil {
		t.Fatal(err)
	}
	if len(store.created) != 1 || store.created[0] != "new@example.test" {
		t.Fatalf("created = %v", store.created)
	}
	if !store.expires.Equal(testNow.Add(24 * time.Hour)) {
		t.Fatalf("expires = %v", store.expires)
	}
	msgs := sender.To("new@example.test")
	if len(msgs) != 1 || len(sender.Messages()) != 1 {
		t.Fatalf("messages = %+v", sender.Messages())
	}
	token := tokenFromMessage(t, msgs[0], "/confirm-email-change")
	if store.hashes[0] != hashSecretToken(token) || store.hashes[0] == token {
		t.Fatal("only the hash of the token may be stored")
	}
	if msgs[0].Subject != "Подтвердите новый адрес электронной почты" || !strings.Contains(msgs[0].Text, "24 ч") {
		t.Fatalf("message = %+v", msgs[0])
	}
}

func TestEmailChangeRequestRules(t *testing.T) {
	cases := []struct {
		name     string
		email    string
		password string
		last     *time.Time
		want     error
	}{
		{"wrong password", "new@example.test", "nope", nil, ErrInvalidCredentials},
		{"same address", "ANN@example.test", "good-password-1", nil, ErrSameEmail},
		{"taken address", "bob@example.test", "good-password-1", nil, domain.ErrEmailTaken},
		{"cooldown", "new@example.test", "good-password-1", ptrTime(testNow.Add(-30 * time.Second)), ErrEmailChangeCooldown},
		{"after the cooldown", "new@example.test", "good-password-1", ptrTime(testNow.Add(-2 * time.Minute)), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, sender := newChangeFixture(t)
			store.last = tc.last
			err := svc.Request(context.Background(), 1, tc.email, tc.password)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil && (len(store.created) != 0 || len(sender.Messages()) != 0) {
				t.Fatal("a rejected request stored a token or sent a mail")
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestEmailChangeRequestDiscardsTheTokenWhenDeliveryFails(t *testing.T) {
	svc, store, sender := newChangeFixture(t)
	boom := errors.New("smtp down")
	sender.FailWith(boom)
	if err := svc.Request(context.Background(), 1, "new@example.test", "good-password-1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if len(store.discarded) != 1 || store.discarded[0] != store.hashes[0] {
		t.Fatalf("discarded = %v, created = %v", store.discarded, store.hashes)
	}
}

func TestEmailChangeConfirmNotifiesTheOldAddressWithAMaskedNewOne(t *testing.T) {
	svc, store, sender := newChangeFixture(t)
	store.confirmed = domain.EmailChange{UserID: 1, OldEmail: "ann@example.test", NewEmail: "jordan@secret.test"}
	if err := svc.Confirm(context.Background(), " tok "); err != nil {
		t.Fatal(err)
	}
	if store.hashes[0] != hashSecretToken("tok") {
		t.Fatal("the token must be trimmed and hashed")
	}
	msgs := sender.To("ann@example.test")
	if len(msgs) != 1 || len(sender.Messages()) != 1 {
		t.Fatalf("messages = %+v", sender.Messages())
	}
	if !strings.Contains(msgs[0].Text, "j***@secret.test") || strings.Contains(msgs[0].Text, "jordan") || strings.Contains(msgs[0].HTML, "jordan") {
		t.Fatalf("notice = %q", msgs[0].Text)
	}
}

func TestEmailChangeConfirmErrorsSendNothing(t *testing.T) {
	for _, want := range []error{domain.ErrInvalidToken, domain.ErrEmailTaken} {
		svc, store, sender := newChangeFixture(t)
		store.confirmErr = want
		if err := svc.Confirm(context.Background(), "tok"); !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
		if len(sender.Messages()) != 0 {
			t.Fatal("a notice was sent for a failed change")
		}
	}
}

func TestEmailChangeConfirmSucceedsWhenTheNoticeCannotBeSent(t *testing.T) {
	svc, store, sender := newChangeFixture(t)
	store.confirmed = domain.EmailChange{UserID: 1, OldEmail: "ann@example.test", NewEmail: "new@example.test"}
	sender.FailWith(errors.New("smtp down"))
	if err := svc.Confirm(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{
		"jordan@secret.test": "j***@secret.test",
		"é@x.test":           "é***@x.test",
		"nonsense":           "***",
		"@x.test":            "***",
	} {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmailChangeTemplatesExistInEveryLocale(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeRussian} {
		for _, kind := range []string{TokenKindChangeEmail, noticeKindEmailChanged} {
			subject, text, html, err := renderMail(locale, kind, mailView{Link: "https://x.test/?token=abc", TTL: "24 hours", Address: "a***@x.test"})
			if err != nil || subject == "" || text == "" || html == "" {
				t.Fatalf("%s/%s: %q %q %q %v", locale, kind, subject, text, html, err)
			}
			if kind == TokenKindChangeEmail && !strings.Contains(text, "https://x.test/?token=abc") {
				t.Errorf("%s: the link is missing", locale)
			}
			if kind == noticeKindEmailChanged && !strings.Contains(html, "a***@x.test") {
				t.Errorf("%s: the masked address is missing", locale)
			}
		}
	}
}
