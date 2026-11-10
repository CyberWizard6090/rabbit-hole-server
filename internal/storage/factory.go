package storage

import (
	"context"
	"fmt"

	"rabbit-hole-server/internal/config"
)

func New(ctx context.Context, cfg config.StorageConfig) (Store, error) {
	switch cfg.Driver {
	case "local":
		return NewLocal(cfg.LocalDir)
	case "s3":
		return NewS3(ctx, S3Options{
			Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
			SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
		})
	}
	return nil, fmt.Errorf("unknown storage driver %q", cfg.Driver)
}
