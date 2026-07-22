package common

import "context"

type requestIdContextKey struct{}

// WithRequestId returns a child context carrying the internal request ID.
// The private key type prevents collisions with context values owned by other
// packages while RequestIdKey remains the public HTTP/Gin key.
func WithRequestId(ctx context.Context, requestId string) context.Context {
	return context.WithValue(ctx, requestIdContextKey{}, requestId)
}

// RequestIdFromContext reads a request ID installed by WithRequestId.
func RequestIdFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	requestId, ok := ctx.Value(requestIdContextKey{}).(string)
	return requestId, ok && requestId != ""
}
