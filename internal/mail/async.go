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
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

func NewAsync(inner Sender, logger *slog.Logger) *Async {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &Async{
		inner: inner, logger: logger, queue: make(chan Message, asyncQueueSize),
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
	}
	for i := 0; i < asyncWorkers; i++ {
		a.wg.Add(1)
		go a.run()
	}
	go func() {
		a.wg.Wait()
		close(a.done)
	}()
	return a
}

func (a *Async) run() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			for range a.queue {
			}
			return
		case msg, ok := <-a.queue:
			if !ok {
				return
			}
			if a.ctx.Err() != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(a.ctx, asyncSendLimit)
			if err := a.inner.Send(ctx, msg); err != nil {
				a.logger.Error("mail: cannot send a message", "subject", msg.Subject, "error", err)
			}
			cancel()
		}
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
	if err := a.Shutdown(context.Background()); err != nil {
		a.logger.Error("mail: cannot shut down sender", "error", err)
	}
}

func (a *Async) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.queue)
	}
	a.mu.Unlock()

	select {
	case <-a.done:
		a.cancel()
		return nil
	case <-ctx.Done():
		a.cancel()
		return ctx.Err()
	}
}
