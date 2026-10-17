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
