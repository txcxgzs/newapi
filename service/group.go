package service

import (
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

var autoRoundRobinCounter atomic.Uint64

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if after, ok := strings.CutPrefix(specialGroup, "-:"); ok {
					// 移除分组
					groupToRemove := after
					delete(groupsCopy, groupToRemove)
				} else if after, ok := strings.CutPrefix(specialGroup, "+:"); ok {
					// 添加分组
					groupToAdd := after
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup。
		// 描述是英文源文本，由 web 控制台翻译；管理员配置的描述原样显示。
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "User group"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroups(userGroup, groupName) && ratio_setting.ContainsGroupRatio(groupName)
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	appendIfSelectable := func(group string) {
		if !IsUserSelectableGroup(userGroup, group) {
			return
		}
		if _, ok := seen[group]; ok {
			return
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	for _, group := range setting.GetAutoGroups() {
		appendIfSelectable(group)
	}
	if setting.GetAutoIncludeMerchantGroups() {
		for _, group := range model.GetEnabledMerchantGroups() {
			appendIfSelectable(group)
		}
	}
	return autoGroups
}

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroup(userGroup)
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroups(userGroup, groups)
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}

// ResolveAutoGroupsForRequest resolves the ordered Auto group sequence for the
// current request according to the configured AutoRoutingStrategy, and memoizes
// the result on c so cross-group retries within the same request walk a stable order.
func ResolveAutoGroupsForRequest(c *gin.Context, userGroup string, modelName string, filters []dto.ChannelFilter) []string {
	if c != nil {
		if cached, ok := common.GetContextKey(c, constant.ContextKeyAutoGroupResolved); ok {
			if resolved, ok := cached.([]string); ok {
				return resolved
			}
		}
	}

	groups := GetRequestAutoGroups(c, userGroup)
	if len(groups) > 1 {
		policy := ResolveEffectiveRoutingPolicy(c)
		groups = orderAutoGroupsByStrategy(groups, userGroup, modelName, policy.Strategy, filters)
	}

	if c != nil {
		common.SetContextKey(c, constant.ContextKeyAutoGroupResolved, groups)
	}
	return groups
}

func orderAutoGroupsByStrategy(groups []string, userGroup string, modelName string, strategy string, filters []dto.ChannelFilter) []string {
	switch setting.NormalizeAutoRoutingStrategy(strategy) {
	case setting.AutoRoutingStrategyRoundRobin:
		available := make([]string, 0, len(groups))
		unavailable := make([]string, 0)
		for _, g := range groups {
			info := model.GetGroupModelRoutingInfo(g, modelName, filters)
			if info.HasEnabledChannel {
				available = append(available, g)
			} else {
				unavailable = append(unavailable, g)
			}
		}
		if len(available) == 0 {
			return groups
		}
		if len(available) > 1 {
			offset := int((autoRoundRobinCounter.Add(1) - 1) % uint64(len(available)))
			if offset > 0 {
				rotated := make([]string, 0, len(available))
				rotated = append(rotated, available[offset:]...)
				rotated = append(rotated, available[:offset]...)
				available = rotated
			}
		}
		ordered := make([]string, 0, len(groups))
		ordered = append(ordered, available...)
		ordered = append(ordered, unavailable...)
		return ordered

	case setting.AutoRoutingStrategyMerchantFirst:
		type merchantCandidate struct {
			group string
			tier  int
		}
		candidates := make([]merchantCandidate, 0, len(groups))
		for _, g := range groups {
			info := model.GetGroupModelRoutingInfo(g, modelName, filters)
			tier := 3
			switch {
			case info.HasEnabledChannel && info.HasMerchant:
				tier = 0
			case info.HasEnabledChannel && !info.HasMerchant:
				tier = 1
			case !info.HasEnabledChannel && info.HasMerchant:
				tier = 2
			}
			candidates = append(candidates, merchantCandidate{group: g, tier: tier})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].tier < candidates[j].tier
		})
		ordered := make([]string, len(candidates))
		for i, c := range candidates {
			ordered[i] = c.group
		}
		return ordered

	case setting.AutoRoutingStrategyLowestPrice,
		setting.AutoRoutingStrategyPriceTTFT,
		setting.AutoRoutingStrategyLowestTTFT:
		return RankAutoGroupsByOrderBook(groups, userGroup, modelName, strategy, filters)

	default:
		return groups
	}
}

func computeAutoGroupEffectivePrice(userGroup, group, modelName string, minQuotePrice float64) float64 {
	userGroupRatio := GetUserGroupRatio(userGroup, group)
	if userGroupRatio <= 0 {
		userGroupRatio = 1
	}
	if minQuotePrice > 0 {
		baseGroupRatio := ratio_setting.GetGroupRatio(group)
		if baseGroupRatio > 0 {
			return minQuotePrice * (userGroupRatio / baseGroupRatio)
		}
		return minQuotePrice
	}
	if modelRatio, ok, _ := ratio_setting.GetModelRatio(modelName); ok && modelRatio > 0 {
		completion := ratio_setting.GetCompletionRatio(modelName)
		if completion <= 0 {
			completion = 1
		}
		return userGroupRatio * modelRatio * model.QuoteRatioBase * (1 + completion) / 2
	}
	if modelPrice, ok := ratio_setting.GetModelPrice(modelName, false); ok && modelPrice > 0 {
		return userGroupRatio * modelPrice
	}
	return userGroupRatio
}
