package controller

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// RoutingSummary 一轮智能路由优化的汇总。
type RoutingSummary struct {
	ChannelsEvaluated int `json:"channels_evaluated"`
	ChannelsUpdated   int `json:"channels_updated"`
}

// 质量分档 → 渠道 priority。分层的意义：一次请求失败重试时会落到更低优先级层，
// 高质量渠道承接首挑流量，低质量渠道兜底。
const (
	qualityTierTopPriority    = 30
	qualityTierHighPriority   = 20
	qualityTierMediumPriority = 10
	qualityTierLowPriority    = 0

	qualityTierTopThreshold    = 95
	qualityTierHighThreshold   = 85
	qualityTierMediumThreshold = 70

	// 层内权重的取值范围
	routingMaxWeight = 100

	// 价格竞争分下限：最贵的渠道仍保有该比例的价格分，避免彻底饿死
	priceScoreFloor = 40
)

// qualityTierPriority 质量分 → priority 档位。
func qualityTierPriority(qualityScore float64) int64 {
	switch {
	case qualityScore >= qualityTierTopThreshold:
		return qualityTierTopPriority
	case qualityScore >= qualityTierHighThreshold:
		return qualityTierHighPriority
	case qualityScore >= qualityTierMediumThreshold:
		return qualityTierMediumPriority
	default:
		return qualityTierLowPriority
	}
}

// computeQualityScore 综合质量分 0..100：
// 测活成功率 EMA（50%）+ 近24h真实流量成功率（30%）+ 延迟分（20%）。
func computeQualityScore(metric *model.ChannelMetric) float64 {
	score := 50 * metric.TestSuccessEma

	totalReal := metric.RealSuccess24h + metric.RealFail24h
	if totalReal > 0 {
		score += 30 * float64(metric.RealSuccess24h) / float64(totalReal)
	} else {
		// 无真实流量时用测活成功率补足该权重，避免新渠道被系统性压低
		score += 30 * metric.TestSuccessEma
	}

	latencyScore := 1.0
	if metric.LatencyEmaMs > 0 {
		latencyScore = math.Max(0, 1-metric.LatencyEmaMs/5000) // 5s 以上延迟记 0 分
	}
	score += 20 * latencyScore
	return math.Min(100, math.Max(0, score))
}

// priceScoreWithinGroup 层内比价：blended 最低价 100 分，最高价 priceScoreFloor 分，线性插值。
func priceScoreWithinGroup(blended, minPrice, maxPrice float64) float64 {
	if maxPrice <= minPrice {
		return 100 // 全组同价，不因价格拉扯
	}
	ratio := (maxPrice - blended) / (maxPrice - minPrice)
	return priceScoreFloor + (100-priceScoreFloor)*ratio
}

// blendedQuotePrice 报价的混合参考价（输入输出均值），用于比价排序。
func blendedQuotePrice(input, output float64) float64 {
	if output <= 0 {
		return input
	}
	return (input + output) / 2
}

// RunRoutingOptimize 执行一轮智能路由优化：
//  1. 从成交/错误日志滚动更新渠道真实流量指标；
//  2. 重算质量分并映射 priority（质量分层）；
//  3. 层内按价格竞争力 × 质量权重算 composite，映射 weight；
//  4. 仅对 auto_routing=true 的生效渠道生效，绝不触碰手工管理渠道。
func RunRoutingOptimize(ctx context.Context) *RoutingSummary {
	setting := operation_setting.GetMarketSetting()
	summary := &RoutingSummary{}

	var channels []*model.Channel
	if err := model.DB.Where("status = ? AND auto_routing = ?", common.ChannelStatusEnabled, true).
		Find(&channels).Error; err != nil {
		logger.LogError(ctx, "routing optimize: query channels failed: "+err.Error())
		return summary
	}
	if len(channels) == 0 {
		return summary
	}
	summary.ChannelsEvaluated = len(channels)

	// 真实流量指标滚动窗口（近24h）
	trafficByChannel := fetchChannelTraffic24h(ctx)

	channelIds := make([]int, 0, len(channels))
	for _, ch := range channels {
		channelIds = append(channelIds, ch.Id)
	}
	// 渠道质量分
	qualityById := make(map[int]float64, len(channels))
	for _, ch := range channels {
		metric, err := model.GetOrCreateChannelMetric(ch.Id)
		if err != nil {
			continue
		}
		if traffic, ok := trafficByChannel[ch.Id]; ok {
			metric.RealSuccess24h = traffic.success
			metric.RealFail24h = traffic.fail
		} else {
			metric.RealSuccess24h = 0
			metric.RealFail24h = 0
		}
		quality := computeQualityScore(metric)
		qualityById[ch.Id] = quality
		if err := model.DB.Model(&model.ChannelMetric{}).Where("id = ?", metric.Id).Updates(map[string]any{
			"real_success_24h":   metric.RealSuccess24h,
			"real_fail_24h":      metric.RealFail24h,
			"quality_score":      quality,
			"last_aggregated_at": time.Now().Unix(),
		}).Error; err != nil {
			// 不能静默：列名/权限类错误会让质量画像长期停留在初始值。
			logger.LogWarn(ctx, fmt.Sprintf("routing optimize: update channel metric failed on %s(#%d): %s",
				ch.Name, ch.Id, err.Error()))
		}
	}

	// 拉取这些渠道的生效报价，按 (pool, model) 分组求价格区间
	var quotes []model.ChannelQuote
	if err := model.DB.Where("channel_id IN ? AND status = ?", channelIds, model.QuoteStatusEnabled).
		Find(&quotes).Error; err != nil {
		logger.LogError(ctx, "routing optimize: query quotes failed: "+err.Error())
		return summary
	}
	type groupKey struct{ pool, model string }
	groupMin := make(map[groupKey]float64)
	groupMax := make(map[groupKey]float64)
	for _, q := range quotes {
		blended := blendedQuotePrice(q.InputPrice, q.OutputPrice)
		key := groupKey{q.PoolGroup, q.ModelName}
		if min, ok := groupMin[key]; !ok || blended < min {
			groupMin[key] = blended
		}
		if max, ok := groupMax[key]; !ok || blended > max {
			groupMax[key] = blended
		}
	}

	// 每个渠道：对其所有 (pool, model) 报价求平均 composite。
	// 计数与累加必须同源：质量分读取失败的渠道不计入，避免 composite=0
	// 被当成最低分把整条渠道的权重/优先级压到最低档。
	compositeById := make(map[int]float64, len(channels))
	quoteCountById := make(map[int]int)
	for _, q := range quotes {
		quality, ok := qualityById[q.ChannelId]
		if !ok {
			continue
		}
		key := groupKey{q.PoolGroup, q.ModelName}
		priceScore := priceScoreWithinGroup(blendedQuotePrice(q.InputPrice, q.OutputPrice), groupMin[key], groupMax[key])
		composite := setting.QualityWeight*quality + (1-setting.QualityWeight)*priceScore
		compositeById[q.ChannelId] += composite
		quoteCountById[q.ChannelId]++
	}

	cacheDirty := false
	for _, ch := range channels {
		count := quoteCountById[ch.Id]
		if count == 0 {
			continue // 尚无报价的渠道不动，保持管理员手工配置
		}
		composite := compositeById[ch.Id] / float64(count)
		quality := qualityById[ch.Id]

		newPriority := qualityTierPriority(quality)
		newWeight := int(math.Max(float64(setting.MinWeight), math.Min(routingMaxWeight, composite)))
		if newWeight == ch.GetWeight() && newPriority == ch.GetPriority() {
			continue
		}

		err := model.DB.Model(&model.Channel{}).Where("id = ?", ch.Id).Updates(map[string]any{
			"priority": newPriority,
			"weight":   newWeight,
		}).Error
		if err != nil {
			logger.LogWarn(ctx, "routing optimize: update channel failed: "+err.Error())
			continue
		}
		// abilities 表冗余了 priority/weight，一并同步
		_ = model.DB.Model(&model.Ability{}).Where("channel_id = ?", ch.Id).Updates(map[string]any{
			"priority": newPriority,
			"weight":   uint(newWeight),
		}).Error
		summary.ChannelsUpdated++
		cacheDirty = true
	}

	if cacheDirty && common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	return summary
}

// channelTraffic 渠道近24h真实流量计数。
type channelTraffic struct {
	success int64
	fail    int64
}

// fetchChannelTraffic24h 从日志库聚合近24h各渠道成交/错误笔数。
func fetchChannelTraffic24h(ctx context.Context) map[int]channelTraffic {
	result := make(map[int]channelTraffic)
	if model.LOG_DB == nil {
		return result
	}
	dayAgo := time.Now().Add(-24 * time.Hour).Unix()

	// 列名必须与结构体字段映射出的列名一致（channel_id）；GORM 不读取 json tag，
	// 用别名扫描会静默得到零值。
	type row struct {
		ChannelId int   `json:"channel_id"`
		Success   int64 `json:"success"`
		Fail      int64 `json:"fail"`
	}
	var rows []row
	err := model.LOG_DB.Model(&model.Log{}).
		Select("channel_id, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) as success, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) as fail", model.LogTypeConsume, model.LogTypeError).
		Where("created_at > ?", dayAgo).
		Group("channel_id").
		Find(&rows).Error
	if err != nil {
		logger.LogWarn(ctx, "routing optimize: aggregate traffic failed: "+err.Error())
		return result
	}
	for _, r := range rows {
		result[r.ChannelId] = channelTraffic{success: r.Success, fail: r.Fail}
	}
	return result
}

// routingOptimizerHandler 定时执行智能路由优化。
type routingOptimizerHandler struct{}

func (routingOptimizerHandler) Type() string { return model.SystemTaskTypeRoutingOptimize }

func (routingOptimizerHandler) Enabled() bool {
	return operation_setting.GetMarketSetting().RoutingOptimizerEnabled
}

func (routingOptimizerHandler) Interval() time.Duration {
	minutes := operation_setting.GetMarketSetting().RoutingMinutes
	if minutes <= 0 {
		minutes = 5
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (routingOptimizerHandler) NewPayload() any { return nil }

func (routingOptimizerHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := RunRoutingOptimize(ctx)
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}
