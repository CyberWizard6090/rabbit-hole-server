package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type scenario struct {
	name   string
	weight int
	run    func(e *environment, r *rand.Rand) (int, time.Duration, error)
}

type sample struct {
	d  time.Duration
	ok bool
}

type result struct {
	mu      sync.Mutex
	samples map[string][]sample
	codes   map[string]map[int]int
}

type report struct {
	result  *result
	elapsed time.Duration
	order   []string
}

func (e *environment) pick(r *rand.Rand) (token string, userIdx int) {
	i := r.Intn(len(e.tokens))
	return e.tokens[i], i
}

func (e *environment) randomList(r *rand.Rand) uint { return e.listIDs[r.Intn(len(e.listIDs))] }
func (e *environment) randomTask(r *rand.Rand) uint { return e.taskIDs[r.Intn(len(e.taskIDs))] }

var queries = []string{"deploy", "invoice", "refactor", "customer", "report", "release", "login"}

func scenarios() []scenario {
	do := func(method string, path func(e *environment, r *rand.Rand) string, body func(e *environment, r *rand.Rand) any) func(*environment, *rand.Rand) (int, time.Duration, error) {
		return func(e *environment, r *rand.Rand) (int, time.Duration, error) {
			token, _ := e.pick(r)
			var payload any
			if body != nil {
				payload = body(e, r)
			}
			status, _, d, err := e.call(method, path(e, r), token, payload, nil)
			return status, d, err
		}
	}
	return []scenario{
		{"list tasks (newest first)", 24, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/lists/%d/tasks?limit=50", e.randomList(r))
		}, nil)},
		{"list tasks (sort+filter)", 12, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/lists/%d/tasks?limit=50&sort=-priority,due_date&priority=%d&has_due=true", e.randomList(r), 1+r.Intn(5))
		}, nil)},
		{"list tasks (text filter)", 4, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/lists/%d/tasks?limit=50&q=%s", e.randomList(r), url.QueryEscape(e.words[r.Intn(len(e.words))]))
		}, nil)},
		{"get task", 14, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/tasks/%d", e.randomTask(r))
		}, nil)},
		{"create task", 8, func(e *environment, r *rand.Rand) (int, time.Duration, error) {
			token, _ := e.pick(r)
			list := e.randomList(r)
			st := e.statuses[list]
			status, _, d, err := e.call(http.MethodPost, fmt.Sprintf("/lists/%d/tasks", list), token,
				map[string]any{"title": fmt.Sprintf("Load task %d", r.Int63()), "status_id": st[r.Intn(len(st))], "priority": 1 + r.Intn(5)}, nil)
			return status, d, err
		}},
		{"update task", 8, do(http.MethodPatch, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/tasks/%d", e.randomTask(r))
		}, func(e *environment, r *rand.Rand) any { return map[string]any{"priority": 1 + r.Intn(5)} })},
		{"add comment", 8, do(http.MethodPost, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/tasks/%d/comments", e.randomTask(r))
		}, func(e *environment, r *rand.Rand) any { return map[string]any{"body": "load comment"} })},
		{"search (specific word)", 6, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/workspaces/%d/search?q=%s&limit=20", e.workspaceID, url.QueryEscape(e.words[r.Intn(len(e.words))]))
		}, nil)},
		{"search (common word)", 1, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/workspaces/%d/search?q=%s&limit=20", e.workspaceID, url.QueryEscape(queries[r.Intn(len(queries))]))
		}, nil)},
		{"task activity", 3, do(http.MethodGet, func(e *environment, r *rand.Rand) string {
			return fmt.Sprintf("/tasks/%d/activity", e.randomTask(r))
		}, nil)},
		{"notifications inbox", 4, do(http.MethodGet, func(e *environment, r *rand.Rand) string { return "/notifications?limit=30" }, nil)},
		{"unread count", 4, do(http.MethodGet, func(e *environment, r *rand.Rand) string { return "/notifications/unread-count" }, nil)},
		{"workspace list", 2, do(http.MethodGet, func(e *environment, r *rand.Rand) string { return "/workspaces" }, nil)},
		{"public form submit", 3, func(e *environment, r *rand.Rand) (int, time.Duration, error) {
			status, _, d, err := e.call(http.MethodPost, "/public/forms/"+e.formSlug+"/submissions", "",
				map[string]any{"answers": map[string]any{fmt.Sprint(e.questionID): "load submission"}},
				map[string]string{"Idempotency-Key": fmt.Sprintf("load-%d", r.Int63())})
			return status, d, err
		}},
		{"login", 1, func(e *environment, r *rand.Rand) (int, time.Duration, error) {
			status, _, d, err := e.call(http.MethodPost, "/auth/login", "",
				map[string]any{"email": fmt.Sprintf("load-%d@example.test", r.Intn(len(e.tokens))), "password": password}, nil)
			return status, d, err
		}},
	}
}

func (e *environment) workload(ctx context.Context, o options) *report {
	list := scenarios()
	if o.only != "" {
		var kept []scenario
		for _, s := range list {
			if strings.Contains(s.name, o.only) {
				kept = append(kept, s)
			}
		}
		list = kept
	}
	if o.exclude != "" {
		var kept []scenario
		for _, s := range list {
			if !strings.Contains(s.name, o.exclude) {
				kept = append(kept, s)
			}
		}
		list = kept
	}
	total := 0
	for _, s := range list {
		total += s.weight
	}
	res := &result{samples: map[string][]sample{}, codes: map[string]map[int]int{}}
	deadline := time.Now().Add(o.duration)
	started := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < o.concurrency; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for time.Now().Before(deadline) && ctx.Err() == nil {
				n := r.Intn(total)
				var chosen scenario
				for _, s := range list {
					if n < s.weight {
						chosen = s
						break
					}
					n -= s.weight
				}
				status, d, err := chosen.run(e, r)
				ok := err == nil && status >= 200 && status < 300
				res.mu.Lock()
				res.samples[chosen.name] = append(res.samples[chosen.name], sample{d: d, ok: ok})
				if res.codes[chosen.name] == nil {
					res.codes[chosen.name] = map[int]int{}
				}
				res.codes[chosen.name][status]++
				res.mu.Unlock()
			}
		}(time.Now().UnixNano() + int64(w))
	}
	wg.Wait()
	order := make([]string, 0, len(list))
	for _, s := range list {
		order = append(order, s.name)
	}
	return &report{result: res, elapsed: time.Since(started), order: order}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

func (r *report) print(o options) {
	fmt.Printf("\nworkload: %d virtual users for %s\n\n", o.concurrency, r.elapsed.Round(time.Second))
	fmt.Printf("| scenario | requests | rps | errors | p50 | p95 | p99 | max |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
	totalReq, totalErr := 0, 0
	for _, name := range r.order {
		samples := r.result.samples[name]
		if len(samples) == 0 {
			continue
		}
		durations := make([]time.Duration, len(samples))
		errs := 0
		for i, s := range samples {
			durations[i] = s.d
			if !s.ok {
				errs++
			}
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		totalReq += len(samples)
		totalErr += errs
		fmt.Printf("| %s | %d | %.1f | %d | %s | %s | %s | %s |\n", name, len(samples), float64(len(samples))/r.elapsed.Seconds(), errs,
			ms(percentile(durations, 0.5)), ms(percentile(durations, 0.95)), ms(percentile(durations, 0.99)), ms(durations[len(durations)-1]))
	}
	fmt.Printf("\ntotal: %d requests, %.1f rps, %d errors\n", totalReq, float64(totalReq)/r.elapsed.Seconds(), totalErr)
	for _, name := range r.order {
		codes := r.result.codes[name]
		var bad []string
		for code, n := range codes {
			if code < 200 || code >= 300 {
				bad = append(bad, fmt.Sprintf("%d x%d", code, n))
			}
		}
		if len(bad) > 0 {
			fmt.Printf("non-2xx in %q: %s\n", name, strings.Join(bad, ", "))
		}
	}
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000) }

func (e *environment) outboxLag() {
	var pending int64
	e.db.Raw("SELECT count(*) FROM outbox_events WHERE published_at IS NULL AND dead_at IS NULL").Scan(&pending)
	fmt.Printf("\noutbox right after the run: %d events pending\n", pending)
	started := time.Now()
	deadline := started.Add(5 * time.Minute)
	for pending > 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		e.db.Raw("SELECT count(*) FROM outbox_events WHERE published_at IS NULL AND dead_at IS NULL").Scan(&pending)
	}
	var dead int64
	e.db.Raw("SELECT count(*) FROM outbox_events WHERE dead_at IS NOT NULL").Scan(&dead)
	fmt.Printf("outbox drained in %s, dead events: %d\n", time.Since(started).Round(time.Millisecond), dead)
}

func printDatabaseStats(db *gorm.DB) {
	type tableRow struct {
		Relname    string
		SeqScan    int64
		SeqTupRead int64
		IdxScan    int64
		Live       int64
		Size       string
	}
	var rows []tableRow
	db.Raw(`SELECT s.relname, s.seq_scan, s.seq_tup_read, coalesce(s.idx_scan, 0) AS idx_scan, c.reltuples::bigint AS live,
		pg_size_pretty(pg_total_relation_size(s.relid)) AS size
		FROM pg_stat_user_tables s JOIN pg_class c ON c.oid = s.relid ORDER BY s.seq_tup_read DESC LIMIT 12`).Scan(&rows)
	fmt.Printf("\ntables by rows read through sequential scans (since the workload started):\n\n")
	fmt.Printf("| table | live rows | size | seq scans | rows read by seq scans | index scans |\n|---|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		fmt.Printf("| %s | %d | %s | %d | %d | %d |\n", r.Relname, r.Live, r.Size, r.SeqScan, r.SeqTupRead, r.IdxScan)
	}
}
