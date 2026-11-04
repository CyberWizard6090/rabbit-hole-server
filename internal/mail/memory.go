package mail

import (
	"context"
	"sync"
)

type Memory struct {
	mu       sync.Mutex
	messages []Message
	err      error
}

func NewMemory() *Memory { return &Memory{} }

func (m *Memory) Send(_ context.Context, msg Message) error {
	if _, err := msg.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *Memory) Messages() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.messages...)
}

func (m *Memory) To(address string) []Message {
	var out []Message
	for _, msg := range m.Messages() {
		if msg.To == address {
			out = append(out, msg)
		}
	}
	return out
}

func (m *Memory) FailWith(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *Memory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
}
