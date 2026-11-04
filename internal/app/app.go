package app

import (
	"context"
	"fmt"
	"net/http"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/realtime"
)

type App struct {
	router   *gin.Engine
	realtime *realtime.Hub
}

func NewApp(db *gorm.DB, cfg *config.Config) (*App, error) {
	container, err := NewContainer(db, cfg)
	if err != nil {
		return nil, fmt.Errorf("create app dependencies: %w", err)
	}
	r := SetupRouter(container, cfg)
	return &App{router: r, realtime: container.Realtime}, nil
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
	return nil
}
