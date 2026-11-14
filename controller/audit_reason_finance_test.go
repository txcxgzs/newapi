package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerchantQualificationOnboardingProfileChangeAndWithdrawalQRClosedLoop(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.MerchantWithdrawal{}, &model.FinanceLedger{}))

	applicant := model.User{
		Id:          701,
		Username:    "merchant701",
		DisplayName: "Merchant701",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(&applicant).Error)

	const sampleQR = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

	// 1. Submit pure qualification onboarding (no site_url / site_key yet)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/apply", strings.NewReader(fmt.Sprintf(
		`{"shop_name":"云枢旗舰店","platform_url":"https://yunshu.example.com","operation_tenure":"3年以上","payout_method":"alipay","payout_account":"ys@pay.com","payout_holder":"张三","payout_qrcode":%q,"contact_name":"张三","contact_info":"wx:ys","description":"全栈稳定算力平台","agreed_terms":true}`,
		sampleQR,
	)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", applicant.Id)
	PostMerchantApplication(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var applyResp struct {
		Success bool                      `json:"success"`
		Message string                    `json:"message"`
		Data    model.MerchantApplication `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &applyResp))
	require.True(t, applyResp.Success, w.Body.String())
	assert.Equal(t, "云枢旗舰店", applyResp.Data.ShopName)
	assert.Equal(t, "https://yunshu.example.com", applyResp.Data.PlatformUrl)
	assert.Equal(t, "3年以上", applyResp.Data.OperationTenure)
	assert.Equal(t, sampleQR, applyResp.Data.PayoutQrcode)
	assert.True(t, applyResp.Data.IsOnboarding)

	// 2. Admin approves qualification-only onboarding without needing channel pools
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(applyResp.Data.Id)}}
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/merchant_manage/applications/%d/review", applyResp.Data.Id),
		strings.NewReader(`{"action":"approve","profit_ratio":0.95,"review_note":"资质核验通过"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	ReviewMerchantApplication(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var reviewResp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reviewResp))
	require.True(t, reviewResp.Success, w.Body.String())
	assert.Equal(t, true, reviewResp.Data["qualification_only"])
	assert.Equal(t, "云枢旗舰店", reviewResp.Data["shop_name"])

	profile, err := model.GetMerchantProfileByUserId(applicant.Id)
	require.NoError(t, err)
	assert.Equal(t, "云枢旗舰店", profile.ShopName)
	assert.Equal(t, sampleQR, profile.PayoutQrcode)

	var updatedUser model.User
	require.NoError(t, db.First(&updatedUser, applicant.Id).Error)
	assert.Equal(t, common.RoleMerchant, updatedUser.Role)

	// 3. Merchant submits shop name change application (requires review)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/apply", strings.NewReader(
		`{"app_type":"profile_change","requested_shop_name":"云枢优选店","description":"品牌升级更名"}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", applicant.Id)
	PostMerchantApplication(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var nameAppResp struct {
		Success bool                      `json:"success"`
		Data    model.MerchantApplication `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &nameAppResp))
	require.True(t, nameAppResp.Success, w.Body.String())
	assert.Equal(t, model.MerchantAppTypeProfileChange, nameAppResp.Data.AppType)
	assert.Equal(t, "云枢优选店", nameAppResp.Data.RequestedShopName)

	// Admin approves shop name change
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(nameAppResp.Data.Id)}}
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/merchant_manage/applications/%d/review", nameAppResp.Data.Id),
		strings.NewReader(`{"action":"approve","review_note":"同意更名"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	ReviewMerchantApplication(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	profile, err = model.GetMerchantProfileByUserId(applicant.Id)
	require.NoError(t, err)
	assert.Equal(t, "云枢优选店", profile.ShopName)

	// 4. Withdrawal snapshots payout_qrcode and Finance approves -> pays closed loop
	require.NoError(t, db.Model(&model.MerchantWallet{}).Where("merchant_user_id = ?", applicant.Id).Update("balance", 120.0).Error)

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/merchant/portal/withdrawals", strings.NewReader(`{"amount":50,"note":"月度结算"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", applicant.Id)
	PostMerchantWithdrawal(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var withdrawResp struct {
		Success bool                     `json:"success"`
		Data    model.MerchantWithdrawal `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &withdrawResp))
	require.True(t, withdrawResp.Success, w.Body.String())
	assert.Equal(t, sampleQR, withdrawResp.Data.PayoutQrcode)
	assert.Equal(t, model.WithdrawalPending, withdrawResp.Data.Status)

	// Finance list returns shop_name + payout_qrcode
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/finance/withdrawals", nil)
	c.Set("id", 1)
	AdminGetMerchantWithdrawals(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "云枢优选店")
	assert.Contains(t, w.Body.String(), sampleQR)

	// Approve -> Paid with proof
	for _, payload := range []string{
		`{"action":"approve","review_note":"核对收款码无误"}`,
		`{"action":"paid","paid_proof":"TXN-20261114-9988","review_note":"已扫码打款"}`,
	} {
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(withdrawResp.Data.Id)}}
		c.Request = httptest.NewRequest(http.MethodPost, "/api/finance/withdrawals/review", strings.NewReader(payload))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", 802)
		ReviewMerchantWithdrawal(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	var finalW model.MerchantWithdrawal
	require.NoError(t, db.First(&finalW, withdrawResp.Data.Id).Error)
	assert.Equal(t, model.WithdrawalPaid, finalW.Status)
	assert.Equal(t, "TXN-20261114-9988", finalW.PaidProof)
}

func TestFinanceOfficerIsolationAndAuditReasonMiddleware(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	prevMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = prevMaster })
	require.NoError(t, db.AutoMigrate(&model.AuthzRole{}, &model.CasbinRule{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.AuditLog{}))
	require.NoError(t, authz.Init(db))

	financeUser := model.User{Id: 810, Username: "finance810", AffCode: "aff810", Role: common.RoleFinanceUser, Status: common.UserStatusEnabled}
	adminUser := model.User{Id: 811, Username: "admin811", AffCode: "aff811", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	rootUser := model.User{Id: 812, Username: "root812", AffCode: "aff812", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&financeUser).Error)
	require.NoError(t, db.Create(&adminUser).Error)
	require.NoError(t, db.Create(&rootUser).Error)

	// 1. Finance officer is isolated to finance permissions only
	assert.True(t, authz.Can(financeUser.Id, common.RoleFinanceUser, authz.FinanceRead))
	assert.True(t, authz.Can(financeUser.Id, common.RoleFinanceUser, authz.FinanceSettle))
	assert.True(t, authz.Can(financeUser.Id, common.RoleFinanceUser, authz.FinancePayout))
	assert.False(t, authz.Can(financeUser.Id, common.RoleFinanceUser, authz.ChannelRead))
	assert.False(t, authz.Can(financeUser.Id, common.RoleFinanceUser, authz.UserRead))
	assert.False(t, authz.Can(financeUser.Id, common.RoleFinanceUser, authz.MerchantReview))

	// 2. RequireAuditReason enforces X-Audit-Reason on GET detail views for admins and logs reason
	r := gin.New()
	r.GET("/api/channel/:id",
		func(c *gin.Context) {
			if uid := c.GetHeader("X-Test-Uid"); uid == "root" {
				c.Set("id", rootUser.Id)
				c.Set("username", rootUser.Username)
				c.Set("role", rootUser.Role)
			} else {
				c.Set("id", adminUser.Id)
				c.Set("username", adminUser.Username)
				c.Set("role", adminUser.Role)
			}
			c.Next()
		},
		middleware.RequireAuditReason(authz.ChannelSecretView, middleware.DefaultAuditReasonMinLength),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": true}) },
	)

	// Too short -> 400 AUDIT_REASON_REQUIRED
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/channel/42", nil)
	req.Header.Set("X-Audit-Reason", "too short")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "AUDIT_REASON_REQUIRED")

	// Valid >= 15 runes -> 200 and recorded in AuditLog
	longReason := "排查渠道 42 上游返回异常原因，需核对接口配置与状态"
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/channel/42", nil)
	req.Header.Set("X-Audit-Reason", longReason)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var logs []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&logs).Error)
	require.NotEmpty(t, logs)
	assert.Equal(t, longReason, logs[len(logs)-1].Other.AuditReason)

	// Root user exempt
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/channel/42", nil)
	req.Header.Set("X-Test-Uid", "root")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 3. Per-user AuditReasonRules override can disable requirement for ChannelSecretView
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", adminUser.Id).
		Update("audit_reason_rules", `{"channel":{"secret_view":false}}`).Error)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/channel/42", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
