package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FinanceDomainUserQuota       = "user_quota"
	FinanceDomainPlatformRevenue = "platform_revenue"
	FinanceDomainMerchantWallet  = "merchant_wallet"
)

const (
	FinanceBizTopupCredit          = "topup_credit"
	FinanceBizSubscriptionPay      = "subscription_pay"
	FinanceBizRedemptionCredit     = "redemption_credit"
	FinanceBizAdminQuotaAdjust     = "admin_quota_adjust"
	FinanceBizAdminGiftQuotaAdjust = "admin_gift_quota_adjust"
	FinanceBizMerchantEarning      = "merchant_earning"
	FinanceBizMerchantCommission   = "merchant_commission"
	FinanceBizMerchantSettle       = "merchant_settle"
	FinanceBizMerchantReverse      = "merchant_earning_reverse"
	FinanceBizWithdrawFreeze       = "merchant_withdraw_freeze"
	FinanceBizWithdrawRefund       = "merchant_withdraw_refund"
	FinanceBizWithdrawPaid         = "merchant_withdraw_paid"
)

const (
	FinanceDirIn       = "in"
	FinanceDirOut      = "out"
	FinanceDirFreeze   = "freeze"
	FinanceDirUnfreeze = "unfreeze"
)

// FinanceLedger 统一不可变资金流水总账：贯通充值、订阅、兑换码、人工调额、商家分润、平台抽佣与提现打款。
// LedgerNo 带唯一约束以保障重放/重试幂等。
type FinanceLedger struct {
	Id            int     `json:"id" gorm:"primaryKey;autoIncrement"`
	LedgerNo      string  `json:"ledger_no" gorm:"type:varchar(96);uniqueIndex"`
	Domain        string  `json:"domain" gorm:"type:varchar(32);not null;index"`
	UserId        int     `json:"user_id" gorm:"default:0;index"`
	BizType       string  `json:"biz_type" gorm:"type:varchar(48);not null;index"`
	Direction     string  `json:"direction" gorm:"type:varchar(16);not null"`
	AmountUSD     float64 `json:"amount_usd" gorm:"default:0"`
	Money         float64 `json:"money" gorm:"default:0"`
	Currency      string  `json:"currency" gorm:"type:varchar(16);default:'USD'"`
	QuotaDelta    int64   `json:"quota_delta" gorm:"default:0"`
	BeforeBalance float64 `json:"before_balance" gorm:"default:0"`
	AfterBalance  float64 `json:"after_balance" gorm:"default:0"`
	RefType       string  `json:"ref_type" gorm:"type:varchar(32);default:''"`
	RefId         string  `json:"ref_id" gorm:"type:varchar(128);index;default:''"`
	OperatorId    int     `json:"operator_id" gorm:"default:0"`
	Remark        string  `json:"remark" gorm:"type:varchar(512);default:''"`
	CreatedAt     int64   `json:"created_at" gorm:"bigint;index"`
}

func (FinanceLedger) TableName() string {
	return "finance_ledgers"
}

// RecordFinanceLedger 幂等写入一条资金总账流水；若提供 tx 则同事务落库。
func RecordFinanceLedger(tx *gorm.DB, entry *FinanceLedger) error {
	if entry == nil {
		return nil
	}
	db := tx
	if db == nil {
		db = DB
	}
	if db == nil {
		return nil
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	if entry.Currency == "" {
		entry.Currency = "USD"
	}
	if entry.LedgerNo != "" {
		return db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ledger_no"}},
			DoNothing: true,
		}).Create(entry).Error
	}
	return db.Create(entry).Error
}

// RecordFinanceLedgerBestEffort 用于充值/兑换/订阅等外部主交易完成后的总账旁路记账，
// 避免局部测试库未迁移 finance_ledgers 时阻塞主流程。
func RecordFinanceLedgerBestEffort(entry *FinanceLedger) {
	if DB == nil || entry == nil {
		return
	}
	if !DB.Migrator().HasTable(&FinanceLedger{}) {
		return
	}
	if err := RecordFinanceLedger(nil, entry); err != nil {
		common.SysError("record finance ledger failed: " + err.Error())
	}
}
