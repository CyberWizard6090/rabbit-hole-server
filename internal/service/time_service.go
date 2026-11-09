package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

type TimeService struct {
	repo domain.TimeEntryRepository
	now  func() time.Time
}

func NewTimeService(repo domain.TimeEntryRepository) *TimeService {
	return &TimeService{repo: repo, now: time.Now}
}

func timeNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > domain.MaxTimeNoteRunes {
		return "", domain.InvalidField(domain.ErrInvalidTimeEntry, "note", "TOO_LONG").WithParams(map[string]any{"max": domain.MaxTimeNoteRunes})
	}
	return note, nil
}

func (s *TimeService) Start(ctx context.Context, taskID, userID uint, note string) (*domain.TimeEntry, error) {
	note, err := timeNote(note)
	if err != nil {
		return nil, err
	}
	return s.repo.Start(ctx, taskID, userID, note)
}

func (s *TimeService) Stop(ctx context.Context, taskID, userID uint) (*domain.TimeEntry, error) {
	return s.repo.Stop(ctx, taskID, userID)
}

func (s *TimeService) Add(ctx context.Context, taskID, userID uint, seconds int, startedAt *time.Time, note string) (*domain.TimeEntry, error) {
	note, err := timeNote(note)
	if err != nil {
		return nil, err
	}
	if seconds < 1 || seconds > domain.MaxTimeEntrySeconds {
		return nil, domain.InvalidField(domain.ErrInvalidTimeEntry, "seconds", "OUT_OF_RANGE").
			WithParams(map[string]any{"min": 1, "max": domain.MaxTimeEntrySeconds})
	}
	now := s.now().UTC()
	start := now.Add(-time.Duration(seconds) * time.Second)
	if startedAt != nil {
		start = startedAt.UTC()
	}
	end := start.Add(time.Duration(seconds) * time.Second)
	if end.After(now.Add(time.Minute)) {
		return nil, domain.InvalidField(domain.ErrInvalidTimeEntry, "started_at", "IN_THE_FUTURE")
	}
	return s.repo.Add(ctx, domain.TimeEntryDraft{TaskID: taskID, UserID: userID, StartedAt: start, EndedAt: end, Seconds: seconds, Note: note})
}

func (s *TimeService) List(ctx context.Context, taskID uint) ([]domain.TimeEntry, int, error) {
	entries, err := s.repo.List(ctx, taskID)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	for _, e := range entries {
		if !e.Running {
			total += e.Seconds
		}
	}
	return entries, total, nil
}

func (s *TimeService) Get(ctx context.Context, taskID, entryID uint) (*domain.TimeEntry, error) {
	return s.repo.Get(ctx, taskID, entryID)
}

func (s *TimeService) Update(ctx context.Context, taskID, entryID uint, seconds *int, note *string) (*domain.TimeEntry, error) {
	if note != nil {
		trimmed, err := timeNote(*note)
		if err != nil {
			return nil, err
		}
		note = &trimmed
	}
	return s.repo.Update(ctx, taskID, entryID, seconds, note)
}

func (s *TimeService) Delete(ctx context.Context, taskID, entryID uint) error {
	return s.repo.Delete(ctx, taskID, entryID)
}

func (s *TimeService) Running(ctx context.Context, userID uint) (*domain.RunningTimer, error) {
	return s.repo.Running(ctx, userID)
}

const maxReportDays = 366

func (s *TimeService) Report(ctx context.Context, workspaceID uint, from, to *time.Time, userID *uint) ([]domain.TimeReportRow, time.Time, time.Time, error) {
	end := s.now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	if to != nil {
		end = to.UTC().Add(24 * time.Hour)
	}
	start := end.Add(-30 * 24 * time.Hour)
	if from != nil {
		start = from.UTC()
	}
	if !end.After(start) {
		return nil, start, end, domain.InvalidField(domain.ErrInvalidTimeEntry, "to", "BEFORE_FROM")
	}
	if end.Sub(start) > maxReportDays*24*time.Hour {
		return nil, start, end, domain.InvalidField(domain.ErrInvalidTimeEntry, "to", "RANGE_TOO_LONG").WithParams(map[string]any{"max_days": maxReportDays})
	}
	rows, err := s.repo.Report(ctx, workspaceID, start, end, userID)
	return rows, start, end, err
}
