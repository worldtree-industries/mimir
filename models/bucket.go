package models

import (
	"errors"
	"regexp"
)

// bucketPrefixPattern is what a bucket prefix may look like: the start of an
// S3 bucket name.
var bucketPrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// ErrBucketPrefix means a bucket prefix can't start an S3 bucket name.
var ErrBucketPrefix = errors.New("bucket prefix must be lowercase letters, digits, '.' and '-'")

// CheckBucketPrefix reports whether prefix can be the prefix of each
// Environment's bucket, "<prefix>-<environment>" (ADR 0006).
func CheckBucketPrefix(prefix string) error {
	if !bucketPrefixPattern.MatchString(prefix) {
		return ErrBucketPrefix
	}

	return nil
}
