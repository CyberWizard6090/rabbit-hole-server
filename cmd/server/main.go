package main

import (
	// Embed the IANA time zone database so time_zone validation does not depend on
	// the host having tzdata installed.
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/logging"
)

func main() {
	logging.Setup(os.Getenv("APP_ENV"), os.Stdout)

	env, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "refusing to start: %v\n", err)
		os.Exit(1)
	}
	logging.Setup(env.Environment, os.Stdout)
	config.LogStartup(env)

	if env.Environment != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := config.InitDB(env)
	if err != nil {
		fatal("failed to init database", err)
	}

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), schemaCheckTimeout)
	schemaErr := database.CheckSchema(schemaCtx, db)
	cancelSchema()
	if schemaErr != nil {
		fatal("database schema check failed", schemaErr)
	}

	application, err := app.NewApp(db, env)
	if err != nil {
		fatal("failed to create application", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := fmt.Sprintf("%s:%d", env.Server.Host, env.Server.Port)
	srv := newHTTPServer(addr, application.Handler())

	go func() {
		slog.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("server failed", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), env.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}

	if err := application.Close(shutdownCtx); err != nil {
		slog.Error("error closing application", "error", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		if err := sqlDB.Close(); err != nil {
			slog.Error("error closing database", "error", err)
		}
	}

	slog.Info("server stopped")
}

const schemaCheckTimeout = 10 * time.Second

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
