package operation_setting

import (
	"os"
	"strconv"
	"strings"
	"sync"

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

	// UpstreamUSDCNYRate 上游进货美元兑人民币汇率，独立于前台充值的
	// USDExchangeRate（后者只是站内余额展示口径，不能用于成本核算）。
	UpstreamUSDCNYRate float64 `json:"upstream_usd_cny_rate"`
	// EnableFloorProtection 开启最低安全售价保护：上游未涨价时保留当前盘口价，
	// 涨破安全线才自动上调；关闭时每轮同步直接按最低安全价覆盖。
	EnableFloorProtection bool    `json:"enable_floor_protection"`
	FeeMultiplier         float64 `json:"fee_multiplier"`  // 上游成本附加费率（支付/通道等）
	MinProfitMult         float64 `json:"min_profit_mult"` // 最低利润倍数

	RoutingOptimizerEnabled bool    `json:"routing_optimizer_enabled"`
	RoutingMinutes          float64 `json:"routing_minutes"`
	QualityWeight           float64 `json:"quality_weight"` // 综合评分中质量权重 0..1，价格占其余部分
	MinWeight               int     `json:"min_weight"`     // 智能路由写入的最小权重，保证低价渠道仍有流量

	CandleAggEnabled    bool `json:"candle_agg_enabled"`
	CandleRetentionDays int  `json:"candle_retention_days"`
	MarketPublicEnabled bool `json:"market_public_enabled"` // 未登录是否可见行情页
}

const (
	defaultUpstreamUSDCNYRate    = 6.6707
	defaultEnableFloorProtection = true
	defaultFeeMultiplier         = 1.06
	defaultMinProfitMult         = 1.5
)

var marketSettingMutex sync.RWMutex

var marketSetting = MarketSetting{
	PriceSyncEnabled:        true,
	PriceSyncMinutes:        10,
	PriceSyncConcurrency:    8,
	UpstreamUSDCNYRate:      defaultUpstreamUSDCNYRate,
	EnableFloorProtection:   defaultEnableFloorProtection,
	FeeMultiplier:           defaultFeeMultiplier,
	MinProfitMult:           defaultMinProfitMult,
	RoutingOptimizerEnabled: true,
	RoutingMinutes:          5,
	QualityWeight:           0.6,
	MinWeight:               5,
	CandleAggEnabled:        true,
	CandleRetentionDays:     30,
	MarketPublicEnabled:     true,
}

func init() {
	// 环境变量只作为初始默认值；随后 LoadFromDB 会用后台保存的选项覆盖，
	// 因此网页设置始终优先于环境变量。
	if v, ok := os.LookupEnv("PRICE_SYNC_ENABLED"); ok {
		if parsed, err := strconv.ParseBool(v); err == nil {
			marketSetting.PriceSyncEnabled = parsed
		}
	}
	if v, ok := os.LookupEnv("UPSTREAM_USD_CNY_RATE"); ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && parsed > 0 {
			marketSetting.UpstreamUSDCNYRate = parsed
		}
	}
	if v, ok := os.LookupEnv("PRICE_SYNC_ENABLE_FLOOR_PROTECTION"); ok {
		if parsed, err := strconv.ParseBool(v); err == nil {
			marketSetting.EnableFloorProtection = parsed
		}
	}
	if v, ok := os.LookupEnv("PRICE_SYNC_FEE_MULTIPLIER"); ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && parsed > 0 {
			marketSetting.FeeMultiplier = parsed
		}
	}
	if v, ok := os.LookupEnv("PRICE_SYNC_MIN_PROFIT_MULT"); ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && parsed > 0 {
			marketSetting.MinProfitMult = parsed
		}
	}
	config.GlobalConfig.Register("market_setting", &marketSetting)
}

// GetMarketSetting 返回市场配置的并发安全值拷贝，并对非法值做本地兜底修正。
func GetMarketSetting() MarketSetting {
	marketSettingMutex.RLock()
	setting := marketSetting
	marketSettingMutex.RUnlock()

	if setting.PriceSyncMinutes < 1 {
		setting.PriceSyncMinutes = 10
	}
	if setting.PriceSyncConcurrency <= 0 {
		setting.PriceSyncConcurrency = 8
	}
	if setting.UpstreamUSDCNYRate <= 0 {
		setting.UpstreamUSDCNYRate = defaultUpstreamUSDCNYRate
	}
	if setting.FeeMultiplier <= 0 {
		setting.FeeMultiplier = defaultFeeMultiplier
	}
	if setting.MinProfitMult <= 0 {
		setting.MinProfitMult = defaultMinProfitMult
	}
	if setting.RoutingMinutes < 1 {
		setting.RoutingMinutes = 5
	}
	if setting.QualityWeight < 0 || setting.QualityWeight > 1 {
		setting.QualityWeight = 0.6
	}
	if setting.MinWeight < 0 {
		setting.MinWeight = 0
	}
	if setting.CandleRetentionDays <= 0 {
		setting.CandleRetentionDays = 30
	}
	return setting
}

// SetMarketSetting 加写锁整体更新市场配置（供测试或受控写入使用）。
func SetMarketSetting(setting MarketSetting) {
	marketSettingMutex.Lock()
	marketSetting = setting
	marketSettingMutex.Unlock()
}
