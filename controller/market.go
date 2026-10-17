package controller

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// MarketStockOverview 一只"股票"（号池 × 标准模型）的行情快照。
// 其中成交价来自真实调用日志的聚合，挂牌字段来自商家渠道报价（channel_quotes），两者相互独立。
type MarketStockOverview struct {
	Pool         string  `json:"pool"`
	Model        string  `json:"model"`
	LastPrice    float64 `json:"last_price"`     // 最新混合成交价 $/1M tokens
	Open24h      float64 `json:"open_24h"`       // 24h 前的成交价
	Change24hPct float64 `json:"change_24h_pct"` // 涨跌幅 %
	High24h      float64 `json:"high_24h"`
	Low24h       float64 `json:"low_24h"`
	VolCalls24h  int64   `json:"vol_calls_24h"` // 24h 成交笔数
	VolQuota24h  float64 `json:"vol_quota_24h"` // 24h 成交额（$）

	// 商家挂牌信息：同一(号池,模型)下所有启用报价的聚合，倍率为模型倍率口径
	ListedRatio       float64 `json:"listed_ratio"`        // 最低挂牌模型倍率
	ListedRatioAvg    float64 `json:"listed_ratio_avg"`    // 平均挂牌模型倍率
	ListedInputPrice  float64 `json:"listed_input_price"`  // 最低挂牌输入价 $/1M tokens
	ListedOutputPrice float64 `json:"listed_output_price"` // 最低挂牌输入价对应的输出价 $/1M tokens
	ListedChannels    int     `json:"listed_channels"`     // 挂牌渠道数

	UpdatedAt int64 `json:"updated_at"`
}

// marketKey 行情快照中(号池,模型)的唯一标识。
type marketKey struct {
	pool  string
	model string
}

// listedQuote 同一(号池,模型)下所有启用报价的聚合结果。
type listedQuote struct {
	pool      string
	model     string
	minRatio  float64 // 最低挂牌倍率（有效倍率 = 平台标准倍率 × 渠道倍率）
	minInput  float64 // 最低挂牌倍率对应的输入价 $/1M
	minOutput float64 // 最低挂牌倍率对应的输出价 $/1M
	sumRatio  float64 // 挂牌倍率累加，用于求均值
	channels  int     // 挂牌渠道数
}

type marketSnapshot struct {
	Stocks    []MarketStockOverview `json:"stocks"`
	UpdatedAt int64                 `json:"updated_at"`
}

var (
	marketSnapshotMu    sync.RWMutex
	marketSnapshotCache *marketSnapshot
	marketSnapshotBuilt time.Time
	marketSnapshotTTL   = 30 * time.Second
)

// getMarketSnapshot 取行情快照，过期自动重建（多实例各自持有，数据一致来自库）。
func getMarketSnapshot() *marketSnapshot {
	marketSnapshotMu.RLock()
	if marketSnapshotCache != nil && time.Since(marketSnapshotBuilt) < marketSnapshotTTL {
		defer marketSnapshotMu.RUnlock()
		return marketSnapshotCache
	}
	marketSnapshotMu.RUnlock()

	marketSnapshotMu.Lock()
	defer marketSnapshotMu.Unlock()
	if marketSnapshotCache != nil && time.Since(marketSnapshotBuilt) < marketSnapshotTTL {
		return marketSnapshotCache
	}
	snapshot := buildMarketSnapshot()
	marketSnapshotCache = snapshot
	marketSnapshotBuilt = time.Now()
	return snapshot
}

// buildMarketSnapshot 从 1h K线构建 24h 行情快照。
// 窗口取 bucket_ts > now-24h：高/低/量与涨跌幅基准（首根 open）都严格落在
// 最近 24 小时内（小时线粒度下基准点最远 24h 前、最近 23h 前）。
func buildMarketSnapshot() *marketSnapshot {
	snapshot := &marketSnapshot{
		Stocks:    []MarketStockOverview{},
		UpdatedAt: time.Now().Unix(),
	}
	if model.DB == nil {
		return snapshot
	}
	since := time.Now().Add(-24 * time.Hour).Unix()
	var candles []model.MarketCandle
	if err := model.DB.Where("interval = ? AND bucket_ts > ?", model.CandleInterval1h, since).
		Order("bucket_ts ASC").Find(&candles).Error; err != nil {
		logger.LogError(context.Background(), "market overview: query candles failed: "+err.Error())
		return snapshot
	}

	type key struct{ pool, model string }
	type state struct {
		stock     MarketStockOverview
		high, low float64
		hasRef    bool
	}
	states := make(map[key]*state)
	for _, c := range candles {
		k := key{c.PoolGroup, c.ModelName}
		s, ok := states[k]
		if !ok {
			s = &state{stock: MarketStockOverview{Pool: c.PoolGroup, Model: c.ModelName, Open24h: c.Open}}
			states[k] = s
		}
		// 升序遍历，最后一行的 close 即现价
		s.stock.LastPrice = c.Close
		s.stock.UpdatedAt = c.BucketTs
		if !s.hasRef {
			s.high, s.low = c.High, c.Low
			s.hasRef = true
		} else {
			if c.High > s.high {
				s.high = c.High
			}
			if c.Low < s.low {
				s.low = c.Low
			}
		}
		s.stock.VolCalls24h += c.VolumeCalls
		s.stock.VolQuota24h += float64(c.VolumeQuota) / common.QuotaPerUnit
	}

	for _, s := range states {
		s.stock.High24h = s.high
		s.stock.Low24h = s.low
		if s.stock.Open24h > 0 {
			s.stock.Change24hPct = (s.stock.LastPrice - s.stock.Open24h) / s.stock.Open24h * 100
		}
		snapshot.Stocks = append(snapshot.Stocks, s.stock)
	}
	// 按成交额降序，热门模型排前；同额用 (号池, 模型) 字典序兜底，
	// 保证每次 30s 重建快照时顺序稳定，避免前端榜单抖动。
	sort.Slice(snapshot.Stocks, func(i, j int) bool {
		a, b := snapshot.Stocks[i], snapshot.Stocks[j]
		if a.VolQuota24h != b.VolQuota24h {
			return a.VolQuota24h > b.VolQuota24h
		}
		if a.Pool != b.Pool {
			return a.Pool < b.Pool
		}
		return a.Model < b.Model
	})
	// 合并商家挂牌倍率（含仅挂牌、暂无成交的模型）
	applyListing(snapshot)
	return snapshot
}

// loadListedQuotes 从 channel_quotes 聚合商家挂牌价。
// 只统计启用中的报价；倍率换算基准为 priceRatioBase（模型倍率 1.0 = $2/1M tokens）。
func loadListedQuotes() map[marketKey]*listedQuote {
	result := make(map[marketKey]*listedQuote)
	if model.DB == nil {
		return result
	}
	var quotes []model.ChannelQuote
	// 只统计启用中的渠道与启用中的报价：停用的渠道不应继续在行情里挂牌。
	if err := model.DB.Where("status = ? AND input_price > 0", model.QuoteStatusEnabled).
		Where("channel_id IN (?)", model.DB.Model(&model.Channel{}).
			Select("id").Where("status = ?", common.ChannelStatusEnabled)).
		Find(&quotes).Error; err != nil {
		logger.LogError(context.Background(), "market overview: query channel quotes failed: "+err.Error())
		return result
	}
	for i := range quotes {
		q := &quotes[i]
		// 有效倍率：新数据直接取字段；历史数据（列刚加入时为 0）用输入价反推兜底，
		// 避免升级后行情上的挂牌倍率全部变成 "--"。
		ratio := q.RateMultiplier
		if ratio <= 0 && q.InputPrice > 0 {
			ratio = q.InputPrice / priceRatioBase
		}
		if ratio <= 0 {
			continue
		}
		k := marketKey{pool: q.PoolGroup, model: q.ModelName}
		item, ok := result[k]
		if !ok {
			item = &listedQuote{
				pool:      q.PoolGroup,
				model:     q.ModelName,
				minInput:  q.InputPrice,
				minOutput: q.OutputPrice,
				minRatio:  ratio,
			}
			result[k] = item
		} else if ratio < item.minRatio {
			// 记录最低挂牌倍率，并同步其对应的输入/输出单价
			item.minRatio = ratio
			item.minInput = q.InputPrice
			item.minOutput = q.OutputPrice
		}
		item.sumRatio += ratio
		item.channels++
	}
	return result
}

// fillListing 把挂牌聚合结果写入股票快照。
func fillListing(stock *MarketStockOverview, item *listedQuote) {
	stock.ListedInputPrice = item.minInput
	stock.ListedOutputPrice = item.minOutput
	stock.ListedChannels = item.channels
	if item.channels <= 0 {
		return
	}
	stock.ListedRatio = item.minRatio
	stock.ListedRatioAvg = item.sumRatio / float64(item.channels)
}

// applyListing 把商家挂牌信息合并进行情快照：
// 已有成交的股票补齐挂牌字段；只有挂牌、暂无成交的模型也进入行情板（最新价为 0，排序自然靠后）。
func applyListing(snapshot *marketSnapshot) {
	listed := loadListedQuotes()
	if len(listed) == 0 {
		return
	}
	index := make(map[marketKey]int, len(snapshot.Stocks))
	for i := range snapshot.Stocks {
		index[marketKey{pool: snapshot.Stocks[i].Pool, model: snapshot.Stocks[i].Model}] = i
	}
	for k, item := range listed {
		if i, ok := index[k]; ok {
			fillListing(&snapshot.Stocks[i], item)
			continue
		}
		stock := MarketStockOverview{
			Pool:      item.pool,
			Model:     item.model,
			UpdatedAt: snapshot.UpdatedAt,
		}
		fillListing(&stock, item)
		snapshot.Stocks = append(snapshot.Stocks, stock)
	}
	// 仅挂牌的股票成交额为 0，稳定排序后仍排在成交活跃的模型之后
	sort.SliceStable(snapshot.Stocks, func(i, j int) bool {
		return snapshot.Stocks[i].VolQuota24h > snapshot.Stocks[j].VolQuota24h
	})
}

// requireMarketVisible 行情页被设为非公开时拒绝匿名访问（系统设置可切换）。
func requireMarketVisible(c *gin.Context) bool {
	if operation_setting.GetMarketSetting().MarketPublicEnabled {
		return true
	}
	if c.GetInt("id") > 0 {
		return true
	}
	c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "请先登录后查看行情"})
	return false
}

// GetMarketOverview 公开行情总览：号池列表 + 各股票现价/涨跌/成交额。
func GetMarketOverview(c *gin.Context) {
	if !requireMarketVisible(c) {
		return
	}
	snapshot := getMarketSnapshot()
	pool := c.Query("pool")
	stocks := snapshot.Stocks
	if pool != "" {
		filtered := make([]MarketStockOverview, 0, len(stocks))
		for _, s := range stocks {
			if s.Pool == pool {
				filtered = append(filtered, s)
			}
		}
		stocks = filtered
	}
	pools := make([]string, 0)
	seen := make(map[string]bool)
	for _, s := range snapshot.Stocks {
		if !seen[s.Pool] {
			seen[s.Pool] = true
			pools = append(pools, s.Pool)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"pools":      pools,
			"stocks":     stocks,
			"updated_at": snapshot.UpdatedAt,
		},
	})
}

// GetMarketKline 公开K线接口。参数：pool, model, interval(1m/5m/1h), limit
func GetMarketKline(c *gin.Context) {
	if !requireMarketVisible(c) {
		return
	}
	pool := c.Query("pool")
	modelName := c.Query("model")
	interval := c.Query("interval")
	if !model.IsValidCandleInterval(interval) {
		interval = model.CandleInterval1m
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if pool == "" || modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "pool 与 model 参数必填"})
		return
	}
	candles, err := model.GetCandles(pool, modelName, interval, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"pool":     pool,
			"model":    modelName,
			"interval": interval,
			"candles":  candles,
		},
	})
}
