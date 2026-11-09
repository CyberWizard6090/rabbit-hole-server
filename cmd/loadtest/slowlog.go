package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

type slowLog struct {
	mu      sync.Mutex
	queries map[string]*slowAggregate
}

type slowAggregate struct {
	count int
	total time.Duration
	max   time.Duration
}

func newSlowLog() *slowLog { return &slowLog{queries: map[string]*slowAggregate{}} }

func (s *slowLog) Enabled(context.Context, slog.Level) bool { return true }

func (s *slowLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "SQL executed" || r.Level < slog.LevelWarn {
		return nil
	}
	var sqlText string
	var duration time.Duration
	var visit func(prefix string, a slog.Attr)
	visit = func(prefix string, a slog.Attr) {
		if a.Value.Kind() == slog.KindGroup {
			for _, inner := range a.Value.Group() {
				visit(prefix+a.Key+".", inner)
			}
			return
		}
		switch prefix + a.Key {
		case "trace.sql":
			sqlText = a.Value.String()
		case "trace.duration":
			duration = durationOf(a.Value)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		visit("", a)
		return true
	})
	key := shorten(sqlText)
	s.mu.Lock()
	defer s.mu.Unlock()
	agg := s.queries[key]
	if agg == nil {
		agg = &slowAggregate{}
		s.queries[key] = agg
	}
	agg.count++
	agg.total += duration
	if duration > agg.max {
		agg.max = duration
	}
	return nil
}

func (s *slowLog) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *slowLog) WithGroup(string) slog.Handler      { return s }

func shorten(sqlText string) string {
	sqlText = strings.Join(strings.Fields(sqlText), " ")
	if len(sqlText) > 160 {
		return sqlText[:160] + "..."
	}
	return sqlText
}

func (s *slowLog) print(threshold time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type entry struct {
		sql string
		agg *slowAggregate
	}
	entries := make([]entry, 0, len(s.queries))
	for k, v := range s.queries {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].agg.total > entries[j].agg.total })
	fmt.Printf("\nqueries slower than %s (grouped by their first 160 characters, worst total time first):\n\n", threshold)
	if len(entries) == 0 {
		fmt.Println("none")
		return
	}
	fmt.Printf("| count | total | max | query |\n|---:|---:|---:|---|\n")
	for i, e := range entries {
		if i == 8 {
			break
		}
		fmt.Printf("| %d | %s | %s | `%s` |\n", e.agg.count, ms(e.agg.total), ms(e.agg.max), strings.ReplaceAll(e.sql, "|", "\\|"))
	}
}

func durationOf(v slog.Value) time.Duration {
	switch v.Kind() {
	case slog.KindDuration:
		return v.Duration()
	case slog.KindFloat64:
		return time.Duration(v.Float64() * float64(time.Millisecond))
	default:
		if d, err := time.ParseDuration(strings.TrimSpace(v.String())); err == nil {
			return d
		}
		return 0
	}
}
