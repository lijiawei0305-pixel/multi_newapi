package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

var (
	httpClient              *http.Client
	ssrfProtectedHTTPClient *http.Client
	proxyClientLock         sync.Mutex
	proxyClients            = make(map[string]*http.Client)
	ssrfProxyClients        = make(map[string]*http.Client)
)

const (
	defaultRelayDialTimeoutSeconds           = 10
	defaultRelayResponseHeaderTimeoutSeconds = 600
	minimumRelayTransportTimeoutSeconds      = 5
)

func relayTransportTimeout(envName string, defaultSeconds int) time.Duration {
	seconds := common.GetEnvOrDefault(envName, defaultSeconds)
	if seconds < minimumRelayTransportTimeoutSeconds {
		seconds = minimumRelayTransportTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func relayDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   relayTransportTimeout("RELAY_DIAL_TIMEOUT_SECONDS", defaultRelayDialTimeoutSeconds),
		KeepAlive: 30 * time.Second,
	}
}

func applyRelayTransportTimeouts(transport *http.Transport) {
	transport.TLSHandshakeTimeout = relayTransportTimeout("RELAY_TLS_HANDSHAKE_TIMEOUT_SECONDS", defaultRelayDialTimeoutSeconds)
	transport.ResponseHeaderTimeout = relayTransportTimeout("RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS", defaultRelayResponseHeaderTimeoutSeconds)
}

type ipAddressResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type exactDialContext func(ctx context.Context, network string, address string) (net.Conn, error)

func currentSSRFProtection() (*common.SSRFProtection, bool, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if !fetchSetting.EnableSSRFProtection {
		return nil, false, nil
	}
	protection, err := common.NewSSRFProtectionFromFetchSetting(
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	)
	if err != nil {
		return nil, true, err
	}
	return protection, true, nil
}

func newSSRFProtectedDialContext(resolver ipAddressResolver, dialExact exactDialContext) exactDialContext {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		protection, enabled, err := currentSSRFProtection()
		if err != nil {
			return nil, fmt.Errorf("SSRF protection configuration: %w", err)
		}
		if !enabled {
			return dialExact(ctx, network, address)
		}

		host, portText, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid dial address %q: %w", address, err)
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return nil, fmt.Errorf("invalid dial port %q: %w", portText, err)
		}
		if err := protection.ValidateDialTarget(host, port); err != nil {
			return nil, fmt.Errorf("request blocked at dial: %w", err)
		}

		var candidates []net.IPAddr
		if ip := net.ParseIP(host); ip != nil {
			candidates = []net.IPAddr{{IP: ip}}
		} else {
			candidates, err = resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("DNS resolution returned no addresses for %s", host)
		}
		// Reject the complete answer before attempting any connection. A mixed
		// public/private response must not get a chance to reach the private IP.
		for _, candidate := range candidates {
			if err := protection.ValidateResolvedIP(host, candidate.IP); err != nil {
				return nil, fmt.Errorf("request blocked at dial: %w", err)
			}
		}

		var lastErr error
		for _, candidate := range candidates {
			exactAddress := net.JoinHostPort(candidate.IP.String(), portText)
			conn, err := dialExact(ctx, network, exactAddress)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("failed to dial approved addresses for %s: %w", host, lastErr)
	}
}

// sanitizedHTTPError preserves the operation and concrete error type without
// retaining net/http's URL string. URL errors include the full query by
// default, which can contain notification tokens or user content.
func sanitizedHTTPError(operation string, err error) error {
	return fmt.Errorf("%s: error_type=%T", operation, err)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	fetchSetting := system_setting.GetFetchSetting()
	urlStr := req.URL.String()
	if err := common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		metadata := common.PayloadMetadata([]byte(urlStr))
		return fmt.Errorf("redirect blocked url_%s error_type=%T", metadata, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func InitHttpClient() {
	directDialer := relayDialer()
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment, // Support HTTP_PROXY, HTTPS_PROXY, NO_PROXY env vars
		DialContext:         directDialer.DialContext,
	}
	protectedTransport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		ForceAttemptHTTP2:   true,
		// Environment proxies resolve the target outside this process and break
		// the validated-IP guarantee. Protected fetches use an explicit proxy API.
		Proxy:       nil,
		DialContext: newSSRFProtectedDialContext(net.DefaultResolver, directDialer.DialContext),
	}
	applyRelayTransportTimeouts(transport)
	applyRelayTransportTimeouts(protectedTransport)
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
		protectedTransport.TLSClientConfig = common.InsecureTLSConfig
	}

	if common.RelayTimeout == 0 {
		httpClient = &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
		ssrfProtectedHTTPClient = &http.Client{Transport: protectedTransport, CheckRedirect: checkRedirect}
	} else {
		httpClient = &http.Client{
			Transport:     transport,
			Timeout:       time.Duration(common.RelayTimeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
		ssrfProtectedHTTPClient = &http.Client{
			Transport:     protectedTransport,
			Timeout:       time.Duration(common.RelayTimeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
	}
}

func GetHttpClient() *http.Client {
	return httpClient
}

// GetHttpClientWithProxy returns the default client or a proxy-enabled one when proxyURL is provided.
func GetHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return GetHttpClient(), nil
	}
	return NewProxyHttpClient(proxyURL)
}

// GetSSRFProtectedHttpClientWithProxy returns a client whose actual dial uses
// the exact IP address that passed the current fetch policy. HTTP CONNECT
// proxies are rejected while protection is enabled because they re-resolve
// the target remotely; SOCKS uses a locally approved literal IP.
func GetSSRFProtectedHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	_, enabled, err := currentSSRFProtection()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return GetHttpClientWithProxy(proxyURL)
	}
	if strings.TrimSpace(proxyURL) == "" {
		if ssrfProtectedHTTPClient == nil {
			return nil, errors.New("SSRF-protected HTTP client is not initialized")
		}
		return ssrfProtectedHTTPClient, nil
	}

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme == "http" || parsedURL.Scheme == "https" {
		return nil, errors.New("HTTP proxy is not supported for SSRF-protected fetches because target DNS cannot be pinned")
	}
	if parsedURL.Scheme != "socks5" && parsedURL.Scheme != "socks5h" {
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be socks5 or socks5h for SSRF-protected fetches", parsedURL.Scheme)
	}

	proxyClientLock.Lock()
	if client, ok := ssrfProxyClients[proxyURL]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	var auth *proxy.Auth
	if parsedURL.User != nil {
		auth = &proxy.Auth{User: parsedURL.User.Username()}
		if password, ok := parsedURL.User.Password(); ok {
			auth.Password = password
		}
	}
	socksDialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, relayDialer())
	if err != nil {
		return nil, err
	}
	contextDialer, ok := socksDialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("SOCKS dialer does not support context cancellation")
	}
	protectedDial := newSSRFProtectedDialContext(net.DefaultResolver, func(ctx context.Context, network string, address string) (net.Conn, error) {
		return contextDialer.DialContext(ctx, network, address)
	})
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		ForceAttemptHTTP2:   true,
		DialContext:         protectedDial,
	}
	applyRelayTransportTimeouts(transport)
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	if common.RelayTimeout > 0 {
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
	}
	proxyClientLock.Lock()
	ssrfProxyClients[proxyURL] = client
	proxyClientLock.Unlock()
	return client, nil
}

// ResetProxyClientCache 清空代理客户端缓存，确保下次使用时重新初始化
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	for _, client := range ssrfProxyClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	proxyClients = make(map[string]*http.Client)
	ssrfProxyClients = make(map[string]*http.Client)
}

// NewProxyHttpClient 创建支持代理的 HTTP 客户端
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}

	proxyClientLock.Lock()
	if client, ok := proxyClients[proxyURL]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch parsedURL.Scheme {
	case "http", "https":
		directDialer := relayDialer()
		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyURL(parsedURL),
			DialContext:         directDialer.DialContext,
		}
		applyRelayTransportTimeouts(transport)
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}
		client := &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	case "socks5", "socks5h":
		// 获取认证信息
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		// 创建 SOCKS5 代理拨号器
		// proxy.SOCKS5 使用 tcp 参数，所有 TCP 连接包括 DNS 查询都将通过代理进行。行为与 socks5h 相同
		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, relayDialer())
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS dialer does not support context cancellation")
		}

		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
			ForceAttemptHTTP2:   true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return contextDialer.DialContext(ctx, network, addr)
			},
		}
		applyRelayTransportTimeouts(transport)
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}

		client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}
}
