package service

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticIPResolver struct {
	addresses []net.IPAddr
	err       error
}

func (r staticIPResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	return r.addresses, r.err
}

func withStrictSSRFProtection(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	original := *setting
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         false,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{"80", "443"},
		ApplyIPFilterForDomain: true,
	}
	t.Cleanup(func() { *setting = original })
}

func TestSSRFProtectedDialRejectsReboundPrivateIPBeforeConnect(t *testing.T) {
	withStrictSSRFProtection(t)

	var dialCalls atomic.Int32
	dial := newSSRFProtectedDialContext(staticIPResolver{
		addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}, func(_ context.Context, _ string, _ string) (net.Conn, error) {
		dialCalls.Add(1)
		return nil, errors.New("must not dial")
	})

	conn, err := dial(context.Background(), "tcp", "rebind.example:80")
	assert.Nil(t, conn)
	assert.ErrorContains(t, err, "private IP address not allowed")
	assert.Zero(t, dialCalls.Load())
}

func TestSSRFProtectedDialRejectsMixedDNSAnswerBeforeConnect(t *testing.T) {
	withStrictSSRFProtection(t)

	var dialCalls atomic.Int32
	dial := newSSRFProtectedDialContext(staticIPResolver{
		addresses: []net.IPAddr{
			{IP: net.ParseIP("8.8.8.8")},
			{IP: net.ParseIP("169.254.169.254")},
		},
	}, func(_ context.Context, _ string, _ string) (net.Conn, error) {
		dialCalls.Add(1)
		return nil, errors.New("must not dial")
	})

	_, err := dial(context.Background(), "tcp", "mixed.example:443")
	assert.ErrorContains(t, err, "private IP address not allowed")
	assert.Zero(t, dialCalls.Load())
}

func TestSSRFProtectedDialConnectsExactApprovedIP(t *testing.T) {
	withStrictSSRFProtection(t)

	var dialedAddress string
	peer, peerOther := net.Pipe()
	t.Cleanup(func() {
		_ = peer.Close()
		_ = peerOther.Close()
	})
	dial := newSSRFProtectedDialContext(staticIPResolver{
		addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}},
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialedAddress = address
		return peer, nil
	})

	conn, err := dial(context.Background(), "tcp", "approved.example:443")
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, "8.8.8.8:443", dialedAddress)
}

func TestSSRFProtectedFetchRejectsRemoteResolvingHTTPProxy(t *testing.T) {
	withStrictSSRFProtection(t)

	client, err := GetSSRFProtectedHttpClientWithProxy("http://proxy.example:8080")
	assert.Nil(t, client)
	assert.ErrorContains(t, err, "target DNS cannot be pinned")
}

func TestSSRFProtectedSOCKSProxyReceivesLocallyResolvedLiteralIP(t *testing.T) {
	withStrictSSRFProtection(t)
	setting := system_setting.GetFetchSetting()
	setting.AllowPrivateIp = true
	setting.AllowedPorts = []string{"80"}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	type socksTarget struct {
		addressType byte
		ip          net.IP
		port        int
	}
	observed := make(chan socksTarget, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		greeting := make([]byte, 2)
		if _, err := io.ReadFull(conn, greeting); err != nil || greeting[0] != 5 {
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, err := io.ReadFull(conn, methods); err != nil {
			return
		}
		if _, err := conn.Write([]byte{5, 0}); err != nil {
			return
		}

		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 || header[1] != 1 {
			return
		}
		var targetIP net.IP
		switch header[3] {
		case 1:
			targetIP = make(net.IP, net.IPv4len)
		case 4:
			targetIP = make(net.IP, net.IPv6len)
		case 3:
			length := []byte{0}
			if _, err := io.ReadFull(conn, length); err != nil {
				return
			}
			domain := make([]byte, int(length[0]))
			if _, err := io.ReadFull(conn, domain); err != nil {
				return
			}
			observed <- socksTarget{addressType: header[3]}
			return
		default:
			return
		}
		if _, err := io.ReadFull(conn, targetIP); err != nil {
			return
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(conn, portBytes); err != nil {
			return
		}
		observed <- socksTarget{
			addressType: header[3],
			ip:          targetIP,
			port:        int(portBytes[0])<<8 | int(portBytes[1]),
		}
		_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	}()

	client, err := GetSSRFProtectedHttpClientWithProxy("socks5://" + listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(ResetProxyClientCache)
	_, _ = client.Get("http://localhost/")

	got := <-observed
	assert.NotEqual(t, byte(3), got.addressType, "SOCKS proxy must not receive a hostname")
	assert.True(t, got.ip.IsLoopback(), got.ip.String())
	assert.Equal(t, 80, got.port)
}

func TestSanitizedHTTPErrorDoesNotRetainURLOrQuery(t *testing.T) {
	const secretURL = "https://notify.example/message?token=private-token&content=private-content"
	err := sanitizedHTTPError("notification failed", &url.Error{
		Op:  http.MethodGet,
		URL: secretURL,
		Err: errors.New("transport failed"),
	})

	assert.Contains(t, err.Error(), "notification failed")
	assert.Contains(t, err.Error(), "error_type=*url.Error")
	assert.NotContains(t, err.Error(), secretURL)
	assert.NotContains(t, err.Error(), "private-token")
	assert.NotContains(t, err.Error(), "private-content")
}

func TestSSRFProtectedTransportPreservesOriginalHostAndSNI(t *testing.T) {
	observed := make(chan struct {
		host string
		sni  string
	}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- struct {
			host string
			sni  string
		}{host: r.Host, sni: r.TLS.ServerName}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, portText, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	setting := system_setting.GetFetchSetting()
	original := *setting
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{portText},
		ApplyIPFilterForDomain: true,
	}
	t.Cleanup(func() { *setting = original })

	transport := &http.Transport{
		DialContext: newSSRFProtectedDialContext(staticIPResolver{
			addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
		}, (&net.Dialer{}).DialContext),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // test server certificate
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}

	resp, err := client.Get("https://sni.example:" + strconv.Itoa(port) + "/video")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	got := <-observed
	assert.Equal(t, "sni.example:"+portText, got.host)
	assert.Equal(t, "sni.example", got.sni)
}

func TestSSRFProtectedRedirectCannotReachUnapprovedPrivateListener(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2/private?token=redirect-secret", http.StatusFound)
	}))
	defer source.Close()
	sourceURL, err := url.Parse(source.URL)
	require.NoError(t, err)
	_, sourcePort, err := net.SplitHostPort(sourceURL.Host)
	require.NoError(t, err)

	setting := system_setting.GetFetchSetting()
	original := *setting
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           true,
		IpList:                 []string{"127.0.0.1/32"},
		AllowedPorts:           []string{sourcePort, "80"},
		ApplyIPFilterForDomain: true,
	}
	t.Cleanup(func() { *setting = original })

	var privateDialCalls atomic.Int32
	directDialer := &net.Dialer{}
	transport := &http.Transport{DialContext: newSSRFProtectedDialContext(staticIPResolver{
		addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}, func(ctx context.Context, network string, address string) (net.Conn, error) {
		if address == "127.0.0.2:80" {
			privateDialCalls.Add(1)
		}
		return directDialer.DialContext(ctx, network, address)
	})}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}

	resp, err := client.Get("http://public.example:" + sourcePort + "/start")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirect blocked")
	safeErr := sanitizedHTTPError("redirect fetch failed", err)
	assert.NotContains(t, safeErr.Error(), "redirect-secret")
	assert.Zero(t, privateDialCalls.Load())
}
