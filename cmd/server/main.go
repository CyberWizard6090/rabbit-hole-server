package main

import (
	"log"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
)

func main() {

	db, err := config.InitDB()
	if err != nil {
		log.Fatal("Failed to init DB:", err)
	}

	application := app.NewApp(db)

	if err := application.Run(":8080"); err != nil {
		log.Fatal("Failed to run application:", err)
	}
}
