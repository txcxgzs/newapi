package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// GetUserGiftQuota returns the user's gift quota balance from the database.
// Gift quota is intentionally not cached: it is a small separate ledger whose
// deductions must always be atomic against the current column value.
func GetUserGiftQuota(id int) (int, error) {
	var quota int
	err := DB.Model(&User{}).Where("id = ?", id).Select("gift_quota").Find(&quota).Error
	if err != nil {
		return 0, err
	}
	return quota, nil
}

// TryReserveGiftQuota atomically checks and deducts gift quota. It always uses
// a conditional database update instead of the wallet Redis fast path so
// concurrent requests cannot overdraw the gift ledger.
func TryReserveGiftQuota(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	result := DB.Model(&User{}).
		Where("id = ? AND gift_quota >= ?", id, quota).
		Update("gift_quota", gorm.Expr("gift_quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

// IncreaseGiftQuota credits gift quota (refund or grant). The balance is
// bounded above like the wallet so a runaway credit cannot overflow int64.
func IncreaseGiftQuota(id int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if err := common.ValidateWalletQuota(quota); err != nil {
		return err
	}
	result := DB.Model(&User{}).
		Where("id = ? AND gift_quota <= ?", id, common.MaxWalletQuota-quota).
		Update("gift_quota", gorm.Expr("gift_quota + ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := DB.Model(&User{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrWalletQuotaLimitExceeded
}

// DecreaseGiftQuota charges gift quota without a balance precondition.
// Settlement may overdraw the gift ledger (negative balance) exactly like the
// wallet, so the recorded pre-consume and the ledger change always reconcile.
func DecreaseGiftQuota(id int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	return DB.Model(&User{}).Where("id = ?", id).
		Update("gift_quota", gorm.Expr("gift_quota - ?", quota)).Error
}

// AdjustUserGiftQuota applies a manual gift quota adjustment. It mirrors
// AdjustUserQuota: row-locked transaction, operator role check, and
// wallet-domain bounds. Gift quota has no Redis cache, so no cache sync runs.
func AdjustUserGiftQuota(userID, operatorRole int, mode string, value int) (*UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != "override" && value <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}

	var adjustment UserQuotaAdjustment
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		if user.GiftQuota > common.MaxWalletQuota || user.GiftQuota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.GiftQuota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.GiftQuota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		if after != user.GiftQuota {
			result := tx.Model(&User{}).Where("id = ?", userID).Update("gift_quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.GiftQuota, After: after}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &adjustment, nil
}
