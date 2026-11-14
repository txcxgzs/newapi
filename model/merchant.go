package model

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// 商家资料状态
const (
	MerchantStatusActive    = 1
	MerchantStatusSuspended = 2
)

// 入驻申请状态
const (
	MerchantApplicationPending  = 0
	MerchantApplicationApproved = 1
	MerchantApplicationRejected = 2
)

// 商家申请类型
const (
	MerchantAppTypeChannel       = "channel"        // 入驻或新增渠道/分组
	MerchantAppTypeGroupChange   = "group_change"   // 申请调整已有商家分组的倍率/描述/Auto路由
	MerchantAppTypeProfileChange = "profile_change" // 申请变更商户名称（须管理员审核）
)

// 提现状态
const (
	WithdrawalPending  = 0
	WithdrawalApproved = 1
	WithdrawalPaid     = 2
	WithdrawalRejected = 3
)

// MerchantProfile 商家资料：审核通过后创建，与用户一对一。商家门户的数据隔离锚点。
type MerchantProfile struct {
	Id              int     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId          int     `json:"user_id" gorm:"not null;uniqueIndex"`
	MerchantCode    string  `json:"merchant_code" gorm:"type:varchar(32);default:''"` // 稳定商户短码，如 m12
	ShopName        string  `json:"shop_name" gorm:"type:varchar(128);default:''"`    // 店铺/商户名称（对外抬头/对账/分组前缀，更改须审核）
	PlatformUrl     string  `json:"platform_url" gorm:"type:varchar(255);default:''"` // 商家自有平台官网/展示站网址（仅审核展示，不作 API 调用）
	OperationTenure string  `json:"operation_tenure" gorm:"type:varchar(64);default:''"`
	SiteName        string  `json:"site_name" gorm:"type:varchar(128);not null"`
	SiteUrl         string  `json:"site_url" gorm:"type:varchar(255);not null"`
	ContactName     string  `json:"contact_name" gorm:"type:varchar(64);default:''"`
	ContactInfo     string  `json:"contact_info" gorm:"type:varchar(255);default:''"`
	Description     string  `json:"description" gorm:"type:varchar(512);default:''"`
	PayoutMethod    string  `json:"payout_method" gorm:"type:varchar(32);default:''"` // usdt_trc20 / usdt_bep20 / alipay / wechat / bank / other
	PayoutAccount   string  `json:"payout_account" gorm:"type:varchar(512);default:''"`
	PayoutHolder    string  `json:"payout_holder" gorm:"type:varchar(128);default:''"`
	PayoutQrcode    string  `json:"payout_qrcode" gorm:"type:text"`  // 收款二维码 Data URL（仅图片 MIME）
	SettleHoldDays  int     `json:"settle_hold_days" gorm:"default:0"` // T+N 结算延迟天数，0 表示即时入可用余额
	MinWithdraw     float64 `json:"min_withdraw" gorm:"default:0"`     // 最低提现门槛（USD），0 表示不限制
	AdminNote       string  `json:"admin_note" gorm:"type:varchar(512);default:''"`
	ProfitRatio     float64 `json:"profit_ratio" gorm:"default:0.95"` // 商家分成比例 0..1，默认商家 95% / 平台 5%
	Status          int     `json:"status" gorm:"default:1"`
	CreatedAt       int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt       int64   `json:"updated_at" gorm:"bigint"`
}

func (MerchantProfile) TableName() string {
	return "merchant_profiles"
}

// MerchantApplication 商家入驻与分组变更申请：支持首次入驻、扩容渠道/自建专属分组，
// 以及调整已有商家分组的倍率与 Auto 自动路由状态；管理员审核通过后写入原生分组配置并派生盘口。
type MerchantApplication struct {
	Id                int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId            int    `json:"user_id" gorm:"not null;index"`
	AppType           string `json:"app_type" gorm:"type:varchar(32);default:'channel'"`
	IsOnboarding      bool   `json:"is_onboarding" gorm:"default:true"`
	ShopName          string `json:"shop_name" gorm:"type:varchar(128);default:''"`
	RequestedShopName string `json:"requested_shop_name" gorm:"type:varchar(128);default:''"`
	PlatformUrl       string `json:"platform_url" gorm:"type:varchar(255);default:''"`
	OperationTenure   string `json:"operation_tenure" gorm:"type:varchar(64);default:''"`
	SiteName          string `json:"site_name" gorm:"type:varchar(128);not null"`
	SiteUrl           string `json:"site_url" gorm:"type:varchar(255);not null"`
	SiteKey           string `json:"-" gorm:"type:text;not null"` // 上游密钥（AES-GCM 加密暂存），永不回显；审核结束后清空
	ContactName       string `json:"contact_name" gorm:"type:varchar(64);default:''"`
	ContactInfo       string `json:"contact_info" gorm:"type:varchar(255);default:''"`
	Description       string `json:"description" gorm:"type:varchar(512);default:''"`
	PayoutMethod      string `json:"payout_method" gorm:"type:varchar(32);default:''"`
	PayoutAccount     string `json:"payout_account" gorm:"type:varchar(512);default:''"`
	PayoutHolder      string `json:"payout_holder" gorm:"type:varchar(128);default:''"`
	PayoutQrcode      string `json:"payout_qrcode" gorm:"type:text"`
	AgreedTerms       bool   `json:"agreed_terms" gorm:"default:false"`
	Models         string `json:"models" gorm:"type:text"` // json: [{model,pool}]
	GroupName      string `json:"group_name" gorm:"type:varchar(64);default:''"`
	GroupDesc      string `json:"group_desc" gorm:"type:varchar(255);default:''"`
	JoinAutoGroup  bool   `json:"join_auto_group" gorm:"default:false"`
	// RateMultiplier 申请时声明的分组倍率（GroupRatio）。
	// 审核时管理员可覆盖；未填写（0）视为 1.0 原价。
	RateMultiplier float64 `json:"rate_multiplier" gorm:"default:1"`
	// UpstreamKind 申请的网关类型：newapi / sub2api / openai，空为自动判定。
	// 决定审核通过后渠道的中继端点家族与报价同步能力。
	UpstreamKind string `json:"upstream_kind" gorm:"type:varchar(16);default:''"`
	Status       int    `json:"status" gorm:"default:0"`
	ReviewNote   string `json:"review_note" gorm:"type:varchar(512)"`
	ReviewedBy   int    `json:"reviewed_by"`
	ReviewedAt   int64  `json:"reviewed_at" gorm:"bigint"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

func (MerchantApplication) TableName() string {
	return "merchant_applications"
}

// MerchantWithdrawal 商家提现申请。审批通过并打款后标记 paid 并扣减钱包冻结余额。
type MerchantWithdrawal struct {
	Id             int     `json:"id" gorm:"primaryKey;autoIncrement"`
	TradeNo        string  `json:"trade_no" gorm:"type:varchar(64);index;default:''"`
	MerchantUserId int     `json:"merchant_user_id" gorm:"not null;index"`
	Amount         float64 `json:"amount" gorm:"not null"` // USD
	Status         int     `json:"status" gorm:"default:0"`
	PayoutMethod   string  `json:"payout_method" gorm:"type:varchar(32);default:''"`
	AccountInfo    string  `json:"account_info" gorm:"type:varchar(1024)"` // 收款方式说明（支付宝/USDT/银行卡等）
	PayoutQrcode   string  `json:"payout_qrcode" gorm:"type:text"`          // 提现建单时的收款二维码快照
	Note           string  `json:"note" gorm:"type:varchar(512)"`
	ReviewNote     string  `json:"review_note" gorm:"type:varchar(512)"`
	ReviewedBy     int     `json:"reviewed_by"`
	ReviewedAt     int64   `json:"reviewed_at" gorm:"bigint"`
	PaidAt         int64   `json:"paid_at" gorm:"bigint"`
	PaidProof      string  `json:"paid_proof" gorm:"type:varchar(512)"`
	CreatedAt      int64   `json:"created_at" gorm:"bigint"`
}

func (MerchantWithdrawal) TableName() string {
	return "merchant_withdrawals"
}

// GetMerchantProfileByUserId 取商家资料。
func GetMerchantProfileByUserId(userId int) (*MerchantProfile, error) {
	var profile MerchantProfile
	err := DB.Where("user_id = ?", userId).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// GetMerchantChannelIds 取商家名下全部渠道ID，数据隔离的过滤条件。
func GetMerchantChannelIds(userId int) ([]int, error) {
	var ids []int
	err := DB.Model(&Channel{}).Where("merchant_id = ?", userId).Pluck("id", &ids).Error
	return ids, err
}

// GetMerchantApplicationForUpdate 事务内锁定申请行：并发审核只允许一个成功。
func GetMerchantApplicationForUpdate(tx *gorm.DB, id int) (*MerchantApplication, error) {
	var app MerchantApplication
	if err := lockForUpdate(tx).First(&app, id).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// GetMerchantWithdrawalForUpdate 事务内锁定提现行：避免重复退款或重复累计已提现。
func GetMerchantWithdrawalForUpdate(tx *gorm.DB, id int) (*MerchantWithdrawal, error) {
	var withdrawal MerchantWithdrawal
	if err := lockForUpdate(tx).First(&withdrawal, id).Error; err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

// MerchantDisplayInfo holds lightweight cached merchant identity for order-book routing,
// response headers, logs, and public orderbook views without hitting DB on every request.
type MerchantDisplayInfo struct {
	UserId       int    `json:"user_id"`
	MerchantCode string `json:"merchant_code"`
	DisplayName  string `json:"display_name"`
	ShopName     string `json:"shop_name"`
	SiteName     string `json:"site_name"`
	Status       int    `json:"status"`
}

var (
	merchantDisplayCacheMu     sync.RWMutex
	merchantDisplayCache       = make(map[int]MerchantDisplayInfo)
	merchantDisplayCacheLoaded time.Time
)

const merchantDisplayCacheTTL = 30 * time.Second

func formatMerchantDisplayName(profile MerchantProfile) string {
	if s := strings.TrimSpace(profile.ShopName); s != "" {
		return s
	}
	if s := strings.TrimSpace(profile.SiteName); s != "" {
		return s
	}
	if s := strings.TrimSpace(profile.MerchantCode); s != "" {
		return s
	}
	if profile.UserId > 0 {
		return fmt.Sprintf("Merchant #%d", profile.UserId)
	}
	return "Platform"
}

// RefreshMerchantDisplayCache reloads all merchant display entries from the DB.
func RefreshMerchantDisplayCache() map[int]MerchantDisplayInfo {
	if DB == nil {
		merchantDisplayCacheMu.RLock()
		defer merchantDisplayCacheMu.RUnlock()
		out := make(map[int]MerchantDisplayInfo, len(merchantDisplayCache))
		for k, v := range merchantDisplayCache {
			out[k] = v
		}
		return out
	}

	var profiles []MerchantProfile
	if err := DB.Select("user_id, merchant_code, shop_name, site_name, status").Find(&profiles).Error; err != nil {
		merchantDisplayCacheMu.RLock()
		defer merchantDisplayCacheMu.RUnlock()
		out := make(map[int]MerchantDisplayInfo, len(merchantDisplayCache))
		for k, v := range merchantDisplayCache {
			out[k] = v
		}
		return out
	}

	next := make(map[int]MerchantDisplayInfo, len(profiles))
	for _, p := range profiles {
		if p.UserId <= 0 {
			continue
		}
		next[p.UserId] = MerchantDisplayInfo{
			UserId:       p.UserId,
			MerchantCode: strings.TrimSpace(p.MerchantCode),
			DisplayName:  formatMerchantDisplayName(p),
			ShopName:     strings.TrimSpace(p.ShopName),
			SiteName:     strings.TrimSpace(p.SiteName),
			Status:       p.Status,
		}
	}

	merchantDisplayCacheMu.Lock()
	merchantDisplayCache = next
	merchantDisplayCacheLoaded = time.Now()
	merchantDisplayCacheMu.Unlock()

	return next
}

// GetMerchantDisplayMap returns cached merchant display metadata keyed by merchant UserId.
func GetMerchantDisplayMap() map[int]MerchantDisplayInfo {
	merchantDisplayCacheMu.RLock()
	fresh := !merchantDisplayCacheLoaded.IsZero() && time.Since(merchantDisplayCacheLoaded) < merchantDisplayCacheTTL
	if fresh || DB == nil {
		out := make(map[int]MerchantDisplayInfo, len(merchantDisplayCache))
		for k, v := range merchantDisplayCache {
			out[k] = v
		}
		merchantDisplayCacheMu.RUnlock()
		return out
	}
	merchantDisplayCacheMu.RUnlock()
	return RefreshMerchantDisplayCache()
}

// GetMerchantDisplayInfo returns cached display metadata for a merchant UserId (0 -> Platform).
func GetMerchantDisplayInfo(merchantUserId int) MerchantDisplayInfo {
	if merchantUserId <= 0 {
		return MerchantDisplayInfo{
			UserId:       0,
			MerchantCode: "platform",
			DisplayName:  "Platform",
			Status:       MerchantStatusActive,
		}
	}
	m := GetMerchantDisplayMap()
	if info, ok := m[merchantUserId]; ok {
		return info
	}
	return MerchantDisplayInfo{
		UserId:       merchantUserId,
		MerchantCode: fmt.Sprintf("m%d", merchantUserId),
		DisplayName:  fmt.Sprintf("Merchant #%d", merchantUserId),
		Status:       MerchantStatusActive,
	}
}

// InvalidateMerchantDisplayCache forces the next read to reload from DB.
func InvalidateMerchantDisplayCache() {
	merchantDisplayCacheMu.Lock()
	merchantDisplayCacheLoaded = time.Time{}
	merchantDisplayCacheMu.Unlock()
}

// SetMerchantDisplayCacheForTest seeds merchant display cache entries in unit tests without DB.
func SetMerchantDisplayCacheForTest(entries map[int]MerchantDisplayInfo) {
	merchantDisplayCacheMu.Lock()
	defer merchantDisplayCacheMu.Unlock()
	merchantDisplayCache = make(map[int]MerchantDisplayInfo, len(entries))
	for k, v := range entries {
		merchantDisplayCache[k] = v
	}
	merchantDisplayCacheLoaded = time.Now()
}
