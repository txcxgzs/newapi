package controller

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	candleAggMaxBucketsPerRun = 120 // 单次最多回补的分钟桶数，防止积压时拖垮任务
	candleLogBatchLimit       = 200000
)

// candleAggHandler 每分钟把成交日志聚合成 K 线（真实成交行情）。
type candleAggHandler struct{}

func (candleAggHandler) Type() string { return model.SystemTaskTypeCandleAgg }

func (candleAggHandler) Enabled() bool {
	return operation_setting.GetMarketSetting().CandleAggEnabled
}

func (candleAggHandler) Interval() time.Duration { return time.Minute }

func (candleAggHandler) NewPayload() any { return nil }

func (candleAggHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := RunCandleAggregation(ctx)
	status := model.SystemTaskStatusSucceeded
	var runErr error
	if summary.Error != "" {
		status = model.SystemTaskStatusFailed
		runErr = errors.New(summary.Error)
	}
	finishSystemTaskHandler(task, runnerID, status, summary, runErr)
}

// CandleAggSummary 一次聚合任务的汇总。
type CandleAggSummary struct {
	Buckets     int    `json:"buckets"`    // 本次聚合的分钟桶数
	Candles1m   int    `json:"candles_1m"` // 写入的 1m K线数
	Candles5m   int    `json:"candles_5m"`
	Candles1h   int    `json:"candles_1h"`
	LogsScanned int64  `json:"logs_scanned"`
	Error       string `json:"error,omitempty"`
}

// RunCandleAggregation 聚合日志 → 1m K线 → 滚动生成 5m/1h K线。
// 幂等：以 market_candles 中该周期最新桶为游标，缺口自动回补。
func RunCandleAggregation(ctx context.Context) *CandleAggSummary {
	summary := &CandleAggSummary{}
	if model.LOG_DB == nil || model.DB == nil {
		summary.Error = "database not initialized"
		return summary
	}

	// 当前分钟桶尚未走完，聚合到上一分钟为止
	nowBucket := time.Now().Unix() / 60 * 60
	lastTs := lastCandleBucketTs(model.CandleInterval1m)
	startBucket := nowBucket - 60
	if lastTs > 0 {
		startBucket = lastTs + 60
	} else {
		// 首次运行（尚无任何K线）：回补最近 2 小时，新部署的行情页即刻有数据
		startBucket = nowBucket - 120*60
	}
	if startBucket <= nowBucket-60 {
		if nowBucket-startBucket > int64(candleAggMaxBucketsPerRun)*60 {
			// 积压过多：只回补最近 N 个桶，更早的放弃（K线从有数据处开始）
			startBucket = nowBucket - int64(candleAggMaxBucketsPerRun)*60
		}
		for bucket := startBucket; bucket < nowBucket; bucket += 60 {
			summary.Buckets++
			written, scanned, err := aggregateOneMinuteBucket(ctx, bucket)
			if err != nil {
				summary.Error = err.Error()
				break
			}
			summary.Candles1m += written
			summary.LogsScanned += scanned
		}
	}

	// 5m/1h 各自持有独立游标回补：上一次运行即使写完 1m 后崩溃，也不会留下永久缺口。
	rollupFrom := func(dstInterval string, size int64) int64 {
		from := model.FirstCandleBucketTs(model.CandleInterval1m)
		if last := lastCandleBucketTs(dstInterval); last > 0 && last+size > from {
			from = last + size
		}
		return from
	}
	summary.Candles5m = rollupCandles(model.CandleInterval1m, model.CandleInterval5m, 300,
		rollupFrom(model.CandleInterval5m, 300), nowBucket)
	summary.Candles1h = rollupCandles(model.CandleInterval1m, model.CandleInterval1h, 3600,
		rollupFrom(model.CandleInterval1h, 3600), nowBucket)

	// 按保留天数清理过期K线（每小时收尾时执行一次）
	if time.Now().Unix()%(3600) < 60 {
		retention := operation_setting.GetMarketSetting().CandleRetentionDays
		for _, interval := range model.CandleIntervalsSupported() {
			keep := int64(retention) * 86400
			if interval == model.CandleInterval1m {
				keep = int64(retention) * 3600 // 分钟线最多保留 1 天量级，控制表体积
			}
			_ = model.CleanupCandlesBefore(interval, time.Now().Unix()-keep)
		}
	}
	return summary
}

// lastCandleBucketTs 返回某周期最新K线的桶起始时间，无数据返回 0。
func lastCandleBucketTs(interval string) int64 {
	var candle model.MarketCandle
	err := model.DB.Where(&model.MarketCandle{Interval: interval}).Order("bucket_ts DESC").First(&candle).Error
	if err != nil {
		return 0
	}
	return candle.BucketTs
}

// aggregateOneMinuteBucket 把一个分钟桶内的消费日志聚合成 K 线并落库。
func aggregateOneMinuteBucket(ctx context.Context, bucket int64) (int, int64, error) {
	var logs []model.Log
	// 必须显式排序：open/close 依赖日志时间顺序，且不同数据库默认返回顺序不保证。
	err := model.LOG_DB.Model(&model.Log{}).
		Select([]string{"id", "created_at", "type", "model_name", "quota", "prompt_tokens", "completion_tokens", "channel_id", "group"}).
		Where("type = ? AND created_at >= ? AND created_at < ?",
			model.LogTypeConsume, bucket, bucket+60).
		Order("created_at ASC").
		Order("id ASC").
		Limit(candleLogBatchLimit).
		Find(&logs).Error
	if err != nil {
		return 0, 0, err
	}

	type key struct{ pool, model string }
	type agg struct {
		open, high, low, close float64
		hasPrice               bool
		calls                  int64
		quota                  int64
	}
	aggs := make(map[key]*agg)

	for _, lg := range logs {
		k := key{pool: lg.Group, model: lg.ModelName}
		a, ok := aggs[k]
		if !ok {
			a = &agg{}
			aggs[k] = a
		}
		a.calls++
		a.quota += int64(lg.Quota)

		totalTokens := lg.PromptTokens + lg.CompletionTokens
		if totalTokens <= 0 {
			continue
		}
		// 混合成交价：$ / 1M tokens
		price := float64(lg.Quota) / float64(totalTokens) * 1e6 / common.QuotaPerUnit
		if !a.hasPrice {
			a.open, a.high, a.low, a.close = price, price, price, price
			a.hasPrice = true
		} else {
			if price > a.high {
				a.high = price
			}
			if price < a.low {
				a.low = price
			}
			a.close = price // 日志按时间升序返回，末次价即收盘价
		}
	}

	written := 0
	for k, a := range aggs {
		if !a.hasPrice {
			continue // 桶内全是 0-token 记录，无法定价
		}
		candle := &model.MarketCandle{
			PoolGroup:   k.pool,
			ModelName:   k.model,
			Interval:    model.CandleInterval1m,
			BucketTs:    bucket,
			Open:        a.open,
			High:        a.high,
			Low:         a.low,
			Close:       a.close,
			VolumeCalls: a.calls,
			VolumeQuota: a.quota,
		}
		if err := model.UpsertCandle(candle); err != nil {
			return written, int64(len(logs)), err
		}
		written++
	}
	return written, int64(len(logs)), nil
}

// rollupCandles 把 src 周期K线滚动聚合成 dst 周期K线（如 1m → 5m/1h）。
func rollupCandles(srcInterval, dstInterval string, dstSeconds int64, fromTs, toTs int64) int {
	var src []model.MarketCandle
	if err := model.DB.Where(&model.MarketCandle{Interval: srcInterval}).
		Where("bucket_ts >= ? AND bucket_ts < ?", fromTs, toTs).
		Order("bucket_ts ASC").Find(&src).Error; err != nil {
		return 0
	}
	type key struct {
		pool, model string
		bucket      int64
	}
	type agg struct {
		open, high, low, close float64
		has                    bool
		calls, quota           int64
	}
	aggs := make(map[key]*agg)
	for _, c := range src {
		k := key{c.PoolGroup, c.ModelName, c.BucketTs / dstSeconds * dstSeconds}
		a, ok := aggs[k]
		if !ok {
			a = &agg{}
			aggs[k] = a
		}
		if !a.has {
			a.open, a.high, a.low, a.close = c.Open, c.High, c.Low, c.Close
			a.has = true
		} else {
			if c.High > a.high {
				a.high = c.High
			}
			if c.Low < a.low {
				a.low = c.Low
			}
			a.close = c.Close
		}
		a.calls += c.VolumeCalls
		a.quota += c.VolumeQuota
	}

	written := 0
	for k, a := range aggs {
		if !a.has {
			continue
		}
		candle := &model.MarketCandle{
			PoolGroup:   k.pool,
			ModelName:   k.model,
			Interval:    dstInterval,
			BucketTs:    k.bucket,
			Open:        a.open,
			High:        a.high,
			Low:         a.low,
			Close:       a.close,
			VolumeCalls: a.calls,
			VolumeQuota: a.quota,
		}
		if err := model.UpsertCandle(candle); err == nil {
			written++
		}
	}
	return written
}
