package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/ratelimit"
)

type hardeningStore struct {
	AuthUserStore
	user     *domain.User
	lookups  int
	sessions int
}

func (s *hardeningStore) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	s.lookups++
	if s.user != nil && s.user.Email == email {
		return s.user, nil
	}
	return nil, domain.ErrNotFound
}

func (s *hardeningStore) GetByID(context.Context, uint) (*domain.User, error) { return s.user, nil }

func (s *hardeningStore) CreateSession(context.Context, *domain.UserSession) error {
	s.sessions++
	return nil
}

func (s *hardeningStore) RevokeAll(context.Context, uint) (int, error) { return 2, nil }

func (s *hardeningStore) UpdatePasswordAndRevoke(context.Context, uint, string) (int, error) {
	return 3, nil
}

func (s *hardeningStore) AnonymizeIfNoOwnedWorkspaces(context.Context, uint) error { return nil }

type recordingRevoker struct {
	mu    sync.Mutex
	users []uint
	feeds []uint
}

func (r *recordingRevoker) DisconnectUser(id uint) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = append(r.users, id)
	return 1
}

func (r *recordingRevoker) DisconnectUserFeeds(id uint) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.feeds = append(r.feeds, id)
	return 1
}

type deletingMemberRepo struct {
	memberRepoStub
	members []domain.MemberDetail
}

func (d *deletingMemberRepo) ListMembers(context.Context, uint) ([]domain.MemberDetail, error) {
	return d.members, nil
}

func (d *deletingMemberRepo) Delete(context.Context, uint) error { return nil }

func newHardeningService(t *testing.T) (*AuthService, *hardeningStore) {
	t.Helper()
	hash, err := hashPassword("correct-password-1")
	if err != nil {
		t.Fatal(err)
	}
	store := &hardeningStore{user: &domain.User{Email: "user@example.test", PasswordHash: hash}}
	store.user.ID = 7
	return NewAuthService(store, "12345678901234567890123456789012", time.Hour, time.Hour), store
}

func TestLoginLocksAfterRepeatedFailuresWithTheUsualAnswer(t *testing.T) {
	svc, store := newHardeningService(t)
	svc.WithLoginBackoff(ratelimit.NewBackoff(ratelimit.BackoffConfig{Threshold: 3, Base: time.Hour, Max: time.Hour}))
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := svc.Login(ctx, "user@example.test", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	lookups := store.lookups
	if _, err := svc.Login(ctx, "User@Example.test", "correct-password-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("a locked account must answer like a wrong password, got %v", err)
	}
	if store.lookups != lookups || store.sessions != 0 {
		t.Fatal("a locked attempt must not reach the database or hash the password")
	}
}

func TestLoginLockCoversUnknownAddressesTheSameWay(t *testing.T) {
	svc, store := newHardeningService(t)
	svc.WithLoginBackoff(ratelimit.NewBackoff(ratelimit.BackoffConfig{Threshold: 2, Base: time.Hour, Max: time.Hour}))
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.Login(ctx, "ghost@example.test", "whatever"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("unknown address attempt %d: %v", i+1, err)
		}
	}
	lookups := store.lookups
	if _, err := svc.Login(ctx, "ghost@example.test", "whatever"); !errors.Is(err, ErrInvalidCredentials) || store.lookups != lookups {
		t.Fatal("unknown addresses must be locked exactly like known ones")
	}
}

func TestSuccessfulLoginClearsTheFailureCounter(t *testing.T) {
	svc, _ := newHardeningService(t)
	svc.WithLoginBackoff(ratelimit.NewBackoff(ratelimit.BackoffConfig{Threshold: 3, Base: time.Hour, Max: time.Hour}))
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = svc.Login(ctx, "user@example.test", "wrong")
	}
	if _, err := svc.Login(ctx, "user@example.test", "correct-password-1"); err != nil {
		t.Fatalf("login before the lock: %v", err)
	}
	for i := 0; i < 2; i++ {
		_, _ = svc.Login(ctx, "user@example.test", "wrong")
	}
	if _, err := svc.Login(ctx, "user@example.test", "correct-password-1"); err != nil {
		t.Fatalf("the counter must restart after a success: %v", err)
	}
}

func TestPasswordGateReturnsBusyWhenSaturated(t *testing.T) {
	ConfigurePasswordHashing(1, 20*time.Millisecond)
	t.Cleanup(func() { ConfigurePasswordHashing(0, DefaultPasswordHashWait) })

	release, err := acquirePasswordSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hashPasswordLimited(context.Background(), "password-123"); !errors.Is(err, ErrPasswordHashBusy) {
		t.Fatalf("hash while saturated: %v", err)
	}
	if _, err := verifyPasswordLimited(context.Background(), "p", "h"); !errors.Is(err, ErrPasswordHashBusy) {
		t.Fatalf("verify while saturated: %v", err)
	}
	svc, _ := newHardeningService(t)
	if _, err := svc.Login(context.Background(), "user@example.test", "correct-password-1"); !errors.Is(err, ErrPasswordHashBusy) {
		t.Fatalf("login while saturated: %v", err)
	}
	if PasswordHashRetryAfter() < 1 {
		t.Fatal("Retry-After must be at least one second")
	}
	release()
	if _, err := hashPasswordLimited(context.Background(), "password-123"); err != nil {
		t.Fatalf("hash after release: %v", err)
	}
}

func TestPasswordGateWaitsForASlot(t *testing.T) {
	ConfigurePasswordHashing(1, 2*time.Second)
	t.Cleanup(func() { ConfigurePasswordHashing(0, DefaultPasswordHashWait) })

	release, _ := acquirePasswordSlot(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		release()
	}()
	if _, err := hashPasswordLimited(context.Background(), "password-123"); err != nil {
		t.Fatalf("a request must wait for a free slot: %v", err)
	}
}

func TestPasswordGateStopsWaitingWhenTheContextEnds(t *testing.T) {
	ConfigurePasswordHashing(1, time.Minute)
	t.Cleanup(func() { ConfigurePasswordHashing(0, DefaultPasswordHashWait) })

	release, _ := acquirePasswordSlot(context.Background())
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := hashPasswordLimited(ctx, "password-123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the context error, got %v", err)
	}
}

func TestSessionRevocationsDisconnectRealtimeConnections(t *testing.T) {
	svc, _ := newHardeningService(t)
	revoker := &recordingRevoker{}
	svc.WithConnectionRevoker(revoker)
	ctx := context.Background()

	if err := svc.LogoutAll(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangePassword(ctx, 7, "correct-password-1", "another-password-2"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAccount(ctx, 7, "correct-password-1"); err != nil {
		t.Fatal(err)
	}
	if len(revoker.users) != 3 {
		t.Fatalf("disconnected users = %v, want three calls", revoker.users)
	}
}

func TestWorkspaceRemovalDisconnectsOnlyTheFeeds(t *testing.T) {
	revoker := &recordingRevoker{}
	repo := &deletingMemberRepo{memberRepoStub: memberRepoStub{ownerID: 1}, members: []domain.MemberDetail{{UserID: 3}, {UserID: 4}}}
	svc := NewWorkspaceServiceWithRevoker(repo, revoker)
	ctx := context.Background()

	if err := svc.RemoveMember(ctx, 10, 3); err != nil {
		t.Fatal(err)
	}
	if len(revoker.feeds) != 1 || revoker.feeds[0] != 3 || len(revoker.users) != 0 {
		t.Fatalf("feeds = %v users = %v", revoker.feeds, revoker.users)
	}
	if err := svc.DeleteWorkspace(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if len(revoker.feeds) != 3 {
		t.Fatalf("deleting the workspace must close the feeds of its members: %v", revoker.feeds)
	}
}
