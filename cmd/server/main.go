package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
)

func main() {
	env, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load environment: ", err)
	}

	if env.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := config.InitDB(env)
	if err != nil {
		log.Fatal("Failed to init DB:", err)
	}

	application := app.NewApp(db, env)

	if err := application.Run(fmt.Sprintf("%s:%d", env.Server.Host, env.Server.Port)); err != nil {
		log.Fatal("Failed to run application:", err)
	}
}
