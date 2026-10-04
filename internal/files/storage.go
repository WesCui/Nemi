package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"nemi/internal/config"
)

type Objects interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
}
type localObjects struct{ root string }

const maxObjectBytes = MaxBytes*4/3 + MaxContent + 8192

var objectKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}/[a-f0-9]{32}$`)

func (l *localObjects) path(key string) (string, error) {
	if !objectKeyPattern.MatchString(key) {
		return "", errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}
func (l *localObjects) Put(ctx context.Context, key string, data []byte) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(p)
		return errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	return nil
}
func (l *localObjects) Get(ctx context.Context, key string) ([]byte, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxObjectBytes+1))
	if len(b) > maxObjectBytes {
		return nil, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	return b, err
}

type s3Objects struct {
	client *minio.Client
	bucket string
}

func (s *s3Objects) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/octet-stream", DisableMultipart: true})
	return err
}
func (s *s3Objects) Get(ctx context.Context, key string) ([]byte, error) {
	o, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer o.Close()
	b, err := io.ReadAll(io.LimitReader(o, maxObjectBytes+1))
	if len(b) > maxObjectBytes {
		return nil, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	return b, err
}
func objects(c config.Config) (Objects, string, error) {
	if c.FilesS3Endpoint != "" {
		client, err := minio.New(c.FilesS3Endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.FilesS3Access, c.FilesS3Secret, ""), Secure: c.FilesS3Secure, Region: c.FilesS3Region, BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
		if err != nil {
			return nil, "", err
		}
		return &s3Objects{client, c.FilesS3Bucket}, "s3", nil
	}
	root := c.FilesRoot
	if root == "" {
		root = "data/files"
	}
	return &localObjects{root}, "local", nil
}
