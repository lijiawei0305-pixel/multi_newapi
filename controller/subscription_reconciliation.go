package controller

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const maxSubscriptionReconciliationPageSize = 100

type resolveSubscriptionOrderReviewRequest struct {
	ReceiptId int    `json:"receipt_id"`
	Action    string `json:"action"`
	Note      string `json:"note"`
}

func AdminListSubscriptionOrderReviews(c *gin.Context) {
	page := 1
	pageSize := common.ItemsPerPage
	var err error
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		page, err = strconv.Atoi(rawPage)
		if err != nil || page <= 0 {
			common.ApiErrorMsg(c, "无效的分页参数")
			return
		}
	}
	if rawPageSize := strings.TrimSpace(c.Query("page_size")); rawPageSize != "" {
		pageSize, err = strconv.Atoi(rawPageSize)
		if err != nil || pageSize <= 0 || pageSize > maxSubscriptionReconciliationPageSize {
			common.ApiErrorMsg(c, "无效的分页参数")
			return
		}
	}
	if page-1 > math.MaxInt/pageSize {
		common.ApiErrorMsg(c, "无效的分页参数")
		return
	}

	items, total, err := model.ListSubscriptionOrderReviews((page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func AdminResolveSubscriptionOrderReview(c *gin.Context) {
	orderId, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || orderId <= 0 {
		common.ApiErrorMsg(c, "无效的订单ID")
		return
	}

	var req resolveSubscriptionOrderReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.Note = strings.TrimSpace(req.Note)
	if req.ReceiptId <= 0 {
		common.ApiErrorMsg(c, "无效的收款凭证ID")
		return
	}
	switch req.Action {
	case model.SubscriptionReviewActionGrant, model.SubscriptionReviewActionClose, model.SubscriptionReviewActionExternallyRefunded:
	default:
		common.ApiErrorMsg(c, "无效的处理动作")
		return
	}
	if req.Note == "" || len(req.Note) > 4000 {
		common.ApiErrorMsg(c, "处理备注不能为空或超过4000字节")
		return
	}

	if err := model.ResolveSubscriptionOrderReview(orderId, c.GetInt("id"), req.ReceiptId, req.Action, req.Note); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "success",
	})
}
