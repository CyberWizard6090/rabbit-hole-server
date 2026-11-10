package domain

import (
	"context"
	"time"
)

type RetentionPolicy struct {
	OutboxDone    time.Duration
	OutboxDead    time.Duration
	ExpiredTokens time.Duration
}

type RetentionResult struct {
	OutboxDone    int64
	OutboxDead    int64
	EmailTokens   int64
	Confirmations int64
	Sessions      int64
	Idempotency   int64
}

func (r RetentionResult) Total() int64 {
	return r.OutboxDone + r.OutboxDead + r.EmailTokens + r.Confirmations + r.Sessions + r.Idempotency
}

type RetentionRepository interface {
	Purge(ctx context.Context, policy RetentionPolicy) (RetentionResult, error)
}

type AccessSyncResult struct {
	Permissions int64
	Roles       int64
	Granted     int64
	Revoked     int64
}

func (r AccessSyncResult) Changed() bool {
	return r.Permissions+r.Roles+r.Granted+r.Revoked > 0
}

type AccessSyncRepository interface {
	SyncDefaultRoles(ctx context.Context) (AccessSyncResult, error)
}
