package service

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"time"
)

var ErrPasswordHashBusy = errors.New("password hashing is saturated, retry shortly")

const DefaultPasswordHashWait = time.Second

type hashGate struct {
	slots chan struct{}
	wait  time.Duration
}

var passwordGate atomic.Pointer[hashGate]

func init() {
	ConfigurePasswordHashing(0, DefaultPasswordHashWait)
}

func ConfigurePasswordHashing(concurrency int, wait time.Duration) {
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}
	passwordGate.Store(&hashGate{slots: make(chan struct{}, concurrency), wait: wait})
}

func PasswordHashRetryAfter() int {
	gate := passwordGate.Load()
	seconds := int((gate.wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

func acquirePasswordSlot(ctx context.Context) (func(), error) {
	gate := passwordGate.Load()
	release := func() { <-gate.slots }
	select {
	case gate.slots <- struct{}{}:
		return release, nil
	default:
	}
	if gate.wait <= 0 {
		return nil, ErrPasswordHashBusy
	}
	timer := time.NewTimer(gate.wait)
	defer timer.Stop()
	select {
	case gate.slots <- struct{}{}:
		return release, nil
	case <-timer.C:
		return nil, ErrPasswordHashBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func hashPasswordLimited(ctx context.Context, password string) (string, error) {
	release, err := acquirePasswordSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return hashPassword(password)
}

func verifyPasswordLimited(ctx context.Context, password, encodedHash string) (bool, error) {
	release, err := acquirePasswordSlot(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return verifyPassword(password, encodedHash), nil
}
