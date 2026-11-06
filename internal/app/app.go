package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
}

func NewApp(db *gorm.DB, cfg *config.Config) (*App, error) {
	container, err := NewContainer(db, cfg)
	if err != nil {
		return nil, fmt.Errorf("create app dependencies: %w", err)
	}
	r := SetupRouter(container, cfg)
	return &App{router: r, realtime: container.Realtime, closeMail: container.CloseMail, notifier: container.Notifications}, nil
}

func (a *App) Run(addr string) error {
	return a.router.Run(addr)
}

func (a *App) Handler() http.Handler {
	return a.router
}

func (a *App) Close(ctx context.Context) error {
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
