package service

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// EffectiveRoutingPolicy merges global AutoRoutingStrategy, per-Token preferences,
// and per-request HTTP header overrides (headers take highest priority).
type EffectiveRoutingPolicy struct {
	Strategy           string
	StrategyOverridden bool
	MaxTtftMs          int
	MinTps             float64
	MaxPrice           float64
	MerchantWhitelist  []string
	MerchantBlacklist  []string
}

func (p EffectiveRoutingPolicy) HasFilters() bool {
	return p.MaxTtftMs > 0 || p.MinTps > 0 || p.MaxPrice > 0 ||
		len(p.MerchantWhitelist) > 0 || len(p.MerchantBlacklist) > 0
}

func splitHeaderCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// ResolveEffectiveRoutingPolicy extracts the buyer's effective routing strategy & risk constraints
// from request headers > token settings > system global AutoRoutingStrategy.
func ResolveEffectiveRoutingPolicy(c *gin.Context) EffectiveRoutingPolicy {
	policy := EffectiveRoutingPolicy{
		Strategy: setting.GetAutoRoutingStrategy(),
	}
	if c == nil {
		return policy
	}

	if rawTokenPolicy, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutingPolicy); ok {
		if tp, ok := rawTokenPolicy.(*model.TokenRoutingPolicy); ok && tp != nil {
			if s := strings.TrimSpace(tp.Strategy); s != "" && setting.IsValidAutoRoutingStrategy(s) {
				policy.Strategy = setting.NormalizeAutoRoutingStrategy(s)
				policy.StrategyOverridden = true
			}
			if tp.MaxTtftMs > 0 {
				policy.MaxTtftMs = tp.MaxTtftMs
			}
			if tp.MinTps > 0 {
				policy.MinTps = tp.MinTps
			}
			if tp.MaxPrice > 0 {
				policy.MaxPrice = tp.MaxPrice
			}
			if len(tp.MerchantWhitelist) > 0 {
				policy.MerchantWhitelist = append([]string(nil), tp.MerchantWhitelist...)
			}
			if len(tp.MerchantBlacklist) > 0 {
				policy.MerchantBlacklist = append([]string(nil), tp.MerchantBlacklist...)
			}
		}
	}
	if c.Request == nil {
		return policy
	}

	if hStrat := strings.TrimSpace(c.GetHeader("X-Routing-Strategy")); hStrat != "" && setting.IsValidAutoRoutingStrategy(hStrat) {
		policy.Strategy = setting.NormalizeAutoRoutingStrategy(hStrat)
		policy.StrategyOverridden = true
	}
	if hTtft := strings.TrimSpace(c.GetHeader("X-Max-TTFT-Ms")); hTtft != "" {
		if v, err := strconv.Atoi(hTtft); err == nil && v > 0 {
			policy.MaxTtftMs = v
		}
	}
	if hTps := strings.TrimSpace(c.GetHeader("X-Min-TPS")); hTps != "" {
		if v, err := strconv.ParseFloat(hTps, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			policy.MinTps = v
		}
	}
	if hMaxPrice := strings.TrimSpace(c.GetHeader("X-Max-Price")); hMaxPrice != "" {
		if v, err := strconv.ParseFloat(hMaxPrice, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			policy.MaxPrice = v
		}
	}
	if hAllow := splitHeaderCSV(c.GetHeader("X-Merchant-Allow")); len(hAllow) > 0 {
		policy.MerchantWhitelist = hAllow
	}
	if hDeny := splitHeaderCSV(c.GetHeader("X-Merchant-Deny")); len(hDeny) > 0 {
		seen := make(map[string]struct{}, len(policy.MerchantBlacklist)+len(hDeny))
		merged := make([]string, 0, len(policy.MerchantBlacklist)+len(hDeny))
		for _, item := range append(policy.MerchantBlacklist, hDeny...) {
			k := strings.ToLower(strings.TrimSpace(item))
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			merged = append(merged, strings.TrimSpace(item))
		}
		policy.MerchantBlacklist = merged
	}
	return policy
}

func matchesMerchantSelectors(ch *model.Channel, merchantMap map[int]model.MerchantDisplayInfo, selectors []string) bool {
	if ch == nil || len(selectors) == 0 {
		return false
	}
	mid := ch.GetMerchantId()
	tags := make([]string, 0, 5)
	if mid <= 0 {
		tags = append(tags, "0", "platform", "平台")
	} else {
		tags = append(tags, strconv.Itoa(mid))
		if info, ok := merchantMap[mid]; ok {
			if info.MerchantCode != "" {
				tags = append(tags, strings.ToLower(info.MerchantCode))
			}
			if info.DisplayName != "" {
				tags = append(tags, strings.ToLower(info.DisplayName))
			}
			if info.ShopName != "" {
				tags = append(tags, strings.ToLower(info.ShopName))
			}
			if info.SiteName != "" {
				tags = append(tags, strings.ToLower(info.SiteName))
			}
		} else {
			tags = append(tags, fmtMerchantCodeFallback(mid))
		}
	}
	for _, sel := range selectors {
		normSel := strings.ToLower(strings.TrimSpace(sel))
		if normSel == "" {
			continue
		}
		for _, tag := range tags {
			if normSel == tag {
				return true
			}
		}
	}
	return false
}

func fmtMerchantCodeFallback(mid int) string {
	return "m" + strconv.Itoa(mid)
}

// FilterOrderBookCandidates enforces the buyer's hard routing constraints (MaxPrice, MaxTTFT, MinTPS, merchant allow/deny).
func FilterOrderBookCandidates(candidates []OrderBookCandidate, policy EffectiveRoutingPolicy) []OrderBookCandidate {
	if len(candidates) == 0 || !policy.HasFilters() {
		return candidates
	}
	var merchantMap map[int]model.MerchantDisplayInfo
	if len(policy.MerchantWhitelist) > 0 || len(policy.MerchantBlacklist) > 0 {
		merchantMap = model.GetMerchantDisplayMap()
	}

	filtered := make([]OrderBookCandidate, 0, len(candidates))
	for _, cand := range candidates {
		if cand.Channel == nil {
			continue
		}
		if policy.MaxPrice > 0 && cand.EffectivePrice > policy.MaxPrice+1e-9 {
			continue
		}
		if policy.MaxTtftMs > 0 && cand.HasLiveMetric && cand.TtftMs > float64(policy.MaxTtftMs) {
			continue
		}
		if policy.MinTps > 0 && cand.HasLiveMetric && cand.Tps > 0 && cand.Tps < policy.MinTps {
			continue
		}
		if len(policy.MerchantWhitelist) > 0 && !matchesMerchantSelectors(cand.Channel, merchantMap, policy.MerchantWhitelist) {
			continue
		}
		if len(policy.MerchantBlacklist) > 0 && matchesMerchantSelectors(cand.Channel, merchantMap, policy.MerchantBlacklist) {
			continue
		}
		filtered = append(filtered, cand)
	}
	return filtered
}

const (
	// OrderBookDefaultColdStartTTFTMs provides a neutral prior before a node has live samples.
	OrderBookDefaultColdStartTTFTMs = 1200.0
	orderBookTTFTScaleMs            = 2000.0
	orderBookHealthSampleMin        = 2
	orderBookUnhealthyThreshold     = 0.85
)

// OrderBookCandidate represents one scorable (group, channel) entry for a single request.
type OrderBookCandidate struct {
	Group          string
	Channel        *model.Channel
	EffectivePrice float64
	TtftMs         float64
	Tps            float64
	SuccessRate    float64
	SampleCount    int
	HasLiveMetric  bool
	Score          float64
	Priority       int64
}

func sanitizeHeaderValue(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// ApplySelectedChannelOrderBookContext applies any active ChannelQuote on (channel, selectGroup, model)
// directly into relayInfo.PriceData for real per-channel settlement, records selected merchant &
// strategy metadata into Gin context for consume logs, and attaches X-Relay-* response headers.
func ApplySelectedChannelOrderBookContext(c *gin.Context, relayInfo *relaycommon.RelayInfo, channel *model.Channel, selectGroup string) {
	if c == nil || relayInfo == nil || channel == nil {
		return
	}

	policy := ResolveEffectiveRoutingPolicy(c)
	merchantId := channel.GetMerchantId()
	merchantInfo := model.GetMerchantDisplayInfo(merchantId)

	quoteSource := "platform"
	quote, hasQuote := model.GetEffectiveChannelQuoteForModel(channel.Id, selectGroup, relayInfo.OriginModelName)
	if hasQuote && quote != nil {
		inPrice := quote.InputPrice
		if inPrice <= 0 && quote.RateMultiplier > 0 {
			inPrice = quote.RateMultiplier * model.QuoteRatioBase
		}
		if inPrice > 0 {
			if strings.TrimSpace(quote.Source) != "" {
				quoteSource = strings.TrimSpace(quote.Source)
			}
			// Override static PriceData for explicit per-channel quotes (merchant / auto / manual).
			if quote.Source == model.QuoteSourceMerchant || quote.Source == model.QuoteSourceAuto || quote.Source == model.QuoteSourceManual {
				poolGroupRatio := ratio_setting.GetGroupRatio(selectGroup)
				if poolGroupRatio <= 0 {
					poolGroupRatio = 1
				}
				relayInfo.PriceData.ModelRatio = (inPrice / model.QuoteRatioBase) / poolGroupRatio
				if quote.OutputPrice > 0 {
					relayInfo.PriceData.CompletionRatio = quote.OutputPrice / inPrice
				} else if relayInfo.PriceData.CompletionRatio <= 0 {
					relayInfo.PriceData.CompletionRatio = 1
				}
				relayInfo.PriceData.UsePrice = false
				relayInfo.PriceData.ModelPrice = 0
			}
		}
	}

	effGroupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	if effGroupRatio <= 0 {
		effGroupRatio = 1
	}
	effInputPrice := relayInfo.PriceData.ModelRatio * effGroupRatio * model.QuoteRatioBase
	compRatio := relayInfo.PriceData.CompletionRatio
	if compRatio <= 0 {
		compRatio = 1
	}
	effOutputPrice := effInputPrice * compRatio
	if relayInfo.PriceData.UsePrice && relayInfo.PriceData.ModelPrice > 0 {
		effInputPrice = relayInfo.PriceData.ModelPrice * effGroupRatio
		effOutputPrice = effInputPrice
	}

	common.SetContextKey(c, constant.ContextKeySelectedMerchantId, merchantId)
	common.SetContextKey(c, constant.ContextKeySelectedMerchantName, merchantInfo.DisplayName)
	common.SetContextKey(c, constant.ContextKeySelectedMerchantCode, merchantInfo.MerchantCode)
	common.SetContextKey(c, constant.ContextKeySelectedRoutingStrategy, policy.Strategy)
	common.SetContextKey(c, constant.ContextKeySelectedQuoteSource, quoteSource)
	common.SetContextKey(c, constant.ContextKeySelectedQuoteInputPrice, effInputPrice)
	common.SetContextKey(c, constant.ContextKeySelectedQuoteOutputPrice, effOutputPrice)
	common.SetContextKey(c, constant.ContextKeySelectedStreamTps, 0.0)

	if c.Writer != nil && !c.Writer.Written() {
		h := c.Writer.Header()
		h.Del("X-Relay-Provider")
		h.Del("X-Relay-Provider-Code")
		if provider := sanitizeHeaderValue(merchantInfo.DisplayName); provider != "" {
			h.Set("X-Relay-Provider", provider)
		}
		if code := sanitizeHeaderValue(merchantInfo.MerchantCode); code != "" {
			h.Set("X-Relay-Provider-Code", code)
		}
		if grp := sanitizeHeaderValue(selectGroup); grp != "" {
			h.Set("X-Relay-Group", grp)
		}
		if strat := sanitizeHeaderValue(policy.Strategy); strat != "" {
			h.Set("X-Relay-Strategy", strat)
		}
		h.Set("X-Relay-Quote-Source", quoteSource)
		h.Set("X-Relay-Price-Input", strconv.FormatFloat(effInputPrice, 'f', 4, 64))
		h.Set("X-Relay-Price-Output", strconv.FormatFloat(effOutputPrice, 'f', 4, 64))
	}
}

// IsOrderBookRoutingStrategy returns true for competitive price/latency strategies.
func IsOrderBookRoutingStrategy(strategy string) bool {
	switch setting.NormalizeAutoRoutingStrategy(strategy) {
	case setting.AutoRoutingStrategyLowestPrice,
		setting.AutoRoutingStrategyPriceTTFT,
		setting.AutoRoutingStrategyLowestTTFT:
		return true
	default:
		return false
	}
}

// RecordRelayChannelSuccess records live TTFT, TPS, and a success event for the winning channel attempt.
func RecordRelayChannelSuccess(relayInfo *relaycommon.RelayInfo, attemptStart time.Time, outputTokens int64) {
	if relayInfo == nil || relayInfo.ChannelId <= 0 || relayInfo.OriginModelName == "" {
		return
	}
	now := time.Now()
	if attemptStart.IsZero() {
		attemptStart = relayInfo.StartTime
	}
	if attemptStart.IsZero() {
		attemptStart = now
	}

	var ttftMs float64
	var genSec float64
	if relayInfo.FirstResponseTime.After(attemptStart) {
		ttftMs = relayInfo.FirstResponseTime.Sub(attemptStart).Seconds() * 1000.0
		genSec = now.Sub(relayInfo.FirstResponseTime).Seconds()
	} else {
		ttftMs = now.Sub(attemptStart).Seconds() * 1000.0
		genSec = now.Sub(attemptStart).Seconds()
	}
	if ttftMs < 1 {
		ttftMs = 1
	}

	var tps float64
	if outputTokens > 0 {
		if genSec < 0.05 {
			genSec = 0.05
		}
		tps = float64(outputTokens) / genSec
	}
	model.RecordChannelLiveOutcome(relayInfo.ChannelId, relayInfo.OriginModelName, true, ttftMs, tps)
}

// RecordRelayChannelFailure records a failed upstream channel attempt in live telemetry.
func RecordRelayChannelFailure(channelId int, modelName string, apiErr *types.NewAPIError, attemptStart time.Time) {
	if channelId <= 0 || modelName == "" {
		return
	}
	if apiErr != nil && apiErr.GetErrorType() == types.ErrorTypeNewAPIError {
		switch apiErr.GetErrorCode() {
		case types.ErrorCodeInvalidRequest,
			types.ErrorCodeBadRequestBody,
			types.ErrorCodeReadRequestBodyFailed,
			types.ErrorCodeAccessDenied,
			types.ErrorCodeSensitiveWordsDetected,
			types.ErrorCodeInsufficientUserQuota,
			types.ErrorCodePreConsumeTokenQuotaFailed,
			types.ErrorCodeModelNotFound:
			return
		}
	}
	var elapsedMs float64
	if !attemptStart.IsZero() {
		elapsedMs = time.Since(attemptStart).Seconds() * 1000.0
	}
	model.RecordChannelLiveOutcome(channelId, modelName, false, elapsedMs, 0)
}

// ScoreOrderBookNode computes a deterministic lower-is-better rank score for a candidate node.
func ScoreOrderBookNode(effectivePrice float64, live *model.ChannelLiveMetricSnapshot, strategy string) float64 {
	if effectivePrice <= 0 || math.IsNaN(effectivePrice) || math.IsInf(effectivePrice, 0) {
		effectivePrice = 1.0
	}

	ttftMs := OrderBookDefaultColdStartTTFTMs
	successRate := 1.0
	sampleCount := 0
	if live != nil && live.SampleCount > 0 {
		sampleCount = live.SampleCount
		successRate = live.SuccessEma
		if live.TtftEmaMs > 0 {
			ttftMs = live.TtftEmaMs
		}
	}
	if ttftMs < 10 {
		ttftMs = 10
	} else if ttftMs > 60000 {
		ttftMs = 60000
	}

	healthMultiplier := 1.0
	if sampleCount >= orderBookHealthSampleMin && successRate < orderBookUnhealthyThreshold {
		clamped := math.Max(successRate, 0.15)
		healthMultiplier = 1.0 / (clamped * clamped)
	}

	switch setting.NormalizeAutoRoutingStrategy(strategy) {
	case setting.AutoRoutingStrategyLowestTTFT:
		return ttftMs * healthMultiplier
	case setting.AutoRoutingStrategyPriceTTFT:
		return effectivePrice * (1.0 + ttftMs/orderBookTTFTScaleMs) * healthMultiplier
	default: // lowest_price and fallback
		return effectivePrice * healthMultiplier
	}
}

func computeChannelGroupEffectivePrice(userGroup, group, modelName string, channelQuotePrice float64) float64 {
	return computeAutoGroupEffectivePrice(userGroup, group, modelName, channelQuotePrice)
}

// BuildOrderBookCandidates collects all enabled channels across groups for modelName,
// attaches per-channel quotes + live TTFT/TPS telemetry, and sorts them by strategy score.
func BuildOrderBookCandidates(groups []string, userGroup string, modelName string, strategy string, filters []dto.ChannelFilter) []OrderBookCandidate {
	if len(groups) == 0 || modelName == "" {
		return nil
	}
	strategy = setting.NormalizeAutoRoutingStrategy(strategy)

	channelsByGroup := make(map[string][]*model.Channel, len(groups))
	channelIdsByGroup := make(map[string][]int, len(groups))
	seenChannelIds := make(map[int]struct{})
	allChannelIds := make([]int, 0)

	for _, g := range groups {
		chs := model.GetSatisfiedChannelsForGroupModel(g, modelName, filters)
		if len(chs) == 0 {
			continue
		}
		channelsByGroup[g] = chs
		ids := make([]int, 0, len(chs))
		for _, ch := range chs {
			if ch == nil {
				continue
			}
			ids = append(ids, ch.Id)
			if _, ok := seenChannelIds[ch.Id]; !ok {
				seenChannelIds[ch.Id] = struct{}{}
				allChannelIds = append(allChannelIds, ch.Id)
			}
		}
		channelIdsByGroup[g] = ids
	}
	if len(allChannelIds) == 0 {
		return nil
	}

	quotePrices := model.GetChannelQuotePricesForGroupModel(groups, modelName, channelIdsByGroup)
	liveMetrics := model.GetChannelLiveMetricsForModel(allChannelIds, modelName)

	candidates := make([]OrderBookCandidate, 0, len(allChannelIds))
	for _, g := range groups {
		chs := channelsByGroup[g]
		for _, ch := range chs {
			if ch == nil {
				continue
			}
			var rawQuote float64
			if byCh := quotePrices[g]; byCh != nil {
				rawQuote = byCh[ch.Id]
			}
			effectivePrice := computeChannelGroupEffectivePrice(userGroup, g, modelName, rawQuote)

			var livePtr *model.ChannelLiveMetricSnapshot
			if snap, ok := liveMetrics[ch.Id]; ok {
				copySnap := snap
				livePtr = &copySnap
			}

			score := ScoreOrderBookNode(effectivePrice, livePtr, strategy)
			cand := OrderBookCandidate{
				Group:          g,
				Channel:        ch,
				EffectivePrice: effectivePrice,
				TtftMs:         OrderBookDefaultColdStartTTFTMs,
				SuccessRate:    1.0,
				Score:          score,
				Priority:       ch.GetPriority(),
			}
			if livePtr != nil {
				cand.HasLiveMetric = true
				cand.SampleCount = livePtr.SampleCount
				cand.SuccessRate = livePtr.SuccessEma
				cand.Tps = livePtr.TpsEma
				if livePtr.TtftEmaMs > 0 {
					cand.TtftMs = livePtr.TtftEmaMs
				}
			}
			candidates = append(candidates, cand)
		}
	}

	sortOrderBookCandidates(candidates, strategy)
	return candidates
}

func sortOrderBookCandidates(candidates []OrderBookCandidate, strategy string) {
	strategy = setting.NormalizeAutoRoutingStrategy(strategy)
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if math.Abs(a.Score-b.Score) > 1e-9 {
			return a.Score < b.Score
		}
		if strategy == setting.AutoRoutingStrategyLowestTTFT {
			if math.Abs(a.EffectivePrice-b.EffectivePrice) > 1e-9 {
				return a.EffectivePrice < b.EffectivePrice
			}
			if math.Abs(a.Tps-b.Tps) > 1e-6 {
				return a.Tps > b.Tps
			}
		} else {
			if math.Abs(a.TtftMs-b.TtftMs) > 1.0 {
				return a.TtftMs < b.TtftMs
			}
			if math.Abs(a.EffectivePrice-b.EffectivePrice) > 1e-9 {
				return a.EffectivePrice < b.EffectivePrice
			}
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.Channel.Id < b.Channel.Id
	})
}

func sortOrderBookByGroupThenPriority(candidates []OrderBookCandidate, groupRank map[string]int) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		ra, rb := groupRank[a.Group], groupRank[b.Group]
		if ra != rb {
			return ra < rb
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.Channel.Id < b.Channel.Id
	})
}

// RankAutoGroupsByOrderBook orders candidate groups by each group's best-scoring
// channel node under the active Order Book strategy, keeping unavailable groups at the end.
func RankAutoGroupsByOrderBook(groups []string, userGroup string, modelName string, strategy string, filters []dto.ChannelFilter) []string {
	candidates := BuildOrderBookCandidates(groups, userGroup, modelName, strategy, filters)
	if len(candidates) == 0 {
		return groups
	}

	bestByGroup := make(map[string]OrderBookCandidate, len(groups))
	for _, cand := range candidates {
		if _, exists := bestByGroup[cand.Group]; !exists {
			// candidates is already sorted best-first
			bestByGroup[cand.Group] = cand
		}
	}

	type groupRank struct {
		group      string
		hasChannel bool
		best       OrderBookCandidate
		origIdx    int
	}
	ranked := make([]groupRank, 0, len(groups))
	for idx, g := range groups {
		best, ok := bestByGroup[g]
		ranked = append(ranked, groupRank{
			group:      g,
			hasChannel: ok,
			best:       best,
			origIdx:    idx,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].hasChannel != ranked[j].hasChannel {
			return ranked[i].hasChannel
		}
		if !ranked[i].hasChannel && !ranked[j].hasChannel {
			return computeAutoGroupEffectivePrice(userGroup, ranked[i].group, modelName, 0) <
				computeAutoGroupEffectivePrice(userGroup, ranked[j].group, modelName, 0)
		}
		if math.Abs(ranked[i].best.Score-ranked[j].best.Score) > 1e-9 {
			return ranked[i].best.Score < ranked[j].best.Score
		}
		return ranked[i].origIdx < ranked[j].origIdx
	})

	ordered := make([]string, len(ranked))
	for i, item := range ranked {
		ordered[i] = item.group
	}
	return ordered
}
