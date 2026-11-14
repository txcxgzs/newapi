package setting

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

const DefaultMaxTokenAutoGroups = 5

const (
	AutoRoutingStrategyOrder         = "order"
	AutoRoutingStrategyRoundRobin    = "round_robin"
	AutoRoutingStrategyLowestPrice   = "lowest_price"
	AutoRoutingStrategyMerchantFirst = "merchant_first"
	AutoRoutingStrategyPriceTTFT     = "price_ttft"
	AutoRoutingStrategyLowestTTFT    = "lowest_ttft"
)

var autoGroups = []string{
	"default",
}

var DefaultUseAutoGroup = false

var maxTokenAutoGroups atomic.Int64
var autoRoutingStrategy atomic.Value
var autoIncludeMerchantGroups atomic.Bool

func init() {
	maxTokenAutoGroups.Store(DefaultMaxTokenAutoGroups)
	autoRoutingStrategy.Store(AutoRoutingStrategyOrder)
}

func ContainsAutoGroup(group string) bool {
	return slices.Contains(autoGroups, group)
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	autoGroups = make([]string, 0)
	return common.Unmarshal([]byte(jsonString), &autoGroups)
}

func AutoGroups2JsonString() string {
	jsonBytes, err := common.Marshal(autoGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	return autoGroups
}

func GetMaxTokenAutoGroups() int {
	return int(maxTokenAutoGroups.Load())
}

func ValidateMaxTokenAutoGroups(value string) error {
	maxCount, err := strconv.Atoi(value)
	if err != nil || maxCount <= 0 {
		return fmt.Errorf("MaxTokenAutoGroups must be a positive integer")
	}
	return nil
}

func UpdateMaxTokenAutoGroups(value string) error {
	if err := ValidateMaxTokenAutoGroups(value); err != nil {
		return err
	}
	maxCount, _ := strconv.Atoi(value)
	maxTokenAutoGroups.Store(int64(maxCount))
	return nil
}

func NormalizeAutoRoutingStrategy(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return AutoRoutingStrategyOrder
	}
	return trimmed
}

func GetAutoRoutingStrategy() string {
	if v, ok := autoRoutingStrategy.Load().(string); ok && v != "" {
		return v
	}
	return AutoRoutingStrategyOrder
}

func ValidateAutoRoutingStrategy(value string) error {
	switch strings.TrimSpace(value) {
	case AutoRoutingStrategyOrder,
		AutoRoutingStrategyRoundRobin,
		AutoRoutingStrategyLowestPrice,
		AutoRoutingStrategyMerchantFirst,
		AutoRoutingStrategyPriceTTFT,
		AutoRoutingStrategyLowestTTFT:
		return nil
	default:
		return fmt.Errorf("AutoRoutingStrategy must be one of: order, round_robin, lowest_price, merchant_first, price_ttft, lowest_ttft")
	}
}

func IsValidAutoRoutingStrategy(value string) bool {
	return ValidateAutoRoutingStrategy(value) == nil
}

func UpdateAutoRoutingStrategy(value string) error {
	if err := ValidateAutoRoutingStrategy(value); err != nil {
		return err
	}
	autoRoutingStrategy.Store(strings.TrimSpace(value))
	return nil
}

func GetAutoIncludeMerchantGroups() bool {
	return autoIncludeMerchantGroups.Load()
}

func SetAutoIncludeMerchantGroups(enabled bool) {
	autoIncludeMerchantGroups.Store(enabled)
}
