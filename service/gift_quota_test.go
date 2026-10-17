package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type giftBillingFixture struct {
	user  *model.User
	token *model.Token
	ctx   *gin.Context
	info  *relaycommon.RelayInfo
}

func seedGiftBilling(t *testing.T, userQuota, giftQuota int) *giftBillingFixture {
	t.Helper()
	truncate(t)

	user := model.User{Username: "gift-billing-user", Quota: userQuota, GiftQuota: giftQuota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: fmt.Sprintf("gift-billing-%d", user.Id), Name: "gift-billing", RemainQuota: 2_000_000, Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(&token).Error)
	channel := model.Channel{Name: "gift-billing", Key: "unused", Status: common.ChannelStatusEnabled, UseGiftQuota: common.GetPointer(true)}
	require.NoError(t, model.DB.Create(&channel).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)

	info := &relaycommon.RelayInfo{
		UserId:          user.Id,
		TokenId:         token.Id,
		TokenKey:        token.Key,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channel.Id},
		OriginModelName: "gift-test",
		UsingGroup:      "default",
		UserGroup:       "default",
		UserSetting:     kitdto.UserSetting{BillingPreference: "wallet_first"},
		StartTime:       time.Now(),
	}
	return &giftBillingFixture{user: &user, token: &token, ctx: ctx, info: info}
}

func (f *giftBillingFixture) reloadToken(t *testing.T) *model.Token {
	t.Helper()
	token := &model.Token{}
	require.NoError(t, model.DB.First(token, f.token.Id).Error)
	return token
}

func TestGiftChannelBillingSessionIgnoresWalletPreference(t *testing.T) {
	fixture := seedGiftBilling(t, 5_000_000, 1_000)

	apiErr := PreConsumeBilling(fixture.ctx, 1000, fixture.info)
	require.Nil(t, apiErr)
	require.NotNil(t, fixture.info.Billing)
	assert.Equal(t, BillingSourceGift, fixture.info.BillingSource)
	assert.Equal(t, 1000, fixture.info.Billing.GetPreConsumedQuota())
	assert.Equal(t, 1000, fixture.info.FinalPreConsumedQuota)

	gift, err := model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Zero(t, gift)
	wallet, err := model.GetUserQuota(fixture.user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 5_000_000, wallet, "gift funding must not touch the wallet")
	token := fixture.reloadToken(t)
	assert.Equal(t, 1_999_000, token.RemainQuota)
}

func TestGiftChannelInsufficientGiftNeverFallsBackToWallet(t *testing.T) {
	fixture := seedGiftBilling(t, 5_000_000, 1)

	apiErr := PreConsumeBilling(fixture.ctx, 1000, fixture.info)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Nil(t, fixture.info.Billing, "a failed pre-consume must not publish a billing session")

	gift, err := model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, gift)
	wallet, err := model.GetUserQuota(fixture.user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 5_000_000, wallet, "the wallet preference must not rescue an insufficient gift balance")
	token := fixture.reloadToken(t)
	assert.Equal(t, 2_000_000, token.RemainQuota)
}

func TestGiftBillingSessionSettlesAndRefundsAgainstGiftLedger(t *testing.T) {
	fixture := seedGiftBilling(t, 5_000_000, 10_000)

	apiErr := PreConsumeBilling(fixture.ctx, 1000, fixture.info)
	require.Nil(t, apiErr)

	require.NoError(t, fixture.info.Billing.Settle(1500))
	gift, err := model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 8_500, gift)
	wallet, err := model.GetUserQuota(fixture.user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 5_000_000, wallet)

	require.NoError(t, fixture.info.Billing.Settle(1500), "a repeated settlement must not charge again")
	gift, err = model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 8_500, gift)
	assert.False(t, fixture.info.Billing.NeedsRefund())
	token := fixture.reloadToken(t)
	assert.Equal(t, 1_998_500, token.RemainQuota)
}

func TestGiftBillingSessionRefundReturnsPreConsumeOnly(t *testing.T) {
	fixture := seedGiftBilling(t, 5_000_000, 10_000)

	apiErr := PreConsumeBilling(fixture.ctx, 1000, fixture.info)
	require.Nil(t, apiErr)
	require.True(t, fixture.info.Billing.NeedsRefund())

	refunded := make(chan struct{}, 1)
	callback := "gift_billing_refund_observed"
	require.NoError(t, model.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" && tx.Error == nil {
			select {
			case refunded <- struct{}{}:
			default:
			}
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })

	fixture.info.Billing.Refund(fixture.ctx)
	fixture.info.Billing.Refund(fixture.ctx)
	select {
	case <-refunded:
	case <-time.After(5 * time.Second):
		t.Fatal("refund did not finish")
	}

	assert.False(t, fixture.info.Billing.NeedsRefund())
	gift, err := model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10_000, gift)
	token := fixture.reloadToken(t)
	assert.Equal(t, 2_000_000, token.RemainQuota)
}

func TestGiftBillingSessionReserveRefundsAllPreConsumedDeltas(t *testing.T) {
	fixture := seedGiftBilling(t, 5_000_000, 10_000)

	apiErr := PreConsumeBilling(fixture.ctx, 1000, fixture.info)
	require.Nil(t, apiErr)

	fixture.info.ImageRequestCount = 1
	require.NoError(t, fixture.info.Billing.Reserve(1500))
	assert.Equal(t, 1500, fixture.info.Billing.GetPreConsumedQuota())

	gift, err := model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 8_500, gift)
	token := fixture.reloadToken(t)
	assert.Equal(t, 1_998_500, token.RemainQuota)

	refunded := make(chan struct{}, 1)
	callback := "gift_billing_reserve_refund_observed"
	require.NoError(t, model.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" && tx.Error == nil {
			select {
			case refunded <- struct{}{}:
			default:
			}
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })

	fixture.info.Billing.Refund(fixture.ctx)
	select {
	case <-refunded:
	case <-time.After(5 * time.Second):
		t.Fatal("refund did not finish")
	}

	gift, err = model.GetUserGiftQuota(fixture.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10_000, gift, "refund must return every pre-consumed delta")
	token = fixture.reloadToken(t)
	assert.Equal(t, 2_000_000, token.RemainQuota)
}

func TestMidjourneySubmitQuotaCheckUsesResolvedFundingSource(t *testing.T) {
	truncate(t)

	user := model.User{Username: "mj-check-user", Quota: 10_000, GiftQuota: 5_000, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	giftChannel := model.Channel{Name: "mj-gift-check", Key: "unused", Status: common.ChannelStatusEnabled, UseGiftQuota: common.GetPointer(true)}
	plainChannel := model.Channel{Name: "mj-plain-check", Key: "unused", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(&giftChannel).Error)
	require.NoError(t, model.DB.Create(&plainChannel).Error)

	giftInfo := &relaycommon.RelayInfo{UserId: user.Id, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: giftChannel.Id}}
	enough, err := CheckMidjourneySubmitQuota(giftInfo, 4_000)
	require.NoError(t, err)
	assert.True(t, enough)

	enough, err = CheckMidjourneySubmitQuota(giftInfo, 8_000)
	require.NoError(t, err)
	assert.False(t, enough, "an affordable wallet must not rescue an insufficient gift balance on gift channels")

	plainInfo := &relaycommon.RelayInfo{UserId: user.Id, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: plainChannel.Id}}
	enough, err = CheckMidjourneySubmitQuota(plainInfo, 9_000)
	require.NoError(t, err)
	assert.True(t, enough)

	enough, err = CheckMidjourneySubmitQuota(plainInfo, 11_000)
	require.NoError(t, err)
	assert.False(t, enough)
}

func TestShouldTrustNeverBypassesGiftFunding(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 1, TokenUnlimited: true, UserQuota: int(100 * common.QuotaPerUnit)}

	wallet := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 1}}
	assert.True(t, wallet.shouldTrust(ctx), "wallet funding still trusts rich users")

	gift := &BillingSession{relayInfo: info, funding: &GiftFunding{userId: 1}}
	assert.False(t, gift.shouldTrust(ctx), "gift funding must never bypass pre-consume")
}

func TestEnsureGiftBillingFilterIsIdempotentAndSourceScoped(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	EnsureGiftBillingFilter(ctx, BillingSourceGift)
	EnsureGiftBillingFilter(ctx, BillingSourceWallet)
	filters := GetChannelConstraints(ctx).Filters
	require.Len(t, filters, 1, "repeated registration must not duplicate the filter")
	assert.Equal(t, dto.FilterGiftBilling, filters[0].Kind)
	require.NotNil(t, filters[0].GiftBilling)
	assert.True(t, *filters[0].GiftBilling, "the first registration wins and pins gift-only retries")

	walletCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	EnsureGiftBillingFilter(walletCtx, BillingSourceWallet)
	walletFilters := GetChannelConstraints(walletCtx).Filters
	require.Len(t, walletFilters, 1)
	require.NotNil(t, walletFilters[0].GiftBilling)
	assert.False(t, *walletFilters[0].GiftBilling, "wallet sessions must pin non-gift retries")

	assert.NotPanics(t, func() { EnsureGiftBillingFilter(nil, BillingSourceGift) })
}

func TestMidjourneyGiftBillingDetectionAndRefund(t *testing.T) {
	truncate(t)

	user := model.User{Username: "mj-gift-user", Quota: 5_000, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	giftChannel := model.Channel{Name: "mj-gift", Key: "unused", Status: common.ChannelStatusEnabled, UseGiftQuota: common.GetPointer(true)}
	plainChannel := model.Channel{Name: "mj-plain", Key: "unused", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(&giftChannel).Error)
	require.NoError(t, model.DB.Create(&plainChannel).Error)

	giftTask := &model.Midjourney{MjId: "mj-gift-1", UserId: user.Id, ChannelId: giftChannel.Id, Action: "IMAGINE", Status: "SUBMITTED", Progress: "0%"}
	prepared, err := PrepareMidjourneyTaskBilling(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: giftChannel.Id}}, giftTask, 100, true)
	require.NoError(t, err)
	assert.True(t, prepared)
	assert.Equal(t, BillingSourceGift, giftTask.BillingSource)

	plainTask := &model.Midjourney{MjId: "mj-plain-1", UserId: user.Id, ChannelId: plainChannel.Id, Action: "IMAGINE", Status: "SUBMITTED", Progress: "0%"}
	prepared, err = PrepareMidjourneyTaskBilling(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: plainChannel.Id}}, plainTask, 100, true)
	require.NoError(t, err)
	assert.True(t, prepared)
	assert.Empty(t, plainTask.BillingSource)

	require.NoError(t, model.DB.Create(giftTask).Error)
	require.True(t, RefundMidjourneyQuota(context.Background(), giftTask, "unit-test"))

	gift, err := model.GetUserGiftQuota(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 100, gift, "gift-billed task refunds to the gift ledger")
	wallet, err := model.GetUserQuota(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 5_000, wallet, "gift-billed task refund must not touch the wallet")
	assert.Zero(t, giftTask.Quota)

	// 退款日志必须标记资金来源，前端成本列才能看出退款回到了哪里
	refundLog := getLastLog(t)
	require.NotNil(t, refundLog)
	assert.Equal(t, model.LogTypeRefund, refundLog.Type)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(refundLog.Other, &other))
	assert.Equal(t, BillingSourceGift, other["billing_source"])
	assert.Equal(t, "unit-test", other["reason"])
}

func TestDecreaseGiftQuotaReportsMissingUser(t *testing.T) {
	truncate(t)

	err := model.DecreaseGiftQuota(999_999, 100)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "扣减不存在的用户必须暴露，否则扣费静默成功")

	user := model.User{Username: "gift-decrease", Quota: 500, GiftQuota: 500, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DecreaseGiftQuota(user.Id, 100))

	gift, err := model.GetUserGiftQuota(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 400, gift)
}
