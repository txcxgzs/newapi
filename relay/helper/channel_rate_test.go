package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// TestApplyChannelRateRescalesToChannel 锁定重试换渠道的重估语义：
// PriceData 与 tiered 快照按「新渠道倍率 / 旧渠道倍率」整体缩放，
// 使实扣归属实际服务渠道（无 DB 环境下任何渠道倍率兜底为 1，即平台标准价）。
func TestApplyChannelRateRescalesToChannel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ImageQuotaBeforeGroup: 4,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               billing_setting.BillingModeTieredExpr,
			ChannelRate:               2,
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 2000,
			EstimatedQuotaAfterGroup:  2000,
		},
	}
	info.PriceData = hosttypes.PriceData{
		ModelRatio:  3, // 平台 1.5 × 旧渠道 2
		ModelPrice:  1,
		ChannelRate: 2,
	}

	ApplyChannelRate(info, 42)

	require.Equal(t, 1.0, info.PriceData.ChannelRate)
	require.Equal(t, 1.5, info.PriceData.ModelRatio)
	require.Equal(t, 0.5, info.PriceData.ModelPrice)
	require.Equal(t, 2.0, info.ImageQuotaBeforeGroup)
	require.Equal(t, 1.0, info.TieredBillingSnapshot.ChannelRate)
	require.Equal(t, 1000.0, info.TieredBillingSnapshot.EstimatedQuotaBeforeGroup)
	require.Equal(t, 1000, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
}

// TestApplyChannelRateLegacyZeroRate 旧数据/未设乘数（ChannelRate=0）按 1 处理，
// 重估到 1.0 时数值不变，只补齐基准字段。
func TestApplyChannelRateLegacyZeroRate(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.PriceData = hosttypes.PriceData{ModelRatio: 2.5}

	ApplyChannelRate(info, 42)

	require.Equal(t, 1.0, info.PriceData.ChannelRate)
	require.Equal(t, 2.5, info.PriceData.ModelRatio)
}
