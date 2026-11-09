package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

type App struct {
	router   *gin.Engine
	realtime *realtime.Hub

	stopWorkers context.CancelFunc
	running     sync.WaitGroup
}

func NewApp(db *gorm.DB, cfg *config.Config) (*App, error) {
	container, err := NewContainer(db, cfg)
	if err != nil {
		return nil, fmt.Errorf("create app dependencies: %w", err)
	}
	r := SetupRouter(container, cfg)
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	a := &App{
		router: r, realtime: container.Realtime, stopWorkers: stopWorkers,
	}
	for _, worker := range container.Workers {
		a.running.Add(1)
		go func() {
			defer a.running.Done()
			worker.Run(workerCtx)
		}()
	}
	a.running.Add(1)
	go func() {
		defer a.running.Done()
		service.RunFormUploadCleanup(workerCtx, container.FormUploads, formUploadCleanupInterval)
	}()
	if container.PurgeRateLimits != nil {
		a.running.Add(1)
		go func() {
			defer a.running.Done()
			runRateLimitPurge(workerCtx, container.PurgeRateLimits, rateLimitPurgeInterval)
		}()
	}
	return a, nil
}

const (
	formUploadCleanupInterval = 10 * time.Minute
	rateLimitPurgeInterval    = 10 * time.Minute
)

func runRateLimitPurge(ctx context.Context, purge func(context.Context) error, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := purge(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("rate limit: cannot purge expired counters", "error", err)
			}
		}
	}
}

func (a *App) Run(addr string) error {
	return a.router.Run(addr)
}

func (a *App) Handler() http.Handler {
	return a.router
}

func (a *App) Close(ctx context.Context) error {
	if a.stopWorkers != nil {
		a.stopWorkers()
	}
	done := make(chan struct{})
	go func() {
		a.running.Wait()
		close(done)
	}()
	var closeErr error
	select {
	case <-done:
	case <-ctx.Done():
		closeErr = fmt.Errorf("wait for background workers: %w", ctx.Err())
	}
	if a.realtime != nil {
		a.realtime.Close()
	}
	return closeErr
}
