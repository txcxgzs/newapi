package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 渠道报价来源
const (
	QuoteSourceAuto    = "auto"    // 定时同步上游价格（无价格接口的上游不会产生）
	QuoteSourceDerived = "derived" // 由「平台标准倍率 × 渠道倍率」派生（商家只填倍率）
	QuoteSourceManual  = "manual"  // 管理员手工锁定的盘口
)

// 渠道报价状态
const (
	QuoteStatusEnabled  = 1
	QuoteStatusDisabled = 2
)

// QuoteRatioBase 模型倍率与美元单价的折算基数：倍率 1.0 对应 $2/1M tokens。
// 与 controller 层 priceRatioBase 保持同一口径（controller 直接引用本常量）。
const QuoteRatioBase = 2.0

// ChannelQuote 盘口挂单：一家渠道在一个号池内对某个标准模型的报价。
//
// 商家不直接填写价格。盘口由「平台标准倍率 × 渠道倍率」派生，
// 字段语义如下：
//   - RateMultiplier：该模型在该渠道上的有效模型倍率（= 平台模型倍率 × 渠道倍率）
//   - InputPrice / OutputPrice：由有效倍率折算出的 $/1M 单价，仅用于展示与比价
//
// 用户看到的成交价由成交日志聚合而来，与此表无关。
type ChannelQuote struct {
	Id             int     `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId      int     `json:"channel_id" gorm:"not null;uniqueIndex:idx_quote_channel_pool_model,priority:1"`
	PoolGroup      string  `json:"pool_group" gorm:"type:varchar(64);not null;uniqueIndex:idx_quote_channel_pool_model,priority:2"`
	ModelName      string  `json:"model_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_quote_channel_pool_model,priority:3"`
	RateMultiplier float64 `json:"rate_multiplier"` // 有效模型倍率（平台标准倍率 × 渠道倍率）
	InputPrice     float64 `json:"input_price"`     // $/1M prompt tokens
	OutputPrice    float64 `json:"output_price"`    // $/1M completion tokens
	Currency       string  `json:"currency" gorm:"type:varchar(8);default:'USD'"`
	Source         string  `json:"source" gorm:"type:varchar(16);default:'auto'"`
	Status         int     `json:"status" gorm:"default:1"`
	RawPayload     string  `json:"raw_payload,omitempty" gorm:"type:text"`
	CreatedAt      int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64   `json:"updated_at" gorm:"bigint"`
}

func (ChannelQuote) TableName() string {
	return "channel_quotes"
}

// UpsertChannelQuotes 批量写入报价，按唯一索引 (channel_id, pool_group, model_name) 覆盖价格与更新时间。
func UpsertChannelQuotes(quotes []ChannelQuote) error {
	return UpsertChannelQuotesWithTx(DB, quotes)
}

// UpsertChannelQuotesWithTx 在给定连接（通常是事务）内批量写入报价。
func UpsertChannelQuotesWithTx(db *gorm.DB, quotes []ChannelQuote) error {
	if len(quotes) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for i := range quotes {
		if quotes[i].CreatedAt == 0 {
			quotes[i].CreatedAt = now
		}
		quotes[i].UpdatedAt = now
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "pool_group"}, {Name: "model_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"rate_multiplier", "input_price", "output_price", "currency", "source", "status", "raw_payload", "updated_at",
		}),
	}).Create(&quotes).Error
}

// GetQuotesByChannel 返回渠道的全部生效报价。
func GetQuotesByChannel(channelId int) ([]ChannelQuote, error) {
	var quotes []ChannelQuote
	err := DB.Where("channel_id = ? AND status = ?", channelId, QuoteStatusEnabled).Find(&quotes).Error
	return quotes, err
}

// GetManualQuote 返回渠道对指定(号池,模型)的手工锁定报价；没有则返回 nil。
func GetManualQuote(channelId int, poolGroup string, modelName string) (*ChannelQuote, error) {
	var quote ChannelQuote
	err := DB.Where("channel_id = ? AND pool_group = ? AND model_name = ? AND source = ?",
		channelId, poolGroup, modelName, QuoteSourceManual).First(&quote).Error
	if err != nil {
		return nil, err
	}
	return &quote, nil
}

// splitQuoteList 拆分渠道的 group / models 字段（逗号分隔，忽略空白项）。
func splitQuoteList(raw string) []string {
	items := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

// BuildDerivedQuotes 依据平台标准倍率与所属分组倍率，派生该渠道在全部(分组,模型)上的盘口。
//
// 计算方式：
//
//	有效倍率 = 平台模型倍率 × 分组倍率（GroupRatio）
//	输入单价 = 有效倍率 × QuoteRatioBase
//	输出单价 = 有效倍率 × 补全倍率 × QuoteRatioBase
//
// 平台未配置倍率的模型会被跳过（无法派生，避免写入 0 价盘口污染比价）。
func BuildDerivedQuotes(channel *Channel) []ChannelQuote {
	pools := splitQuoteList(channel.Group)
	models := splitQuoteList(channel.Models)
	if len(pools) == 0 || len(models) == 0 {
		return nil
	}
	now := time.Now().Unix()
	rows := make([]ChannelQuote, 0, len(pools)*len(models))
	for _, pool := range pools {
		groupRatio := ratio_setting.GetGroupRatio(pool)
		if groupRatio <= 0 {
			groupRatio = 1
		}
		for _, name := range models {
			modelRatio, ok, _ := ratio_setting.GetModelRatio(name)
			if !ok || modelRatio <= 0 {
				continue
			}
			completion := ratio_setting.GetCompletionRatio(name)
			if completion <= 0 {
				completion = 1
			}
			effective := modelRatio * groupRatio
			rows = append(rows, ChannelQuote{
				ChannelId:      channel.Id,
				PoolGroup:      pool,
				ModelName:      name,
				RateMultiplier: effective,
				InputPrice:     effective * QuoteRatioBase,
				OutputPrice:    effective * completion * QuoteRatioBase,
				Currency:       "USD",
				Source:         QuoteSourceDerived,
				Status:         QuoteStatusEnabled,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		}
	}
	return rows
}

// CleanupStaleAutoQuotes 删除不再属于渠道当前「分组 × 模型」范围的 auto 与
// manual 报价行，避免商家下架模型 / 换分组 / 上游下架模型后留下"幽灵盘口"
// 污染行情与智能路由。范围内 manual（管理员锁定）行保持原样。
// valid 的键为 "pool|model"。
func CleanupStaleAutoQuotes(tx *gorm.DB, channelId int, valid map[string]bool) error {
	var rows []ChannelQuote
	if err := tx.Where("channel_id = ? AND source IN ?", channelId,
		[]string{QuoteSourceAuto, QuoteSourceManual}).
		Find(&rows).Error; err != nil {
		return err
	}
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		if !valid[r.PoolGroup+"|"+r.ModelName] {
			ids = append(ids, r.Id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return tx.Where("id IN ?", ids).Delete(&ChannelQuote{}).Error
}

// RebuildChannelQuotesWithTx 重建该渠道的派生盘口（事务内版本）。
//
// 先清掉旧的派生行再写入，避免渠道改分组/改模型后留下幽灵报价；
// 管理员手工锁定的 manual 行与定时同步写入的 auto 行不受影响：
// auto 行是上游的真实挂牌价，优先级高于派生兜底，改倍率/改分组时
// 会跳过这些 (pool, model)，避免把同步结果冲回派生价。
//
// 注意一个必须防住的退化场景：平台还没为渠道上的模型配置标准倍率时，
// 派生结果为空。此时若照常"先删后写"，会把商家已有的盘口整片删空
// （表现为"改一次倍率，行情里这个渠道就没了"）。所以这种情况下保留既有盘口。
func RebuildChannelQuotesWithTx(tx *gorm.DB, channel *Channel) error {
	pools := splitQuoteList(channel.Group)
	models := splitQuoteList(channel.Models)
	// 渠道没有分组或没有模型：派生盘口本就应当为空，auto 行也全部失效，直接清干净
	if len(pools) == 0 || len(models) == 0 {
		if err := tx.Where("channel_id = ? AND source = ?", channel.Id, QuoteSourceDerived).
			Delete(&ChannelQuote{}).Error; err != nil {
			return err
		}
		return CleanupStaleAutoQuotes(tx, channel.Id, map[string]bool{})
	}
	// 渠道当前「分组 × 模型」范围：auto 行超出此范围的都属于幽灵盘口
	validScope := make(map[string]bool, len(pools)*len(models))
	for _, pool := range pools {
		for _, name := range models {
			validScope[pool+"|"+name] = true
		}
	}
	if err := CleanupStaleAutoQuotes(tx, channel.Id, validScope); err != nil {
		return err
	}
	rows := BuildDerivedQuotes(channel)
	if len(rows) == 0 {
		// 平台未配置这些模型的倍率，无法派生；保留既有盘口而不是删空
		return nil
	}
	// 自动同步与管理员锁定的报价优先：这些 (pool, model) 不写派生行，
	// 否则 upsert 会把范围内的 manual 锁定价整体冲成派生价
	var protectedRows []ChannelQuote
	if err := tx.Where("channel_id = ? AND source IN ?", channel.Id,
		[]string{QuoteSourceAuto, QuoteSourceManual}).
		Find(&protectedRows).Error; err != nil {
		return err
	}
	if len(protectedRows) > 0 {
		protectedSet := make(map[string]bool, len(protectedRows))
		for _, r := range protectedRows {
			protectedSet[r.PoolGroup+"|"+r.ModelName] = true
		}
		kept := make([]ChannelQuote, 0, len(rows))
		for _, r := range rows {
			if !protectedSet[r.PoolGroup+"|"+r.ModelName] {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	if err := tx.Where("channel_id = ? AND source = ?", channel.Id, QuoteSourceDerived).
		Delete(&ChannelQuote{}).Error; err != nil {
		return err
	}
	return UpsertChannelQuotesWithTx(tx, rows)
}

// RebuildChannelQuotes 按渠道ID重建派生盘口（独立事务版本，供改倍率等场景调用）。
func RebuildChannelQuotes(channelId int) error {
	var channel Channel
	if err := DB.First(&channel, channelId).Error; err != nil {
		return err
	}
	return RebuildChannelQuotesWithTx(DB, &channel)
}
