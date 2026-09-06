package app

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

)


type App struct {
	router *gin.Engine
}

func NewApp(db *gorm.DB) *App {
	container := NewContainer(db)
	r := SetupRouter(container)
	return &App{router: r}
}

func (a *App) Run(addr string) error {
	return a.router.Run(addr)
}
