package realtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidTicket  = errors.New("invalid or expired realtime ticket")
	ErrTooManyTickets = errors.New("too many outstanding realtime tickets")
)

const (
	DefaultTicketTTL        = 30 * time.Second
	maxTicketsPerUser       = 20
	maxTicketsTotal         = 20000
	ticketEntropyBytes      = 32
	ticketSweepEveryNIssues = 64
)

type ticket struct {
	userID  uint
	topic   string
	version int
	expires time.Time
}

type Grant struct {
	UserID  uint
	Version int
}

type TicketStore struct {
	mu      sync.Mutex
	tickets map[string]ticket
	byUser  map[uint]int
	ttl     time.Duration
	now     func() time.Time
	issued  int
}

func NewTicketStore(ttl time.Duration) *TicketStore {
	if ttl <= 0 {
		ttl = DefaultTicketTTL
	}
	return &TicketStore{
		tickets: make(map[string]ticket),
		byUser:  make(map[uint]int),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (s *TicketStore) TTL() time.Duration { return s.ttl }

func hashTicket(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *TicketStore) Issue(userID uint, topic string) (string, error) {
	return s.IssueVersioned(userID, topic, 0)
}

func (s *TicketStore) IssueVersioned(userID uint, topic string, version int) (string, error) {
	buf := make([]byte, ticketEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.issued++
	if s.issued%ticketSweepEveryNIssues == 0 || len(s.tickets) >= maxTicketsTotal {
		s.sweepLocked(now)
	}
	if s.byUser[userID] >= maxTicketsPerUser || len(s.tickets) >= maxTicketsTotal {
		s.sweepLocked(now)
		if s.byUser[userID] >= maxTicketsPerUser || len(s.tickets) >= maxTicketsTotal {
			return "", ErrTooManyTickets
		}
	}

	s.tickets[hashTicket(raw)] = ticket{userID: userID, topic: topic, version: version, expires: now.Add(s.ttl)}
	s.byUser[userID]++
	return raw, nil
}

func (s *TicketStore) Redeem(raw, topic string) (uint, error) {
	grant, err := s.RedeemGrant(raw, topic)
	return grant.UserID, err
}

func (s *TicketStore) RedeemUser(raw string) (uint, error) {
	grant, err := s.RedeemUserGrant(raw)
	return grant.UserID, err
}

func (s *TicketStore) RedeemGrant(raw, topic string) (Grant, error) {
	return s.redeem(raw, func(t ticket) bool { return t.topic == topic })
}

func (s *TicketStore) RedeemUserGrant(raw string) (Grant, error) {
	return s.redeem(raw, func(t ticket) bool { return t.topic == UserTopic(t.userID) })
}

func (s *TicketStore) redeem(raw string, topicOK func(ticket) bool) (Grant, error) {
	if raw == "" {
		return Grant{}, ErrInvalidTicket
	}
	key := hashTicket(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tickets[key]
	if !ok {
		return Grant{}, ErrInvalidTicket
	}
	delete(s.tickets, key)
	s.releaseLocked(t.userID)

	if !s.now().Before(t.expires) || !topicOK(t) {
		return Grant{}, ErrInvalidTicket
	}
	return Grant{UserID: t.userID, Version: t.version}, nil
}

func (s *TicketStore) releaseLocked(userID uint) {
	if s.byUser[userID]--; s.byUser[userID] <= 0 {
		delete(s.byUser, userID)
	}
}

func (s *TicketStore) sweepLocked(now time.Time) {
	for key, t := range s.tickets {
		if !now.Before(t.expires) {
			delete(s.tickets, key)
			s.releaseLocked(t.userID)
		}
	}
}

func (s *TicketStore) Outstanding() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tickets)
}
