package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

var (
	ErrInvalidMessage = errors.New("invalid mail message")
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}

func hasLineBreak(s string) bool {
	return strings.ContainsAny(s, "\r\n\x00")
}

func parseAddress(raw string) (*mail.Address, error) {
	if hasLineBreak(raw) {
		return nil, fmt.Errorf("%w: address contains a line break", ErrInvalidMessage)
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidMessage, err)
	}
	return addr, nil
}

func (m Message) Validate() error {
	_, err := m.validate()
	return err
}

func (m Message) validate() (*mail.Address, error) {
	to, err := parseAddress(m.To)
	if err != nil {
		return nil, err
	}
	if hasLineBreak(m.Subject) {
		return nil, fmt.Errorf("%w: subject contains a line break", ErrInvalidMessage)
	}
	if strings.TrimSpace(m.Subject) == "" || strings.TrimSpace(m.Text) == "" {
		return nil, fmt.Errorf("%w: subject and text are required", ErrInvalidMessage)
	}
	return to, nil
}
