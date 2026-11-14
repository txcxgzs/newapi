package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// withProbeTestClient 关闭 SSRF 保护并初始化全局 HTTP 客户端，
// 让探测可以访问 httptest 的环回地址；用例结束后恢复。
func withProbeTestClient(t *testing.T) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	previous := *fetch
	fetch.EnableSSRFProtection = false
	InitHttpClient()
	t.Cleanup(func() {
		*fetch = previous
		InitHttpClient()
	})
}

// newProbeServer 模拟不同网关：newAPIPricing=true 时暴露 New API 风格价格接口。
// newOpenAIModelsPricingProbeServer 模拟 OpenAI /v1/models 报价协议上游：
// /v1/models 带鉴权并内嵌 USD/1M 价格。
func newOpenAIModelsPricingProbeServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer sk-merchant" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[
				{"id":"gpt-4o","pricing":{"currency":"USD","input_per_m":2.5,"cached_input_per_m":1.25,"output_per_m":10}},
				{"id":"claude-opus-5","pricing":{"currency":"USD","input_per_m":5,"output_per_m":20}}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newProbeServer(t *testing.T, newAPIPricing bool, statusBody string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pricing":
			if !newAPIPricing {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":[{"model_name":"gpt-4o","quota_type":0,"model_ratio":1.25,"completion_ratio":4}]}`))
		case "/api/ratio_config":
			w.WriteHeader(http.StatusNotFound)
		case "/api/status":
			if statusBody == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(statusBody))
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer sk-merchant" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"claude-opus-5"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProbeUpstreamNewAPIPricingEnablesAutoSync(t *testing.T) {
	withProbeTestClient(t)
	server := newProbeServer(t, true, `{"success":true,"data":{"system_name":"New API"}}`)

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindAuto)

	assert.Equal(t, UpstreamKindNewAPI, probe.Kind)
	assert.Equal(t, constant.ChannelTypeNewAPI, probe.ChannelType)
	assert.True(t, probe.NewAPIPricing)
	assert.True(t, probe.PriceSyncCapable)
	assert.Equal(t, string(UpstreamKindNewAPI), probe.PriceSyncProtocol)
	assert.True(t, probe.ModelsEndpointOK)
	assert.Equal(t, 2, probe.ModelCount)
}

func TestProbeUpstreamAutoDoesNotGuessModelsPricingProtocol(t *testing.T) {
	withProbeTestClient(t)
	server := newOpenAIModelsPricingProbeServer(t)

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindAuto)

	assert.Equal(t, UpstreamKindOpenAICompatible, probe.Kind)
	assert.Equal(t, constant.ChannelTypeOpenAI, probe.ChannelType)
	assert.False(t, probe.PriceSyncCapable)
	assert.Empty(t, probe.PriceSyncProtocol)
	assert.Equal(t, []string{"gpt-4o", "claude-opus-5"}, probe.Models)
}

func TestProbeUpstreamDeclaredModelsPricingEnablesSync(t *testing.T) {
	withProbeTestClient(t)
	server := newOpenAIModelsPricingProbeServer(t)

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindOpenAIModelsPricing)

	assert.Equal(t, UpstreamKindOpenAIModelsPricing, probe.Kind)
	assert.Equal(t, constant.ChannelTypeOpenAI, probe.ChannelType)
	assert.True(t, probe.PriceSyncCapable)
	assert.Equal(t, string(UpstreamKindOpenAIModelsPricing), probe.PriceSyncProtocol)
	assert.Equal(t, []string{"gpt-4o", "claude-opus-5"}, probe.Models)
	assert.Empty(t, probe.Warnings)
}

func TestProbeUpstreamDeclaredModelsPricingWithoutPricingWarns(t *testing.T) {
	withProbeTestClient(t)
	server := newProbeServer(t, false, "")

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindOpenAIModelsPricing)

	assert.Equal(t, UpstreamKindOpenAIModelsPricing, probe.Kind)
	assert.True(t, probe.PriceSyncCapable)
	assert.Equal(t, string(UpstreamKindOpenAIModelsPricing), probe.PriceSyncProtocol)
	assert.Contains(t, strings.Join(probe.Warnings, " "), "pricing")
}

func TestProbeUpstreamSub2APIStyleGatewayKeepsManualQuotes(t *testing.T) {
	withProbeTestClient(t)
	// 只有 /v1/* 中继面，没有 New API 的价格接口。
	server := newProbeServer(t, false, `{"success":true,"data":{"system_name":"sub2api"}}`)

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindSub2API)

	assert.Equal(t, UpstreamKindSub2API, probe.Kind)
	assert.Equal(t, constant.ChannelTypeSub2API, probe.ChannelType)
	assert.False(t, probe.NewAPIPricing)
	assert.False(t, probe.PriceSyncCapable)
	require.True(t, probe.ModelsEndpointOK)
	assert.Equal(t, []string{"gpt-4o", "claude-opus-5"}, probe.Models)
	assert.Contains(t, strings.Join(probe.Warnings, " "), "手填")
}

func TestProbeUpstreamAutoDetectsSub2APIStatusSignal(t *testing.T) {
	withProbeTestClient(t)
	server := newProbeServer(t, false, `{"success":true,"data":{"system_name":"sub2api"}}`)

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindAuto)

	assert.Equal(t, UpstreamKindSub2API, probe.Kind)
	assert.Equal(t, constant.ChannelTypeSub2API, probe.ChannelType)
}

func TestProbeUpstreamAutoFallsBackToOpenAICompatible(t *testing.T) {
	withProbeTestClient(t)
	server := newProbeServer(t, false, "")

	probe := ProbeUpstream(context.Background(), server.URL, "sk-merchant", UpstreamKindAuto)

	assert.Equal(t, UpstreamKindOpenAICompatible, probe.Kind)
	assert.Equal(t, constant.ChannelTypeOpenAI, probe.ChannelType)
	assert.False(t, probe.PriceSyncCapable)
}

func TestProbeUpstreamReportsUnauthorizedKey(t *testing.T) {
	withProbeTestClient(t)
	server := newProbeServer(t, false, "")

	probe := ProbeUpstream(context.Background(), server.URL, "sk-wrong", UpstreamKindSub2API)

	assert.False(t, probe.ModelsEndpointOK)
	assert.Zero(t, probe.ModelCount)
	assert.Contains(t, strings.Join(probe.Warnings, " "), "/v1/models")
}

func TestProbeUpstreamRejectsPrivateTarget(t *testing.T) {
	// 不关闭 SSRF 保护：环回站址必须被拒绝，且不发起请求。
	probe := ProbeUpstream(context.Background(), "http://127.0.0.1:9", "sk-merchant", UpstreamKindAuto)

	assert.Contains(t, strings.Join(probe.Warnings, " "), "网络获取策略")
	assert.False(t, probe.NewAPIPricing)
}

func TestUpstreamKindMapping(t *testing.T) {
	assert.Equal(t, constant.ChannelTypeNewAPI, UpstreamChannelType(UpstreamKindNewAPI))
	assert.Equal(t, constant.ChannelTypeSub2API, UpstreamChannelType(UpstreamKindSub2API))
	assert.Equal(t, constant.ChannelTypeOpenAI, UpstreamChannelType(UpstreamKindOpenAIModelsPricing))
	assert.Equal(t, constant.ChannelTypeOpenAI, UpstreamChannelType(UpstreamKindOpenAICompatible))
	assert.Equal(t, constant.ChannelTypeOpenAI, UpstreamChannelType(UpstreamKindAuto))

	assert.Equal(t, UpstreamKindSub2API, ParseUpstreamKind(" Sub2API "))
	assert.Equal(t, UpstreamKindOpenAICompatible, ParseUpstreamKind("openai-compatible"))
	assert.Equal(t, UpstreamKindOpenAIModelsPricing, ParseUpstreamKind("openai_models_pricing"))
	assert.Equal(t, UpstreamKindAuto, ParseUpstreamKind("unknown-gateway"))
}
