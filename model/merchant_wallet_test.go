package model

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestRecordMerchantEarningIsIdempotentAndCreatesWallet 锁定分润入账的三个不变量：
// 同一 dedup_key 只入账一次（有消费日志时为 log:<log_id>）；无幂等键（如 WSS 多轮 +
// 日志未启用）时每条正常入账互不吞并；钱包行不存在时自动创建并累加。
func TestRecordMerchantEarningIsIdempotentAndCreatesWallet(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			withMerchantTestDB(t, dialect)

			dedup := "log:1001"
			require.NoError(t, RecordMerchantEarning(nil, &MerchantEarning{
				MerchantUserId: 42, ChannelId: 5, LogId: 1001, Amount: 1.25, DedupKey: &dedup,
			}))
			require.NoError(t, RecordMerchantEarning(nil, &MerchantEarning{
				MerchantUserId: 42, ChannelId: 5, LogId: 1001, Amount: 1.25, DedupKey: &dedup,
			}))
			// 消费日志未落库（logId=0）且无兜底键：不得因 log_id 或空键被吞并
			require.NoError(t, RecordMerchantEarning(nil, &MerchantEarning{
				MerchantUserId: 42, ChannelId: 5, LogId: 0, Amount: 0.5,
			}))
			require.NoError(t, RecordMerchantEarning(nil, &MerchantEarning{
				MerchantUserId: 42, ChannelId: 5, LogId: 0, Amount: 0.5,
			}))

			wallet, err := GetOrCreateMerchantWallet(42)
			require.NoError(t, err)
			assert.InDelta(t, 2.25, wallet.Balance, 1e-9)
			assert.InDelta(t, 2.25, wallet.TotalEarned, 1e-9)

			var earnings int64
			require.NoError(t, DB.Model(&MerchantEarning{}).Count(&earnings).Error)
			assert.EqualValues(t, 3, earnings, "重复 dedup_key 不得重复入账，无键流水不吞并")

			var wallets int64
			require.NoError(t, DB.Model(&MerchantWallet{}).Count(&wallets).Error)
			assert.EqualValues(t, 1, wallets, "钱包必须恰好一行")
		})
	}
}

// TestRecordMerchantEarningRollsBackWithTransaction 锁定"流水与余额同生共死"：
// 外层事务回滚后既不能留下分润流水，也不能留下余额变动。
func TestRecordMerchantEarningRollsBackWithTransaction(t *testing.T) {
	withMerchantTestDB(t, "sqlite")

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := RecordMerchantEarning(tx, &MerchantEarning{
			MerchantUserId: 7, ChannelId: 3, LogId: 2002, Amount: 9.5,
		}); err != nil {
			return err
		}
		return errors.New("settlement aborted")
	})
	require.Error(t, err)

	var earnings int64
	require.NoError(t, DB.Model(&MerchantEarning{}).Count(&earnings).Error)
	assert.Zero(t, earnings)

	var wallets int64
	require.NoError(t, DB.Model(&MerchantWallet{}).Count(&wallets).Error)
	assert.Zero(t, wallets)
}

// TestMerchantWithdrawalWalletLifecycle 锁定提现资金流的四个动作及其幂等边界。
func TestMerchantWithdrawalWalletLifecycle(t *testing.T) {
	withMerchantTestDB(t, "sqlite")

	require.NoError(t, DB.Create(&MerchantWallet{MerchantUserId: 9, Balance: 10}).Error)

	require.NoError(t, FreezeForWithdrawal(nil, 9, 4))
	wallet, err := GetOrCreateMerchantWallet(9)
	require.NoError(t, err)
	assert.InDelta(t, 6.0, wallet.Balance, 1e-9, "申请提现应冻结（扣减）余额")

	require.ErrorIs(t, FreezeForWithdrawal(nil, 9, 100), ErrInsufficientBalance,
		"余额不足时不得透支")

	require.NoError(t, RefundWithdrawal(nil, 9, 4))
	wallet, err = GetOrCreateMerchantWallet(9)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, wallet.Balance, 1e-9, "拒绝提现应退回冻结金额")

	require.NoError(t, ConfirmWithdrawalPaid(nil, 9, 4))
	wallet, err = GetOrCreateMerchantWallet(9)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, wallet.Balance, 1e-9)
	assert.InDelta(t, 4.0, wallet.TotalWithdrawn, 1e-9, "打款确认只累计已提现")
}

// withMerchantTestDB 为用例准备一个隔离数据库，并在结束时恢复全局句柄。
func withMerchantTestDB(t *testing.T, dialect string) {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "merchant.db"))
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Migrator().DropTable("merchant_earnings", "merchant_wallets"))
	require.NoError(t, db.AutoMigrate(&MerchantWallet{}, &MerchantEarning{}))
	t.Cleanup(func() { _ = db.Migrator().DropTable("merchant_earnings", "merchant_wallets") })

	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
}
