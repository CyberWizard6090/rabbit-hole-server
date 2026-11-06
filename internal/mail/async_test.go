package mail

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type blockingSender struct {
	release chan struct{}
	mu      sync.Mutex
	sent    int
}

func (b *blockingSender) Send(context.Context, Message) error {
	<-b.release
	b.mu.Lock()
	b.sent++
	b.mu.Unlock()
	return nil
}

func valid(n string) Message {
	return Message{To: "ann@example.test", Subject: "Hi " + n, Text: "body"}
}

func TestAsyncDeliversAndDrainsOnClose(t *testing.T) {
	memory := NewMemory()
	a := NewAsync(memory, nil)
	for i := 0; i < 20; i++ {
		if err := a.Send(context.Background(), valid("x")); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()
	if got := len(memory.Messages()); got != 20 {
		t.Fatalf("delivered %d of 20 messages before Close returned", got)
	}
	if err := a.Send(context.Background(), valid("late")); !errors.Is(err, ErrClosed) {
		t.Fatalf("send after Close: %v", err)
	}
	a.Close()
}

func TestAsyncRejectsInvalidMessagesImmediately(t *testing.T) {
	memory := NewMemory()
	a := NewAsync(memory, nil)
	defer a.Close()
	if err := a.Send(context.Background(), Message{To: "bad", Subject: "x", Text: "y"}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("err = %v", err)
	}
}

func TestAsyncDoesNotBlockWhenTheQueueIsFull(t *testing.T) {
	blocker := &blockingSender{release: make(chan struct{})}
	a := NewAsync(blocker, nil)

	var full bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < asyncQueueSize+asyncWorkers+10; i++ {
			if err := a.Send(context.Background(), valid("q")); errors.Is(err, ErrQueueFull) {
				full = true
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked on a full queue")
	}
	if !full {
		t.Fatal("a full queue must report ErrQueueFull")
	}
	close(blocker.release)
	a.Close()
}

type failFirstSender struct {
	mu        sync.Mutex
	calls     int
	delivered []Message
}

func (f *failFirstSender) Send(_ context.Context, msg Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if msg.Subject == "Hi 1" {
		return errors.New("smtp down")
	}
	f.delivered = append(f.delivered, msg)
	return nil
}

func TestAsyncKeepsGoingAfterASendFailure(t *testing.T) {
	sender := &failFirstSender{}
	a := NewAsync(sender, nil)
	for _, n := range []string{"1", "2", "3"} {
		if err := a.Send(context.Background(), valid(n)); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()
	if sender.calls != 3 || len(sender.delivered) != 2 {
		t.Fatalf("calls=%d delivered=%+v", sender.calls, sender.delivered)
	}
}

type contextBlockingSender struct {
	started  chan struct{}
	canceled chan struct{}
}

func (s *contextBlockingSender) Send(ctx context.Context, _ Message) error {
	close(s.started)
	<-ctx.Done()
	close(s.canceled)
	return ctx.Err()
}

func TestAsyncShutdownHonorsContextAndCancelsInFlightSend(t *testing.T) {
	sender := &contextBlockingSender{started: make(chan struct{}), canceled: make(chan struct{})}
	a := NewAsync(sender, nil)
	if err := a.Send(context.Background(), valid("shutdown")); err != nil {
		t.Fatal(err)
	}
	<-sender.started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	select {
	case <-sender.canceled:
	case <-time.After(time.Second):
		t.Fatal("in-flight send was not canceled")
	}
	select {
	case <-a.done:
	case <-time.After(time.Second):
		t.Fatal("mail workers did not stop after cancellation")
	}
}

func TestMemoryAndLogValidateTheirInput(t *testing.T) {
	memory := NewMemory()
	if err := memory.Send(context.Background(), valid("a")); err != nil {
		t.Fatal(err)
	}
	if err := memory.Send(context.Background(), Message{To: "x", Subject: "s", Text: "t"}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("memory accepted a bad address: %v", err)
	}
	if got := memory.To("ann@example.test"); len(got) != 1 {
		t.Fatalf("To() = %+v", got)
	}
	memory.Reset()
	if len(memory.Messages()) != 0 {
		t.Fatal("Reset did not clear the messages")
	}
	if err := NewLog(nil).Send(context.Background(), valid("log")); err != nil {
		t.Fatal(err)
	}
	if err := NewLog(nil).Send(context.Background(), Message{To: "bad", Subject: "s", Text: "t"}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("log accepted a bad address: %v", err)
	}
}
