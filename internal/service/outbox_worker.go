package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"rabbit-hole-server/internal/domain"
)

type OutboxHandler interface {
	Handle(ctx context.Context, event domain.OutboxEvent, final bool) error
}

type OutboxHandlerFunc func(ctx context.Context, event domain.OutboxEvent, final bool) error

func (f OutboxHandlerFunc) Handle(ctx context.Context, event domain.OutboxEvent, final bool) error {
	return f(ctx, event, final)
}

type OutboxRoute struct {
	Type          string
	Handler       OutboxHandler
	Transactional bool
	MaxAttempts   int
	Backoff       []time.Duration
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func PermanentOutboxError(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

const (
	outboxBatchSize    = 20
	outboxLease        = 2 * time.Minute
	outboxPollInterval = time.Second
	outboxErrorCap     = 500
)

type OutboxWorker struct {
	repo        domain.OutboxRepository
	tx          TransactionRunner
	logger      *slog.Logger
	routes      map[string]OutboxRoute
	types       []string
	concurrency int
	now         func() time.Time
	wake        chan struct{}
	mu          sync.Mutex
}

func NewOutboxWorker(repo domain.OutboxRepository, tx TransactionRunner, concurrency int, logger *slog.Logger) *OutboxWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	return &OutboxWorker{
		repo: repo, tx: tx, logger: logger, routes: map[string]OutboxRoute{}, concurrency: concurrency,
		now: time.Now, wake: make(chan struct{}, 1),
	}
}

func (w *OutboxWorker) Register(route OutboxRoute) {
	if route.MaxAttempts <= 0 {
		route.MaxAttempts = 5
	}
	if len(route.Backoff) == 0 {
		route.Backoff = []time.Duration{time.Minute}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.routes[route.Type]; !exists {
		w.types = append(w.types, route.Type)
	}
	w.routes[route.Type] = route
}

func (w *OutboxWorker) Poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *OutboxWorker) routeFor(eventType string) (OutboxRoute, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	route, ok := w.routes[eventType]
	return route, ok
}

func (w *OutboxWorker) claimTypes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.types...)
}

func (w *OutboxWorker) Run(ctx context.Context) {
	var group sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			w.loop(ctx)
		}()
	}
	group.Wait()
}

func (w *OutboxWorker) loop(ctx context.Context) {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()
	for {
		for {
			processed, err := w.RunOnce(ctx)
			if err != nil {
				w.logger.Warn("outbox: cannot process events", "error", err)
			}
			if processed == 0 || err != nil || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

func (w *OutboxWorker) RunOnce(ctx context.Context) (int, error) {
	types := w.claimTypes()
	if len(types) == 0 {
		return 0, nil
	}
	events, err := w.repo.Claim(ctx, types, outboxBatchSize, outboxLease)
	if err != nil {
		return 0, fmt.Errorf("claim outbox events: %w", err)
	}
	for _, event := range events {
		w.process(ctx, event)
	}
	return len(events), nil
}

func (w *OutboxWorker) Drain(ctx context.Context) (int, error) {
	total := 0
	for {
		processed, err := w.RunOnce(ctx)
		total += processed
		if err != nil {
			return total, err
		}
		if processed > 0 {
			continue
		}
		due, err := w.repo.HasDue(ctx, w.claimTypes())
		if err != nil {
			return total, err
		}
		if !due {
			return total, nil
		}
	}
}

func DrainOutbox(ctx context.Context, workers ...*OutboxWorker) error {
	for {
		total := 0
		for _, w := range workers {
			n, err := w.Drain(ctx)
			if err != nil {
				return err
			}
			total += n
		}
		if total == 0 {
			return nil
		}
	}
}

func (w *OutboxWorker) process(ctx context.Context, event domain.OutboxEvent) {
	route, ok := w.routeFor(event.Type)
	if !ok {
		w.settle(ctx, event, OutboxRoute{MaxAttempts: 1}, errors.New("no handler for the event type"))
		return
	}
	final := event.Attempts >= route.MaxAttempts
	err := w.handle(ctx, route, event, final)
	if err == nil {
		if !route.Transactional {
			if completeErr := w.repo.Complete(ctx, event.ID); completeErr != nil {
				w.logger.Warn("outbox: cannot mark an event as done", "id", event.ID, "type", event.Type, "error", completeErr)
			}
		}
		return
	}
	w.settle(ctx, event, route, err)
}

func (w *OutboxWorker) handle(ctx context.Context, route OutboxRoute, event domain.OutboxEvent, final bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	if !route.Transactional || w.tx == nil {
		return route.Handler.Handle(ctx, event, final)
	}
	return w.tx.Run(ctx, func(txCtx context.Context) error {
		if err := route.Handler.Handle(txCtx, event, final); err != nil {
			return err
		}
		return w.repo.Complete(txCtx, event.ID)
	})
}

func (w *OutboxWorker) settle(ctx context.Context, event domain.OutboxEvent, route OutboxRoute, cause error) {
	reason := truncateRunes(cause.Error(), outboxErrorCap)
	var permanent *permanentError
	if errors.As(cause, &permanent) || event.Attempts >= route.MaxAttempts {
		if err := w.repo.Bury(ctx, event.ID, reason); err != nil {
			w.logger.Warn("outbox: cannot bury an event", "id", event.ID, "error", err)
		}
		w.logger.Error("outbox: giving up on an event", "id", event.ID, "type", event.Type, "attempts", event.Attempts, "error", reason)
		return
	}
	index := event.Attempts - 1
	if index < 0 {
		index = 0
	}
	if index >= len(route.Backoff) {
		index = len(route.Backoff) - 1
	}
	if err := w.repo.Retry(ctx, event.ID, reason, w.now().Add(route.Backoff[index])); err != nil {
		w.logger.Warn("outbox: cannot schedule a retry", "id", event.ID, "error", err)
		return
	}
	w.logger.Warn("outbox: event failed, will retry", "id", event.ID, "type", event.Type, "attempts", event.Attempts, "error", reason)
}
