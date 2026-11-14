package setting

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMaxTokenAutoGroupsAcceptsAnyPositiveInteger(t *testing.T) {
	original := GetMaxTokenAutoGroups()
	t.Cleanup(func() {
		require.NoError(t, UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", original)))
	})

	require.NoError(t, UpdateMaxTokenAutoGroups("123456"))
	assert.Equal(t, 123456, GetMaxTokenAutoGroups())
}

func TestUpdateMaxTokenAutoGroupsRejectsInvalidValuesWithoutChangingState(t *testing.T) {
	original := GetMaxTokenAutoGroups()
	for _, value := range []string{"", "0", "-1", "1.5", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, UpdateMaxTokenAutoGroups(value))
			assert.Equal(t, original, GetMaxTokenAutoGroups())
		})
	}
}

func TestAutoRoutingStrategyAndIncludeMerchantGroups(t *testing.T) {
	originalStrategy := GetAutoRoutingStrategy()
	originalInclude := GetAutoIncludeMerchantGroups()
	t.Cleanup(func() {
		require.NoError(t, UpdateAutoRoutingStrategy(originalStrategy))
		SetAutoIncludeMerchantGroups(originalInclude)
	})

	for _, valid := range []string{
		AutoRoutingStrategyOrder,
		AutoRoutingStrategyRoundRobin,
		AutoRoutingStrategyLowestPrice,
		AutoRoutingStrategyMerchantFirst,
	} {
		require.NoError(t, UpdateAutoRoutingStrategy(valid))
		assert.Equal(t, valid, GetAutoRoutingStrategy())
	}

	require.NoError(t, UpdateAutoRoutingStrategy("  lowest_price  "))
	assert.Equal(t, AutoRoutingStrategyLowestPrice, GetAutoRoutingStrategy())

	for _, invalid := range []string{"", "random", "cost_first"} {
		assert.Error(t, ValidateAutoRoutingStrategy(invalid))
		assert.Error(t, UpdateAutoRoutingStrategy(invalid))
		assert.Equal(t, AutoRoutingStrategyLowestPrice, GetAutoRoutingStrategy())
	}

	SetAutoIncludeMerchantGroups(true)
	assert.True(t, GetAutoIncludeMerchantGroups())
	SetAutoIncludeMerchantGroups(false)
	assert.False(t, GetAutoIncludeMerchantGroups())
}
