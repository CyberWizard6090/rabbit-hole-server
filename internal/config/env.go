package config

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/joho/godotenv"
)

type Config struct {
	Environment    string `env:"APP_ENV" envDefault:"development" validate:"oneof=development test staging production"`
	Server         ServerConfig
	DB             DBConfig
	JWT            JWTConfig `envPrefix:"JWT_"`
	Storage        StorageConfig
	Mail           MailConfig
	Notifications  NotificationConfig
	Forms          FormsConfig
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" envDefault:"http://127.0.0.1:3000" envSeparator:"," validate:"required,min=1"`
}

type ServerConfig struct {
	Port           int      `env:"PORT" envDefault:"8080" validate:"required,min=1,max=65535"`
	Host           string   `env:"HOST" envDefault:"0.0.0.0" validate:"required,hostname|ip"`
	TrustedProxies []string `env:"TRUSTED_PROXIES" envDefault:"127.0.0.1,::1" envSeparator:"," validate:"required,min=1"`
}

type DBConfig struct {
	URL                string        `env:"DB_URL" validate:"required"`
	MaxIdleConnections int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5" validate:"gte=0"`
	MaxOpenConnections int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25" validate:"gt=0"`
	ConnMaxLifetime    time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"1h" validate:"gt=0"`
}

type StorageConfig struct {
	Driver             string `env:"STORAGE_DRIVER" envDefault:"local" validate:"oneof=local s3"`
	LocalDir           string `env:"STORAGE_LOCAL_DIR" envDefault:"./data/uploads"`
	PublicBaseURL      string `env:"PUBLIC_BASE_URL"`
	MaxAttachmentBytes int64  `env:"MAX_ATTACHMENT_BYTES" envDefault:"26214400" validate:"gt=0"`
	MaxImageBytes      int64  `env:"MAX_IMAGE_BYTES" envDefault:"10485760" validate:"gt=0"`
	MaxAvatarBytes     int64  `env:"MAX_AVATAR_BYTES" envDefault:"5242880" validate:"gt=0"`
	MaxRehostBytes     int64  `env:"REHOST_MAX_BYTES" validate:"gte=0"`
	S3                 S3Config
}

func (s StorageConfig) RehostLimit() int64 {
	if s.MaxRehostBytes > 0 {
		return s.MaxRehostBytes
	}
	return s.MaxImageBytes
}

type MailConfig struct {
	Driver               string        `env:"MAIL_DRIVER" envDefault:"log" validate:"oneof=log smtp"`
	From                 string        `env:"MAIL_FROM" envDefault:"Rabbit Hole <no-reply@localhost>" validate:"required"`
	SMTPHost             string        `env:"SMTP_HOST"`
	SMTPPort             int           `env:"SMTP_PORT" envDefault:"587" validate:"min=1,max=65535"`
	SMTPUsername         string        `env:"SMTP_USERNAME"`
	SMTPPassword         string        `env:"SMTP_PASSWORD"`
	SMTPSecurity         string        `env:"SMTP_SECURITY" envDefault:"starttls" validate:"oneof=none starttls tls"`
	FrontendURL          string        `env:"FRONTEND_URL" envDefault:"http://localhost:3000"`
	VerificationTTL      time.Duration `env:"EMAIL_VERIFICATION_TTL" envDefault:"48h" validate:"gt=0"`
	ResetTTL             time.Duration `env:"PASSWORD_RESET_TTL" envDefault:"1h" validate:"gt=0"`
	RequireVerifiedEmail bool          `env:"REQUIRE_VERIFIED_EMAIL" envDefault:"false"`
}

type NotificationConfig struct {
	EmailPerHour int `env:"NOTIFICATION_EMAIL_PER_HOUR" envDefault:"30" validate:"gt=0"`
	MaxPerUser   int `env:"NOTIFICATION_MAX_PER_USER" envDefault:"1000" validate:"gt=0"`

	WebhookMaxFailures int `env:"NOTIFICATION_WEBHOOK_MAX_FAILURES" envDefault:"10" validate:"gt=0"`
}

type FormsConfig struct {
	SubmissionsPerHour int           `env:"FORM_SUBMISSIONS_PER_HOUR" envDefault:"200" validate:"gt=0"`
	UploadMaxBytes     int64         `env:"FORM_UPLOAD_MAX_BYTES" envDefault:"10485760" validate:"gt=0"`
	UploadTTL          time.Duration `env:"FORM_UPLOAD_TTL" envDefault:"24h" validate:"gt=0"`
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
	Secret     string        `env:"SECRET" validate:"required,min=32"`
	AccessTTL  time.Duration `env:"ACCESS_TTL" envDefault:"1h" validate:"gt=0"`
	RefreshTTL time.Duration `env:"REFRESH_TTL" envDefault:"168h" validate:"gt=0"`
}

func Load() (*Config, error) {
	if os.Getenv("APP_ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			slog.Debug("no .env file found, reading configuration from OS environment")
		}
	}

	cfg, set := parse()
	validate(cfg, set)
	if err := set.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parse() (*Config, *problemSet) {
	cfg := &Config{}
	set := newProblemSet()
	checkEnvTypes(cfg, "", set)
	if err := env.Parse(cfg); err != nil && len(set.items) == 0 {
		set.add("configuration", "%v", err)
	}
	return cfg, set
}

func Parse() (*Config, error) {
	cfg, set := parse()
	if err := set.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func ParseDB() (*DBConfig, error) {
	cfg := &DBConfig{}
	set := newProblemSet()
	checkEnvTypes(cfg, "", set)
	if err := env.Parse(cfg); err != nil && len(set.items) == 0 {
		set.add("configuration", "%v", err)
	}
	checkStruct(cfg, set)
	if err := set.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Validate(cfg *Config) error {
	set := newProblemSet()
	validate(cfg, set)
	return set.err()
}

func validate(cfg *Config, set *problemSet) {
	checkStruct(cfg, set)
	checkDeployment(cfg, set)
	if cfg.DB.URL != "" && cfg.IsDeployed() && strings.Contains(cfg.DB.URL, "sslmode=disable") {
		slog.Warn("DB_URL uses sslmode=disable; acceptable only when the database is reachable solely over a private network")
	}
}

func LogStartup(cfg *Config) {
	slog.Info("configuration loaded",
		"environment", cfg.Environment,
		"mail_driver", cfg.Mail.Driver,
		"storage_driver", cfg.Storage.Driver,
		"require_verified_email", cfg.Mail.RequireVerifiedEmail,
	)
	if cfg.Mail.Driver == "log" && cfg.Environment != "development" {
		slog.Warn("MAIL_DRIVER=log only writes mail to the server log; users will not receive verification or reset links", "environment", cfg.Environment)
	}
	if cfg.Storage.PublicBaseURL == "" && cfg.IsDeployed() {
		slog.Warn("PUBLIC_BASE_URL is empty; image links are returned as relative paths")
	}
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
