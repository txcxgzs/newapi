package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MerchantWallet 商家分润钱包：余额与累计口径。金额单位统一为 USD。
type MerchantWallet struct {
	Id             int     `json:"id" gorm:"primaryKey;autoIncrement"`
	MerchantUserId int     `json:"merchant_user_id" gorm:"not null;uniqueIndex"`
	Balance        float64 `json:"balance" gorm:"default:0"`
	TotalEarned    float64 `json:"total_earned" gorm:"default:0"`
	TotalWithdrawn float64 `json:"total_withdrawn" gorm:"default:0"`
	UpdatedAt      int64   `json:"updated_at" gorm:"bigint"`
}

func (MerchantWallet) TableName() string {
	return "merchant_wallets"
}

// MerchantEarning 分润流水：每笔真实成交对应一条。幂等由 dedup_key 唯一索引保证：
// 有消费日志时为 "log:<log_id>"；日志未启用或写失败时为请求级兜底键；两者皆无时为
// NULL（唯一索引不约束 NULL，退化为无幂等入账）。log_id 不再有唯一索引——分润
// 与消费日志解耦后，log_id=0 是合法流水。
type MerchantEarning struct {
	Id             int     `json:"id" gorm:"primaryKey;autoIncrement"`
	MerchantUserId int     `json:"merchant_user_id" gorm:"not null;index"`
	ChannelId      int     `json:"channel_id" gorm:"not null;index"`
	LogId          int     `json:"log_id" gorm:"not null"`
	DedupKey       *string `json:"-" gorm:"uniqueIndex"`
	Amount         float64 `json:"amount"` // USD
	ModelName      string  `json:"model_name" gorm:"type:varchar(255)"`
	CreatedAt      int64   `json:"created_at" gorm:"bigint"`
}

func (MerchantEarning) TableName() string {
	return "merchant_earnings"
}

// GetOrCreateMerchantWallet 取商家钱包，不存在则创建。
func GetOrCreateMerchantWallet(userId int) (*MerchantWallet, error) {
	return EnsureMerchantWallet(DB, userId)
}

// GetMerchantWalletByUserId 只读获取商家钱包。读接口必须用它，
// 避免查询请求产生写副作用与并发建行竞态。
func GetMerchantWalletByUserId(userId int) (*MerchantWallet, error) {
	var wallet MerchantWallet
	if err := DB.Where("merchant_user_id = ?", userId).First(&wallet).Error; err != nil {
		return nil, err
	}
	return &wallet, nil
}

// EnsureMerchantWallet 在给定连接（通常是事务）内取或建钱包。
// 并发创建依赖 merchant_user_id 唯一索引去重，冲突时回读已存在的行。
func EnsureMerchantWallet(db *gorm.DB, userId int) (*MerchantWallet, error) {
	var wallet MerchantWallet
	err := db.Where("merchant_user_id = ?", userId).First(&wallet).Error
	if err == nil {
		return &wallet, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	wallet = MerchantWallet{MerchantUserId: userId, UpdatedAt: time.Now().Unix()}
	createErr := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "merchant_user_id"}},
		DoNothing: true,
	}).Create(&wallet).Error
	if createErr != nil {
		return nil, createErr
	}
	if wallet.Id == 0 {
		if err := db.Where("merchant_user_id = ?", userId).First(&wallet).Error; err != nil {
			return nil, err
		}
	}
	return &wallet, nil
}

// RecordMerchantEarning 记一笔分润：流水幂等插入 + 钱包余额累加，两者同一事务。
// 同一 dedup_key 重复调用只入账一次；任一步失败都会回滚，不会出现"有流水没到账"。
func RecordMerchantEarning(tx *gorm.DB, earning *MerchantEarning) error {
	if tx != nil {
		return recordMerchantEarning(tx, earning)
	}
	return DB.Transaction(func(inner *gorm.DB) error {
		return recordMerchantEarning(inner, earning)
	})
}

func recordMerchantEarning(tx *gorm.DB, earning *MerchantEarning) error {
	earning.CreatedAt = time.Now().Unix()
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedup_key"}},
		DoNothing: true,
	}).Create(earning)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil // 已入过账
	}
	wallet, err := EnsureMerchantWallet(tx, earning.MerchantUserId)
	if err != nil {
		return err
	}
	return tx.Model(&MerchantWallet{}).
		Where("id = ?", wallet.Id).
		Updates(map[string]any{
			"balance":      gorm.Expr("balance + ?", earning.Amount),
			"total_earned": gorm.Expr("total_earned + ?", earning.Amount),
			"updated_at":   time.Now().Unix(),
		}).Error
}

// FreezeForWithdrawal 提现申请时冻结余额（直接扣减，拒绝时退回）。
func FreezeForWithdrawal(tx *gorm.DB, userId int, amount float64) error {
	db := tx
	if db == nil {
		db = DB
	}
	res := db.Model(&MerchantWallet{}).
		Where("merchant_user_id = ? AND balance >= ?", userId, amount).
		Updates(map[string]any{
			"balance":    gorm.Expr("balance - ?", amount),
			"updated_at": time.Now().Unix(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

// RefundWithdrawal 提现被拒时退回冻结金额。
func RefundWithdrawal(tx *gorm.DB, userId int, amount float64) error {
	db := tx
	if db == nil {
		db = DB
	}
	return db.Model(&MerchantWallet{}).
		Where("merchant_user_id = ?", userId).
		Updates(map[string]any{
			"balance":    gorm.Expr("balance + ?", amount),
			"updated_at": time.Now().Unix(),
		}).Error
}

// ConfirmWithdrawalPaid 提现打款确认：累计已提现。
func ConfirmWithdrawalPaid(tx *gorm.DB, userId int, amount float64) error {
	db := tx
	if db == nil {
		db = DB
	}
	return db.Model(&MerchantWallet{}).
		Where("merchant_user_id = ?", userId).
		Updates(map[string]any{
			"total_withdrawn": gorm.Expr("total_withdrawn + ?", amount),
			"updated_at":      time.Now().Unix(),
		}).Error
}

// downgradeMerchantEarningLogIdIndex 删除历史版本给 log_id 建的唯一索引。
// 分润与消费日志解耦后 log_id=0 是合法流水，唯一索引会让第二条起全部写失败；
// GORM AutoMigrate 不会修改已存在索引的唯一性，只能显式删除。
func downgradeMerchantEarningLogIdIndex() error {
	if DB.Migrator().HasIndex(&MerchantEarning{}, "idx_merchant_earnings_log_id") {
		if err := DB.Migrator().DropIndex(&MerchantEarning{}, "idx_merchant_earnings_log_id"); err != nil {
			return fmt.Errorf("drop legacy merchant_earnings log_id unique index: %w", err)
		}
	}
	return nil
}

// ensurePendingMerchantApplicationUniqueIndex 入驻申请并发查重的数据库层兜底：
// PostgreSQL 的 FOR UPDATE 不锁不存在的行，先查后插仍有双插竞态窗口。
// SQLite/PostgreSQL 支持部分唯一索引（仅待审行参与唯一约束）；MySQL 不支持，
// 由事务内锁定读（user_id 索引间隙锁，依赖默认 REPEATABLE READ 隔离级别）保证。
// status = 0 即 MerchantApplicationPending，与表名一样是刻意内联的 DDL。
// 存量已有重复待审数据时创建失败：只记日志不阻断启动，应用层查重仍然生效
// （CREATE 原子失败即无索引，后续插入不会撞它，退化仅为失去 DB 层兜底）。
func ensurePendingMerchantApplicationUniqueIndex() error {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return nil
	}
	if err := DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_merchant_applications_pending_user ON merchant_applications (user_id) WHERE status = 0").Error; err != nil {
		common.SysError("create pending merchant application unique index failed, " +
			"check duplicate rows with same user_id and status=0 in merchant_applications: " + err.Error())
	}
	return nil
}
