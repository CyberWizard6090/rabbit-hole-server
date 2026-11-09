package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/mail"
)

type discardMail struct{}

func (discardMail) Send(context.Context, mail.Message) error { return nil }

type options struct {
	dbURL       string
	reset       bool
	tasks       int
	users       int
	duration    time.Duration
	concurrency int
	skipSeed    bool
	only        string
	exclude     string
}

func main() {
	var o options
	flag.StringVar(&o.dbURL, "db-url", "", "PostgreSQL DSN of a DISPOSABLE database whose name contains \"load\"")
	flag.BoolVar(&o.reset, "reset", false, "drop the public schema, migrate and seed (required unless -skip-seed)")
	flag.IntVar(&o.tasks, "tasks", 200000, "tasks to seed")
	flag.IntVar(&o.users, "users", 40, "users to register")
	flag.DurationVar(&o.duration, "duration", 60*time.Second, "how long to run the mixed workload")
	flag.IntVar(&o.concurrency, "concurrency", 32, "concurrent virtual users")
	flag.BoolVar(&o.skipSeed, "skip-seed", false, "reuse the data of a previous run")
	flag.StringVar(&o.exclude, "exclude", "", "skip the scenarios whose name contains this text")
	flag.StringVar(&o.only, "only", "", "run only the scenarios whose name contains this text")
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "loadtest: %v\n", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.dbURL == "" {
		return fmt.Errorf("-db-url is required")
	}
	if !strings.Contains(strings.ToLower(dbName(o.dbURL)), "load") {
		return fmt.Errorf("the database name must contain \"load\": this tool wipes and fills it")
	}
	if !o.reset && !o.skipSeed {
		return fmt.Errorf("pass -reset to wipe and seed the database, or -skip-seed to reuse it")
	}
	uploads, err := os.MkdirTemp("", "rabbit-hole-loadtest-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(uploads) }()

	for k, v := range map[string]string{
		"APP_ENV": "test", "DB_URL": o.dbURL, "JWT_SECRET": strings.Repeat("l", 40), "TRUSTED_PROXIES": "127.0.0.1",
		"STORAGE_DRIVER": "local", "STORAGE_LOCAL_DIR": filepath.Join(uploads, "files"), "LOG_LEVEL": "warn",
		"DB_SLOW_QUERY_THRESHOLD": "100ms", "AUTH_LOGIN_FAILURE_LIMIT": "0", "DB_MAX_OPEN_CONNS": "40",
	} {
		if os.Getenv(k) == "" || k == "DB_URL" {
			_ = os.Setenv(k, v)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slow := newSlowLog()
	slog.SetDefault(slog.New(slow))
	gin.SetMode(gin.ReleaseMode)

	if o.reset {
		if err := resetSchema(o.dbURL); err != nil {
			return err
		}
	}
	db, err := database.ConnectDB(o.dbURL, cfg.DB.Options(cfg.Environment))
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	container, err := app.NewContainerWithMailer(db, cfg, discardMail{})
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var workers sync.WaitGroup
	for _, w := range container.Workers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			w.Run(ctx)
		}()
	}
	server := httptest.NewServer(app.SetupRouter(container, cfg))
	defer server.Close()

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: o.concurrency * 2, MaxConnsPerHost: o.concurrency * 2},
	}
	env := &environment{db: db, base: server.URL + "/api/v1", client: client}

	if o.reset {
		started := time.Now()
		if err := env.seed(o); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		fmt.Printf("seeded in %s\n", time.Since(started).Round(time.Second))
	} else if err := env.reload(o); err != nil {
		return err
	}
	if err := execAll(db, "SELECT pg_stat_reset()"); err != nil {
		return err
	}

	report := env.workload(ctx, o)
	report.print(o)
	env.outboxLag()
	printDatabaseStats(db)
	slow.print(100 * time.Millisecond)

	stop()
	workers.Wait()
	return nil
}

func dbName(dsn string) string {
	for _, part := range strings.Fields(dsn) {
		if strings.HasPrefix(part, "dbname=") {
			return strings.TrimPrefix(part, "dbname=")
		}
	}
	if i := strings.LastIndex(dsn, "/"); i >= 0 {
		name := dsn[i+1:]
		if j := strings.Index(name, "?"); j >= 0 {
			name = name[:j]
		}
		return name
	}
	return ""
}

func resetSchema(dsn string) error {
	boot, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	for _, q := range []string{"DROP SCHEMA IF EXISTS public CASCADE", "CREATE SCHEMA public"} {
		if err := boot.Exec(q).Error; err != nil {
			return err
		}
	}
	if sqlDB, err := boot.DB(); err == nil {
		_ = sqlDB.Close()
	}
	if _, _, err := database.RunMigrations(dsn, "up", 0, 0); err != nil {
		return err
	}
	slog.Info("schema migrated")
	return nil
}

func execAll(db *gorm.DB, queries ...string) error {
	for _, q := range queries {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return nil
}
