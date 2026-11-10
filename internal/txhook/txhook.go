package txhook

import (
	"context"
	"sync"
)

type key struct{}

type collector struct {
	mu    sync.Mutex
	hooks []func()
}

func With(ctx context.Context) (context.Context, func() []func()) {
	if _, ok := ctx.Value(key{}).(*collector); ok {
		return ctx, func() []func() { return nil }
	}
	c := &collector{}
	return context.WithValue(ctx, key{}, c), func() []func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		hooks := c.hooks
		c.hooks = nil
		return hooks
	}
}

func AfterCommit(ctx context.Context, fn func()) {
	c, ok := ctx.Value(key{}).(*collector)
	if !ok {
		fn()
		return
	}
	c.mu.Lock()
	c.hooks = append(c.hooks, fn)
	c.mu.Unlock()
}
