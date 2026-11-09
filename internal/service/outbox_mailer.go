package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"rabbit-hole-server/internal/domain"
	mailer "rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/txhook"
)

const (
	mailOutboxAttempts = 6
	mailSealPurpose    = "outbox-mail"
)

var mailOutboxBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, 3 * time.Hour}

type OutboxMailer struct {
	repo  domain.OutboxRepository
	box   *SecretBox
	inner mailer.Sender
	poke  func()
}

func NewOutboxMailer(repo domain.OutboxRepository, box *SecretBox, inner mailer.Sender) *OutboxMailer {
	return &OutboxMailer{repo: repo, box: box, inner: inner}
}

func (m *OutboxMailer) AttachWorker(worker *OutboxWorker) {
	m.poke = worker.Poke
	worker.Register(OutboxRoute{
		Type: domain.OutboxDeliveryMail, Handler: OutboxHandlerFunc(m.handle),
		MaxAttempts: mailOutboxAttempts, Backoff: mailOutboxBackoff,
	})
}

func (m *OutboxMailer) Send(ctx context.Context, msg mailer.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode mail: %w", err)
	}
	sealed, err := m.box.SealPurpose(mailSealPurpose, string(raw))
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"sealed": sealed})
	if err != nil {
		return err
	}
	if err := m.repo.Add(ctx, []domain.OutboxEvent{{
		Type: domain.OutboxDeliveryMail, AggregateType: "mail", Payload: payload,
	}}); err != nil {
		return err
	}
	if m.poke != nil {
		txhook.AfterCommit(ctx, m.poke)
	}
	return nil
}

func (m *OutboxMailer) handle(ctx context.Context, event domain.OutboxEvent, _ bool) error {
	var payload struct {
		Sealed string `json:"sealed"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return PermanentOutboxError(fmt.Errorf("decode mail delivery: %w", err))
	}
	plain, err := m.box.OpenPurpose(mailSealPurpose, payload.Sealed)
	if err != nil {
		return PermanentOutboxError(err)
	}
	var msg mailer.Message
	if err := json.Unmarshal([]byte(plain), &msg); err != nil {
		return PermanentOutboxError(fmt.Errorf("decode mail message: %w", err))
	}
	if err := m.inner.Send(ctx, msg); err != nil {
		if errors.Is(err, mailer.ErrInvalidMessage) {
			return PermanentOutboxError(err)
		}
		return err
	}
	return nil
}
