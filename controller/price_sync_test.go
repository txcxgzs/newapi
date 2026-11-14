package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func TestParseUpstreamQuotesType2Pricing(t *testing.T) {
	body := []byte(`{
		"success": true,
		"message": "",
		"data": [
			{"model_name": "gpt-5.6", "quota_type": 0, "model_ratio": 1.25, "completion_ratio": 4},
			{"model_name": "claude-opus-5", "quota_type": 0, "model_ratio": 2.5},
			{"model_name": "per-call-model", "quota_type": 1, "model_price": 0.02},
			{"model_name": "", "quota_type": 0, "model_ratio": 1}
		]
	}`)
	drafts, err := parseUpstreamQuotes(body)
	require.NoError(t, err)
	require.Len(t, drafts, 2, "按次计费与空模型名应被跳过")
	require.Equal(t, "gpt-5.6", drafts[0].ModelName)
	// 输入价 = 1.25 × 2 = 2.5 $/1M；输出价 = 1.25 × 4 × 2 = 10 $/1M
	assert.Equal(t, 2.5, drafts[0].InputPrice)
	assert.Equal(t, 10.0, drafts[0].OutputPrice)
	// 未提供补全倍率时默认 1
	assert.Equal(t, drafts[1].InputPrice, drafts[1].OutputPrice,
		"缺少 completion_ratio 时输出价应等于输入价")
}

func TestParseUpstreamQuotesType1RatioConfig(t *testing.T) {
	body := []byte(`{
		"success": true,
		"data": {
			"model_ratio": {"gpt-6": 2.0},
			"completion_ratio": {"gpt-6": 8.0}
		}
	}`)
	drafts, err := parseUpstreamQuotes(body)
	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Equal(t, 4.0, drafts[0].InputPrice)
	assert.Equal(t, 32.0, drafts[0].OutputPrice)
}

func TestQualityTierPriority(t *testing.T) {
	cases := []struct {
		score float64
		want  int64
	}{
		{98, qualityTierTopPriority},
		{95, qualityTierTopPriority},
		{94.9, qualityTierHighPriority},
		{85, qualityTierHighPriority},
		{84.9, qualityTierMediumPriority},
		{70, qualityTierMediumPriority},
		{69.9, qualityTierLowPriority},
		{0, qualityTierLowPriority},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, qualityTierPriority(c.score), "score %v", c.score)
	}
}

func TestPriceScoreWithinGroup(t *testing.T) {
	// 全组同价 → 100
	assert.Equal(t, 100.0, priceScoreWithinGroup(10, 10, 10), "同价应为满分")
	// 最低价 → 100
	assert.Equal(t, 100.0, priceScoreWithinGroup(2, 2, 12), "最低价应为满分")
	// 最高价 → 下限 40
	assert.Equal(t, float64(priceScoreFloor), priceScoreWithinGroup(12, 2, 12), "最高价应为下限")
	// 中位线性插值：40 + (100-40) × (12-7)/(12-2) = 70
	assert.Equal(t, 70.0, priceScoreWithinGroup(7, 2, 12), "中位价应为 70")
}

func TestBlendedQuotePrice(t *testing.T) {
	assert.Equal(t, 6.0, blendedQuotePrice(2, 10))
	// 输出价为 0 时退化为输入价
	assert.Equal(t, 2.0, blendedQuotePrice(2, 0))
}

func TestComputeQualityScore(t *testing.T) {
	// 完美渠道：测活全成功、无延迟、无真实流量 → 测活与流量权重都按成功率补足
	perfect := &model.ChannelMetric{TestSuccessEma: 1, LatencyEmaMs: 0}
	assert.Equal(t, 100.0, computeQualityScore(perfect))
	// 一半测活成功率、5s 延迟、无真实流量：50×0.5 + 30×0.5 + 20×0 = 40
	half := &model.ChannelMetric{TestSuccessEma: 0.5, LatencyEmaMs: 5000}
	assert.Equal(t, 40.0, computeQualityScore(half))
	// 真实流量全失败：50×1 + 30×0 + 20×1 = 70
	failing := &model.ChannelMetric{TestSuccessEma: 1, LatencyEmaMs: 0, RealFail24h: 10}
	assert.Equal(t, 70.0, computeQualityScore(failing))
}

// TestFetchChannelTraffic24hAggregatesPerChannel 锁定近 24h 流量按渠道聚合的列映射。
// 回归点：Select 曾把 channel_id 起别名为 channel，而目标结构体字段映射的是
// channel_id 列，GORM 不读 json tag → 每行 ChannelId=0，真实流量权重整体失效。
func TestFetchChannelTraffic24hAggregatesPerChannel(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openLogTestDB(t, dialect)
			previous := model.LOG_DB
			model.LOG_DB = db
			t.Cleanup(func() { model.LOG_DB = previous })

			now := time.Now().Unix()
			logs := []model.Log{
				{ChannelId: 7, Type: model.LogTypeConsume, CreatedAt: now},
				{ChannelId: 7, Type: model.LogTypeConsume, CreatedAt: now},
				{ChannelId: 7, Type: model.LogTypeError, CreatedAt: now},
				{ChannelId: 9, Type: model.LogTypeError, CreatedAt: now},
				{ChannelId: 11, Type: model.LogTypeConsume, CreatedAt: now - 25*3600},
			}
			require.NoError(t, db.Create(&logs).Error)

			traffic := fetchChannelTraffic24h(context.Background())
			require.Len(t, traffic, 2, "只统计近 24h 出现的渠道")
			assert.Equal(t, channelTraffic{success: 2, fail: 1}, traffic[7])
			assert.Equal(t, channelTraffic{success: 0, fail: 1}, traffic[9])
			assert.NotContains(t, traffic, 0, "聚合结果不得落到 0 号渠道")
		})
	}
}

func openLogTestDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "logs.db"))
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrator().DropTable("logs"))
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	t.Cleanup(func() { _ = db.Migrator().DropTable("logs") })
	return db
}

// withPriceSyncTestClient 关闭 SSRF 保护并初始化全局 HTTP 客户端，
// 让报价同步用例可以访问 httptest 的环回地址；用例结束后恢复。
func withPriceSyncTestClient(t *testing.T) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	previous := *fetch
	fetch.EnableSSRFProtection = false
	service.InitHttpClient()
	t.Cleanup(func() {
		*fetch = previous
		service.InitHttpClient()
	})
}

// TestFetchChannelQuoteDraftsUnsupportedUpstream 锁定 Sub2API 一类网关的行为：
// 没有 New API 风格价格接口时返回 errPriceUnsupported，调用方据此降级为手填报价，
// 而不是把它当成同步失败反复报错。
func TestFetchChannelQuoteDraftsUnsupportedUpstream(t *testing.T) {
	withPriceSyncTestClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL}

	drafts, err := fetchChannelQuoteDrafts(context.Background(), channel)

	require.ErrorIs(t, err, errPriceUnsupported)
	assert.Nil(t, drafts)
}

func TestFetchChannelQuoteDraftsReadsNewAPIPricing(t *testing.T) {
	withPriceSyncTestClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pricing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[{"model_name":"gpt-4o","quota_type":0,"model_ratio":1.25,"completion_ratio":4}]}`))
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL}

	drafts, err := fetchChannelQuoteDrafts(context.Background(), channel)

	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Equal(t, "gpt-4o", drafts[0].ModelName)
	assert.InDelta(t, 2.5, drafts[0].InputPrice, 1e-9)
	assert.InDelta(t, 10.0, drafts[0].OutputPrice, 1e-9)
}

func TestParseOpenAIModelsPricingQuotes(t *testing.T) {
	body := []byte(`{"data":[
		{"id":"gpt-4o","pricing":{"currency":"USD","input_per_m":2.5,"cached_input_per_m":1.25,"output_per_m":10}},
		{"id":"no-currency","pricing":{"currency":"","input_per_m":1,"output_per_m":2}},
		{"id":"eur-model","pricing":{"currency":"EUR","input_per_m":1,"output_per_m":2}},
		{"id":"no-pricing"},
		{"id":"zero-input","pricing":{"currency":"USD","input_per_m":0,"output_per_m":2}},
		{"id":"zero-output","pricing":{"currency":"USD","input_per_m":1,"output_per_m":0}},
		{"id":"negative-output","pricing":{"currency":"USD","input_per_m":1,"output_per_m":-1}},
		{"id":"","pricing":{"currency":"USD","input_per_m":1,"output_per_m":2}}
	]}`)
	drafts, err := parseOpenAIModelsPricingQuotes(body)
	require.NoError(t, err)
	require.Len(t, drafts, 2, "只有 USD、缺价格、非美元价格之外的模型都应被保留")
	assert.Equal(t, channelQuoteDraft{ModelName: "gpt-4o", InputPrice: 2.5, OutputPrice: 10}, drafts[0])
	assert.Equal(t, channelQuoteDraft{ModelName: "no-currency", InputPrice: 1, OutputPrice: 2}, drafts[1])

	_, err = parseOpenAIModelsPricingQuotes([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	require.ErrorIs(t, err, errPriceUnsupported, "没有可用报价时按不支持自动同步降级")
}

func TestFetchChannelQuoteDraftsOpenAIModelsPricingUsesBearerKey(t *testing.T) {
	withPriceSyncTestClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != openAIModelsPricingPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-first" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o","pricing":{"currency":"USD","input_per_m":2.5,"output_per_m":10}}]}`))
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	channel := &model.Channel{
		BaseURL:           &baseURL,
		Key:               "sk-first\nsk-second",
		PriceSyncProtocol: string(service.UpstreamKindOpenAIModelsPricing),
	}

	drafts, err := fetchChannelQuoteDrafts(context.Background(), channel)

	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Equal(t, "gpt-4o", drafts[0].ModelName)
	assert.InDelta(t, 2.5, drafts[0].InputPrice, 1e-9)
	assert.InDelta(t, 10.0, drafts[0].OutputPrice, 1e-9)
}

func TestFetchChannelQuoteDraftsModelsPricingProtocolNeedsKey(t *testing.T) {
	withPriceSyncTestClient(t)
	baseURL := "https://upstream.example.com"
	channel := &model.Channel{
		BaseURL:           &baseURL,
		PriceSyncProtocol: string(service.UpstreamKindOpenAIModelsPricing),
	}

	_, err := fetchChannelQuoteDrafts(context.Background(), channel)

	require.Error(t, err)
	assert.NotErrorIs(t, err, errPriceUnsupported)
	assert.Contains(t, err.Error(), "API Key")
}

func TestFetchChannelQuoteDraftsReportsUnreachableUpstream(t *testing.T) {
	withPriceSyncTestClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL}

	_, err := fetchChannelQuoteDrafts(context.Background(), channel)

	require.Error(t, err)
	assert.NotErrorIs(t, err, errPriceUnsupported, "5xx/网络故障必须仍按失败处理")
}

// TestDraftsFromPublicPriceBody 覆盖复用 ratio_sync 转换器的公开价格源。
func TestDraftsFromPublicPriceBody(t *testing.T) {
	openRouterBody := []byte(`{"data":[{"id":"openai/gpt-5.6","pricing":{"prompt":"0.000002","completion":"0.000008"}}]}`)
	openRouterDrafts, err := draftsFromPublicPriceBody(openRouterHost, openRouterBody)
	require.NoError(t, err)
	require.Len(t, openRouterDrafts, 1)
	assert.Equal(t, "openai/gpt-5.6", openRouterDrafts[0].ModelName)
	assert.InDelta(t, 2.0, openRouterDrafts[0].InputPrice, 1e-9)
	assert.InDelta(t, 8.0, openRouterDrafts[0].OutputPrice, 1e-9)

	modelsDevBody := []byte(`{"openai":{"models":{"gpt-5.6":{"cost":{"input":2.5,"output":10}}}}}`)
	modelsDevDrafts, err := draftsFromPublicPriceBody("models.dev", modelsDevBody)
	require.NoError(t, err)
	require.Len(t, modelsDevDrafts, 1)
	assert.Equal(t, "gpt-5.6", modelsDevDrafts[0].ModelName)
	assert.InDelta(t, 2.5, modelsDevDrafts[0].InputPrice, 1e-9)
	assert.InDelta(t, 10.0, modelsDevDrafts[0].OutputPrice, 1e-9)
}

// TestRunRoutingOptimizePersistsChannelMetric 锁定质量画像的落库：
// 真实流量、质量分与聚合时间必须真的写进 channel_metrics。
// 回归点：RealSuccess24h 的 GORM 默认列名是 real_success24h（数字前无下划线），
// 而 map 形式 Updates 用的是 real_success_24h，导致整条更新失败、画像永远停在初始值。
func TestRunRoutingOptimizePersistsChannelMetric(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Channel{}, &model.Ability{}, &model.ChannelMetric{}, &model.ChannelQuote{}))

	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })

	autoRouting := true
	channel := &model.Channel{
		Type:        constant.ChannelTypeOpenAI,
		Key:         "sk-routing-test",
		Name:        "routing-test",
		Status:      common.ChannelStatusEnabled,
		Models:      "gpt-4o",
		Group:       "default",
		AutoRouting: &autoRouting,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.ChannelQuote{
		ChannelId: channel.Id, PoolGroup: "default", ModelName: "gpt-4o",
		InputPrice: 2, OutputPrice: 8, Source: model.QuoteSourceManual, Status: model.QuoteStatusEnabled,
	}).Error)

	now := time.Now().Unix()
	require.NoError(t, db.Create([]model.Log{
		{ChannelId: channel.Id, Type: model.LogTypeConsume, CreatedAt: now},
		{ChannelId: channel.Id, Type: model.LogTypeConsume, CreatedAt: now},
		{ChannelId: channel.Id, Type: model.LogTypeError, CreatedAt: now},
	}).Error)

	summary := RunRoutingOptimize(context.Background())
	require.Equal(t, 1, summary.ChannelsEvaluated)

	var metric model.ChannelMetric
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&metric).Error)
	assert.EqualValues(t, 2, metric.RealSuccess24h, "真实成交笔数必须落库")
	assert.EqualValues(t, 1, metric.RealFail24h, "真实失败笔数必须落库")
	assert.Positive(t, metric.QualityScore)
	assert.NotZero(t, metric.LastAggregatedAt)

	var updated model.Channel
	require.NoError(t, db.First(&updated, channel.Id).Error)
	assert.NotZero(t, updated.GetPriority())
	assert.Positive(t, updated.GetWeight())
}

// TestSyncQuotesForChannelWithoutDeclaredModelsWritesNoQuotes 回归：
// 渠道没有声明任何模型时不该放行全部上游报价，否则会把不属于本渠道
// 服务范围的模型铺成"幽灵盘口"；历史幽灵行也应被清理分支清掉。
func TestSyncQuotesForChannelWithoutDeclaredModelsWritesNoQuotes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelQuote{}))

	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })

	channel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-price-sync-empty",
		Name:   "empty-models",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	// 上一次错误同步留下的幽灵行：修复后必须被清理
	require.NoError(t, db.Create(&model.ChannelQuote{
		ChannelId: channel.Id, PoolGroup: "default", ModelName: "stale-model",
		InputPrice: 2, OutputPrice: 8, Source: model.QuoteSourceAuto, Status: model.QuoteStatusEnabled,
	}).Error)

	written, err := syncQuotesForChannel(channel, []channelQuoteDraft{
		{ModelName: "upstream-only-model", InputPrice: 1, OutputPrice: 2},
	})
	require.NoError(t, err)
	assert.Zero(t, written, "渠道未声明模型时不得写入任何报价")

	var remaining int64
	require.NoError(t, db.Model(&model.ChannelQuote{}).Where("channel_id = ?", channel.Id).Count(&remaining).Error)
	assert.Zero(t, remaining, "渠道当前不服务任何模型，幽灵盘口应被清理")

	// 声明过模型的渠道仍按交集过滤：只有声明的模型能落盘
	declared := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-price-sync-declared",
		Name:   "declared",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "declared-model",
	}
	require.NoError(t, db.Create(declared).Error)
	written, err = syncQuotesForChannel(declared, []channelQuoteDraft{
		{ModelName: "declared-model", InputPrice: 1, OutputPrice: 2},
		{ModelName: "not-declared", InputPrice: 3, OutputPrice: 4},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, written)

	var quote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ?", declared.Id).First(&quote).Error)
	assert.Equal(t, "declared-model", quote.ModelName)
	assert.Equal(t, model.QuoteSourceAuto, quote.Source)
}

// TestSyncQuotesForChannelFollowsModelMapping 回归：
// 上游按 deepseek-v4.1-flash-fast 报价、渠道对外只声明自定义名
// deepseek-v4.1-flash-official 时，必须沿 model_mapping 反解到对外模型名落盘，
// 不能只按上游原名查找，否则该渠道永远等不到自动报价。
func TestSyncQuotesForChannelFollowsModelMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelQuote{}))

	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })

	channel := &model.Channel{
		Type:         constant.ChannelTypeOpenAI,
		Key:          "sk-price-sync-mapping",
		Name:         "mapped-models",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       "deepseek-v4.1-flash-official",
		ModelMapping: common.GetPointer(`{"deepseek-v4.1-flash-official":"deepseek-v4.1-flash-fast"}`),
	}
	require.NoError(t, db.Create(channel).Error)

	written, err := syncQuotesForChannel(channel, []channelQuoteDraft{
		{ModelName: "deepseek-v4.1-flash-fast", InputPrice: 1.06, OutputPrice: 2.12},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, written)

	var quote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&quote).Error)
	assert.Equal(t, "deepseek-v4.1-flash-official", quote.ModelName, "应沿模型重定向反解为对外模型名")
	assert.InDelta(t, 1.06, quote.InputPrice, 1e-9)
	assert.InDelta(t, 2.12, quote.OutputPrice, 1e-9)
	assert.Equal(t, model.QuoteSourceAuto, quote.Source)
}

// withPriceSyncMarketSetting 固定本轮测试使用的市场成本参数，并在用例结束后恢复。
func withPriceSyncMarketSetting(t *testing.T, rate, fee, profit float64, floor bool) {
	t.Helper()
	previous := operation_setting.GetMarketSetting()
	updated := previous
	updated.UpstreamUSDCNYRate = rate
	updated.FeeMultiplier = fee
	updated.MinProfitMult = profit
	updated.EnableFloorProtection = floor
	operation_setting.SetMarketSetting(updated)
	t.Cleanup(func() { operation_setting.SetMarketSetting(previous) })
}

// newPriceSyncQuoteTestDB 为报价同步用例准备独立的 SQLite 库并接管 model.DB。
func newPriceSyncQuoteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelQuote{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })
	return db
}

func TestSyncQuotesForChannelFloorProtection(t *testing.T) {
	cases := []struct {
		name        string
		floor       bool
		existing    *model.ChannelQuote
		input       float64
		output      float64
		wantInput   float64
		wantOutput  float64
		wantSource  string
		wantWritten int
	}{
		{
			name:       "keeps current manual price above floor",
			floor:      true,
			existing:   &model.ChannelQuote{InputPrice: 100, OutputPrice: 200, Source: model.QuoteSourceManual},
			input:      1,
			output:     2,
			wantInput:  100,
			wantOutput: 200,
			wantSource: model.QuoteSourceManual,
		},
		{
			name:        "raises current manual price when floor is breached",
			floor:       true,
			existing:    &model.ChannelQuote{InputPrice: 1, OutputPrice: 2, Source: model.QuoteSourceManual},
			input:       1,
			output:      2,
			wantInput:   10.6064,
			wantOutput:  21.2128,
			wantSource:  model.QuoteSourceAuto,
			wantWritten: 1,
		},
		{
			name:        "raises only breached output while keeping input above floor",
			floor:       true,
			existing:    &model.ChannelQuote{InputPrice: 50, OutputPrice: 2, Source: model.QuoteSourceManual},
			input:       1,
			output:      2,
			wantInput:   50,
			wantOutput:  21.2128,
			wantSource:  model.QuoteSourceAuto,
			wantWritten: 1,
		},
		{
			name:        "overwrites current price when protection is disabled",
			floor:       false,
			existing:    &model.ChannelQuote{InputPrice: 100, OutputPrice: 200, Source: model.QuoteSourceManual},
			input:       1,
			output:      2,
			wantInput:   10.6064,
			wantOutput:  21.2128,
			wantSource:  model.QuoteSourceAuto,
			wantWritten: 1,
		},
		{
			name:        "writes floor for a new quote",
			floor:       true,
			input:       0.45,
			output:      0.9,
			wantInput:   4.7729,
			wantOutput:  9.5458,
			wantSource:  model.QuoteSourceAuto,
			wantWritten: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newPriceSyncQuoteTestDB(t)
			withPriceSyncMarketSetting(t, 6.6707, 1.06, 1.5, tc.floor)
			channel := &model.Channel{
				Type:              constant.ChannelTypeOpenAI,
				Key:               "sk-floor-" + tc.name,
				Name:              "floor-" + tc.name,
				Status:            common.ChannelStatusEnabled,
				Group:             "default",
				Models:            "gpt-4o",
				PriceSyncProtocol: string(service.UpstreamKindOpenAIModelsPricing),
			}
			require.NoError(t, db.Create(channel).Error)
			if tc.existing != nil {
				tc.existing.ChannelId = channel.Id
				tc.existing.PoolGroup = "default"
				tc.existing.ModelName = "gpt-4o"
				tc.existing.Currency = "USD"
				tc.existing.RateMultiplier = tc.existing.InputPrice / priceRatioBase
				tc.existing.Status = model.QuoteStatusEnabled
				require.NoError(t, db.Create(tc.existing).Error)
			}

			written, err := syncQuotesForChannel(channel, []channelQuoteDraft{
				{ModelName: "gpt-4o", InputPrice: tc.input, OutputPrice: tc.output},
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantWritten, written)

			var quote model.ChannelQuote
			require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?",
				channel.Id, "default", "gpt-4o").First(&quote).Error)
			assert.InDelta(t, tc.wantInput, quote.InputPrice, 1e-4)
			assert.InDelta(t, tc.wantOutput, quote.OutputPrice, 1e-4)
			assert.Equal(t, tc.wantSource, quote.Source)
			assert.InDelta(t, tc.wantInput/priceRatioBase, quote.RateMultiplier, 1e-4)
		})
	}
}

func TestSyncQuotesForChannelProtocolIsolation(t *testing.T) {
	db := newPriceSyncQuoteTestDB(t)
	withPriceSyncMarketSetting(t, 6.6707, 1.06, 1.5, true)

	standardChannel := &model.Channel{
		Type:   constant.ChannelTypeNewAPI,
		Key:    "sk-standard-protocol",
		Name:   "standard-protocol",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o",
	}
	require.NoError(t, db.Create(standardChannel).Error)
	// 预设较高报价：标准协议渠道不应用风控折算与防打穿保护，直接按上游 1.0 倍率同步覆盖，不被放大 10.6 倍
	require.NoError(t, db.Create(&model.ChannelQuote{
		ChannelId: standardChannel.Id, PoolGroup: "default", ModelName: "gpt-4o",
		RateMultiplier: 50, InputPrice: 100, OutputPrice: 200,
		Currency: "USD", Source: model.QuoteSourceAuto, Status: model.QuoteStatusEnabled,
	}).Error)

	written, err := syncQuotesForChannel(standardChannel, []channelQuoteDraft{
		{ModelName: "gpt-4o", InputPrice: 2.5, OutputPrice: 10},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, written)

	var standardQuote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?",
		standardChannel.Id, "default", "gpt-4o").First(&standardQuote).Error)
	assert.InDelta(t, 2.5, standardQuote.InputPrice, 1e-4)
	assert.InDelta(t, 10.0, standardQuote.OutputPrice, 1e-4)
	assert.InDelta(t, 1.25, standardQuote.RateMultiplier, 1e-4)

	pricingChannel := &model.Channel{
		Type:              constant.ChannelTypeOpenAI,
		Key:               "sk-openai-models-pricing",
		Name:              "openai-models-pricing",
		Status:            common.ChannelStatusEnabled,
		Group:             "default",
		Models:            "gpt-4o",
		PriceSyncProtocol: string(service.UpstreamKindOpenAIModelsPricing),
	}
	require.NoError(t, db.Create(pricingChannel).Error)

	written, err = syncQuotesForChannel(pricingChannel, []channelQuoteDraft{
		{ModelName: "gpt-4o", InputPrice: 2.5, OutputPrice: 10},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, written)

	var pricingQuote model.ChannelQuote
	require.NoError(t, db.Where("channel_id = ? AND pool_group = ? AND model_name = ?",
		pricingChannel.Id, "default", "gpt-4o").First(&pricingQuote).Error)
	assert.InDelta(t, 26.516, pricingQuote.InputPrice, 1e-4)
	assert.InDelta(t, 106.0641, pricingQuote.OutputPrice, 1e-4)
}

func TestMarketSettingOptionOverridesEnvDefault(t *testing.T) {
	t.Setenv("PRICE_SYNC_FEE_MULTIPLIER", "9.99")
	t.Setenv("PRICE_SYNC_MIN_PROFIT_MULT", "9.99")
	t.Setenv("UPSTREAM_USD_CNY_RATE", "9.99")
	t.Setenv("PRICE_SYNC_ENABLE_FLOOR_PROTECTION", "true")
	saved := config.GlobalConfig.ExportAllConfigs()
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"market_setting.upstream_usd_cny_rate":   "7.1",
		"market_setting.enable_floor_protection": "false",
		"market_setting.fee_multiplier":          "1.2",
		"market_setting.min_profit_mult":         "1.3",
	}))

	setting := operation_setting.GetMarketSetting()
	assert.InDelta(t, 7.1, setting.UpstreamUSDCNYRate, 1e-9)
	assert.False(t, setting.EnableFloorProtection)
	assert.InDelta(t, 1.2, setting.FeeMultiplier, 1e-9)
	assert.InDelta(t, 1.3, setting.MinProfitMult, 1e-9)
}
