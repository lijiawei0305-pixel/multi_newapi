package relay

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

var taskSubmissionResponseHeaders = []string{
	"Content-Type",
	"Content-Language",
	"Location",
	"Retry-After",
	"X-Request-Id",
	"X-Oneapi-Request-Id",
	"X-New-Api-Other-Ratios",
}

type taskSubmissionClaimState struct {
	Recovery *model.TaskSubmissionRecovery
	Replay   bool
}

func normalizedTaskSubmissionHost(request *http.Request) (string, error) {
	if request == nil {
		return "", errors.New("task submission request is unavailable")
	}
	host := strings.TrimSpace(request.Host)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "/\\") || strings.IndexFunc(host, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return "", errors.New("task submission request host is invalid")
	}
	return host, nil
}

func writeFingerprintPart(writer io.Writer, value string) error {
	if _, err := io.WriteString(writer, strconv.Itoa(len(value))); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, ":"); err != nil {
		return err
	}
	_, err := io.WriteString(writer, value)
	return err
}

func taskSubmissionRequestFingerprint(c *gin.Context) (string, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return "", errors.New("task submission request is unavailable")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	parts := []string{
		strings.ToUpper(c.Request.Method),
		c.Request.URL.EscapedPath(),
		c.Request.URL.RawQuery,
		strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))),
		strings.ToLower(strings.TrimSpace(c.GetHeader("Accept"))),
	}
	for _, part := range parts {
		if err := writeFingerprintPart(hash, part); err != nil {
			return "", err
		}
	}
	if _, err := io.Copy(hash, storage); err != nil {
		return "", err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	c.Request.Body = io.NopCloser(storage)
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func taskSubmissionIdempotencyKey(c *gin.Context) (string, error) {
	if c == nil || c.Request == nil {
		return "", errors.New("task submission request is unavailable")
	}
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 {
		return "", errors.New("Idempotency-Key must be provided exactly once")
	}
	return values[0], nil
}

func buildTaskSubmissionClaimSpec(c *gin.Context, info *relaycommon.RelayInfo, kind string, publicTaskId string) (model.TaskSubmissionClaimSpec, error) {
	if c == nil || info == nil || c.Request == nil || c.Request.URL == nil {
		return model.TaskSubmissionClaimSpec{}, errors.New("task submission recovery context is invalid")
	}
	key, err := taskSubmissionIdempotencyKey(c)
	if err != nil {
		return model.TaskSubmissionClaimSpec{}, err
	}
	requestFingerprint, err := taskSubmissionRequestFingerprint(c)
	if err != nil {
		return model.TaskSubmissionClaimSpec{}, err
	}
	host, err := normalizedTaskSubmissionHost(c.Request)
	if err != nil {
		return model.TaskSubmissionClaimSpec{}, err
	}
	tenantId, err := model.TaskSubmissionTenantID(info.UserId)
	if err != nil {
		return model.TaskSubmissionClaimSpec{}, err
	}
	return model.TaskSubmissionClaimSpec{
		RequestId: info.RequestId, Kind: kind, IdempotencyKey: key,
		RequestFingerprint: requestFingerprint, UserId: info.UserId, TokenId: info.TokenId,
		TenantId: tenantId, Host: host, Route: c.Request.URL.EscapedPath(), Method: c.Request.Method,
		PublicTaskId: publicTaskId,
	}, nil
}

func useTaskSubmissionRecovery(c *gin.Context, info *relaycommon.RelayInfo, kind string, row *model.TaskSubmissionRecovery, owned bool) (taskSubmissionClaimState, error) {
	if row == nil {
		return taskSubmissionClaimState{}, errors.New("task submission recovery is unavailable")
	}
	info.RequestId = row.RequestId
	info.TaskSubmissionRecoveryPrepared = true
	info.TaskSubmissionRecoveryKind = kind
	info.TaskSubmissionClaimOwned = owned
	if row.IdempotencyFingerprint != nil {
		info.TaskSubmissionIdempotencyFingerprint = *row.IdempotencyFingerprint
	}
	if owned {
		info.TaskSubmissionRecoveryProtected = false
		return taskSubmissionClaimState{Recovery: row}, nil
	}

	info.TaskSubmissionRecoveryProtected = true
	switch row.Status {
	case model.TaskSubmissionStatusPreparing:
		return taskSubmissionClaimState{Recovery: row}, model.ErrTaskSubmissionIdempotencyInProgress
	case model.TaskSubmissionStatusUncertain:
		return taskSubmissionClaimState{Recovery: row}, errors.New("a prior submission attempt has uncertain provider state")
	case model.TaskSubmissionStatusAccepted, model.TaskSubmissionStatusCommitted:
		response, err := model.GetTaskSubmissionPublicResponse(row)
		if err != nil {
			return taskSubmissionClaimState{Recovery: row}, err
		}
		writeTaskSubmissionPublicResponse(c, response)
		return taskSubmissionClaimState{Recovery: row, Replay: true}, nil
	case model.TaskSubmissionStatusAborted:
		return taskSubmissionClaimState{Recovery: row}, errors.New("idempotent task submission was aborted and cannot be resubmitted")
	default:
		return taskSubmissionClaimState{Recovery: row}, errors.New("task submission recovery state is invalid")
	}
}

func lookupTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo, spec model.TaskSubmissionClaimSpec) (taskSubmissionClaimState, bool, error) {
	row, found, err := model.LookupTaskSubmissionClaim(spec)
	if err != nil || !found {
		return taskSubmissionClaimState{Recovery: row}, found, err
	}
	state, err := useTaskSubmissionRecovery(c, info, spec.Kind, row, false)
	return state, true, err
}

func claimTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo, spec model.TaskSubmissionClaimSpec) (taskSubmissionClaimState, error) {
	claim, err := model.ClaimTaskSubmission(spec)
	if err != nil {
		return taskSubmissionClaimState{}, err
	}
	return useTaskSubmissionRecovery(c, info, spec.Kind, claim.Recovery, claim.Owned)
}

func taskSubmissionAttemptMetadata(info *relaycommon.RelayInfo, provider string, initialQuota int) model.TaskSubmissionAttemptMetadata {
	publicTaskId := ""
	if info != nil && info.TaskRelayInfo != nil {
		publicTaskId = info.PublicTaskID
	}
	return model.TaskSubmissionAttemptMetadata{
		UserId: info.UserId, TokenId: info.TokenId, ChannelId: info.ChannelId,
		Provider: provider, Model: info.OriginModelName, PublicTaskId: publicTaskId,
		Action: info.Action, UsingGroup: info.UsingGroup, InitialQuota: initialQuota,
	}
}

func TaskSubmissionPublicResponse(writer *common.BufferedResponseWriter) (*model.TaskSubmissionPublicResponse, error) {
	if writer == nil {
		return nil, errors.New("task submission response buffer is unavailable")
	}
	status, headers, body, err := writer.Snapshot(taskSubmissionResponseHeaders, model.TaskSubmissionPublicResponseMaxBytes)
	if err != nil {
		return nil, err
	}
	response := &model.TaskSubmissionPublicResponse{Status: status, Headers: map[string][]string{}, Body: string(body)}
	for name, values := range headers {
		response.Headers[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	if err := response.Validate(); err != nil {
		return nil, err
	}
	return response, nil
}

func writeTaskSubmissionPublicResponse(c *gin.Context, response *model.TaskSubmissionPublicResponse) {
	if c == nil || response == nil {
		return
	}
	for name, values := range response.Headers {
		c.Writer.Header()[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	c.Status(response.Status)
	_, _ = c.Writer.Write([]byte(response.Body))
}
