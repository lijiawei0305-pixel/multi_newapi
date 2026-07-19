package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPprofServerDefaultsToLoopbackAndDedicatedMux(t *testing.T) {
	server, err := newPprofServer("", "")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8005", server.Addr)
	require.NotNil(t, server.Handler)
	require.NotSame(t, http.DefaultServeMux, server.Handler)
	require.Positive(t, server.ReadHeaderTimeout)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	server.Handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestNewPprofServerValidatesPort(t *testing.T) {
	for _, port := range []string{"0", "65536", "not-a-port"} {
		_, err := newPprofServer("127.0.0.1", port)
		require.Error(t, err)
	}
}
