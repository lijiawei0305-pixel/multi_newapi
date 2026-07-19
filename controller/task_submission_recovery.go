package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type taskSubmissionResolutionRequest struct {
	Outcome        string                              `json:"outcome"`
	Reason         string                              `json:"reason"`
	ProviderTaskId string                              `json:"provider_task_id"`
	FinalQuota     int                                 `json:"final_quota"`
	PublicResponse *model.TaskSubmissionPublicResponse `json:"public_response"`
}

func AdminListTaskSubmissionRecoveries(c *gin.Context) {
	limit := 50
	beforeId := 0
	var err error
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "limit is invalid"})
			return
		}
	}
	if raw := strings.TrimSpace(c.Query("before_id")); raw != "" {
		beforeId, err = strconv.Atoi(raw)
		if err != nil || beforeId < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "before_id is invalid"})
			return
		}
	}
	rows, err := model.ListTaskSubmissionRecoveries(c.Query("status"), c.Query("kind"), limit, beforeId)
	if err != nil {
		common.SysError("task submission recovery list failed: error_type=" + fmt.Sprintf("%T", err))
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "task submission recovery filters or query are invalid"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func AdminGetTaskSubmissionRecovery(c *gin.Context) {
	view, err := model.GetTaskSubmissionRecoveryView(c.Param("request_id"), c.Query("kind"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"success": false, "message": "task submission recovery was not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func validTaskSubmissionResolutionReason(reason string) bool {
	if len(reason) < 1 || len(reason) > 256 {
		return false
	}
	return strings.IndexFunc(reason, func(r rune) bool { return unicode.IsControl(r) }) < 0
}

func AdminResolveTaskSubmissionRecovery(c *gin.Context) {
	requestId := c.Param("request_id")
	kind := c.Query("kind")
	var request taskSubmissionResolutionRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "resolution request is invalid"})
		return
	}
	request.Outcome = strings.ToLower(strings.TrimSpace(request.Outcome))
	request.Reason = strings.TrimSpace(request.Reason)
	adminId := c.GetInt("id")

	switch request.Outcome {
	case "unknown":
		view, err := model.GetTaskSubmissionRecoveryView(requestId, kind)
		if err != nil || view.Status != model.TaskSubmissionStatusUncertain {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "only an uncertain submission can remain unresolved"})
			return
		}
		recordManageAudit(c, "task_submission.resolve_unknown", map[string]interface{}{
			"request_id": requestId, "kind": kind,
		})
		c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
	case model.TaskSubmissionResolutionRejected:
		if !validTaskSubmissionResolutionReason(request.Reason) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "a 1-256 character investigation reason is required"})
			return
		}
		err := model.ResolveUncertainTaskSubmissionRejected(requestId, kind, adminId)
		var pending *model.BillingSettlementApplyPendingError
		if err != nil && !errors.As(err, &pending) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "task submission cannot be resolved rejected from its current state"})
			return
		}
		recordManageAudit(c, "task_submission.resolve_rejected", map[string]interface{}{
			"request_id": requestId, "kind": kind, "reason": request.Reason,
		})
		view, _ := model.GetTaskSubmissionRecoveryView(requestId, kind)
		c.JSON(http.StatusOK, gin.H{"success": true, "financial_followup_pending": pending != nil, "data": view})
	case model.TaskSubmissionResolutionAccepted:
		if !validTaskSubmissionResolutionReason(request.Reason) || request.PublicResponse == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "reason and public_response are required"})
			return
		}
		result, err := service.ResolveUncertainTaskSubmissionAccepted(requestId, kind, adminId, service.TaskSubmissionAcceptedResolution{
			ProviderTaskId: request.ProviderTaskId, FinalQuota: request.FinalQuota, PublicResponse: *request.PublicResponse,
		})
		if err != nil {
			common.SysError(fmt.Sprintf("task submission accepted resolution follow-up failed: error_type=%T", err))
		}
		if err != nil && !result.Committed {
			view, viewErr := model.GetTaskSubmissionRecoveryView(requestId, kind)
			if viewErr != nil || view.Status != model.TaskSubmissionStatusAccepted && view.Status != model.TaskSubmissionStatusCommitted {
				c.JSON(http.StatusConflict, gin.H{"success": false, "message": "confirmed acceptance could not be frozen from the supplied evidence"})
				return
			}
		}
		recordManageAudit(c, "task_submission.resolve_accepted", map[string]interface{}{
			"request_id": requestId, "kind": kind, "provider_task_id": strings.TrimSpace(request.ProviderTaskId),
			"final_quota": request.FinalQuota, "reason": request.Reason,
		})
		view, _ := model.GetTaskSubmissionRecoveryView(requestId, kind)
		c.JSON(http.StatusOK, gin.H{"success": true, "local_followup_pending": err != nil, "data": view})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "outcome must be accepted, rejected, or unknown"})
	}
}
