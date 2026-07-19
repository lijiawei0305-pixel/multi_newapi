package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const defaultFileDownloadTimeoutSeconds = 30

type cancelOnCloseResponseBody struct {
	io.ReadCloser
	cancel   context.CancelFunc
	once     sync.Once
	closeErr error
}

func (b *cancelOnCloseResponseBody) Close() error {
	b.once.Do(func() {
		b.cancel()
		b.closeErr = b.ReadCloser.Close()
	})
	return b.closeErr
}

func attachDownloadDeadlineToResponse(resp *http.Response, cancel context.CancelFunc) error {
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("download response body is unavailable")
	}
	resp.Body = &cancelOnCloseResponseBody{ReadCloser: resp.Body, cancel: cancel}
	return nil
}

func boundedFileDownloadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	seconds := common.GetEnvOrDefault("RELAY_FILE_DOWNLOAD_TIMEOUT_SECONDS", defaultFileDownloadTimeoutSeconds)
	if seconds <= 0 {
		seconds = defaultFileDownloadTimeoutSeconds
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}

// WorkerRequest Worker请求的数据结构
type WorkerRequest struct {
	URL     string            `json:"url"`
	Key     string            `json:"key"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// DoWorkerRequest 通过Worker发送请求
func DoWorkerRequest(req *WorkerRequest) (*http.Response, error) {
	return DoWorkerRequestContext(context.Background(), req)
}

func DoWorkerRequestContext(ctx context.Context, req *WorkerRequest) (*http.Response, error) {
	ctx, cancel := boundedFileDownloadContext(ctx)
	deadlineAttached := false
	defer func() {
		if !deadlineAttached {
			cancel()
		}
	}()
	if req == nil {
		return nil, fmt.Errorf("worker request is nil")
	}
	if !system_setting.EnableWorker() {
		return nil, fmt.Errorf("worker not enabled")
	}
	if !system_setting.WorkerAllowHttpImageRequestEnabled && !strings.HasPrefix(req.URL, "https") {
		return nil, fmt.Errorf("only support https url")
	}

	// SSRF防护：验证请求URL
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(req.URL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}

	workerUrl := system_setting.WorkerUrl
	if !strings.HasSuffix(workerUrl, "/") {
		workerUrl += "/"
	}

	// 序列化worker请求数据
	workerPayload, err := common.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker payload: %v", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, workerUrl, bytes.NewReader(workerPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to build worker request: %v", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	resp, err := GetHttpClient().Do(httpRequest)
	if err != nil {
		return nil, sanitizedHTTPError("worker request failed", err)
	}
	if err := attachDownloadDeadlineToResponse(resp, cancel); err != nil {
		return nil, err
	}
	deadlineAttached = true
	return resp, nil
}

func DoDownloadRequest(originUrl string, reason ...string) (resp *http.Response, err error) {
	return DoDownloadRequestContext(context.Background(), originUrl, reason...)
}

func DoDownloadRequestContext(ctx context.Context, originUrl string, reason ...string) (resp *http.Response, err error) {
	ctx, cancel := boundedFileDownloadContext(ctx)
	deadlineAttached := false
	defer func() {
		if !deadlineAttached {
			cancel()
		}
	}()
	reasonMetadata := common.PayloadMetadata([]byte(strings.Join(reason, ", ")))
	if system_setting.EnableWorker() {
		common.SysLog(fmt.Sprintf("downloading file from worker url_%s reason_%s", common.PayloadMetadata([]byte(originUrl)), reasonMetadata))
		req := &WorkerRequest{
			URL: originUrl,
			Key: system_setting.WorkerValidKey,
		}
		resp, err := DoWorkerRequestContext(ctx, req)
		if err != nil {
			return nil, err
		}
		if err := attachDownloadDeadlineToResponse(resp, cancel); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		deadlineAttached = true
		return resp, nil
	} else {
		// SSRF防护：验证请求URL（非Worker模式）
		fetchSetting := system_setting.GetFetchSetting()
		if err := common.ValidateURLWithFetchSetting(originUrl, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
			return nil, fmt.Errorf("request reject: %v", err)
		}

		common.SysLog(fmt.Sprintf("downloading from origin url_%s reason_%s", common.PayloadMetadata([]byte(originUrl)), reasonMetadata))
		client, err := GetSSRFProtectedHttpClientWithProxy("")
		if err != nil {
			return nil, fmt.Errorf("failed to create SSRF-protected client: %v", err)
		}
		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, originUrl, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build download request: %v", err)
		}
		resp, err := client.Do(httpRequest)
		if err != nil {
			return nil, sanitizedHTTPError("download request failed", err)
		}
		if err := attachDownloadDeadlineToResponse(resp, cancel); err != nil {
			return nil, err
		}
		deadlineAttached = true
		return resp, nil
	}
}
