// Package backup uploads database snapshots to Amazon S3.
package backup

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Uploader puts objects into one fixed bucket.
type Uploader struct {
	client *s3.Client
	bucket string
}

// New creates an Uploader using static AWS credentials. It returns nil when
// the bucket or credentials are empty so callers can treat "backups not
// configured" as a graceful no-op (a nil *Uploader's methods are safe to call).
func New(region, bucket, accessKey, secretKey string) *Uploader {
	if bucket == "" || accessKey == "" || secretKey == "" {
		return nil
	}
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}
	return &Uploader{client: s3.NewFromConfig(cfg), bucket: bucket}
}

// Enabled reports whether the uploader is configured.
func (u *Uploader) Enabled() bool { return u != nil }

// Bucket returns the target bucket name ("" when disabled).
func (u *Uploader) Bucket() string {
	if u == nil {
		return ""
	}
	return u.bucket
}

// Upload stores r as key in the bucket. size must be the exact byte length so
// the SDK can send it in one request without buffering.
func (u *Uploader) Upload(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	if u == nil {
		return nil
	}
	// Bounded so a stuck S3 call can never wedge the scheduler goroutine.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(u.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", u.bucket, key, err)
	}
	return nil
}
