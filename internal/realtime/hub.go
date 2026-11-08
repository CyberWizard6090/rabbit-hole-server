package realtime

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

var (
	ErrTooManyConnections = errors.New("too many realtime connections for this user")
	ErrHubClosed          = errors.New("realtime hub is shutting down")
)

const ReasonAccessRevoked = "access revoked"

const (
	DefaultMaxConnectionsPerUser = 10
	subscriberBuffer             = 64
)

type Subscriber struct {
	UserID uint
	Topic  string

	send chan []byte
	done chan struct{}

	once   sync.Once
	mu     sync.Mutex
	reason string
}

func (s *Subscriber) Messages() <-chan []byte { return s.send }

func (s *Subscriber) Done() <-chan struct{} { return s.done }

func (s *Subscriber) Reason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

func (s *Subscriber) kick(reason string) {
	s.once.Do(func() {
		s.mu.Lock()
		s.reason = reason
		s.mu.Unlock()
		close(s.done)
	})
}

type Hub struct {
	mu         sync.RWMutex
	topics     map[string]map[*Subscriber]struct{}
	perUser    map[uint]map[*Subscriber]struct{}
	maxPerUser int
	closed     bool
	log        *slog.Logger
	now        func() time.Time
}

func NewHub(maxPerUser int, log *slog.Logger) *Hub {
	if maxPerUser <= 0 {
		maxPerUser = DefaultMaxConnectionsPerUser
	}
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		topics:     make(map[string]map[*Subscriber]struct{}),
		perUser:    make(map[uint]map[*Subscriber]struct{}),
		maxPerUser: maxPerUser,
		log:        log,
		now:        time.Now,
	}
}

func (h *Hub) Subscribe(userID uint, topic string) (*Subscriber, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, ErrHubClosed
	}
	if len(h.perUser[userID]) >= h.maxPerUser {
		return nil, ErrTooManyConnections
	}

	sub := &Subscriber{
		UserID: userID,
		Topic:  topic,
		send:   make(chan []byte, subscriberBuffer),
		done:   make(chan struct{}),
	}
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[*Subscriber]struct{})
	}
	h.topics[topic][sub] = struct{}{}
	if h.perUser[userID] == nil {
		h.perUser[userID] = make(map[*Subscriber]struct{})
	}
	h.perUser[userID][sub] = struct{}{}
	return sub, nil
}

func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	h.removeLocked(sub)
	h.mu.Unlock()
	sub.kick("")
}

func (h *Hub) removeLocked(sub *Subscriber) {
	subs, ok := h.topics[sub.Topic]
	if !ok {
		return
	}
	if _, present := subs[sub]; !present {
		return
	}
	delete(subs, sub)
	if len(subs) == 0 {
		delete(h.topics, sub.Topic)
	}
	delete(h.perUser[sub.UserID], sub)
	if len(h.perUser[sub.UserID]) == 0 {
		delete(h.perUser, sub.UserID)
	}
}

func (h *Hub) Publish(event Event) {
	if event.At.IsZero() {
		event.At = h.now()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		h.log.Error("realtime: cannot encode event", "type", event.Type, "error", err)
		return
	}

	h.fanOut(payload, ListTopic(event.ListID), SpaceTopic(event.SpaceID))
}

func (h *Hub) PublishUser(userID uint, event UserEvent) {
	if event.At.IsZero() {
		event.At = h.now()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		h.log.Error("realtime: cannot encode event", "type", event.Type, "error", err)
		return
	}
	h.fanOut(payload, UserTopic(userID))
}

func (h *Hub) fanOut(payload []byte, topics ...string) {
	var slow []*Subscriber
	h.mu.RLock()
	for _, topic := range topics {
		for sub := range h.topics[topic] {
			select {
			case sub.send <- payload:
			default:
				slow = append(slow, sub)
			}
		}
	}
	h.mu.RUnlock()

	for _, sub := range slow {
		h.log.Warn("realtime: disconnecting slow consumer", "user_id", sub.UserID, "topic", sub.Topic)
		h.mu.Lock()
		h.removeLocked(sub)
		h.mu.Unlock()
		sub.kick("client too slow")
	}
}

func (h *Hub) disconnect(userID uint, match func(*Subscriber) bool) int {
	h.mu.Lock()
	var victims []*Subscriber
	for sub := range h.perUser[userID] {
		if match(sub) {
			victims = append(victims, sub)
		}
	}
	for _, sub := range victims {
		h.removeLocked(sub)
	}
	h.mu.Unlock()

	for _, sub := range victims {
		sub.kick(ReasonAccessRevoked)
	}
	if len(victims) > 0 {
		h.log.Info("realtime: disconnected revoked connections", "user_id", userID, "count", len(victims))
	}
	return len(victims)
}

func (h *Hub) DisconnectUser(userID uint) int {
	return h.disconnect(userID, func(*Subscriber) bool { return true })
}

func (h *Hub) DisconnectUserTopic(userID uint, topic string) int {
	return h.disconnect(userID, func(sub *Subscriber) bool { return sub.Topic == topic })
}

func (h *Hub) DisconnectUserFeeds(userID uint) int {
	userTopic := UserTopic(userID)
	return h.disconnect(userID, func(sub *Subscriber) bool { return sub.Topic != userTopic })
}

func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	var all []*Subscriber
	for _, subs := range h.topics {
		for sub := range subs {
			all = append(all, sub)
		}
	}
	h.topics = make(map[string]map[*Subscriber]struct{})
	h.perUser = make(map[uint]map[*Subscriber]struct{})
	h.mu.Unlock()

	for _, sub := range all {
		sub.kick("server shutting down")
	}
}

func (h *Hub) Connections() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, subs := range h.topics {
		n += len(subs)
	}
	return n
}
