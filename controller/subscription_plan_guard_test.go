package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type subscriptionPlanGuardResponse struct {
	Success   bool                       `json:"success"`
	Message   string                     `json:"message"`
	ErrorCode string                     `json:"error_code"`
	Data      []AdminSubscriptionPlanDTO `json:"data"`
}

func setupSubscriptionPlanGuardTest(t *testing.T, migrateMapping bool) (*gorm.DB, *gin.Engine) {
	t.Helper()

	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	dsn := "file:subscription-plan-guard-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	if migrateMapping {
		require.NoError(t, db.AutoMigrate(&model.TokenPlanNativeSubscriptionPlan{}))
	}
	require.NoError(t, appI18n.Init())

	model.DB = db
	common.RedisEnabled = false
	confirmPaymentComplianceForTest(t)
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		assert.NoError(t, sqlDB.Close())
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/subscription/plans", AdminListSubscriptionPlans)
	router.PUT("/api/subscription/plans/:id", AdminUpdateSubscriptionPlan)
	router.PATCH("/api/subscription/plans/:id", AdminUpdateSubscriptionPlanStatus)
	return db, router
}

func subscriptionPlanGuardRequest(t *testing.T, router *gin.Engine, method string, planId int, body string, languages ...string) subscriptionPlanGuardResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/api/subscription/plans/"+strconv.Itoa(planId), strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if len(languages) > 0 {
		request.Header.Set("Accept-Language", languages[0])
	}
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response subscriptionPlanGuardResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestNativeSubscriptionPlanAdminWritesProtectTokenPlanMappings(t *testing.T) {
	db, router := setupSubscriptionPlanGuardTest(t, true)

	mappedPlan := model.SubscriptionPlan{
		Id: 8101, Title: "mapped plan", Currency: "USD", PriceAmount: 1,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	nativePlan := model.SubscriptionPlan{
		Id: 8102, Title: "native plan", Currency: "USD", PriceAmount: 2,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, db.Create(&mappedPlan).Error)
	require.NoError(t, db.Create(&nativePlan).Error)
	require.NoError(t, db.Create(&model.TokenPlanNativeSubscriptionPlan{TokenPlanID: 91, NativePlanID: int64(mappedPlan.Id)}).Error)

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/subscription/plans", nil)
	router.ServeHTTP(listRecorder, listRequest)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listResponse subscriptionPlanGuardResponse
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data, 2)
	listedByID := make(map[int]AdminSubscriptionPlanDTO, len(listResponse.Data))
	for _, item := range listResponse.Data {
		listedByID[item.Plan.Id] = item
	}
	assert.Equal(t, model.SubscriptionPlanManagerTokenPlan, listedByID[mappedPlan.Id].ManagedBy)
	assert.True(t, listedByID[mappedPlan.Id].ReadOnly)
	assert.Equal(t, model.SubscriptionPlanManagerNative, listedByID[nativePlan.Id].ManagedBy)
	assert.False(t, listedByID[nativePlan.Id].ReadOnly)

	updateBody := `{"plan":{"title":"changed through legacy API","price_amount":9,"currency":"USD","duration_unit":"month","duration_value":1,"enabled":false}}`
	mappedUpdate := subscriptionPlanGuardRequest(t, router, http.MethodPut, mappedPlan.Id, updateBody, "en")
	assert.False(t, mappedUpdate.Success)
	assert.Equal(t, "This subscription plan is managed in Token Plans. Edit it there.", mappedUpdate.Message)
	assert.Equal(t, "TOKEN_PLAN_MANAGED", mappedUpdate.ErrorCode)

	mappedStatus := subscriptionPlanGuardRequest(t, router, http.MethodPatch, mappedPlan.Id, `{"enabled":false}`, "zh-CN")
	assert.False(t, mappedStatus.Success)
	assert.Equal(t, "该套餐由套餐管理维护，请前往套餐管理修改", mappedStatus.Message)
	assert.Equal(t, "TOKEN_PLAN_MANAGED", mappedStatus.ErrorCode)

	var mappedAfter model.SubscriptionPlan
	require.NoError(t, db.First(&mappedAfter, mappedPlan.Id).Error)
	assert.Equal(t, "mapped plan", mappedAfter.Title)
	assert.True(t, mappedAfter.Enabled)

	nativeUpdate := subscriptionPlanGuardRequest(t, router, http.MethodPut, nativePlan.Id, updateBody)
	assert.True(t, nativeUpdate.Success)
	nativeStatus := subscriptionPlanGuardRequest(t, router, http.MethodPatch, nativePlan.Id, `{"enabled":true}`)
	assert.True(t, nativeStatus.Success)

	var nativeAfter model.SubscriptionPlan
	require.NoError(t, db.First(&nativeAfter, nativePlan.Id).Error)
	assert.Equal(t, "changed through legacy API", nativeAfter.Title)
	assert.True(t, nativeAfter.Enabled)
}

func TestNativeSubscriptionPlanAdminWritesFailClosedWhenOwnershipCannotBeChecked(t *testing.T) {
	db, router := setupSubscriptionPlanGuardTest(t, false)
	plan := model.SubscriptionPlan{
		Id: 8201, Title: "native plan", Currency: "USD", PriceAmount: 2,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, db.Create(&plan).Error)

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/subscription/plans", nil)
	listRequest.Header.Set("Accept-Language", "zh-CN")
	router.ServeHTTP(listRecorder, listRequest)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listResponse subscriptionPlanGuardResponse
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	assert.False(t, listResponse.Success)
	assert.Equal(t, "暂时无法确认套餐归属，已拒绝修改，请稍后重试", listResponse.Message)
	assert.Equal(t, "PLAN_OWNERSHIP_UNAVAILABLE", listResponse.ErrorCode)

	response := subscriptionPlanGuardRequest(t, router, http.MethodPatch, plan.Id, `{"enabled":false}`, "en")
	assert.False(t, response.Success)
	assert.Equal(t, "Subscription plan ownership could not be verified. The update was rejected; please try again later.", response.Message)
	assert.Equal(t, "PLAN_OWNERSHIP_UNAVAILABLE", response.ErrorCode)

	var planAfter model.SubscriptionPlan
	require.NoError(t, db.First(&planAfter, plan.Id).Error)
	assert.True(t, planAfter.Enabled)
}
