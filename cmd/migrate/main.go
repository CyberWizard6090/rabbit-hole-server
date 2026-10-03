package main

import (
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
	steps := flag.Int("steps", 0, "number of migrations for down; zero means the latest migration")
	version := flag.Int("version", 0, "version used by force")
	flag.Parse()

	if err := loadDotEnv(); err != nil {
		log.Fatal(err)
	}

	cfg, err := config.ParseDB()
	if err != nil {
		log.Fatal(err)
	}

	currentVersion, dirty, err := database.RunMigrations(
		cfg.URL,
		*action,
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
