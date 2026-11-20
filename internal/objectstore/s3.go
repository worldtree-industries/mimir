package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"
)

// defaultRegion is used when AWS_REGION is not set; Ceph RGW ignores it but
// the SDK insists on one.
const defaultRegion = "us-east-1"

// S3 is a [Store] over the S3 API, such as Ceph RGW's. Buckets must already
// exist.
type S3 struct {
	client *s3.Client
}

var _ Store = (*S3)(nil)

// S3Configured reports whether getenv holds any S3 settings: an endpoint
// (AWS_ENDPOINT_URL_S3 or AWS_ENDPOINT_URL) or credentials
// (AWS_ACCESS_KEY_ID).
func S3Configured(getenv func(string) string) bool {
	for _, k := range []string{"AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL", "AWS_ACCESS_KEY_ID"} {
		if getenv(k) != "" {
			return true
		}
	}

	return false
}

// NewS3FromEnv builds an S3 store from the standard AWS environment variables
// (endpoint, region, credentials), read through getenv. Credentials never
// come from Mimir itself (ADR 0005). Addressing is path-style, which Ceph RGW
// needs.
func NewS3FromEnv(ctx context.Context, getenv func(string) string) (*S3, error) {
	region := getenv("AWS_REGION")
	if region == "" {
		region = defaultRegion
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}

	endpoint := getenv("AWS_ENDPOINT_URL_S3")
	if endpoint == "" {
		endpoint = getenv("AWS_ENDPOINT_URL")
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		// A Snapshot is small and a plain MD5 is all Ceph needs.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		o.Logger = logging.Nop{}
	})

	return &S3{client: client}, nil
}

// Put implements [Store].
func (s *S3) Put(ctx context.Context, bucket, key string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put s3://%s/%s: %w", bucket, key, err)
	}

	return nil
}

// Get implements [Store].
func (s *S3) Get(ctx context.Context, bucket, key, etag string) (Object, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if etag != "" {
		in.IfNoneMatch = aws.String(etag)
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		return Object{}, mapS3Error(bucket, key, err)
	}
	defer func() { _ = out.Body.Close() }()

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return Object{}, fmt.Errorf("read s3://%s/%s: %w", bucket, key, err)
	}

	return Object{Body: body, ETag: aws.ToString(out.ETag)}, nil
}

// Delete implements [Store].
func (s *S3) Delete(ctx context.Context, bucket, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if errors.Is(mapS3Error(bucket, key, err), ErrNotFound) {
			return nil
		}

		return fmt.Errorf("delete s3://%s/%s: %w", bucket, key, err)
	}

	return nil
}

// mapS3Error turns the SDK's errors into the package's sentinels: a missing
// key is ErrNotFound and a failed If-None-Match is ErrNotModified. A missing
// bucket is deliberately not ErrNotFound: that is a deployment fault.
func mapS3Error(bucket, key string, err error) error {
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotModified {
		return ErrNotModified
	}

	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return ErrNotFound
		}
	}

	return fmt.Errorf("s3://%s/%s: %w", bucket, key, err)
}
