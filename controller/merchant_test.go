package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupMerchantControllerTestDB 为商家控制器用例准备隔离的 sqlite 数据库，
// 覆盖商家资料/申请/渠道/盘口/路由索引等表。
func setupMerchantControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	dialect := os.Getenv("TEST_MERCHANT_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	databaseTypes := map[string]common.DatabaseType{
		"sqlite": common.DatabaseTypeSQLite, "mysql": common.DatabaseTypeMySQL, "postgres": common.DatabaseTypePostgreSQL,
	}
	require.Contains(t, databaseTypes, dialect)
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	db, _ := newAuditTestDatabase(t, dialect, dsn)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetDatabaseTypes(databaseTypes[dialect], databaseTypes[dialect])

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainType, previousLogType)
	})
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Channel{},
		&model.Ability{},
		&model.ChannelQuote{},
		&model.MerchantProfile{},
		&model.MerchantApplication{},
		&model.MerchantWallet{},
	))
	return db
}

// seedActiveMerchant 创建状态为激活的商家资料及其渠道。
func seedActiveMerchant(t *testing.T, db *gorm.DB, userId int, models string) *model.Channel {
	t.Helper()
	profile := model.MerchantProfile{
		UserId:   userId,
		SiteName: "merchant-test",
		SiteUrl:  "https://example.com",
		Status:   model.MerchantStatusActive,
	}
	require.NoError(t, db.Create(&profile).Error)
	channel := model.Channel{
		Type:       constant.ChannelTypeOpenAI,
		Key:        "sk-merchant-test",
		Name:       "商家:merchant-test",
		Status:     common.ChannelStatusEnabled,
		Group:      "default",
		Models:     models,
		MerchantId: userId,
	}
	require.NoError(t, db.Create(&channel).Error)
	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	require.Equal(t, userId, persisted.MerchantId, "channel merchant_id must persist")
	return &persisted
}

// TestUpdateMerchantChannelRejectsUnpricedModel 回归：编辑渠道时逐模型校验，
// 平台没定价的模型必须被拒绝，否则客户请求会撞"价格未配置"。
func TestUpdateMerchantChannelRejectsUnpricedModel(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	const userId = 7
	channel := seedActiveMerchant(t, db, userId, "old-model")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", userId)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/channel/update", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"models":[{"model":"merchant-unpriced-model"}]}`, channel.Id)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantChannel(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "模型未在平台配置价格")

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	assert.Equal(t, "old-model", persisted.Models, "校验失败时不得改动渠道模型清单")
}

// TestUpdateMerchantChannelRejectsCommaInModelName 回归：模型名单逗号拼接存储，
// 含逗号的名字等于绕过逐模型校验，必须拒绝。
func TestUpdateMerchantChannelRejectsCommaInModelName(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	const userId = 8
	channel := seedActiveMerchant(t, db, userId, "old-model")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", userId)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/channel/update", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"models":[{"model":"a,evil-model"}]}`, channel.Id)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantChannel(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "模型名不能包含逗号或空白字符")
}

// TestUpdateMerchantChannelAcceptsPricedModel 证明平台已配置倍率的模型能正常
// 上架：渠道模型清单更新，且派生盘口重建出对应的 derived 报价行。
func TestUpdateMerchantChannelAcceptsPricedModel(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	const userId = 9
	channel := seedActiveMerchant(t, db, userId, "old-model")

	previousRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"merchant-priced-model":1}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateModelRatioByJSONString(previousRatios) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", userId)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/channel/update", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"models":[{"model":"merchant-priced-model"}]}`, channel.Id)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantChannel(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	assert.Equal(t, "merchant-priced-model", persisted.Models)

	var quotes []model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND model_name = ?", channel.Id, "merchant-priced-model").Find(&quotes).Error)
	require.NotEmpty(t, quotes)
	assert.Equal(t, model.QuoteSourceDerived, quotes[0].Source, "模型变更后必须重建派生盘口")
}

func testReviewApplication(t *testing.T, db *gorm.DB, userId int, rateMultiplier float64, sites ...string) *model.MerchantApplication {
	t.Helper()
	siteUrl := "https://example.com"
	if len(sites) > 0 && sites[0] != "" {
		siteUrl = sites[0]
	}
	app := &model.MerchantApplication{
		UserId:         userId,
		SiteName:       "merchant-test",
		SiteUrl:        siteUrl,
		SiteKey:        "sk-application",
		Models:         `[{"model":"gpt-5.6"}]`,
		RateMultiplier: rateMultiplier,
		Status:         model.MerchantApplicationPending,
	}
	require.NoError(t, db.Create(app).Error)
	return app
}

func performReviewRequest(t *testing.T, appId int, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 1)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", appId)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/application/"+fmt.Sprintf("%d", appId)+"/review", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	ReviewMerchantApplication(c)
	return recorder
}

// TestReviewMerchantApplicationRejectsRateMultiplierAboveLimit 回归：申请声明
// 或管理员审核时填写的渠道倍率都不能超过上限，防止天价盘口进渠道。
func TestReviewMerchantApplicationRejectsRateMultiplierAboveLimit(t *testing.T) {
	db := setupMerchantControllerTestDB(t)

	t.Run("applicant-declared", func(t *testing.T) {
		app := testReviewApplication(t, db, 10, 150)
		recorder := performReviewRequest(t, app.Id, `{"action":"approve","pools":["default"]}`)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "超过 100")
	})

	t.Run("admin-override", func(t *testing.T) {
		app := testReviewApplication(t, db, 11, 1.0)
		recorder := performReviewRequest(t, app.Id, `{"action":"approve","pools":["default"],"rate_multiplier":150}`)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "不能超过 100")
	})
}

// TestReviewMerchantApplicationRejectsUnknownPool 回归：审核勾选的号池必须存在，
// 否则渠道挂进"死池"，没有用户组能匹配到它。
func TestReviewMerchantApplicationRejectsUnknownPool(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	app := testReviewApplication(t, db, 12, 1.0)

	recorder := performReviewRequest(t, app.Id, `{"action":"approve","pools":["ghost-pool"]}`)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "号池（分组）不存在")
}

// TestReviewMerchantApplicationApproveRequiresPool 回归：通过审核必须勾选至少一个号池。
func TestReviewMerchantApplicationApproveRequiresPool(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	app := testReviewApplication(t, db, 13, 1.0)

	recorder := performReviewRequest(t, app.Id, `{"action":"approve"}`)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "至少一个号池")
}

// TestPostMerchantApplicationRejectsOverLimitRateMultiplier 回归：提交申请时
// 渠道倍率超限必须拒绝，申报入口就该拦住。
func TestPostMerchantApplicationRejectsOverLimitRateMultiplier(t *testing.T) {
	db := setupMerchantControllerTestDB(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 14)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/application", strings.NewReader(
		`{"site_name":"x","site_url":"https://example.com","site_key":"sk","rate_multiplier":150}`))
	c.Request.Header.Set("Content-Type", "application/json")
	PostMerchantApplication(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "不能超过 100")

	var apps int64
	require.NoError(t, db.Model(&model.MerchantApplication{}).Count(&apps).Error)
	assert.Zero(t, apps, "倍率校验失败时不得落库申请")
}
