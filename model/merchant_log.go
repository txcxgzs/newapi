package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
)

// MerchantLogQueryParams 商家门户调用记录的查询参数。
// 所有查询都会被强制限定在商户名下渠道（ChannelIds）之内，实现数据隔离。
type MerchantLogQueryParams struct {
	ChannelIds     []int
	LogType        int
	ModelName      string
	Group          string
	StartTimestamp int64
	EndTimestamp   int64
	StartIdx       int
	Num            int
}

// MerchantChannelLogStats 商家调用记录的筛选内统计：调用量/失败量/消耗额度。
type MerchantChannelLogStats struct {
	Calls  int64 `json:"calls"`
	Fails  int64 `json:"fails"`
	Quota  int64 `json:"quota"`
	Tokens int64 `json:"tokens"`
}

// merchantLogStatsQuery 构造与 GetMerchantChannelLogs 相同的过滤条件（不含分页），
// 供统计聚合复用，保证"表格里看到的"和"统计里算的"口径一致。
func merchantLogStatsQuery(params MerchantLogQueryParams) (*gorm.DB, error) {
	if len(params.ChannelIds) == 0 {
		return nil, nil
	}
	tx := LOG_DB.Model(&Log{}).Where("logs.channel_id IN ?", params.ChannelIds)
	if params.LogType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", params.LogType)
	}
	tx, err := applyExplicitLogTextFilter(tx, "logs.model_name", params.ModelName)
	if err != nil {
		return nil, err
	}
	if params.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", params.Group)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", params.EndTimestamp)
	}
	return tx, nil
}

// GetMerchantChannelLogStats 按相同筛选条件聚合调用统计。
func GetMerchantChannelLogStats(params MerchantLogQueryParams) (MerchantChannelLogStats, error) {
	var stats MerchantChannelLogStats
	tx, err := merchantLogStatsQuery(params)
	if err != nil || tx == nil {
		return stats, err
	}
	err = tx.Select(
		"COUNT(*) AS calls, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END), 0) AS fails, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.quota ELSE 0 END), 0) AS quota, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.prompt_tokens + logs.completion_tokens ELSE 0 END), 0) AS tokens",
		LogTypeError, LogTypeConsume, LogTypeConsume,
	).Scan(&stats).Error
	return stats, err
}

// GetMerchantChannelLogs 查询商户名下渠道的调用记录（全类型）。
// 只做数据范围过滤，脱敏由 FormatMerchantLogs 完成。
func GetMerchantChannelLogs(params MerchantLogQueryParams) (logs []*Log, total int64, err error) {
	if len(params.ChannelIds) == 0 {
		return []*Log{}, 0, nil
	}
	tx := LOG_DB.Model(&Log{}).Where("logs.channel_id IN ?", params.ChannelIds)
	if params.LogType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", params.LogType)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", params.ModelName); err != nil {
		return nil, 0, err
	}
	if params.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", params.Group)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", params.EndTimestamp)
	}
	if err = tx.Model(&Log{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	if err = tx.Order(order).Limit(params.Num).Offset(params.StartIdx).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, params.StartIdx)
	}
	// 回填渠道名（商家自己的渠道，可展示）
	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}
	if channelIds.Len() > 0 {
		type channelRow struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		var channels []channelRow
		if common.MemoryCacheEnabled {
			for _, channelId := range channelIds.Items() {
				if cacheChannel, cacheErr := CacheGetChannel(channelId); cacheErr == nil {
					channels = append(channels, channelRow{Id: channelId, Name: cacheChannel.Name})
				}
			}
		} else {
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}
	return logs, total, nil
}

// FormatMerchantLogs 商家视角脱敏：剔除用户身份、令牌、IP 与原始 other 元数据，
// 只保留"哪个分组/模型/渠道、消耗多少、成功与否"这类运营信息，避免用户隐私泄露给商家。
func FormatMerchantLogs(logs []*Log) {
	for i := range logs {
		logs[i].UserId = 0
		logs[i].Username = ""
		logs[i].TokenId = 0
		logs[i].TokenName = ""
		logs[i].Ip = ""
		logs[i].Other = ""
	}
}

// MerchantGroupTraffic 商家渠道在某个分组内的近 24h 流量聚合。
// 显式声明 gorm 列名，避免跨数据库（MySQL/PostgreSQL/ClickHouse）扫描错位。
type MerchantGroupTraffic struct {
	Group string `json:"group" gorm:"column:group"`
	Calls int64  `json:"calls" gorm:"column:calls"`
	Quota int64  `json:"quota" gorm:"column:quota"`
	Fails int64  `json:"fails" gorm:"column:fails"`
}

// GetMerchantChannelTrafficByGroup 按分组聚合商家渠道近 24h 的调用量/成交额/失败数。
// 用于商家门户展示"不同分组各调用了多少"。
func GetMerchantChannelTrafficByGroup(channelIds []int, since int64) ([]MerchantGroupTraffic, error) {
	result := make([]MerchantGroupTraffic, 0)
	if len(channelIds) == 0 {
		return result, nil
	}
	err := LOG_DB.Model(&Log{}).
		Select(logGroupCol+", "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) as calls, "+
			"SUM(CASE WHEN type = ? THEN quota ELSE 0 END) as quota, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) as fails",
			LogTypeConsume, LogTypeConsume, LogTypeError).
		Where("channel_id IN ? AND created_at > ?", channelIds, since).
		Group(logGroupCol).
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// MerchantChannelHealth 单个渠道在统计窗口内的健康度。
// 供商家门户在"池子"列表上展示调用量、成功率与平均耗时。
type MerchantChannelHealth struct {
	ChannelId  int     `json:"channel_id" gorm:"column:channel_id"`
	Calls      int64   `json:"calls" gorm:"column:calls"`               // 成功调用笔数
	Fails      int64   `json:"fails" gorm:"column:fails"`               // 失败笔数
	Quota      int64   `json:"quota" gorm:"column:quota"`               // 消耗 quota
	AvgUseTime float64 `json:"avg_use_time" gorm:"column:avg_use_time"` // 平均耗时（秒）
}

// SuccessRate 成功率（0..1）。无调用时返回 0，由前端展示为占位符。
func (h MerchantChannelHealth) SuccessRate() float64 {
	total := h.Calls + h.Fails
	if total <= 0 {
		return 0
	}
	return float64(h.Calls) / float64(total)
}

// GetMerchantChannelHealth 按渠道聚合调用健康度（仅统计商家自己的渠道）。
func GetMerchantChannelHealth(channelIds []int, since int64) ([]MerchantChannelHealth, error) {
	result := make([]MerchantChannelHealth, 0)
	if len(channelIds) == 0 {
		return result, nil
	}
	// 平均耗时只对成功消费日志取，失败请求的耗时没有参考意义
	err := LOG_DB.Model(&Log{}).
		Select("channel_id, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) as calls, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) as fails, "+
			"SUM(CASE WHEN type = ? THEN quota ELSE 0 END) as quota, "+
			"COALESCE(AVG(CASE WHEN type = ? THEN use_time END), 0) as avg_use_time",
			LogTypeConsume, LogTypeError, LogTypeConsume, LogTypeConsume).
		Where("channel_id IN ? AND created_at > ?", channelIds, since).
		Group("channel_id").
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}
