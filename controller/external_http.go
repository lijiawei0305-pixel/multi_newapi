package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func validateControlPlaneURL(targetURL string) error {
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(
		targetURL,
		fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	)
}

func newControlPlaneHTTPClient(timeout time.Duration) (*http.Client, error) {
	client, err := service.GetSSRFProtectedHttpClientWithProxy("")
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("control-plane HTTP client is unavailable")
	}
	clientWithTimeout := *client
	if clientWithTimeout.Timeout <= 0 || clientWithTimeout.Timeout > timeout {
		clientWithTimeout.Timeout = timeout
	}
	return &clientWithTimeout, nil
}
