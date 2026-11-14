package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFinanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupMerchantControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
		&model.Redemption{},
		&model.MerchantEarning{},
		&model.MerchantWithdrawal{},
		&model.FinanceLedger{},
	))
	return db
}

func TestMerchantOnboardingEncryptsSiteKeyAndBindsProfile(t *testing.T) {
	db := setupFinanceTestDB(t)
	prevFetch := *system_setting.GetFetchSetting()
	prevModelRatio := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		*system_setting.GetFetchSetting() = prevFetch
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(prevModelRatio))
	})
	fetchSetting := system_setting.GetFetchSetting()
	fetchSetting.EnableSSRFProtection = false
	fetchSetting.AllowPrivateIp = true
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o":1.25}`))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       301,
		Username: "merchant_onboard_301",
		AffCode:  "AFF301",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("id", 301)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/apply", strings.NewReader(
		`{"site_name":"CloudNine","site_url":"`+upstream.URL+`","site_key":"sk-plain-secret-key","contact_name":"Alice","contact_info":"tg:@alice_relay","description":"24/7 SLA","payout_method":"usdt_trc20","payout_account":"TTestAddress999","payout_holder":"Alice Studio","agreed_terms":true,"models":[{"model":"gpt-4o"}]}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	PostMerchantApplication(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var app model.MerchantApplication
	require.NoError(t, db.Where("user_id = ?", 301).First(&app).Error)
	assert.True(t, app.IsOnboarding)
	assert.True(t, app.AgreedTerms)
	assert.True(t, strings.HasPrefix(app.SiteKey, "enc:v1:"), "待审密钥必须静态加密存储")
	assert.NotContains(t, app.SiteKey, "sk-plain-secret-key")
	plain, err := common.DecryptMerchantSecret(app.SiteKey)
	require.NoError(t, err)
	assert.Equal(t, "sk-plain-secret-key", plain)

	// 管理员审核通过：应复制解密密钥给渠道，生成稳定商户短码并落盘联系/结算信息，随后抹除申请密钥
	reviewRec := performReviewRequest(t, app.Id, `{"action":"approve","pools":["default"],"profit_ratio":0.95}`)
	require.Equal(t, http.StatusOK, reviewRec.Code, reviewRec.Body.String())

	var reviewed model.MerchantApplication
	require.NoError(t, db.First(&reviewed, app.Id).Error)
	assert.Empty(t, reviewed.SiteKey)

	profile, err := model.GetMerchantProfileByUserId(301)
	require.NoError(t, err)
	assert.Equal(t, "m301", profile.MerchantCode)
	assert.Equal(t, "Alice", profile.ContactName)
	assert.Equal(t, "tg:@alice_relay", profile.ContactInfo)
	assert.Equal(t, "usdt_trc20", profile.PayoutMethod)
	assert.Equal(t, "TTestAddress999", profile.PayoutAccount)

	var channel model.Channel
	require.NoError(t, db.Where("merchant_id = ?", 301).First(&channel).Error)
	assert.Equal(t, "sk-plain-secret-key", channel.Key)
}

func TestFinanceCenterOverviewReconciliationAndLedgerCSV(t *testing.T) {
	db := setupFinanceTestDB(t)
	now := time.Now().Unix()

	require.NoError(t, db.Create(&model.User{
		Id:       401,
		Username: "buyer_401",
		AffCode:  "AFF401",
		Quota:    1000000,
		UsedQuota: 500000,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	require.NoError(t, db.Create(&model.TopUp{
		UserId:          401,
		Amount:          20,
		Money:           20,
		TradeNo:         "TOPUP-401",
		PaymentMethod:   "alipay",
		PaymentProvider: "epay",
		CreateTime:      now - 100,
		CompleteTime:    now - 50,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	require.NoError(t, db.Create(&model.SubscriptionOrder{
		UserId:          401,
		PlanId:          1,
		Money:           15,
		TradeNo:         "SUB-401",
		PaymentMethod:   "stripe",
		PaymentProvider: "stripe",
		Status:          common.TopUpStatusSuccess,
		CreateTime:      now - 80,
		CompleteTime:    now - 40,
	}).Error)

	require.NoError(t, db.Create(&model.Redemption{
		UserId:      1,
		Key:         "REDEEM-KEY-0001",
		Name:        "promo",
		Status:      common.RedemptionCodeStatusUsed,
		Quota:       500000,
		UsedUserId:  401,
		CreatedTime: now - 200,
	}).Error)

	dedup := "log:9001"
	require.NoError(t, model.RecordMerchantEarning(nil, &model.MerchantEarning{
		MerchantUserId:   401,
		ChannelId:        12,
		LogId:            9001,
		DedupKey:         &dedup,
		GrossAmount:      10,
		Amount:           9.5,
		CommissionAmount: 0.5,
		Status:           model.MerchantEarningSettled,
		ModelName:        "gpt-4o",
	}))
	// 注入一条带公式前缀的备注测试 CSV 防注入
	require.NoError(t, model.RecordFinanceLedger(nil, &model.FinanceLedger{
		LedgerNo:  "test:csv-formula",
		Domain:    model.FinanceDomainUserQuota,
		UserId:    401,
		BizType:   model.FinanceBizTopupCredit,
		Direction: model.FinanceDirIn,
		AmountUSD: 20,
		Money:     20,
		RefType:   "topup",
		RefId:     "TOPUP-401",
		Remark:    "=CMD|'/C calc'!A0",
		CreatedAt: now,
	}))

	// 1. 财务总览
	ovRec := httptest.NewRecorder()
	ovCtx, _ := gin.CreateTestContext(ovRec)
	ovCtx.Request = httptest.NewRequest(http.MethodGet, "/api/finance/overview", nil)
	GetFinanceOverview(ovCtx)
	require.Equal(t, http.StatusOK, ovRec.Code, ovRec.Body.String())

	var ovResp struct {
		Success bool `json:"success"`
		Data    struct {
			TopupStats struct {
				TotalMoney float64 `json:"total_money"`
			} `json:"topup_stats"`
			SubscriptionStats struct {
				TotalRevenue float64 `json:"total_revenue"`
			} `json:"subscription_stats"`
			MerchantStats struct {
				TotalCommission float64 `json:"total_commission"`
				AvailableBalance float64 `json:"available_balance"`
			} `json:"merchant_stats"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ovRec.Body.Bytes(), &ovResp))
	assert.True(t, ovResp.Success)
	assert.InDelta(t, 20.0, ovResp.Data.TopupStats.TotalMoney, 1e-6)
	assert.InDelta(t, 15.0, ovResp.Data.SubscriptionStats.TotalRevenue, 1e-6)
	assert.InDelta(t, 0.5, ovResp.Data.MerchantStats.TotalCommission, 1e-6)
	assert.InDelta(t, 9.5, ovResp.Data.MerchantStats.AvailableBalance, 1e-6)

	// 2. 平账巡检
	recRec := httptest.NewRecorder()
	recCtx, _ := gin.CreateTestContext(recRec)
	recCtx.Request = httptest.NewRequest(http.MethodGet, "/api/finance/reconcile", nil)
	GetFinanceReconciliation(recCtx)
	require.Equal(t, http.StatusOK, recRec.Code)
	assert.Contains(t, recRec.Body.String(), `"healthy":true`)
	assert.Contains(t, recRec.Body.String(), `"wallet_mismatch_count":0`)

	// 3. CSV 导出公式注入防护
	csvRec := httptest.NewRecorder()
	csvCtx, _ := gin.CreateTestContext(csvRec)
	csvCtx.Request = httptest.NewRequest(http.MethodGet, "/api/finance/ledger/export", nil)
	ExportFinanceLedgersCSV(csvCtx)
	require.Equal(t, http.StatusOK, csvRec.Code)
	csvBody := csvRec.Body.String()
	assert.Contains(t, csvBody, "'=CMD|'/C calc'!A0")
}
