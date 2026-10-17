package model

import (
	"time"
)

// ChannelMetric 渠道质量画像：由测活结果与真实流量错误率滚动计算，
// 供智能路由（质量分层 + 层内比价加权）与商家门户展示使用。
type ChannelMetric struct {
	Id             int     `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId      int     `json:"channel_id" gorm:"not null;uniqueIndex"`
	TestSuccessEma float64 `json:"test_success_ema" gorm:"default:1"` // 测活成功率指数移动平均 0..1
	LatencyEmaMs   float64 `json:"latency_ema_ms" gorm:"default:0"`   // 测活延迟指数移动平均（毫秒）
	TestCount      int64   `json:"test_count" gorm:"default:0"`       // 累计测活次数
	// 注意：带数字的字段必须显式固定列名。GORM 默认命名会把 RealSuccess24h 映射为
	// real_success24h（数字前不加下划线），与按 json 名书写的 real_success_24h 不一致，
	// 会让 map 形式的 Updates 整条失败（质量分与真实流量静默不入库）。
	RealSuccess24h   int64   `json:"real_success_24h" gorm:"column:real_success_24h;default:0"` // 近24h真实成交笔数
	RealFail24h      int64   `json:"real_fail_24h" gorm:"column:real_fail_24h;default:0"`       // 近24h真实失败笔数
	QualityScore     float64 `json:"quality_score" gorm:"default:0"`                            // 0..100，综合质量分
	LastTestedAt     int64   `json:"last_tested_at" gorm:"bigint"`
	LastAggregatedAt int64   `json:"last_aggregated_at" gorm:"bigint"`
}

func (ChannelMetric) TableName() string {
	return "channel_metrics"
}

// GetOrCreateChannelMetric 取渠道质量记录，不存在则建（score 默认中性值）。
func GetOrCreateChannelMetric(channelId int) (*ChannelMetric, error) {
	var metric ChannelMetric
	err := DB.Where("channel_id = ?", channelId).First(&metric).Error
	if err == nil {
		return &metric, nil
	}
	metric = ChannelMetric{
		ChannelId:      channelId,
		TestSuccessEma: 1, // 未知渠道先按满分观察，避免新渠道被永久压底
		QualityScore:   70,
	}
	if err := DB.Create(&metric).Error; err != nil {
		return nil, err
	}
	return &metric, nil
}

// RecordChannelTestOutcome 记录一次测活结果，滚动更新 EMA。
// alpha 控制新样本权重，默认 0.3。
func RecordChannelTestOutcome(channelId int, success bool, latencyMs int, alpha float64) error {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.3
	}
	metric, err := GetOrCreateChannelMetric(channelId)
	if err != nil {
		return err
	}
	if success {
		metric.TestSuccessEma = metric.TestSuccessEma*(1-alpha) + 1*alpha
	} else {
		metric.TestSuccessEma = metric.TestSuccessEma * (1 - alpha)
	}
	if latencyMs > 0 {
		if metric.LatencyEmaMs <= 0 {
			metric.LatencyEmaMs = float64(latencyMs)
		} else {
			metric.LatencyEmaMs = metric.LatencyEmaMs*(1-alpha) + float64(latencyMs)*alpha
		}
	}
	metric.TestCount++
	metric.LastTestedAt = time.Now().Unix()
	return DB.Model(&ChannelMetric{}).Where("id = ?", metric.Id).Updates(map[string]any{
		"test_success_ema": metric.TestSuccessEma,
		"latency_ema_ms":   metric.LatencyEmaMs,
		"test_count":       metric.TestCount,
		"last_tested_at":   metric.LastTestedAt,
	}).Error
}
