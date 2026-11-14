package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMarketOrderBookEndpoint(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	model.ResetChannelLiveMetrics()
	t.Cleanup(func() {
		model.ResetChannelLiveMetrics()
		model.InvalidateMerchantDisplayCache()
	})

	require.NoError(t, db.Create(&model.MerchantProfile{
		UserId:       77,
		MerchantCode: "m77",
		ShopName:     "TurboNode",
		SiteName:     "TurboNode",
		SiteUrl:      "https://turbo.example.com",
		Status:       model.MerchantStatusActive,
	}).Error)
	model.RefreshMerchantDisplayCache()

	ch := model.Channel{
		Name:       "secret-ch-name",
		Key:        "sk-secret-key",
		Type:       constant.ChannelTypeOpenAI,
		Status:     common.ChannelStatusEnabled,
		Group:      "turbo_pool",
		Models:     "gpt-4o",
		MerchantId: 77,
	}
	require.NoError(t, db.Create(&ch).Error)

	require.NoError(t, model.UpsertChannelQuotes([]model.ChannelQuote{
		{
			ChannelId:   ch.Id,
			PoolGroup:   "turbo_pool",
			ModelName:   "gpt-4o",
			InputPrice:  1.2,
			OutputPrice: 4.8,
			Source:      model.QuoteSourceMerchant,
			Status:      model.QuoteStatusEnabled,
		},
	}))
	model.RecordChannelLiveOutcome(ch.Id, "gpt-4o", true, 240, 92.5)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("id", 77)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/market/orderbook?pool=turbo_pool&model=gpt-4o&strategy=price_ttft", nil)
	GetMarketOrderBook(c)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "sk-secret-key")
	assert.NotContains(t, body, "secret-ch-name")

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Strategy string                 `json:"strategy"`
			Entries  []MarketOrderBookEntry `json:"entries"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "price_ttft", resp.Data.Strategy)
	require.Len(t, resp.Data.Entries, 1)
	entry := resp.Data.Entries[0]
	assert.Equal(t, 1, entry.Rank)
	assert.Equal(t, "turbo_pool", entry.Pool)
	assert.Equal(t, "gpt-4o", entry.Model)
	assert.Equal(t, "m77", entry.MerchantCode)
	assert.Equal(t, "TurboNode", entry.MerchantName)
	assert.InDelta(t, 1.2, entry.InputPrice, 1e-6)
	assert.InDelta(t, 4.8, entry.OutputPrice, 1e-6)
	assert.InDelta(t, 240, entry.TtftMs, 1.0)
	assert.InDelta(t, 92.5, entry.Tps, 0.1)
}

func TestTokenRoutingPolicyPersistence(t *testing.T) {
	db := setupMerchantControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))

	user := model.User{
		Username: "buyer_policy",
		Password: "hashedpassword1",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)

	// AddToken with routing_policy
	payload := `{
		"name": "orderbook-key",
		"unlimited_quota": true,
		"expired_time": -1,
		"group": "auto",
		"routing_policy": {
			"strategy": "price_ttft",
			"max_ttft_ms": 600,
			"min_tps": 40,
			"max_price": 2.5,
			"merchant_whitelist": ["m1", "Platform"],
			"merchant_blacklist": ["m9"]
		}
	}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("id", user.Id)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/token/", bytes.NewBufferString(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	AddToken(c)
	require.Equal(t, http.StatusOK, rec.Code)

	var tok model.Token
	require.NoError(t, db.Where("user_id = ? AND name = ?", user.Id, "orderbook-key").First(&tok).Error)
	pol, err := tok.GetRoutingPolicy()
	require.NoError(t, err)
	require.NotNil(t, pol)
	assert.Equal(t, "price_ttft", pol.Strategy)
	assert.Equal(t, 600, pol.MaxTtftMs)
	assert.InDelta(t, 40.0, pol.MinTps, 1e-6)
	assert.InDelta(t, 2.5, pol.MaxPrice, 1e-6)
	assert.Equal(t, []string{"m1", "Platform"}, pol.MerchantWhitelist)
	assert.Equal(t, []string{"m9"}, pol.MerchantBlacklist)
}
