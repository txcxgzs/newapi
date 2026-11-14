package controller

import (
	"encoding/json"
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
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
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
	prevMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	common.IsMasterNode = prevMaster

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainType, previousLogType)
	})
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Option{},
		&model.SystemTask{},
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

func TestMerchantCustomGroupReviewAndGroupChangeApproval(t *testing.T) {
	db := setupMerchantControllerTestDB(t)

	prevFetch := *system_setting.GetFetchSetting()
	prevGroupRatio := ratio_setting.GroupRatio2JSONString()
	prevUsableGroups := setting.UserUsableGroups2JSONString()
	prevAutoGroups := setting.AutoGroups2JsonString()
	prevModelRatio := ratio_setting.ModelRatio2JSONString()
	prevCompletionRatio := ratio_setting.CompletionRatio2JSONString()

	t.Cleanup(func() {
		*system_setting.GetFetchSetting() = prevFetch
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(prevGroupRatio))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(prevUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(prevAutoGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(prevModelRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(prevCompletionRatio))
	})

	fetchSetting := system_setting.GetFetchSetting()
	fetchSetting.EnableSSRFProtection = false
	fetchSetting.AllowPrivateIp = true

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.0}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"default"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.6":2.5}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"gpt-5.6":4.0}`))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       201,
		Username: "merchant_alpha_201",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	t.Run("rejects reserved group default", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("id", 201)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/apply", strings.NewReader(
			`{"site_name":"Alpha","site_url":"`+upstream.URL+`","site_key":"sk-alpha","group_name":"default","rate_multiplier":0.5,"models":[{"model":"gpt-5.6"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		PostMerchantApplication(c)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)

		var apps int64
		require.NoError(t, db.Model(&model.MerchantApplication{}).Where("user_id = ?", 201).Count(&apps).Error)
		assert.Zero(t, apps)
	})

	// 1. 商家提交带专属分组（自动加商户名前缀）、自定义模型定价与加入 auto 路由意向的入驻申请
	applyRecorder := httptest.NewRecorder()
	applyCtx, _ := gin.CreateTestContext(applyRecorder)
	applyCtx.Set("id", 201)
	applyCtx.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/apply", strings.NewReader(
		`{"site_name":"Alpha","site_url":"`+upstream.URL+`","site_key":"sk-alpha","group_name":"特惠通道","group_desc":"Alpha VIP Pool","rate_multiplier":0.5,"join_auto_group":true,"models":[{"model":"gpt-5.6"},{"model":"m201/星辰专属","upstream":"gpt-5.6","input_price":4,"output_price":12}]}`))
	applyCtx.Request.Header.Set("Content-Type", "application/json")
	PostMerchantApplication(applyCtx)
	require.Equal(t, http.StatusOK, applyRecorder.Code, applyRecorder.Body.String())

	const prefixedGroup = "Alpha-特惠通道"

	var app model.MerchantApplication
	require.NoError(t, db.Where("user_id = ? AND status = ?", 201, model.MerchantApplicationPending).First(&app).Error)
	assert.Equal(t, model.MerchantAppTypeChannel, app.AppType)
	assert.Equal(t, prefixedGroup, app.GroupName)
	assert.Equal(t, "Alpha VIP Pool", app.GroupDesc)
	assert.True(t, app.JoinAutoGroup)

	// 1b. 其他商家不能重名申请相同 site_name
	require.NoError(t, db.Create(&model.User{
		Id:       202,
		Username: "merchant_beta_202",
		AffCode:  "AFF202",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	dupRecorder := httptest.NewRecorder()
	dupCtx, _ := gin.CreateTestContext(dupRecorder)
	dupCtx.Set("id", 202)
	dupCtx.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/apply", strings.NewReader(
		`{"site_name":"Alpha","site_url":"`+upstream.URL+`","site_key":"sk-beta","group_name":"vip02","rate_multiplier":0.5,"models":[{"model":"gpt-5.6"}]}`))
	dupCtx.Request.Header.Set("Content-Type", "application/json")
	PostMerchantApplication(dupCtx)
	assert.Equal(t, http.StatusBadRequest, dupRecorder.Code)
	assert.Contains(t, dupRecorder.Body.String(), "被其他商家")

	// 2. 管理员审核通过：应同步原生 GroupRatio / UserUsableGroups / AutoGroups / 自定义模型定价并按分组倍率派生盘口
	reviewRecorder := performReviewRequest(t, app.Id, `{"action":"approve","review_note":"ok","pools":["`+prefixedGroup+`"],"group_name":"`+prefixedGroup+`","group_desc":"Alpha VIP Pool","join_auto_group":true,"rate_multiplier":0.5,"profit_ratio":0.95}`)
	require.Equal(t, http.StatusOK, reviewRecorder.Code, reviewRecorder.Body.String())

	var reviewResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ChannelId int    `json:"channel_id"`
			AppType   string `json:"app_type"`
			GroupName string `json:"group_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(reviewRecorder.Body.Bytes(), &reviewResp))
	assert.True(t, reviewResp.Success, reviewRecorder.Body.String())

	assert.True(t, ratio_setting.ContainsGroupRatio(prefixedGroup))
	assert.InDelta(t, 0.5, ratio_setting.GetGroupRatio(prefixedGroup), 1e-9)
	assert.Equal(t, "Alpha VIP Pool", setting.GetUserUsableGroupsCopy()[prefixedGroup])
	assert.True(t, setting.ContainsAutoGroup(prefixedGroup))

	var channel model.Channel
	require.NoError(t, db.First(&channel, reviewResp.Data.ChannelId).Error)
	assert.Equal(t, prefixedGroup, channel.Group)
	require.NotNil(t, channel.ModelMapping)
	assert.Contains(t, *channel.ModelMapping, `"m201/星辰专属":"gpt-5.6"`)

	assert.InDelta(t, 2.0, ratio_setting.GetModelRatioCopy()["m201/星辰专属"], 1e-9)
	assert.InDelta(t, 3.0, ratio_setting.GetCompletionRatioCopy()["m201/星辰专属"], 1e-9)

	var quote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?", channel.Id, prefixedGroup, "gpt-5.6").First(&quote).Error)
	assert.InDelta(t, 1.25, quote.RateMultiplier, 1e-9)
	assert.InDelta(t, 2.5, quote.InputPrice, 1e-9)
	assert.InDelta(t, 10.0, quote.OutputPrice, 1e-9)

	var customQuote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?", channel.Id, prefixedGroup, "m201/星辰专属").First(&customQuote).Error)
	assert.InDelta(t, 2.0, customQuote.InputPrice, 1e-9)
	assert.InDelta(t, 6.0, customQuote.OutputPrice, 1e-9)

	// 2b. 商家在渠道编辑中可直接修改自己 m{ID}/... 自定义模型定价，但不能篡改平台模型或其他商家模型定价
	forbidRecorder := httptest.NewRecorder()
	forbidCtx, _ := gin.CreateTestContext(forbidRecorder)
	forbidCtx.Set("id", 201)
	forbidCtx.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/channel/update", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"models":[{"model":"gpt-5.6","input_price":1,"output_price":2}]}`, channel.Id)))
	forbidCtx.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantChannel(forbidCtx)
	assert.Equal(t, http.StatusBadRequest, forbidRecorder.Code)

	hijackRecorder := httptest.NewRecorder()
	hijackCtx, _ := gin.CreateTestContext(hijackRecorder)
	hijackCtx.Set("id", 201)
	hijackCtx.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/channel/update", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"models":[{"model":"m999/hijack","input_price":1,"output_price":2}]}`, channel.Id)))
	hijackCtx.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantChannel(hijackCtx)
	assert.Equal(t, http.StatusBadRequest, hijackRecorder.Code)

	repriceRecorder := httptest.NewRecorder()
	repriceCtx, _ := gin.CreateTestContext(repriceRecorder)
	repriceCtx.Set("id", 201)
	repriceCtx.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/channel/update", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"models":[{"model":"gpt-5.6"},{"model":"m201/星辰专属","upstream":"gpt-5.6","input_price":6,"output_price":18}]}`, channel.Id)))
	repriceCtx.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantChannel(repriceCtx)
	require.Equal(t, http.StatusOK, repriceRecorder.Code, repriceRecorder.Body.String())

	var repricedQuote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?", channel.Id, prefixedGroup, "m201/星辰专属").First(&repricedQuote).Error)
	assert.InDelta(t, 3.0, repricedQuote.InputPrice, 1e-9)
	assert.InDelta(t, 9.0, repricedQuote.OutputPrice, 1e-9)

	// 3. 商家提交分组倍率 / Auto 路由变更申请（未审核前不得直接生效）
	rateRecorder := httptest.NewRecorder()
	rateCtx, _ := gin.CreateTestContext(rateRecorder)
	rateCtx.Set("id", 201)
	rateCtx.Request = httptest.NewRequest(http.MethodPut, "/api/merchant/portal/rate", strings.NewReader(
		`{"group_name":"`+prefixedGroup+`","group_desc":"Alpha Updated","rate_multiplier":0.8,"join_auto_group":false}`))
	rateCtx.Request.Header.Set("Content-Type", "application/json")
	UpdateMerchantPortalRate(rateCtx)
	require.Equal(t, http.StatusOK, rateRecorder.Code, rateRecorder.Body.String())

	assert.InDelta(t, 0.5, ratio_setting.GetGroupRatio(prefixedGroup), 1e-9)
	assert.True(t, setting.ContainsAutoGroup(prefixedGroup))

	var changeApp model.MerchantApplication
	require.NoError(t, db.Where("user_id = ? AND app_type = ? AND status = ?", 201, model.MerchantAppTypeGroupChange, model.MerchantApplicationPending).First(&changeApp).Error)
	assert.Equal(t, prefixedGroup, changeApp.GroupName)
	assert.InDelta(t, 0.8, changeApp.RateMultiplier, 1e-9)
	assert.False(t, changeApp.JoinAutoGroup)

	// 4. 管理员审核通过 group_change：更新原生配置、退出 AutoGroups 并重算派生盘口
	changeReviewRecorder := performReviewRequest(t, changeApp.Id, `{"action":"approve","review_note":"ok","rate_multiplier":0.8,"group_desc":"Alpha Updated","join_auto_group":false}`)
	require.Equal(t, http.StatusOK, changeReviewRecorder.Code, changeReviewRecorder.Body.String())

	var changeReviewResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(changeReviewRecorder.Body.Bytes(), &changeReviewResp))
	assert.True(t, changeReviewResp.Success, changeReviewRecorder.Body.String())

	assert.InDelta(t, 0.8, ratio_setting.GetGroupRatio(prefixedGroup), 1e-9)
	assert.Equal(t, "Alpha Updated", setting.GetUserUsableGroupsCopy()[prefixedGroup])
	assert.False(t, setting.ContainsAutoGroup(prefixedGroup))

	var updatedQuote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?", channel.Id, prefixedGroup, "gpt-5.6").First(&updatedQuote).Error)
	assert.InDelta(t, 2.0, updatedQuote.RateMultiplier, 1e-9)
	assert.InDelta(t, 4.0, updatedQuote.InputPrice, 1e-9)
	assert.InDelta(t, 16.0, updatedQuote.OutputPrice, 1e-9)
}
