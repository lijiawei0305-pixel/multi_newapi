package common

import (
	"errors"
	"fmt"
	"io"
	"math"
)

const defaultUpstreamJSONMaxBytes = int64(16 << 20)

var ErrReadLimitExceeded = errors.New("read limit exceeded")

type ReadLimitExceededError struct {
	Limit int64
}

func (e *ReadLimitExceededError) Error() string {
	return fmt.Sprintf("%s: limit=%d", ErrReadLimitExceeded.Error(), e.Limit)
}

func (e *ReadLimitExceededError) Unwrap() error {
	return ErrReadLimitExceeded
}

// UpstreamJSONBodyLimit returns the default hard limit for provider JSON and
// error responses. Large binary endpoints must pass an explicit limit instead.
func UpstreamJSONBodyLimit() int64 {
	limit := int64(GetEnvOrDefault("RELAY_UPSTREAM_JSON_MAX_BYTES", int(defaultUpstreamJSONMaxBytes)))
	if limit <= 0 {
		return defaultUpstreamJSONMaxBytes
	}
	return limit
}

// ReadAllWithLimit reads at most limit+1 bytes so an exact-limit body remains
// valid while an oversized body is rejected instead of silently truncated.
// Without an explicit limit it uses UpstreamJSONBodyLimit.
func ReadAllWithLimit(reader io.Reader, limits ...int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("limited reader is nil")
	}
	limit := UpstreamJSONBodyLimit()
	if len(limits) > 1 {
		return nil, errors.New("multiple read limits are not allowed")
	}
	if len(limits) == 1 {
		limit = limits[0]
	}
	if limit < 0 || limit == math.MaxInt64 {
		return nil, errors.New("read limit is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return data, err
	}
	if int64(len(data)) > limit {
		return nil, &ReadLimitExceededError{Limit: limit}
	}
	return data, nil
}

// CopyWithLimit streams into a private destination while enforcing a hard
// source limit. The destination may receive one probe byte beyond the limit;
// callers must discard it when ErrReadLimitExceeded is returned.
func CopyWithLimit(writer io.Writer, reader io.Reader, limit int64) (int64, error) {
	if writer == nil || reader == nil {
		return 0, errors.New("limited copy reader or writer is nil")
	}
	if limit < 0 || limit == math.MaxInt64 {
		return 0, errors.New("copy limit is invalid")
	}
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	written, err := io.Copy(writer, limited)
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, &ReadLimitExceededError{Limit: limit}
	}
	return written, nil
}
