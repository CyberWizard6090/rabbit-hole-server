package integration

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/repository"
	"rabbit-hole-server/internal/service"
)

func TestContext_CancelledContextAbortsDatabaseCalls(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repository.NewUserRepository(tc.DB).GetByID(ctx, tc.UserID); !errors.Is(err, context.Canceled) {
		t.Fatalf("repository with canceled ctx: err = %v, want context.Canceled", err)
	}

	checker := service.NewPermissionChecker(tc.DB)
	if _, err := checker.HasWorkspacePermission(ctx, tc.UserID, 1, "space.read"); !errors.Is(err, context.Canceled) {
		t.Fatalf("permission checker with canceled ctx: err = %v, want context.Canceled", err)
	}

	if _, err := repository.NewUserRepository(tc.DB).GetByID(context.Background(), tc.UserID); err != nil {
		t.Fatalf("repository with live ctx: %v", err)
	}
}
