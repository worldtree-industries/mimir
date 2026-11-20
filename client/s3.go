package client

import (
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

	"github.com/worldtree-industries/mimir/models"
)

// The client reads S3 by itself, rather than through mimir-server's
// implementation, so that it imports only models and other Projects can
// build it. It only ever reads.

// defaultRegion is used when AWS_REGION is not set; Ceph RGW ignores it but
// the SDK insists on one.
const defaultRegion = "us-east-1"

// s3Configured reports whether getenv holds any S3 settings: an endpoint
// (AWS_ENDPOINT_URL_S3 or AWS_ENDPOINT_URL) or credentials
// (AWS_ACCESS_KEY_ID).
func s3Configured(getenv func(string) string) bool {
	for _, k := range []string{"AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL", "AWS_ACCESS_KEY_ID"} {
		if getenv(k) != "" {
			return true
		}
	}

	return false
}

// s3Reader reads objects over the S3 API, path-style.
type s3Reader struct {
	client *s3.Client
}

// newS3Reader builds an S3 reader from the standard AWS environment
// variables, read through getenv, with endpoint in place of
// AWS_ENDPOINT_URL_S3 when it isn't empty. Credentials come only from the
// standard AWS sources, never from Mimir (ADR 0005).
func newS3Reader(ctx context.Context, getenv func(string) string, endpoint string) (*s3Reader, error) {
	region := getenv("AWS_REGION")
	if region == "" {
		region = defaultRegion
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}

	for _, k := range []string{"AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL"} {
		if endpoint == "" {
			endpoint = getenv(k)
		}
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		o.Logger = logging.Nop{}
	})

	return &s3Reader{client: client}, nil
}

// Get implements [models.ObjectReader].
func (r *s3Reader) Get(ctx context.Context, bucket, key, etag string) (models.Object, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if etag != "" {
		in.IfNoneMatch = aws.String(etag)
	}
	out, err := r.client.GetObject(ctx, in)
	if err != nil {
		var respErr interface{ HTTPStatusCode() int }
		if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotModified {
			return models.Object{}, models.ErrNotModified
		}
		// A missing bucket is deliberately not ErrNotFound: that is a
		// deployment fault, not an absence of Overrides.
		if apiErr, ok := errors.AsType[smithy.APIError](err); ok &&
			(apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
			return models.Object{}, models.ErrNotFound
		}

		return models.Object{}, fmt.Errorf("get s3://%s/%s: %w", bucket, key, err)
	}
	defer func() { _ = out.Body.Close() }()

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return models.Object{}, fmt.Errorf("read s3://%s/%s: %w", bucket, key, err)
	}

	return models.Object{Body: body, ETag: aws.ToString(out.ETag)}, nil
}
