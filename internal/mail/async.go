package mail

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	asyncQueueSize = 256
	asyncWorkers   = 2
	asyncSendLimit = 45 * time.Second
)

type Async struct {
	inner  Sender
	logger *slog.Logger
	queue  chan Message

	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

func NewAsync(inner Sender, logger *slog.Logger) *Async {
	if logger == nil {
		logger = slog.Default()
	}
	a := &Async{inner: inner, logger: logger, queue: make(chan Message, asyncQueueSize)}
	for i := 0; i < asyncWorkers; i++ {
		a.wg.Add(1)
		go a.run()
	}
	return a
}

func (a *Async) run() {
	defer a.wg.Done()
	for msg := range a.queue {
		ctx, cancel := context.WithTimeout(context.Background(), asyncSendLimit)
		if err := a.inner.Send(ctx, msg); err != nil {
			a.logger.Error("mail: cannot send a message", "subject", msg.Subject, "error", err)
		}
		cancel()
	}
}

func (a *Async) Send(_ context.Context, msg Message) error {
	if _, err := msg.validate(); err != nil {
		return err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return ErrClosed
	}
	select {
	case a.queue <- msg:
		return nil
	default:
		return ErrQueueFull
	}
}

func (a *Async) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	close(a.queue)
	a.mu.Unlock()
	a.wg.Wait()
}
