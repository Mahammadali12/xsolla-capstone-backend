package storage

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Storage is the interface for object storage operations.
type Storage interface {
	// Upload stores a file in object storage.
	Upload(ctx context.Context, fileID int64, originalName, storageKey, mimeType string, sizeBytes int64, reader io.Reader) error
	// Download retrieves a file from object storage.
	Download(ctx context.Context, storageKey string) (io.ReadCloser, error)
	// Delete removes a file from object storage.
	Delete(ctx context.Context, storageKey string) error
	// PublicURL returns a public URL for accessing the file.
	PublicURL(storageKey string) string
}

// S3Storage implements Storage using AWS S3-compatible API.
type S3Storage struct {
	client  *s3.Client
	bucket  string
	endpoint string
}

// NewS3Storage creates a new S3-backed storage backend.
func NewS3Storage(endpoint, bucket, region, accessKey, secretKey string) (*S3Storage, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("S3_ENDPOINT is required")
	}
	if bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required")
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return &S3Storage{
		client:  client,
		bucket:  bucket,
		endpoint: endpoint,
	}, nil
}

// Upload stores a file in S3-compatible storage.
func (s *S3Storage) Upload(ctx context.Context, fileID int64, originalName, storageKey, mimeType string, sizeBytes int64, reader io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(storageKey),
		Body:        reader,
		ContentType: aws.String(mimeType),
		ContentLength: aws.Int64(sizeBytes),
		Metadata: map[string]string{
			"original-name": originalName,
			"file-id":       fmt.Sprintf("%d", fileID),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", formatS3Error(err))
	}
	return nil
}

// Download retrieves a file from S3-compatible storage.
func (s *S3Storage) Download(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(storageKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", formatS3Error(err))
	}
	return resp.Body, nil
}

// Delete removes a file from S3-compatible storage.
func (s *S3Storage) Delete(ctx context.Context, storageKey string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(storageKey),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", formatS3Error(err))
	}
	return nil
}

// PublicURL returns a public URL for accessing the file.
func (s *S3Storage) PublicURL(storageKey string) string {
	return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, storageKey)
}

// Ensure S3Storage implements Storage.
var _ Storage = (*S3Storage)(nil)

// NewNoopStorage returns a storage that does nothing.
func NewNoopStorage() *S3Storage {
	return &S3Storage{
		client:  &s3.Client{},
		bucket:  "",
	}
}

// GetContentTypeFromExtension returns the MIME type based on file extension.
func GetContentTypeFromExtension(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".zip":
		return "application/zip"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	case ".rar":
		return "application/vnd.rar"
	default:
		return "application/octet-stream"
	}
}

// formatS3Error wraps S3 errors with additional context for easier debugging.
func formatS3Error(err error) error {
	return fmt.Errorf("failed to access S3: %w", err)
}
