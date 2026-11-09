package service

import (
	"context"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

type replayStore struct {
	AuthUserStore
	rotateErr error
	revoked   []string
}

func (s *replayStore) GetTokenVersion(context.Context, uint) (int, error) { return 0, nil }

func (s *replayStore) RotateSession(context.Context, string, *domain.UserSession) error {
	return s.rotateErr
}

func (s *replayStore) RevokeSessionFamily(_ context.Context, familyID string) error {
	s.revoked = append(s.revoked, familyID)
	return nil
}

func TestRefreshReplayRevokesTheFamilyOnlyAfterTheGrace(t *testing.T) {
	tests := []struct {
		name        string
		rotateErr   error
		wantRevoked []string
	}{
		{"replay inside the grace window", &domain.SessionReplay{FamilyID: "fam", Age: 3 * time.Second}, nil},
		{"replay at the end of the grace window", &domain.SessionReplay{FamilyID: "fam", Age: refreshReuseGrace}, nil},
		{"replay after the grace window", &domain.SessionReplay{FamilyID: "fam", Age: refreshReuseGrace + time.Second}, []string{"fam"}},
		{"unknown token", domain.ErrNotFound, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &replayStore{rotateErr: tt.rotateErr}
			svc := NewAuthService(store, "12345678901234567890123456789012", time.Hour, 24*time.Hour)
			pair, _, err := svc.generateTokenPair(7, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Refresh(context.Background(), pair.RefreshToken); err == nil {
				t.Fatal("a replayed or unknown token must be rejected")
			}
			if len(store.revoked) != len(tt.wantRevoked) || (len(tt.wantRevoked) == 1 && store.revoked[0] != tt.wantRevoked[0]) {
				t.Fatalf("revoked families = %v, want %v", store.revoked, tt.wantRevoked)
			}
		})
	}
}
