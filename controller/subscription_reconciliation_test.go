package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionReconciliationAdminRouter(t *testing.T) *gin.Engine {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	dsn := "file:subscription-reconciliation-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Log{},
		&model.SubscriptionOrder{},
		&model.SubscriptionPaymentReceipt{},
		&model.SubscriptionPaymentEvidence{},
		&model.SubscriptionPaymentReviewDecision{},
	))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		assert.NoError(t, sqlDB.Close())
	})

	users := []model.User{
		{Id: 7601, Username: "reconciliation-user", AffCode: "reconcile-7601", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 7602, Username: "reconciliation-admin", AffCode: "reconcile-7602", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 7603, Username: "reconciliation-root", AffCode: "reconcile-7603", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
	}
	require.NoError(t, db.Create(&users).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("subscription-reconciliation-test"))))
	router.Use(func(c *gin.Context) {
		id, _ := strconv.Atoi(c.GetHeader("X-Test-Session-User"))
		session := sessions.Default(c)
		session.Set("id", id)
		c.Next()
	})
	reconciliationRoute := router.Group("/api/subscription/reconciliation")
	reconciliationRoute.Use(middleware.AdminAuth())
	reconciliationRoute.Use(func(c *gin.Context) {
		// AdminAuth records write-audit logs asynchronously. This suite verifies
		// routing and authorization, so suppress the asynchronous fallback to
		// keep the per-test database lifetime deterministic.
		markAuditLogged(c)
		c.Next()
	})
	{
		reconciliationRoute.GET("", AdminListSubscriptionOrderReviews)
		reconciliationRoute.POST("/:id/resolve", AdminResolveSubscriptionOrderReview)
	}
	return router
}

func subscriptionReconciliationAdminRequest(router *gin.Engine, method string, path string, userId int, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Session-User", strconv.Itoa(userId))
	request.Header.Set("New-Api-User", strconv.Itoa(userId))
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestSubscriptionReconciliationAdminRoutesEnforceRole(t *testing.T) {
	router := setupSubscriptionReconciliationAdminRouter(t)

	ordinaryList := subscriptionReconciliationAdminRequest(router, http.MethodGet, "/api/subscription/reconciliation", 7601, "")
	assert.Equal(t, http.StatusOK, ordinaryList.Code)
	assert.Contains(t, ordinaryList.Body.String(), `"success":false`)

	ordinaryResolve := subscriptionReconciliationAdminRequest(router, http.MethodPost, "/api/subscription/reconciliation/1/resolve", 7601,
		`{"receipt_id":1,"action":"close","note":"verified by support"}`)
	assert.Equal(t, http.StatusOK, ordinaryResolve.Code)
	assert.Contains(t, ordinaryResolve.Body.String(), `"success":false`)

	for _, actorId := range []int{7602, 7603} {
		response := subscriptionReconciliationAdminRequest(router, http.MethodGet, "/api/subscription/reconciliation?page=1&page_size=20", actorId, "")
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"success":true`)
		assert.Contains(t, response.Body.String(), `"page":1`)
		assert.Contains(t, response.Body.String(), `"page_size":20`)
	}
}

func TestSubscriptionReconciliationAdminRoutesRejectInvalidParameters(t *testing.T) {
	router := setupSubscriptionReconciliationAdminRouter(t)

	for _, path := range []string{
		"/api/subscription/reconciliation?page=0",
		"/api/subscription/reconciliation?page=not-a-number",
		"/api/subscription/reconciliation?page_size=0",
		"/api/subscription/reconciliation?page_size=101",
		"/api/subscription/reconciliation?page=9223372036854775807&page_size=100",
	} {
		response := subscriptionReconciliationAdminRequest(router, http.MethodGet, path, 7602, "")
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"success":false`)
	}

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "invalid order id", path: "/api/subscription/reconciliation/not-an-id/resolve", body: `{"receipt_id":1,"action":"close","note":"checked"}`},
		{name: "invalid json", path: "/api/subscription/reconciliation/1/resolve", body: `{`},
		{name: "missing receipt", path: "/api/subscription/reconciliation/1/resolve", body: `{"action":"close","note":"checked"}`},
		{name: "unsupported action", path: "/api/subscription/reconciliation/1/resolve", body: `{"receipt_id":1,"action":"approve","note":"checked"}`},
		{name: "blank note", path: "/api/subscription/reconciliation/1/resolve", body: `{"receipt_id":1,"action":"grant","note":"   "}`},
		{name: "oversized note", path: "/api/subscription/reconciliation/1/resolve", body: `{"receipt_id":1,"action":"close","note":"` + strings.Repeat("a", 4001) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := subscriptionReconciliationAdminRequest(router, http.MethodPost, test.path, 7602, test.body)
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Body.String(), `"success":false`)
		})
	}
}

func TestSubscriptionReconciliationAdminResolveClosesSelectedReceipt(t *testing.T) {
	router := setupSubscriptionReconciliationAdminRouter(t)
	now := common.GetTimestamp()
	order := model.SubscriptionOrder{
		UserId: 7601, PlanId: 91, Money: 12.5, TradeNo: "controller-review-close",
		PaymentMethod: "stripe", PaymentProvider: "stripe",
		Status: model.SubscriptionOrderStatusReconciliationRequired, CreateTime: now, CompleteTime: now,
		ReviewStatus: model.SubscriptionOrderReviewRequired, FirstReviewTime: now, ReviewTime: now,
		ReviewPreviousStatus: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&order).Error)
	receipt := model.SubscriptionPaymentReceipt{
		OrderId: order.Id, ReceiptKey: "stripe:controller-review-close-payment", ClaimId: "controller-review-claim",
		Provider: "stripe", ProviderTransactionId: "controller-review-close-payment",
		Amount: "12.50", PaidAmount: "12.50", Currency: "USD",
		CurrencySource: model.SubscriptionCurrencySourceProviderCallback,
		ProductId:      "price_controller_review", PaymentMethod: "stripe",
		CheckoutMode: model.SubscriptionCheckoutModeOneTime,
		Disposition:  model.SubscriptionReceiptDispositionReconciliation,
		PaidAt:       now, CreatedAt: now,
	}
	require.NoError(t, model.DB.Create(&receipt).Error)
	require.NoError(t, model.DB.Model(&order).Update("review_related_receipt_id", receipt.Id).Error)

	response := subscriptionReconciliationAdminRequest(router, http.MethodPost,
		"/api/subscription/reconciliation/"+strconv.Itoa(order.Id)+"/resolve", 7602,
		`{"receipt_id":`+strconv.Itoa(receipt.Id)+`,"action":"close","note":"provider payment was handled separately"}`)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"success":true`)
	assert.Contains(t, response.Body.String(), `"message":"success"`)

	var persistedOrder model.SubscriptionOrder
	require.NoError(t, model.DB.First(&persistedOrder, order.Id).Error)
	assert.Equal(t, model.SubscriptionOrderReviewResolved, persistedOrder.ReviewStatus)
	assert.Equal(t, model.SubscriptionReviewActionClose, persistedOrder.ResolutionAction)
	assert.Equal(t, receipt.Id, persistedOrder.ResolutionReceiptId)
	assert.Equal(t, 7602, persistedOrder.ResolvedBy)

	var decisions int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPaymentReviewDecision{}).
		Where("order_id = ? AND receipt_id = ?", order.Id, receipt.Id).Count(&decisions).Error)
	assert.EqualValues(t, 1, decisions)
}
