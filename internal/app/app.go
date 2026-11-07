package app

import (
	"context"
	"errors"
	"fmt"
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
	router    *gin.Engine
	realtime  *realtime.Hub
	closeMail func(context.Context) error
	notifier  *service.NotificationDispatcher

	stopWorkers context.CancelFunc
	workers     sync.WaitGroup
}

func NewApp(db *gorm.DB, cfg *config.Config) (*App, error) {
	container, err := NewContainer(db, cfg)
	if err != nil {
		return nil, fmt.Errorf("create app dependencies: %w", err)
	}
	r := SetupRouter(container, cfg)
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	a := &App{
		router: r, realtime: container.Realtime, closeMail: container.CloseMail,
		notifier: container.Notifications, stopWorkers: stopWorkers,
	}
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		service.RunFormUploadCleanup(workerCtx, container.FormUploads, formUploadCleanupInterval)
	}()
	return a, nil
}

const formUploadCleanupInterval = 10 * time.Minute

func (a *App) Run(addr string) error {
	return a.router.Run(addr)
}

func (a *App) Handler() http.Handler {
	return a.router
}

func (a *App) Close(ctx context.Context) error {
	if a.stopWorkers != nil {
		a.stopWorkers()
		a.workers.Wait()
	}
	if a.realtime != nil {
		a.realtime.Close()
	}
	var closeErr error
	if a.notifier != nil {
		if err := a.notifier.Shutdown(ctx); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close notification dispatcher: %w", err))
		}
	}
	if a.closeMail != nil {
		if err := a.closeMail(ctx); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close mail sender: %w", err))
		}
	}
	return closeErr
}
