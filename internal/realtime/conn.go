package realtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
)

type Options struct {
	PingInterval    time.Duration
	RecheckInterval time.Duration
	WriteTimeout    time.Duration
}

func (o Options) withDefaults() Options {
	if o.PingInterval <= 0 {
		o.PingInterval = 25 * time.Second
	}
	if o.RecheckInterval <= 0 {
		o.RecheckInterval = DefaultRecheckInterval
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}
	return o
}

const (
	maxClientFrame         = 1024
	DefaultRecheckInterval = 2 * time.Minute
)

func Serve(ctx context.Context, conn *websocket.Conn, sub *Subscriber, recheck func(context.Context) bool, opts Options) {
	opts = opts.withDefaults()
	conn.SetReadLimit(maxClientFrame)

	ctx = conn.CloseRead(ctx)

	write := func(payload []byte) error {
		writeCtx, cancel := context.WithTimeout(ctx, opts.WriteTimeout)
		defer cancel()
		return conn.Write(writeCtx, websocket.MessageText, payload)
	}

	ready, _ := json.Marshal(Event{Type: EventReady, Topic: sub.Topic, At: time.Now()})
	if err := write(ready); err != nil {
		_ = conn.CloseNow()
		return
	}

	ping := time.NewTicker(opts.PingInterval)
	defer ping.Stop()
	recheckTick := time.NewTicker(opts.RecheckInterval)
	defer recheckTick.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.CloseNow()
			return

		case <-sub.Done():
			status, reason := websocket.StatusGoingAway, sub.Reason()
			switch reason {
			case "":
				reason = "closed"
			case "client too slow", ReasonAccessRevoked:
				status = websocket.StatusPolicyViolation
			}
			_ = conn.Close(status, reason)
			return

		case payload := <-sub.Messages():
			if err := write(payload); err != nil {
				_ = conn.CloseNow()
				return
			}

		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, opts.WriteTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = conn.CloseNow()
				return
			}

		case <-recheckTick.C:
			if recheck != nil && !recheck(ctx) {
				_ = conn.Close(websocket.StatusPolicyViolation, ReasonAccessRevoked)
				return
			}
		}
	}
}
