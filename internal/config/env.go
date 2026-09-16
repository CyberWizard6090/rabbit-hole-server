package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

type Config struct {
	Environment    string `env:"APP_ENV" envDefault:"development" validate:"oneof=development test staging production"`
	Server         ServerConfig
	DB             DBConfig
	JWT            JWTConfig `envPrefix:"JWT_"`
	AppPepper      string    `env:"APP_PEPPER,required" validate:"required,min=16"`
	AllowedOrigins []string  `env:"ALLOWED_ORIGINS" envDefault:"http://127.0.0.1:3000" envSeparator:"," validate:"required,min=1,dive,url"`
}

type ServerConfig struct {
	Port int    `env:"PORT" envDefault:"8080" validate:"required,min=1,max=65535"`
	Host string `env:"HOST" envDefault:"0.0.0.0" validate:"required,hostname|ip"`
}

type DBConfig struct {
	URL string `env:"DB_URL,required" validate:"required,min=1"`
}

type JWTConfig struct {
	Secret string        `env:"SECRET,required" validate:"required,min=32"`
	TTL    time.Duration `env:"TTL" envDefault:"24h" validate:"gt=0"`
}

func Load() (*Config, error) {
	if os.Getenv("APP_ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			log.Println("No .env file found, reading configuration from OS environment")
		}
	}

	cfg, err := Parse()
	if err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func Parse() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing env config: %w", err)
	}
	return cfg, nil
}

func Validate(cfg *Config) error {
	return validator.New().Struct(cfg)
}
