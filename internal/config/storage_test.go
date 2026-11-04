package config

import (
	"strings"
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_URL", "postgres://localhost/rabbit_hole")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
}

func TestStorageDefaultsToLocalDisk(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.Storage
	if s.Driver != "local" || s.LocalDir != "./data/uploads" || s.PublicBaseURL != "" {
		t.Fatalf("storage = %+v", s)
	}
	if s.MaxAttachmentBytes != 25<<20 || s.MaxImageBytes != 10<<20 || s.MaxAvatarBytes != 5<<20 {
		t.Fatalf("limits = %d, %d, %d", s.MaxAttachmentBytes, s.MaxImageBytes, s.MaxAvatarBytes)
	}
}

func TestStorageS3NeedsItsSettings(t *testing.T) {
	baseEnv(t)
	t.Setenv("STORAGE_DRIVER", "s3")
	t.Setenv("S3_ENDPOINT", "minio:9000")

	_, err := Load()
	if err == nil {
		t.Fatal("expected missing S3 settings to be rejected")
	}
	for _, name := range []string{"S3_ACCESS_KEY", "S3_BUCKET", "S3_SECRET_KEY"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
	if strings.Contains(err.Error(), "S3_ENDPOINT") {
		t.Errorf("error %q lists a setting that is present", err)
	}

	t.Setenv("S3_BUCKET", "files")
	t.Setenv("S3_ACCESS_KEY", "access")
	t.Setenv("S3_SECRET_KEY", "secret")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("complete S3 settings: %v", err)
	}
	if cfg.Storage.S3.Region != "us-east-1" || cfg.Storage.S3.UseSSL {
		t.Fatalf("s3 = %+v", cfg.Storage.S3)
	}
}

func TestStorageRejectsBadValues(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unknown driver":     {"STORAGE_DRIVER": "ftp"},
		"zero attachment":    {"MAX_ATTACHMENT_BYTES": "0"},
		"negative image":     {"MAX_IMAGE_BYTES": "-5"},
		"public url no host": {"PUBLIC_BASE_URL": "not a url"},
	} {
		t.Run(name, func(t *testing.T) {
			baseEnv(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatal("expected the configuration to be rejected")
			}
		})
	}
}
