package collab

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/coder/websocket"

	"rabbit-hole-server/internal/domain"
)

// Change is a stored update, as sent to a client catching up.
type Change struct {
	Seq    int64
	UserID uint
	Update []byte
}

// SyncResult is the answer to "what happened after number N".
type SyncResult struct {
	// Resync is true when the history up to N was compacted into a snapshot: the client must reload the state.
	Resync  bool
	Updates []Change
	LastSeq int64
}

// Backend is what a connection needs from the rest of the server. Implementations check the rights of the user.
type Backend interface {
	// AppendUpdate stores one update and publishes it to the other connections; origin is this connection.
	AppendUpdate(ctx context.Context, update []byte, touched []string, origin uint64) (int64, error)
	Sync(ctx context.Context, since int64) (SyncResult, error)
	// Recheck says whether the user may still read the document and whether they may write.
	Recheck(ctx context.Context) (allowed, canWrite bool, err error)
}

// Options are timings of a connection.
type Options struct {
	PingInterval    time.Duration
	RecheckInterval time.Duration
	WriteTimeout    time.Duration
	// UpdatesPerSecond is the sustained rate of updates one connection may send; the burst is twice that.
	UpdatesPerSecond float64
}

func (o Options) withDefaults() Options {
	if o.PingInterval <= 0 {
		o.PingInterval = 25 * time.Second
	}

	if o.RecheckInterval <= 0 {
		o.RecheckInterval = 2 * time.Minute
	}

	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}

	if o.UpdatesPerSecond <= 0 {
		o.UpdatesPerSecond = 50
	}

	return o
}

type bucket struct {
	tokens float64
	max    float64
	rate   float64
	last   time.Time
}

func newBucket(rate float64) *bucket {
	return &bucket{tokens: rate * 2, max: rate * 2, rate: rate, last: time.Now()}
}

func (b *bucket) allow() bool {
	now := time.Now()
	b.tokens = min(b.max, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--

	return true
}

// Serve runs one connection of a joined peer until it ends. `since` is the number of the last update the client
// already has; the server then sends everything after it (the client takes the state from GET /state first).
// The caller leaves the room afterwards.
func Serve(
	ctx context.Context, conn *websocket.Conn, manager *Manager, peer *Peer, since *int64, backend Backend, opts Options,
) {
	opts = opts.withDefaults()
	ctx, cancel := context.WithCancel(ctx)

	defer cancel()

	conn.SetReadLimit(MaxFrameBytes)

	write := func(frame []byte) error {
		writeCtx, stop := context.WithTimeout(ctx, opts.WriteTimeout)
		defer stop()

		return conn.Write(writeCtx, websocket.MessageText, frame)
	}

	// `ready` is always the first frame; the catch-up follows it, then live frames.
	select {
	case first := <-peer.Frames():
		if err := write(first); err != nil {
			_ = conn.CloseNow()
			return
		}
	case <-peer.Done():
		_ = conn.Close(websocket.StatusGoingAway, peer.Reason())
		return
	}

	if since != nil {
		if err := catchUp(ctx, backend, *since, write); err != nil {
			_ = conn.Close(websocket.StatusInternalError, "sync failed")
			return
		}
	}

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		readLoop(ctx, conn, manager, peer, backend, opts)
	}()

	ping := time.NewTicker(opts.PingInterval)
	defer ping.Stop()

	recheck := time.NewTicker(opts.RecheckInterval)
	defer recheck.Stop()

	for {
		select {
		case <-finished:
			_ = conn.CloseNow()
			return

		case <-ctx.Done():
			_ = conn.CloseNow()
			return

		case <-peer.Done():
			status := websocket.StatusGoingAway

			if reason := peer.Reason(); reason == ReasonSlowClient || reason == ReasonAccessRevoked {
				status = websocket.StatusPolicyViolation
			}

			_ = conn.Close(status, peer.Reason())

			return

		case frame := <-peer.Frames():
			if err := write(frame); err != nil {
				_ = conn.CloseNow()
				return
			}

		case <-ping.C:
			pingCtx, stop := context.WithTimeout(ctx, opts.WriteTimeout)
			err := conn.Ping(pingCtx)

			stop()

			if err != nil {
				_ = conn.CloseNow()
				return
			}

		case <-recheck.C:
			allowed, canWrite, err := backend.Recheck(ctx)

			if err == nil && !allowed {
				_ = conn.Close(websocket.StatusPolicyViolation, ReasonAccessRevoked)
				return
			}

			if err == nil {
				peer.SetCanWrite(canWrite)
			}
		}
	}
}

func catchUp(ctx context.Context, backend Backend, since int64, write func([]byte) error) error {
	result, err := backend.Sync(ctx, since)

	if err != nil {
		return err
	}

	if result.Resync {
		return write(encode(resyncFrame{Type: TypeResync, Reason: "snapshot"}))
	}

	for _, change := range result.Updates {
		if err := write(encode(updateFrame{Type: TypeUpdate, Seq: change.Seq, UserID: change.UserID, Update: change.Update})); err != nil {
			return err
		}
	}

	return write(encode(syncedFrame{Type: TypeSynced, LastSeq: result.LastSeq}))
}

func readLoop(ctx context.Context, conn *websocket.Conn, manager *Manager, peer *Peer, backend Backend, opts Options) {
	limiter := newBucket(opts.UpdatesPerSecond)
	refuse := func(code, ref string) { peer.Enqueue(encode(errorFrame{Type: TypeError, Code: code, Ref: ref})) }

	for {
		kind, raw, err := conn.Read(ctx)

		if err != nil {
			return
		}

		if kind != websocket.MessageText {
			refuse(CodeInvalidMessage, "")
			continue
		}

		var message ClientMessage

		if err := json.Unmarshal(raw, &message); err != nil {
			refuse(CodeInvalidMessage, "")
			continue
		}

		switch message.Type {
		case TypeUpdate:
			if !peer.CanWrite() {
				refuse(CodeForbidden, message.Ref)
				continue
			}

			if !limiter.allow() {
				refuse(CodeRateLimited, message.Ref)
				continue
			}

			seq, err := backend.AppendUpdate(ctx, message.Update, message.Touched, peer.ID)

			if err != nil {
				var fieldErr *domain.FieldError

				switch {
				case errors.Is(err, domain.ErrDocumentNotFound):
					peer.kick(ReasonDocumentClosed)
					return
				case errors.Is(err, domain.ErrUpdateTooLarge):
					refuse(CodeUpdateTooLarge, message.Ref)
				case errors.As(err, &fieldErr):
					refuse(CodeInvalidUpdate, message.Ref)
				default:
					refuse(CodeInternal, message.Ref)
				}

				continue
			}

			peer.Enqueue(encode(ackFrame{Type: TypeAck, Ref: message.Ref, Seq: seq}))

		case TypeAwareness:
			if len(message.State) > MaxAwarenessBytes {
				refuse(CodeAwarenessTooBig, message.Ref)
				continue
			}

			manager.SetAwareness(peer, message.State)

		case TypeSync:
			if message.Since < 0 {
				refuse(CodeInvalidMessage, message.Ref)
				continue
			}

			if err := catchUpQueued(ctx, backend, message.Since, peer); err != nil {
				refuse(CodeInternal, message.Ref)
			}

		default:
			refuse(CodeInvalidMessage, message.Ref)
		}
	}
}

// catchUpQueued answers a `sync` request through the ordered frame queue, so the answer cannot overtake frames
// that were queued before it. A very large answer overflows the queue and disconnects the client, which then
// reconnects with `since` and gets the catch-up written directly.
func catchUpQueued(ctx context.Context, backend Backend, since int64, peer *Peer) error {
	return catchUp(ctx, backend, since, func(frame []byte) error {
		peer.Enqueue(frame)

		return nil
	})
}
