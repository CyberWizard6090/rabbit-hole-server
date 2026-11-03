package realtime

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

func quietHub(maxPerUser int) *Hub {
	return NewHub(maxPerUser, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func receive(t *testing.T, sub *Subscriber) Event {
	t.Helper()
	select {
	case raw := <-sub.Messages():
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("event is not JSON: %v: %s", err, raw)
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("no event received")
		return Event{}
	}
}

func expectSilence(t *testing.T, sub *Subscriber) {
	t.Helper()
	select {
	case raw := <-sub.Messages():
		t.Fatalf("unexpected event: %s", raw)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestPublishReachesOnlyTheMatchingTopics(t *testing.T) {
	hub := quietHub(5)
	onList, _ := hub.Subscribe(1, ListTopic(10))
	otherList, _ := hub.Subscribe(2, ListTopic(11))
	onSpace, _ := hub.Subscribe(3, SpaceTopic(7))
	otherSpace, _ := hub.Subscribe(4, SpaceTopic(8))

	task := &domain.Task{SpaceID: 7, ListID: 10, Title: "t"}
	task.ID = 99
	hub.Publish(Event{Type: EventTaskUpdated, SpaceID: 7, ListID: 10, TaskID: 99, Task: task, ActorID: 5})

	for name, sub := range map[string]*Subscriber{"list follower": onList, "space follower": onSpace} {
		ev := receive(t, sub)
		if ev.Type != EventTaskUpdated || ev.TaskID != 99 || ev.ActorID != 5 || ev.Task == nil || ev.Task.Title != "t" || ev.At.IsZero() {
			t.Errorf("%s got %+v", name, ev)
		}
	}
	expectSilence(t, otherList)
	expectSilence(t, otherSpace)
}

func TestPublishSkipsTheTaskPayloadOnDelete(t *testing.T) {
	hub := quietHub(5)
	sub, _ := hub.Subscribe(1, ListTopic(3))
	hub.Publish(Event{Type: EventTaskDeleted, SpaceID: 1, ListID: 3, TaskID: 8})

	ev := receive(t, sub)
	if ev.Task != nil || ev.TaskID != 8 {
		t.Fatalf("delete event = %+v, want task id without a payload", ev)
	}
}

func TestUnsubscribeStopsDeliveryAndFreesTheUserSlot(t *testing.T) {
	hub := quietHub(1)
	sub, err := hub.Subscribe(1, ListTopic(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Subscribe(1, ListTopic(2)); !errors.Is(err, ErrTooManyConnections) {
		t.Fatalf("second connection: err=%v, want ErrTooManyConnections", err)
	}

	hub.Unsubscribe(sub)
	hub.Unsubscribe(sub)
	if hub.Connections() != 0 {
		t.Fatalf("connections = %d after unsubscribe", hub.Connections())
	}
	hub.Publish(Event{Type: EventTaskCreated, ListID: 1})
	expectSilence(t, sub)

	if _, err := hub.Subscribe(1, ListTopic(2)); err != nil {
		t.Fatalf("slot was not released: %v", err)
	}
}

func TestSlowConsumerIsDisconnectedWithoutBlockingPublishers(t *testing.T) {
	hub := quietHub(5)
	slow, _ := hub.Subscribe(1, ListTopic(1))
	fast, _ := hub.Subscribe(2, ListTopic(1))

	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := 0; i < subscriberBuffer+20; i++ {
			hub.Publish(Event{Type: EventTaskUpdated, ListID: 1, TaskID: uint(i)})
			<-fast.Messages()
		}
	}()

	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a client that does not read")
	}

	select {
	case <-slow.Done():
	default:
		t.Fatal("slow consumer was not disconnected")
	}
	if slow.Reason() != "client too slow" {
		t.Fatalf("reason = %q", slow.Reason())
	}
	if hub.Connections() != 1 {
		t.Fatalf("connections = %d, want only the healthy client left", hub.Connections())
	}
}

func TestCloseDisconnectsEveryoneAndRefusesNewSubscribers(t *testing.T) {
	hub := quietHub(5)
	a, _ := hub.Subscribe(1, ListTopic(1))
	b, _ := hub.Subscribe(2, SpaceTopic(1))

	hub.Close()
	for _, sub := range []*Subscriber{a, b} {
		select {
		case <-sub.Done():
		default:
			t.Fatal("subscriber still connected after Close")
		}
		if sub.Reason() != "server shutting down" {
			t.Errorf("reason = %q", sub.Reason())
		}
	}
	if _, err := hub.Subscribe(3, ListTopic(1)); !errors.Is(err, ErrHubClosed) {
		t.Fatalf("subscribe after close: err=%v, want ErrHubClosed", err)
	}
	hub.Publish(Event{Type: EventTaskCreated, ListID: 1})
}

func TestConcurrentUseIsRaceFree(t *testing.T) {
	hub := quietHub(1000)
	var wg sync.WaitGroup
	for u := 1; u <= 8; u++ {
		wg.Add(1)
		go func(user uint) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				sub, err := hub.Subscribe(user, ListTopic(uint(i%3)))
				if err != nil {
					t.Error(err)
					return
				}
				hub.Publish(Event{Type: EventTaskUpdated, ListID: uint(i % 3), SpaceID: 1})
				select {
				case <-sub.Messages():
				default:
				}
				hub.Unsubscribe(sub)
			}
		}(uint(u))
	}
	wg.Wait()
	if hub.Connections() != 0 {
		t.Fatalf("leaked %d subscriptions", hub.Connections())
	}
}
