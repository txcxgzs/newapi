package model

import "gorm.io/gorm"

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
	MerchantAppTypeChannel     = "channel"      // 入驻或新增渠道/分组
	MerchantAppTypeGroupChange = "group_change" // 申请调整已有商家分组的倍率/描述/Auto路由
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
	Id          int     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int     `json:"user_id" gorm:"not null;uniqueIndex"`
	SiteName    string  `json:"site_name" gorm:"type:varchar(128);not null"`
	SiteUrl     string  `json:"site_url" gorm:"type:varchar(255);not null"`
	ProfitRatio float64 `json:"profit_ratio" gorm:"default:0.95"` // 商家分成比例 0..1，默认商家 95% / 平台 5%
	Status      int     `json:"status" gorm:"default:1"`
	CreatedAt   int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64   `json:"updated_at" gorm:"bigint"`
}

func (MerchantProfile) TableName() string {
	return "merchant_profiles"
}

// MerchantApplication 商家入驻与分组变更申请：支持首次入驻、扩容渠道/自建专属分组，
// 以及调整已有商家分组的倍率与 Auto 自动路由状态；管理员审核通过后写入原生分组配置并派生盘口。
type MerchantApplication struct {
	Id            int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId        int    `json:"user_id" gorm:"not null;index"`
	AppType       string `json:"app_type" gorm:"type:varchar(32);default:'channel'"`
	SiteName      string `json:"site_name" gorm:"type:varchar(128);not null"`
	SiteUrl       string `json:"site_url" gorm:"type:varchar(255);not null"`
	SiteKey       string `json:"-" gorm:"type:text;not null"` // 上游密钥，永不回显；审核结束后清空
	Models        string `json:"models" gorm:"type:text"`     // json: [{model,pool}]
	GroupName     string `json:"group_name" gorm:"type:varchar(64);default:''"`
	GroupDesc     string `json:"group_desc" gorm:"type:varchar(255);default:''"`
	JoinAutoGroup bool   `json:"join_auto_group" gorm:"default:false"`
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

// MerchantWithdrawal 商家提现申请。审批通过并打款后标记 paid 并扣减钱包余额。
type MerchantWithdrawal struct {
	Id             int     `json:"id" gorm:"primaryKey;autoIncrement"`
	MerchantUserId int     `json:"merchant_user_id" gorm:"not null;index"`
	Amount         float64 `json:"amount" gorm:"not null"` // USD
	Status         int     `json:"status" gorm:"default:0"`
	AccountInfo    string  `json:"account_info" gorm:"type:varchar(1024)"` // 收款方式说明（支付宝/USDT/银行卡等）
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
