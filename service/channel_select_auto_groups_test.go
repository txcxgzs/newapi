package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSelectAutoGroupsTest(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRetryTimes := common.RetryTimes
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalMaxTokenAutoGroups := setting.GetMaxTokenAutoGroups()
	originalRoutingStrategy := setting.GetAutoRoutingStrategy()
	originalIncludeMerchants := setting.GetAutoIncludeMerchantGroups()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ChannelQuote{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.RetryTimes = 0

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyOrder))
	setting.SetAutoIncludeMerchantGroups(false)
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))

	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RetryTimes = originalRetryTimes
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateAutoRoutingStrategy(originalRoutingStrategy))
		setting.SetAutoIncludeMerchantGroups(originalIncludeMerchants)
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMaxTokenAutoGroups)))

		if originalMemoryCacheEnabled && originalDB != nil &&
			originalDB.Migrator().HasTable(&model.Channel{}) && originalDB.Migrator().HasTable(&model.Ability{}) {
			model.InitChannelCache()
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	return db
}

func createChannelSelectAutoGroupsChannel(t *testing.T, db *gorm.DB, id int, group, modelName string) {
	t.Helper()
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Models:   modelName,
		Group:    group,
		Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestCacheGetRandomSatisfiedChannelUsesTokenAutoGroupsWhenGlobalAutoIsEmpty(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-groups-runtime-model"
	createChannelSelectAutoGroupsChannel(t, db, 2101, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2102, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)

	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}

	first, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 2101, first.Id)
	assert.Equal(t, "vip", selectedGroup)
	assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
	assert.Empty(t, setting.GetAutoGroups(), "the selection must not depend on the global Auto list")

	param.IncreaseRetry()
	second, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, 2102, second.Id)
	assert.Equal(t, "default", selectedGroup)
	assert.Equal(t, "default", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
}

func createAutoRoutingTestChannel(t *testing.T, db *gorm.DB, id int, merchantId int, group, modelName string) {
	t.Helper()
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:         id,
		MerchantId: merchantId,
		Type:       constant.ChannelTypeOpenAI,
		Key:        fmt.Sprintf("key-%d", id),
		Status:     common.ChannelStatusEnabled,
		Name:       fmt.Sprintf("channel-%d", id),
		Weight:     &weight,
		Models:     modelName,
		Group:      group,
		Priority:   &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestCacheGetRandomSatisfiedChannelAutoRoutingRoundRobin(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-rr-model"
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyRoundRobin))
	autoRoundRobinCounter.Store(0)

	createAutoRoutingTestChannel(t, db, 2201, 0, "default", modelName)
	createAutoRoutingTestChannel(t, db, 2202, 0, "vip", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)

	newParam := func() (*gin.Context, *RetryParam) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
		retry := 0
		return ctx, &RetryParam{
			Ctx:         ctx,
			TokenGroup:  "auto",
			ModelName:   modelName,
			RequestPath: "/v1/chat/completions",
			Retry:       &retry,
		}
	}

	ctx1, p1 := newParam()
	ch1, g1, err := CacheGetRandomSatisfiedChannel(p1)
	require.NoError(t, err)
	require.NotNil(t, ch1)
	assert.Equal(t, "default", g1)
	assert.Equal(t, 2201, ch1.Id)

	// Cross-group retry within the same request must continue on the same request's rotated order.
	p1.IncreaseRetry()
	ch1Retry, g1Retry, err := CacheGetRandomSatisfiedChannel(p1)
	require.NoError(t, err)
	require.NotNil(t, ch1Retry)
	assert.Equal(t, "vip", g1Retry)
	assert.Equal(t, 2202, ch1Retry.Id)
	_ = ctx1

	// Next request advances round-robin offset to vip -> default.
	_, p2 := newParam()
	ch2, g2, err := CacheGetRandomSatisfiedChannel(p2)
	require.NoError(t, err)
	require.NotNil(t, ch2)
	assert.Equal(t, "vip", g2)
	assert.Equal(t, 2202, ch2.Id)

	p2.IncreaseRetry()
	ch2Retry, g2Retry, err := CacheGetRandomSatisfiedChannel(p2)
	require.NoError(t, err)
	require.NotNil(t, ch2Retry)
	assert.Equal(t, "default", g2Retry)
	assert.Equal(t, 2201, ch2Retry.Id)
}

func TestCacheGetRandomSatisfiedChannelAutoRoutingLowestPrice(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-lowest-price-model"
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","m-cheap":"Merchant Cheap"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.5,"vip":2.0,"m-cheap":1.2}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default","m-cheap"]`))
	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyLowestPrice))

	createAutoRoutingTestChannel(t, db, 2301, 0, "vip", modelName)
	createAutoRoutingTestChannel(t, db, 2302, 0, "default", modelName)
	createAutoRoutingTestChannel(t, db, 2303, 5, "m-cheap", modelName)
	model.InitChannelCache()

	// Without quotes, m-cheap has lowest GroupRatio (1.2 < 1.5 < 2.0).
	gin.SetMode(gin.TestMode)
	ctx1, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx1, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx1, constant.ContextKeyTokenCrossGroupRetry, true)
	r1 := 0
	p1 := &RetryParam{Ctx: ctx1, TokenGroup: "auto", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: &r1}
	ch, selectedGroup, err := CacheGetRandomSatisfiedChannel(p1)
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, "m-cheap", selectedGroup)
	assert.Equal(t, 2303, ch.Id)

	// When explicit ChannelQuote makes "vip" cheaper than "m-cheap", lowest_price follows ChannelQuote.
	require.NoError(t, model.UpsertChannelQuotes([]model.ChannelQuote{
		{ChannelId: 2301, PoolGroup: "vip", ModelName: modelName, InputPrice: 0.4, OutputPrice: 0.6, Source: model.QuoteSourceManual, Status: model.QuoteStatusEnabled},
		{ChannelId: 2302, PoolGroup: "default", ModelName: modelName, InputPrice: 1.5, OutputPrice: 2.0, Source: model.QuoteSourceDerived, Status: model.QuoteStatusEnabled},
		{ChannelId: 2303, PoolGroup: "m-cheap", ModelName: modelName, InputPrice: 1.0, OutputPrice: 1.4, Source: model.QuoteSourceDerived, Status: model.QuoteStatusEnabled},
	}))

	ctx2, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx2, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx2, constant.ContextKeyTokenCrossGroupRetry, true)
	r2 := 0
	p2 := &RetryParam{Ctx: ctx2, TokenGroup: "auto", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: &r2}
	ch2, g2, err := CacheGetRandomSatisfiedChannel(p2)
	require.NoError(t, err)
	require.NotNil(t, ch2)
	assert.Equal(t, "vip", g2)
	assert.Equal(t, 2301, ch2.Id)

	p2.IncreaseRetry()
	ch2Next, g2Next, err := CacheGetRandomSatisfiedChannel(p2)
	require.NoError(t, err)
	require.NotNil(t, ch2Next)
	assert.Equal(t, "m-cheap", g2Next)
	assert.Equal(t, 2303, ch2Next.Id)
}

func TestCacheGetRandomSatisfiedChannelAutoRoutingMerchantFirstAndAutoInclude(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-merchant-first-model"
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","m-alpha":"Merchant Alpha","m-beta":"Merchant Beta"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.0,"m-alpha":1.0,"m-beta":1.0}`))
	// Only "default" is explicitly listed in AutoGroups; merchant groups must be pulled in by AutoIncludeMerchantGroups.
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))
	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyMerchantFirst))
	setting.SetAutoIncludeMerchantGroups(true)

	createAutoRoutingTestChannel(t, db, 2401, 0, "default", modelName)
	createAutoRoutingTestChannel(t, db, 2402, 11, "m-beta", modelName)
	createAutoRoutingTestChannel(t, db, 2403, 12, "m-alpha", modelName)
	model.InitChannelCache()

	resolvedGlobal := GetUserAutoGroup("default")
	assert.Equal(t, []string{"default", "m-alpha", "m-beta"}, resolvedGlobal)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	retry := 0
	param := &RetryParam{Ctx: ctx, TokenGroup: "auto", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: &retry}

	ch1, g1, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, ch1)
	assert.Equal(t, "m-alpha", g1)
	assert.Equal(t, 2403, ch1.Id)

	param.IncreaseRetry()
	ch2, g2, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, ch2)
	assert.Equal(t, "m-beta", g2)
	assert.Equal(t, 2402, ch2.Id)

	param.IncreaseRetry()
	ch3, g3, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, ch3)
	assert.Equal(t, "default", g3)
	assert.Equal(t, 2401, ch3.Id)
}

func TestCacheGetRandomSatisfiedChannelAutoRoutingOrderBookPriceTTFTAndFallback(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	model.ResetChannelLiveMetrics()
	defer model.ResetChannelLiveMetrics()

	const modelName = "order-book-model"
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","m-fast":"Merchant Fast","m-laggy":"Merchant Laggy"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2.0,"m-fast":1.2,"m-laggy":1.0}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","m-laggy","m-fast"]`))
	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyPriceTTFT))

	createAutoRoutingTestChannel(t, db, 2501, 21, "default", modelName)
	createAutoRoutingTestChannel(t, db, 2502, 22, "m-laggy", modelName)
	createAutoRoutingTestChannel(t, db, 2503, 23, "m-fast", modelName)
	model.InitChannelCache()

	require.NoError(t, model.UpsertChannelQuotes([]model.ChannelQuote{
		{ChannelId: 2501, PoolGroup: "default", ModelName: modelName, InputPrice: 3.0, OutputPrice: 3.0, Source: model.QuoteSourceDerived, Status: model.QuoteStatusEnabled},
		{ChannelId: 2502, PoolGroup: "m-laggy", ModelName: modelName, InputPrice: 0.9, OutputPrice: 0.9, Source: model.QuoteSourceMerchant, Status: model.QuoteStatusEnabled},
		{ChannelId: 2503, PoolGroup: "m-fast", ModelName: modelName, InputPrice: 1.0, OutputPrice: 1.0, Source: model.QuoteSourceMerchant, Status: model.QuoteStatusEnabled},
	}))

	// 2502 is slightly cheaper than 2503, but has 5000ms TTFT vs 200ms on 2503, so price_ttft picks 2503 first!
	model.RecordChannelLiveOutcome(2502, modelName, true, 5000, 10)
	model.RecordChannelLiveOutcome(2503, modelName, true, 200, 80)
	model.RecordChannelLiveOutcome(2501, modelName, true, 400, 50)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	retry := 0
	param := &RetryParam{Ctx: ctx, TokenGroup: "auto", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: &retry}

	ch1, g1, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, ch1)
	assert.Equal(t, 2503, ch1.Id)
	assert.Equal(t, "m-fast", g1)

	// Simulate relay tracking used channel + retry to fallback down ranked nodes (allowing pricier 2501 before laggy 2502!).
	AppendUsedChannel(ctx, ch1.Id)
	param.IncreaseRetry()
	ch2, g2, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, ch2)
	assert.Equal(t, 2502, ch2.Id)
	assert.Equal(t, "m-laggy", g2)

	AppendUsedChannel(ctx, ch2.Id)
	param.IncreaseRetry()
	ch3, g3, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, ch3)
	assert.Equal(t, 2501, ch3.Id) // pricier backup allowed!
	assert.Equal(t, "default", g3)

	// Lowest TTFT strategy ignores price gap and puts 2503 (200ms) -> 2501 (400ms) -> 2502 (5000ms).
	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyLowestTTFT))
	ctxFast, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctxFast, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctxFast, constant.ContextKeyTokenCrossGroupRetry, true)
	retryFast := 0
	pFast := &RetryParam{Ctx: ctxFast, TokenGroup: "auto", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: &retryFast}

	fast1, _, err := CacheGetRandomSatisfiedChannel(pFast)
	require.NoError(t, err)
	require.NotNil(t, fast1)
	assert.Equal(t, 2503, fast1.Id)

	AppendUsedChannel(ctxFast, fast1.Id)
	pFast.IncreaseRetry()
	fast2, _, err := CacheGetRandomSatisfiedChannel(pFast)
	require.NoError(t, err)
	require.NotNil(t, fast2)
	assert.Equal(t, 2501, fast2.Id)
}
