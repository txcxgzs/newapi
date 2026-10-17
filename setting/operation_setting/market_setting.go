package operation_setting

import (
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/setting/config"
)

// MarketSetting New API Pro 行情/报价/智能路由设置。
// 全局开关默认开启，实际生效范围由渠道级开关控制：
//   - 价格同步：channel.price_sync = true 的渠道才会被拉取报价
//   - 智能路由：channel.auto_routing = true 的渠道才会被调整 priority/weight
type MarketSetting struct {
	PriceSyncEnabled     bool    `json:"price_sync_enabled"`
	PriceSyncMinutes     float64 `json:"price_sync_minutes"`
	PriceSyncConcurrency int     `json:"price_sync_concurrency"`

	RoutingOptimizerEnabled bool    `json:"routing_optimizer_enabled"`
	RoutingMinutes          float64 `json:"routing_minutes"`
	QualityWeight           float64 `json:"quality_weight"` // 综合评分中质量权重 0..1，价格占其余部分
	MinWeight               int     `json:"min_weight"`     // 智能路由写入的最小权重，保证低价渠道仍有流量

	CandleAggEnabled    bool `json:"candle_agg_enabled"`
	CandleRetentionDays int  `json:"candle_retention_days"`
	MarketPublicEnabled bool `json:"market_public_enabled"` // 未登录是否可见行情页
}

var marketSetting = MarketSetting{
	PriceSyncEnabled:        true,
	PriceSyncMinutes:        10,
	PriceSyncConcurrency:    8,
	RoutingOptimizerEnabled: true,
	RoutingMinutes:          5,
	QualityWeight:           0.6,
	MinWeight:               5,
	CandleAggEnabled:        true,
	CandleRetentionDays:     30,
	MarketPublicEnabled:     true,
}

func init() {
	config.GlobalConfig.Register("market_setting", &marketSetting)
}

func GetMarketSetting() *MarketSetting {
	if v, ok := os.LookupEnv("PRICE_SYNC_ENABLED"); ok {
		if parsed, err := strconv.ParseBool(v); err == nil {
			marketSetting.PriceSyncEnabled = parsed
		}
	}
	if marketSetting.PriceSyncMinutes < 1 {
		marketSetting.PriceSyncMinutes = 10
	}
	if marketSetting.PriceSyncConcurrency <= 0 {
		marketSetting.PriceSyncConcurrency = 8
	}
	if marketSetting.RoutingMinutes < 1 {
		marketSetting.RoutingMinutes = 5
	}
	if marketSetting.QualityWeight < 0 || marketSetting.QualityWeight > 1 {
		marketSetting.QualityWeight = 0.6
	}
	if marketSetting.MinWeight < 0 {
		marketSetting.MinWeight = 0
	}
	if marketSetting.CandleRetentionDays <= 0 {
		marketSetting.CandleRetentionDays = 30
	}
	return &marketSetting
}
