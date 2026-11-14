package model

import (
	"gorm.io/gorm/clause"
)

// K线周期
const (
	CandleInterval1m = "1m"
	CandleInterval5m = "5m"
	CandleInterval1h = "1h"
)

// MarketCandle 一根K线：某号池某标准模型在一个时间桶内的真实成交行情。
// 价格口径为混合成交价（美元/百万tokens）= 桶内成交总额 ÷ 桶内总tokens × 1M，
// 反映市场真实成交，与商家挂牌价（channel_quotes）分离。
type MarketCandle struct {
	Id          int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	PoolGroup   string  `json:"pool_group" gorm:"type:varchar(64);not null;uniqueIndex:idx_candle_pool_model_interval_ts,priority:1"`
	ModelName   string  `json:"model_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_candle_pool_model_interval_ts,priority:2"`
	Interval    string  `json:"interval" gorm:"type:varchar(8);not null;uniqueIndex:idx_candle_pool_model_interval_ts,priority:3"`
	BucketTs    int64   `json:"bucket_ts" gorm:"not null;uniqueIndex:idx_candle_pool_model_interval_ts,priority:4"` // 桶起始时间 unix 秒
	Open        float64 `json:"open"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Close       float64 `json:"close"`
	VolumeCalls int64   `json:"volume_calls"`
	VolumeQuota int64   `json:"volume_quota" gorm:"bigint"` // 原始 quota，展示时按 QuotaPerUnit 折算美元
}

func (MarketCandle) TableName() string {
	return "market_candles"
}

// UpsertCandle 写入/覆盖一根K线（幂等，聚合任务重跑安全）。
func UpsertCandle(candle *MarketCandle) error {
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "pool_group"}, {Name: "model_name"}, {Name: "interval"}, {Name: "bucket_ts"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"open", "high", "low", "close", "volume_calls", "volume_quota",
		}),
	}).Create(candle).Error
}

// GetCandles 按时间升序返回K线序列。
func GetCandles(poolGroup string, modelName string, interval string, limit int) ([]MarketCandle, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var candles []MarketCandle
	err := DB.Where(&MarketCandle{PoolGroup: poolGroup, ModelName: modelName, Interval: interval}).
		Order("bucket_ts DESC").Limit(limit).Find(&candles).Error
	if err != nil {
		return nil, err
	}
	// 反转为升序，方便前端直接画图
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}
	return candles, nil
}

// FirstCandleBucketTs 返回某周期最早的桶起始时间，无数据返回 0。
// 滚动聚合用它确定回补起点，避免上次运行崩溃后留下永久缺口。
func FirstCandleBucketTs(interval string) int64 {
	var candle MarketCandle
	if err := DB.Where(&MarketCandle{Interval: interval}).Order("bucket_ts ASC").First(&candle).Error; err != nil {
		return 0
	}
	return candle.BucketTs
}

// GetLatestCandle 返回最近一根K线，用于行情列表的"现价"。
func GetLatestCandle(poolGroup string, modelName string, interval string) (*MarketCandle, error) {
	var candle MarketCandle
	err := DB.Where(&MarketCandle{PoolGroup: poolGroup, ModelName: modelName, Interval: interval}).
		Order("bucket_ts DESC").First(&candle).Error
	if err != nil {
		return nil, err
	}
	return &candle, nil
}

// CandleIntervalsSupported 返回支持的K线周期集合。
func CandleIntervalsSupported() []string {
	return []string{CandleInterval1m, CandleInterval5m, CandleInterval1h}
}

// IsValidCandleInterval 校验周期参数。
func IsValidCandleInterval(interval string) bool {
	for _, i := range CandleIntervalsSupported() {
		if i == interval {
			return true
		}
	}
	return false
}

// CleanupCandlesBefore 清理指定周期早于 cutoffTs 的K线，控制表体积。
func CleanupCandlesBefore(interval string, cutoffTs int64) error {
	return DB.Where(&MarketCandle{Interval: interval}).
		Where("bucket_ts < ?", cutoffTs).
		Delete(&MarketCandle{}).Error
}
