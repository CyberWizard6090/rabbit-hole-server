package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	AllowedOrigins []string  `env:"ALLOWED_ORIGINS" envDefault:"http://127.0.0.1:3000" envSeparator:"," validate:"required,min=1,dive,url"`
}

type ServerConfig struct {
	Port           int      `env:"PORT" envDefault:"8080" validate:"required,min=1,max=65535"`
	Host           string   `env:"HOST" envDefault:"0.0.0.0" validate:"required,hostname|ip"`
	TrustedProxies []string `env:"TRUSTED_PROXIES" envDefault:"127.0.0.1,::1" envSeparator:"," validate:"required,min=1,dive"`
}

type DBConfig struct {
	URL                string        `env:"DB_URL,required" validate:"required,min=1"`
	MaxIdleConnections int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5" validate:"gte=0"`
	MaxOpenConnections int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25" validate:"gt=0"`
	ConnMaxLifetime    time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"1h" validate:"gt=0"`
}

type JWTConfig struct {
	Secret     string        `env:"SECRET,required" validate:"required,min=32"`
	AccessTTL  time.Duration `env:"ACCESS_TTL" envDefault:"1h" validate:"gt=0"`
	RefreshTTL time.Duration `env:"REFRESH_TTL" envDefault:"168h" validate:"gt=0"`
}

func Load() (*Config, error) {
	if os.Getenv("APP_ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			slog.Debug("no .env file found, reading configuration from OS environment")
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

func ParseDB() (*DBConfig, error) {
	cfg := &DBConfig{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing db env config: %w", err)
	}
	if err := validator.New().Struct(cfg); err != nil {
		return nil, fmt.Errorf("validating db config: %w", err)
	}
	return cfg, nil
}

func Validate(cfg *Config) error {
	if err := validator.New().Struct(cfg); err != nil {
		return err
	}
	return validateDeployment(cfg)
}

func (c *Config) IsDeployed() bool {
	return c.Environment == "production" || c.Environment == "staging"
}

func (c *Config) SecureCookies() bool { return c.IsDeployed() }

var knownWeakSecretMarkers = []string{
	"replace-with", "change-me", "changeme", "change_me", "your-secret", "your_secret",
	"example", "placeholder", "default", "password", "secret-key", "insecure",
}

const minDistinctSecretChars = 12

func validateDeployment(cfg *Config) error {
	if !cfg.IsDeployed() {
		return nil
	}

	var problems []string

	secret := strings.ToLower(cfg.JWT.Secret)
	for _, marker := range knownWeakSecretMarkers {
		if strings.Contains(secret, marker) {
			problems = append(problems, fmt.Sprintf("JWT_SECRET contains the placeholder fragment %q; generate a random secret (for example `openssl rand -base64 48`)", marker))
			break
		}
	}
	distinct := map[rune]struct{}{}
	for _, r := range cfg.JWT.Secret {
		distinct[r] = struct{}{}
	}
	if len(distinct) < minDistinctSecretChars {
		problems = append(problems, fmt.Sprintf("JWT_SECRET uses only %d distinct characters (need at least %d); it does not look random", len(distinct), minDistinctSecretChars))
	}

	for _, origin := range cfg.AllowedOrigins {
		if !strings.HasPrefix(origin, "https://") {
			problems = append(problems, fmt.Sprintf("ALLOWED_ORIGINS entry %q must use https:// in %s", origin, cfg.Environment))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("unsafe %s configuration:\n  - %s", cfg.Environment, strings.Join(problems, "\n  - "))
	}

	if strings.Contains(cfg.DB.URL, "sslmode=disable") {
		slog.Warn("DB_URL uses sslmode=disable; acceptable only when the database is reachable solely over a private network")
	}
	return nil
}
