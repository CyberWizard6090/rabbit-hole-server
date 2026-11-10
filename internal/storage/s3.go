package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Options struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	Transport http.RoundTripper
}

type S3 struct {
	client *minio.Client
	bucket string
}

func NewS3(ctx context.Context, opts S3Options) (*S3, error) {
	client, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure:    opts.UseSSL,
		Region:    opts.Region,
		Transport: opts.Transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create object storage client: %w", err)
	}
	store := &S3{client: client, bucket: opts.Bucket}
	if err := store.Ping(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *S3) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return object, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3) Ping(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check object storage bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("object storage bucket %q does not exist", s.bucket)
	}
	return nil
}
