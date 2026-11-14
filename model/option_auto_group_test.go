package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueRejectsInvalidMaxTokenAutoGroups(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1.5", "invalid"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, validateOptionValue("MaxTokenAutoGroups", value))
		})
	}
	require.NoError(t, validateOptionValue("MaxTokenAutoGroups", "999999"))
}

func TestValidateOptionValueRejectsSystemTaskRetentionBelowOneDay(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1.5", "invalid"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, validateOptionValue("system_task_setting.retention_days", value))
		})
	}
	require.NoError(t, validateOptionValue("system_task_setting.retention_days", "1"))
	require.NoError(t, validateOptionValue("system_task_setting.retention_days", "30"))
}

func TestAutoRoutingOptionsValidationAndUpdate(t *testing.T) {
	originalStrategy := setting.GetAutoRoutingStrategy()
	originalInclude := setting.GetAutoIncludeMerchantGroups()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	prevMapStrategy := common.OptionMap["AutoRoutingStrategy"]
	prevMapInclude := common.OptionMap["AutoIncludeMerchantGroups"]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		_ = setting.UpdateAutoRoutingStrategy(originalStrategy)
		setting.SetAutoIncludeMerchantGroups(originalInclude)
		common.OptionMapRWMutex.Lock()
		common.OptionMap["AutoRoutingStrategy"] = prevMapStrategy
		common.OptionMap["AutoIncludeMerchantGroups"] = prevMapInclude
		common.OptionMapRWMutex.Unlock()
	})

	assert.Error(t, validateOptionValue("AutoRoutingStrategy", "unknown"))
	require.NoError(t, validateOptionValue("AutoRoutingStrategy", setting.AutoRoutingStrategyMerchantFirst))

	require.NoError(t, updateOptionMap("AutoRoutingStrategy", setting.AutoRoutingStrategyLowestPrice))
	assert.Equal(t, setting.AutoRoutingStrategyLowestPrice, setting.GetAutoRoutingStrategy())

	require.NoError(t, updateOptionMap("AutoIncludeMerchantGroups", "true"))
	assert.True(t, setting.GetAutoIncludeMerchantGroups())
	require.NoError(t, updateOptionMap("AutoIncludeMerchantGroups", "false"))
	assert.False(t, setting.GetAutoIncludeMerchantGroups())
}
