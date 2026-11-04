package mail

import (
	"context"
	"log/slog"
)

type Log struct {
	logger *slog.Logger
}

func NewLog(logger *slog.Logger) *Log {
	if logger == nil {
		logger = slog.Default()
	}
	return &Log{logger: logger}
}

func (l *Log) Send(_ context.Context, msg Message) error {
	if _, err := msg.validate(); err != nil {
		return err
	}
	l.logger.Info("mail (log driver, nothing was sent)", "to", msg.To, "subject", msg.Subject, "text", msg.Text)
	return nil
}
