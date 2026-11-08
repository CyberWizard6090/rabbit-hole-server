package mail

import (
	"context"
	"log/slog"

	"rabbit-hole-server/internal/logging"
)

type Log struct {
	logger *slog.Logger
	body   bool
}

func NewLog(logger *slog.Logger) *Log {
	if logger == nil {
		logger = slog.Default()
	}
	return &Log{logger: logger}
}

func (l *Log) WithBody(enabled bool) *Log {
	l.body = enabled
	return l
}

func (l *Log) Send(_ context.Context, msg Message) error {
	if _, err := msg.validate(); err != nil {
		return err
	}
	attrs := []any{"to", logging.MaskEmail(msg.To), "subject", msg.Subject}
	if l.body {
		attrs = append(attrs, "text", msg.Text)
	}
	l.logger.Info("mail (log driver, nothing was sent)", attrs...)
	return nil
}
