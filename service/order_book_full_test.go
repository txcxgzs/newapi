package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplySelectedChannelOrderBookContext_RealQuoteBillingAndHeaders(t *testing.T) {
	setupChannelSelectAutoGroupsTest(t)
	model.ResetChannelLiveMetrics()
	t.Cleanup(func() {
		model.ResetChannelLiveMetrics()
		model.InvalidateMerchantDisplayCache()
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"pool_b":2}`))
	model.SetMerchantDisplayCacheForTest(map[int]model.MerchantDisplayInfo{
		55: {
			UserId:       55,
			MerchantCode: "m55",
			DisplayName:  "Alpha Shop",
			ShopName:     "Alpha Shop",
			Status:       model.MerchantStatusActive,
		},
	})

	require.NoError(t, model.UpsertChannelQuotes([]model.ChannelQuote{
		{
			ChannelId:   11,
			PoolGroup:   "pool_b",
			ModelName:   "gpt-4o",
			InputPrice:  0.80,
			OutputPrice: 3.20,
			Source:      model.QuoteSourceMerchant,
			Status:      model.QuoteStatusEnabled,
		},
	}))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Routing-Strategy", "price_ttft")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		StartTime:       time.Now().Add(-500 * time.Millisecond),
		ChannelMeta:     &relaycommon.ChannelMeta{},
		PriceData: hosttypes.PriceData{
			ModelRatio:      5.0, // static platform fallback should be overridden
			CompletionRatio: 2.0,
			UsePrice:        true,
			ModelPrice:      0.5,
			GroupRatioInfo: hosttypes.GroupRatioInfo{
				GroupRatio: 2.0, // pool_b group ratio
			},
		},
	}

	ch := &model.Channel{
		Id:         11,
		Name:       "alpha-11",
		MerchantId: 55,
	}

	ApplySelectedChannelOrderBookContext(c, relayInfo, ch, "pool_b")

	// Effective input price ($0.80/1M) = ModelRatio * GroupRatio * QuoteRatioBase(2.0)
	// With pool_b ratio 2.0: ModelRatio = 0.80 / 2.0 / 2.0 = 0.20; CompletionRatio = 3.20 / 0.80 = 4.0
	assert.False(t, relayInfo.PriceData.UsePrice)
	assert.Equal(t, 0.0, relayInfo.PriceData.ModelPrice)
	assert.InDelta(t, 0.20, relayInfo.PriceData.ModelRatio, 1e-6)
	assert.InDelta(t, 4.0, relayInfo.PriceData.CompletionRatio, 1e-6)

	assert.Equal(t, "Alpha Shop", rec.Header().Get("X-Relay-Provider"))
	assert.Equal(t, "m55", rec.Header().Get("X-Relay-Provider-Code"))
	assert.Equal(t, "pool_b", rec.Header().Get("X-Relay-Group"))
	assert.Equal(t, "price_ttft", rec.Header().Get("X-Relay-Strategy"))
	assert.Equal(t, "merchant", rec.Header().Get("X-Relay-Quote-Source"))
	assert.Equal(t, "0.8000", rec.Header().Get("X-Relay-Price-Input"))
	assert.Equal(t, "3.2000", rec.Header().Get("X-Relay-Price-Output"))

	common.SetContextKey(c, constant.ContextKeySelectedStreamTps, 88.5)
	relayInfo.FirstResponseTime = relayInfo.StartTime.Add(150 * time.Millisecond)
	other := GenerateTextOtherInfo(c, relayInfo, relayInfo.PriceData.ModelRatio, relayInfo.PriceData.GroupRatioInfo.GroupRatio, relayInfo.PriceData.CompletionRatio, 0, 1.0, relayInfo.PriceData.ModelPrice, 0)
	m := other.Snapshot()
	assert.Equal(t, "Alpha Shop", m["merchant_name"])
	assert.Equal(t, "m55", m["merchant_code"])
	assert.Equal(t, "price_ttft", m["routing_strategy"])
	assert.Equal(t, "merchant", m["quote_source"])
	assert.InDelta(t, 0.80, m["quote_input_price"], 1e-6)
	assert.InDelta(t, 3.20, m["quote_output_price"], 1e-6)
	assert.InDelta(t, 88.5, m["stream_tps"], 1e-6)
}

func TestResolveEffectiveRoutingPolicyAndFiltering(t *testing.T) {
	setupChannelSelectAutoGroupsTest(t)
	model.ResetChannelLiveMetrics()
	t.Cleanup(func() {
		model.ResetChannelLiveMetrics()
		model.InvalidateMerchantDisplayCache()
	})

	require.NoError(t, setting.UpdateAutoRoutingStrategy(setting.AutoRoutingStrategyPriceTTFT))
	model.SetMerchantDisplayCacheForTest(map[int]model.MerchantDisplayInfo{
		1: {UserId: 1, MerchantCode: "m1", DisplayName: "FastCloud", ShopName: "FastCloud", Status: 1},
		2: {UserId: 2, MerchantCode: "m2", DisplayName: "SlowCloud", ShopName: "SlowCloud", Status: 1},
	})

	c1 := &model.Channel{Id: 201, MerchantId: 1, Priority: common.GetPointer(int64(1))}
	c2 := &model.Channel{Id: 202, MerchantId: 2, Priority: common.GetPointer(int64(1))}

	cands := []OrderBookCandidate{
		{Group: "vip", Channel: c1, EffectivePrice: 1.5, TtftMs: 300, Tps: 60, SuccessRate: 0.99, HasLiveMetric: true, SampleCount: 5},
		{Group: "vip", Channel: c2, EffectivePrice: 0.9, TtftMs: 1400, Tps: 15, SuccessRate: 0.95, HasLiveMetric: true, SampleCount: 5},
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTokenRoutingPolicy, &model.TokenRoutingPolicy{
		Strategy:  setting.AutoRoutingStrategyLowestPrice,
		MaxTtftMs: 2000,
	})
	// Headers override token settings
	c.Request.Header.Set("X-Routing-Strategy", "lowest_ttft")
	c.Request.Header.Set("X-Max-TTFT-Ms", "500")
	c.Request.Header.Set("X-Min-TPS", "30")
	c.Request.Header.Set("X-Merchant-Allow", "FastCloud, m99")

	policy := ResolveEffectiveRoutingPolicy(c)
	assert.Equal(t, setting.AutoRoutingStrategyLowestTTFT, policy.Strategy)
	assert.True(t, policy.StrategyOverridden)
	assert.Equal(t, 500, policy.MaxTtftMs)
	assert.Equal(t, 30.0, policy.MinTps)
	assert.Equal(t, []string{"FastCloud", "m99"}, policy.MerchantWhitelist)

	filtered := FilterOrderBookCandidates(cands, policy)
	require.Len(t, filtered, 1)
	assert.Equal(t, 201, filtered[0].Channel.Id)

	// Strictly impossible price rejects everything
	policy.MaxPrice = 0.2
	empty := FilterOrderBookCandidates(cands, policy)
	assert.Empty(t, empty)
}
