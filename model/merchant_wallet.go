package model

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// MerchantEarningSettled 已结算进入可提现余额（兼容旧行/零值默认视为已结算）。
	MerchantEarningSettled = 1
	// MerchantEarningUnsettled 待结算（T+N 账期冻结中）。
	MerchantEarningUnsettled = 2
	// MerchantEarningReversed 已冲正扣回（退款或风控扣款）。
	MerchantEarningReversed = 3
)

// MerchantWallet 商家分润钱包：可用、待结算(T+N)、提现锁定与累计口径。金额单位统一为 USD。
type MerchantWallet struct {
	Id              int     `json:"id" gorm:"primaryKey;autoIncrement"`
	MerchantUserId  int     `json:"merchant_user_id" gorm:"not null;uniqueIndex"`
	Balance         float64 `json:"balance" gorm:"default:0"`          // 可提现可用余额
	PendingBalance  float64 `json:"pending_balance" gorm:"default:0"`  // T+N 待结算余额
	FrozenBalance   float64 `json:"frozen_balance" gorm:"default:0"`   // 提现审核锁定余额
	TotalEarned     float64 `json:"total_earned" gorm:"default:0"`     // 累计应得分成（未扣冲正前有效累计）
	TotalWithdrawn  float64 `json:"total_withdrawn" gorm:"default:0"`  // 累计已打款提现
	TotalCommission float64 `json:"total_commission" gorm:"default:0"` // 平台从该商家渠道累计抽佣
	UpdatedAt       int64   `json:"updated_at" gorm:"bigint"`
}

func (MerchantWallet) TableName() string {
	return "merchant_wallets"
}

// MerchantEarning 分润流水：每笔真实成交对应一条。幂等由 dedup_key 唯一索引保证：
// 有消费日志时为 "log:<log_id>"；日志未启用或写失败时为请求级兜底键；两者皆无时为
// NULL（唯一索引不约束 NULL，退化为无幂等入账）。log_id 不再有唯一索引——分润
// 与消费日志解耦后，log_id=0 是合法流水。
type MerchantEarning struct {
	Id               int     `json:"id" gorm:"primaryKey;autoIncrement"`
	MerchantUserId   int     `json:"merchant_user_id" gorm:"not null;index"`
	ChannelId        int     `json:"channel_id" gorm:"not null;index"`
	LogId            int     `json:"log_id" gorm:"not null"`
	DedupKey         *string `json:"-" gorm:"type:varchar(191);uniqueIndex"`
	GrossAmount      float64 `json:"gross_amount" gorm:"default:0"`      // 成交总额（USD）
	Amount           float64 `json:"amount"`                           // 商家实得分成（USD）
	CommissionAmount float64 `json:"commission_amount" gorm:"default:0"` // 平台抽佣（USD）
	Status           int     `json:"status" gorm:"default:1;index"`    // 1=已结算 2=待结算(T+N) 3=已冲正
	SettleAt         int64   `json:"settle_at" gorm:"bigint;default:0;index"`
	SettledAt        int64   `json:"settled_at" gorm:"bigint;default:0"`
	ModelName        string  `json:"model_name" gorm:"type:varchar(255)"`
	CreatedAt        int64   `json:"created_at" gorm:"bigint"`
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
	now := time.Now().Unix()
	if earning.CreatedAt == 0 {
		earning.CreatedAt = now
	}
	if earning.GrossAmount <= 0 {
		earning.GrossAmount = earning.Amount + earning.CommissionAmount
	}
	if earning.Status == 0 {
		if earning.SettleAt > now {
			earning.Status = MerchantEarningUnsettled
		} else {
			earning.Status = MerchantEarningSettled
		}
	}
	if earning.Status == MerchantEarningSettled && earning.SettledAt == 0 {
		earning.SettledAt = now
	}

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
	balanceField := "balance"
	beforeBal := wallet.Balance
	if earning.Status == MerchantEarningUnsettled {
		balanceField = "pending_balance"
		beforeBal = wallet.PendingBalance
	}
	updates := map[string]any{
		balanceField:       gorm.Expr(balanceField+" + ?", earning.Amount),
		"total_earned":     gorm.Expr("total_earned + ?", earning.Amount),
		"total_commission": gorm.Expr("total_commission + ?", earning.CommissionAmount),
		"updated_at":       now,
	}
	if err := tx.Model(&MerchantWallet{}).Where("id = ?", wallet.Id).Updates(updates).Error; err != nil {
		return err
	}

	if tx.Migrator().HasTable(&FinanceLedger{}) {
		refId := fmt.Sprintf("%d", earning.Id)
		remark := fmt.Sprintf("model=%s channel=%d", earning.ModelName, earning.ChannelId)
		if earning.Status == MerchantEarningUnsettled {
			remark += " (pending T+N)"
		}
		if err := RecordFinanceLedger(tx, &FinanceLedger{
			LedgerNo:      fmt.Sprintf("merchant_earning:%d", earning.Id),
			Domain:        FinanceDomainMerchantWallet,
			UserId:        earning.MerchantUserId,
			BizType:       FinanceBizMerchantEarning,
			Direction:     FinanceDirIn,
			AmountUSD:     earning.Amount,
			BeforeBalance: beforeBal,
			AfterBalance:  beforeBal + earning.Amount,
			RefType:       "merchant_earning",
			RefId:         refId,
			Remark:        remark,
			CreatedAt:     now,
		}); err != nil {
			return err
		}
		if earning.CommissionAmount > 0 {
			if err := RecordFinanceLedger(tx, &FinanceLedger{
				LedgerNo:  fmt.Sprintf("merchant_commission:%d", earning.Id),
				Domain:    FinanceDomainPlatformRevenue,
				UserId:    earning.MerchantUserId,
				BizType:   FinanceBizMerchantCommission,
				Direction: FinanceDirIn,
				AmountUSD: earning.CommissionAmount,
				RefType:   "merchant_earning",
				RefId:     refId,
				Remark:    remark,
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// FreezeForWithdrawal 提现申请时冻结余额：从可用余额转移至 frozen_balance。
func FreezeForWithdrawal(tx *gorm.DB, userId int, amount float64) error {
	db := tx
	if db == nil {
		db = DB
	}
	res := db.Model(&MerchantWallet{}).
		Where("merchant_user_id = ? AND balance >= ?", userId, amount).
		Updates(map[string]any{
			"balance":        gorm.Expr("balance - ?", amount),
			"frozen_balance": gorm.Expr("frozen_balance + ?", amount),
			"updated_at":     time.Now().Unix(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

// RefundWithdrawal 提现被拒时退回冻结金额：可用余额加回，frozen_balance 扣减（下限 0）。
func RefundWithdrawal(tx *gorm.DB, userId int, amount float64) error {
	db := tx
	if db == nil {
		db = DB
	}
	return db.Model(&MerchantWallet{}).
		Where("merchant_user_id = ?", userId).
		Updates(map[string]any{
			"balance":        gorm.Expr("balance + ?", amount),
			"frozen_balance": gorm.Expr("CASE WHEN frozen_balance >= ? THEN frozen_balance - ? ELSE 0 END", amount, amount),
			"updated_at":     time.Now().Unix(),
		}).Error
}

// ConfirmWithdrawalPaid 提现打款确认：扣除 frozen_balance 并累计已提现。
func ConfirmWithdrawalPaid(tx *gorm.DB, userId int, amount float64) error {
	db := tx
	if db == nil {
		db = DB
	}
	return db.Model(&MerchantWallet{}).
		Where("merchant_user_id = ?", userId).
		Updates(map[string]any{
			"total_withdrawn": gorm.Expr("total_withdrawn + ?", amount),
			"frozen_balance":  gorm.Expr("CASE WHEN frozen_balance >= ? THEN frozen_balance - ? ELSE 0 END", amount, amount),
			"updated_at":      time.Now().Unix(),
		}).Error
}

// SettleMerchantEarningsForUser 在事务内把单个商家到期的 T+N 待结算分润解冻转入可用余额。
func SettleMerchantEarningsForUser(tx *gorm.DB, userId int, nowUnix int64) (int64, float64, error) {
	if nowUnix <= 0 {
		nowUnix = time.Now().Unix()
	}
	var due []MerchantEarning
	if err := lockForUpdate(tx).
		Where("merchant_user_id = ? AND status = ? AND settle_at <= ?", userId, MerchantEarningUnsettled, nowUnix).
		Find(&due).Error; err != nil {
		return 0, 0, err
	}
	if len(due) == 0 {
		return 0, 0, nil
	}
	var totalAmount float64
	ids := make([]int, 0, len(due))
	for _, e := range due {
		totalAmount += e.Amount
		ids = append(ids, e.Id)
	}
	res := tx.Model(&MerchantEarning{}).
		Where("id IN ? AND status = ?", ids, MerchantEarningUnsettled).
		Updates(map[string]any{
			"status":     MerchantEarningSettled,
			"settled_at": nowUnix,
		})
	if res.Error != nil {
		return 0, 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, 0, nil
	}
	wallet, err := EnsureMerchantWallet(tx, userId)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Model(&MerchantWallet{}).
		Where("id = ?", wallet.Id).
		Updates(map[string]any{
			"balance":         gorm.Expr("balance + ?", totalAmount),
			"pending_balance": gorm.Expr("CASE WHEN pending_balance >= ? THEN pending_balance - ? ELSE 0 END", totalAmount, totalAmount),
			"updated_at":      nowUnix,
		}).Error; err != nil {
		return 0, 0, err
	}
	if tx.Migrator().HasTable(&FinanceLedger{}) {
		if err := RecordFinanceLedger(tx, &FinanceLedger{
			LedgerNo:      fmt.Sprintf("merchant_settle:%d:%d", userId, ids[len(ids)-1]),
			Domain:        FinanceDomainMerchantWallet,
			UserId:        userId,
			BizType:       FinanceBizMerchantSettle,
			Direction:     FinanceDirUnfreeze,
			AmountUSD:     totalAmount,
			BeforeBalance: wallet.Balance,
			AfterBalance:  wallet.Balance + totalAmount,
			RefType:       "merchant_settle",
			RefId:         fmt.Sprintf("count:%d", len(ids)),
			Remark:        fmt.Sprintf("settled %d T+N earnings", len(ids)),
			CreatedAt:     nowUnix,
		}); err != nil {
			return 0, 0, err
		}
	}
	return res.RowsAffected, totalAmount, nil
}

// SettleDueMerchantEarnings 批量结算全平台所有已到期的 T+N 商家分润。
func SettleDueMerchantEarnings(nowUnix int64) (int64, float64, error) {
	if DB == nil {
		return 0, 0, nil
	}
	if nowUnix <= 0 {
		nowUnix = time.Now().Unix()
	}
	var userIds []int
	if err := DB.Model(&MerchantEarning{}).
		Where("status = ? AND settle_at <= ?", MerchantEarningUnsettled, nowUnix).
		Distinct("merchant_user_id").
		Pluck("merchant_user_id", &userIds).Error; err != nil {
		return 0, 0, err
	}
	var totalCount int64
	var totalAmount float64
	for _, uid := range userIds {
		var count int64
		var amount float64
		err := DB.Transaction(func(tx *gorm.DB) error {
			var innerErr error
			count, amount, innerErr = SettleMerchantEarningsForUser(tx, uid, nowUnix)
			return innerErr
		})
		if err != nil {
			return totalCount, totalAmount, err
		}
		totalCount += count
		totalAmount += amount
	}
	return totalCount, totalAmount, nil
}

// ReverseMerchantEarning 管理员冲正争议/退款分润：将分润标记为已冲正，并从待结算/可用余额及总收益中扣回。
func ReverseMerchantEarning(earningId int, adminId int, reason string) error {
	now := time.Now().Unix()
	return DB.Transaction(func(tx *gorm.DB) error {
		var earning MerchantEarning
		if err := lockForUpdate(tx).First(&earning, earningId).Error; err != nil {
			return err
		}
		if earning.Status == MerchantEarningReversed {
			return errors.New("该分润流水已冲正")
		}
		wasUnsettled := earning.Status == MerchantEarningUnsettled
		wallet, err := EnsureMerchantWallet(tx, earning.MerchantUserId)
		if err != nil {
			return err
		}
		var beforeBal float64
		updates := map[string]any{
			"total_earned":     gorm.Expr("CASE WHEN total_earned >= ? THEN total_earned - ? ELSE 0 END", earning.Amount, earning.Amount),
			"total_commission": gorm.Expr("CASE WHEN total_commission >= ? THEN total_commission - ? ELSE 0 END", earning.CommissionAmount, earning.CommissionAmount),
			"updated_at":       now,
		}
		if wasUnsettled {
			beforeBal = wallet.PendingBalance
			updates["pending_balance"] = gorm.Expr("CASE WHEN pending_balance >= ? THEN pending_balance - ? ELSE 0 END", earning.Amount, earning.Amount)
		} else {
			beforeBal = wallet.Balance
			if wallet.Balance+1e-9 < earning.Amount {
				return ErrInsufficientBalance
			}
			updates["balance"] = gorm.Expr("balance - ?", earning.Amount)
		}
		if err := tx.Model(&MerchantWallet{}).Where("id = ?", wallet.Id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&MerchantEarning{}).Where("id = ?", earning.Id).Update("status", MerchantEarningReversed).Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable(&FinanceLedger{}) {
			afterBal := math.Max(0, beforeBal-earning.Amount)
			if err := RecordFinanceLedger(tx, &FinanceLedger{
				LedgerNo:      fmt.Sprintf("merchant_reverse:%d", earning.Id),
				Domain:        FinanceDomainMerchantWallet,
				UserId:        earning.MerchantUserId,
				BizType:       FinanceBizMerchantReverse,
				Direction:     FinanceDirOut,
				AmountUSD:     earning.Amount,
				BeforeBalance: beforeBal,
				AfterBalance:  afterBal,
				RefType:       "merchant_earning",
				RefId:         fmt.Sprintf("%d", earning.Id),
				OperatorId:    adminId,
				Remark:        reason,
				CreatedAt:     now,
			}); err != nil {
				return err
			}
			if earning.CommissionAmount > 0 {
				if err := RecordFinanceLedger(tx, &FinanceLedger{
					LedgerNo:   fmt.Sprintf("merchant_commission_reverse:%d", earning.Id),
					Domain:     FinanceDomainPlatformRevenue,
					UserId:     earning.MerchantUserId,
					BizType:    FinanceBizMerchantReverse,
					Direction:  FinanceDirOut,
					AmountUSD:  earning.CommissionAmount,
					RefType:    "merchant_earning",
					RefId:      fmt.Sprintf("%d", earning.Id),
					OperatorId: adminId,
					Remark:     reason,
					CreatedAt:  now,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
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
