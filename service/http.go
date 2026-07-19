package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// ShouldCopyUpstreamHeader permits only response metadata that is safe and
// useful to API clients. Provider cookies, hop-by-hop headers, authentication
// challenges, CORS policy, and gateway security headers must never control the
// gateway origin.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	if len(v) == 0 {
		return false
	}
	if strings.EqualFold(k, common.RequestIdKey) {
		if c != nil {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	key := strings.ToLower(strings.TrimSpace(k))
	switch key {
	case "content-type", "content-encoding", "content-disposition", "content-language",
		"accept-ranges", "content-range", "retry-after", "x-accel-buffering",
		"x-request-id", "openai-organization", "openai-processing-ms", "openai-version":
		return true
	default:
		return strings.HasPrefix(key, "x-ratelimit-") || strings.HasPrefix(key, "ratelimit-")
	}
}

// CopyUpstreamResponseHeaders evaluates the whole header map first so tokens
// named by Connection are suppressed independent of Go map iteration order.
func CopyUpstreamResponseHeaders(c *gin.Context, destination http.Header, source http.Header) {
	if destination == nil || source == nil {
		return
	}
	connectionHeaders := make(map[string]struct{})
	for _, value := range source.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if key := http.CanonicalHeaderKey(strings.TrimSpace(token)); key != "" {
				connectionHeaders[key] = struct{}{}
			}
		}
	}
	for key, values := range source {
		canonical := http.CanonicalHeaderKey(key)
		if _, forbidden := connectionHeaders[canonical]; forbidden || !ShouldCopyUpstreamHeader(c, canonical, values) {
			continue
		}
		destination[canonical] = append([]string(nil), values...)
	}
	destination.Set("Cache-Control", "no-store, private")
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		CopyUpstreamResponseHeaders(c, c.Writer.Header(), src.Header)
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
