package config

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
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
	Storage        StorageConfig
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" envDefault:"http://127.0.0.1:3000" envSeparator:"," validate:"required,min=1,dive,url"`
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

type StorageConfig struct {
	Driver             string `env:"STORAGE_DRIVER" envDefault:"local" validate:"oneof=local s3"`
	LocalDir           string `env:"STORAGE_LOCAL_DIR" envDefault:"./data/uploads"`
	PublicBaseURL      string `env:"PUBLIC_BASE_URL" validate:"omitempty,url"`
	MaxAttachmentBytes int64  `env:"MAX_ATTACHMENT_BYTES" envDefault:"26214400" validate:"gt=0"`
	MaxImageBytes      int64  `env:"MAX_IMAGE_BYTES" envDefault:"10485760" validate:"gt=0"`
	MaxAvatarBytes     int64  `env:"MAX_AVATAR_BYTES" envDefault:"5242880" validate:"gt=0"`
	S3                 S3Config
}

type S3Config struct {
	Endpoint  string `env:"S3_ENDPOINT"`
	Bucket    string `env:"S3_BUCKET"`
	AccessKey string `env:"S3_ACCESS_KEY"`
	SecretKey string `env:"S3_SECRET_KEY"`
	Region    string `env:"S3_REGION" envDefault:"us-east-1"`
	UseSSL    bool   `env:"S3_USE_SSL" envDefault:"false"`
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
	if err := validateStorage(cfg); err != nil {
		return err
	}
	return validateDeployment(cfg)
}

func validateStorage(cfg *Config) error {
	if cfg.Storage.Driver != "s3" {
		return nil
	}
	var missing []string
	for name, value := range map[string]string{
		"S3_ENDPOINT": cfg.Storage.S3.Endpoint, "S3_BUCKET": cfg.Storage.S3.Bucket,
		"S3_ACCESS_KEY": cfg.Storage.S3.AccessKey, "S3_SECRET_KEY": cfg.Storage.S3.SecretKey,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("STORAGE_DRIVER=s3 needs %s", strings.Join(missing, ", "))
	}
	return nil
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
