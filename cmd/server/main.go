package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := fmt.Sprintf("%s:%d", env.Server.Host, env.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      application.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to run application: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	if err := application.Close(shutdownCtx); err != nil {
		log.Printf("error closing application: %v", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		if err := sqlDB.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}

	log.Println("Server stopped successfully")
}
