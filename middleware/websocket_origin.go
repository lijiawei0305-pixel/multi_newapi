package middleware

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	websocketAllowedOriginsEnv    = "WEBSOCKET_ALLOWED_ORIGINS"
	websocketAllowCrossOriginEnv  = "WEBSOCKET_ALLOW_CROSS_ORIGIN"
	websocketTrustProxyHeadersEnv = "WEBSOCKET_TRUST_PROXY_HEADERS"
)

// WebSocketOriginGuard rejects cross-origin browser handshakes before token
// authentication can copy a credential out of Sec-WebSocket-Protocol. Clients
// without an Origin header (CLI, SDK and server-to-server callers) are allowed.
func WebSocketOriginGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if websocket.IsWebSocketUpgrade(c.Request) && !IsWebSocketOriginAllowed(c.Request) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

// IsWebSocketOriginAllowed applies the realtime WebSocket browser-origin
// policy. By default only same-origin browser requests are accepted.
//
// Operators can add exact HTTP(S) origins with WEBSOCKET_ALLOWED_ORIGINS
// (comma-separated), or deliberately allow any syntactically valid browser
// origin with WEBSOCKET_ALLOW_CROSS_ORIGIN=true. Reverse proxies that rewrite
// Host or terminate TLS can opt in to Forwarded/X-Forwarded-* processing with
// WEBSOCKET_TRUST_PROXY_HEADERS=true; those headers must only be set by a
// trusted proxy and stripped from direct client traffic.
func IsWebSocketOriginAllowed(r *http.Request) bool {
	if r == nil {
		return false
	}

	originValues := r.Header.Values("Origin")
	if len(originValues) == 0 || (len(originValues) == 1 && strings.TrimSpace(originValues[0]) == "") {
		return true
	}
	if len(originValues) != 1 {
		return false
	}

	origin, ok := canonicalBrowserOrigin(originValues[0], false)
	if !ok {
		return false
	}

	allowCrossOrigin, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(websocketAllowCrossOriginEnv)))
	if allowCrossOrigin {
		return true
	}

	for _, configured := range strings.Split(os.Getenv(websocketAllowedOriginsEnv), ",") {
		allowed, valid := canonicalBrowserOrigin(configured, true)
		if valid && allowed == origin {
			return true
		}
	}

	for _, requestOrigin := range websocketRequestOrigins(r) {
		if requestOrigin == origin {
			return true
		}
	}
	return false
}

func websocketRequestOrigins(r *http.Request) []string {
	directScheme := websocketRequestScheme(r)
	origins := make([]string, 0, 3)
	if direct, ok := canonicalRequestOrigin(directScheme, r.Host); ok {
		origins = append(origins, direct)
	}
	// TLS-terminating proxies normally preserve Host but the Go server sees a
	// plain HTTP connection. Honoring only the forwarded scheme cannot turn a
	// foreign hostname into a same-origin request, and keeps the secure default
	// usable behind the project's standard Nginx configuration.
	forwardedProto := firstForwardedParameter(r.Header.Get("Forwarded"), "proto")
	if forwardedProto == "" {
		forwardedProto = firstCommaSeparatedHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	}
	if forwardedProto != "" {
		if forwardedSchemeOrigin, ok := canonicalRequestOrigin(forwardedProto, r.Host); ok {
			origins = append(origins, forwardedSchemeOrigin)
		}
	}

	trustProxyHeaders, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(websocketTrustProxyHeadersEnv)))
	if !trustProxyHeaders {
		return origins
	}

	forwardedHost := firstForwardedParameter(r.Header.Get("Forwarded"), "host")
	if forwardedHost == "" {
		forwardedHost = firstCommaSeparatedHeaderValue(r.Header.Get("X-Forwarded-Host"))
	}
	if forwardedHost == "" {
		forwardedHost = r.Host
	}
	if forwardedProto == "" {
		forwardedProto = directScheme
	}
	if forwarded, ok := canonicalRequestOrigin(forwardedProto, forwardedHost); ok {
		origins = append(origins, forwarded)
	}
	return origins
}

func websocketRequestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if r.URL != nil {
		if scheme, ok := canonicalWebSocketScheme(r.URL.Scheme); ok {
			return scheme
		}
	}
	return "http"
}

func canonicalRequestOrigin(scheme, host string) (string, bool) {
	canonicalScheme, ok := canonicalWebSocketScheme(scheme)
	if !ok || strings.TrimSpace(host) == "" {
		return "", false
	}
	return canonicalBrowserOrigin(canonicalScheme+"://"+strings.TrimSpace(host), false)
}

func canonicalBrowserOrigin(raw string, allowTrailingSlash bool) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return "", false
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if parsed.Path != "" && !(allowTrailingSlash && parsed.Path == "/") {
		return "", false
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", false
	}
	port := parsed.Port()
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", false
		}
		if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
			port = ""
		}
	}

	if ip := net.ParseIP(hostname); ip != nil && strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	if port != "" {
		hostname = net.JoinHostPort(strings.Trim(hostname, "[]"), port)
	}
	return scheme + "://" + hostname, true
}

func canonicalWebSocketScheme(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "http", "ws":
		return "http", true
	case "https", "wss":
		return "https", true
	default:
		return "", false
	}
}

func firstCommaSeparatedHeaderValue(value string) string {
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	return strings.TrimSpace(value)
}

func firstForwardedParameter(header, name string) string {
	first := firstCommaSeparatedHeaderValue(header)
	for _, parameter := range strings.Split(first, ";") {
		parts := strings.SplitN(parameter, "=", 2)
		if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), name) {
			continue
		}
		value := strings.TrimSpace(parts[1])
		if strings.HasPrefix(value, "\"") {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return ""
			}
			value = unquoted
		}
		return strings.TrimSpace(value)
	}
	return ""
}
