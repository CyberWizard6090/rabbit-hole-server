package storage

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("stored object not found")

type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
}
