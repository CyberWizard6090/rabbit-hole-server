package app

import (
	"gorm.io/gorm"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/config"
)

type App struct {
	router *gin.Engine
}

func NewApp(db *gorm.DB, cfg *config.Config) *App {
	container := NewContainer(db, cfg)
	r := SetupRouter(container, cfg)
	return &App{router: r}
}

func (a *App) Run(addr string) error {
	return a.router.Run(addr)
}
