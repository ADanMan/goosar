package storage

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)

	DeleteObject(ctx context.Context, key string) error
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string

	ObjectURL(key string) string
	CdnDomain() string

	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

type Presigner interface {
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type DownloadPresigner interface {
	PresignGetWithContentDisposition(ctx context.Context, key string, ttl time.Duration, contentDisposition string) (string, error)
}

func FromEnv() Storage {
	if s3 := NewS3StorageFromEnv(); s3 != nil {
		return s3
	}
	if local := NewLocalStorageFromEnv(); local != nil {
		return local
	}
	return nil
}
