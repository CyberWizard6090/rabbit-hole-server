package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
)

func main() {
	action := flag.String("action", "up", "migration action: up, down, version, or force")
	steps := flag.Int("steps", 0, "number of migrations to roll back for down")
	all := flag.Bool("all", false, "roll back every migration with down (refused when APP_ENV=production)")
	version := flag.Int("version", 0, "version used by force")
	flag.Parse()

	if err := loadDotEnv(); err != nil {
		log.Fatal(err)
	}

	cfg, err := config.ParseDB()
	if err != nil {
		log.Fatal(err)
	}

	resolved, err := resolveAction(*action, *steps, *all, os.Getenv("APP_ENV"))
	if err != nil {
		log.Fatal(err)
	}

	currentVersion, dirty, err := database.RunMigrations(
		cfg.URL,
		resolved,
		*steps,
		*version,
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("migration version: %d, dirty: %t\n", currentVersion, dirty)
}

func loadDotEnv() error {
	if _, err := os.Stat(".env"); err != nil {
		return nil
	}
	return godotenv.Load()
}

func resolveAction(action string, steps int, all bool, environment string) (string, error) {
	if action != "down" {
		if all {
			return "", errors.New("-all only applies to -action down")
		}
		return action, nil
	}
	if all {
		if steps > 0 {
			return "", errors.New("use either -steps N or -all, not both")
		}
		if environment == "production" {
			return "", errors.New("refusing to roll back every migration while APP_ENV=production; use -steps N")
		}
		return database.ActionDownAll, nil
	}
	if steps <= 0 {
		return "", errors.New("down needs -steps N, or -all to roll back every migration")
	}
	return action, nil
}
