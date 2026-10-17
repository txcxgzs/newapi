package service

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// UpstreamKind 商家上游的网关类型。
// 不同网关的对外能力不同：New API 系暴露公开的 /api/pricing、/api/ratio_config，
// 可以直接抓取挂牌价；Sub2API 系只有 /v1/* 中继面 + 自身后台的分组定价，
// 价格必须手填，但中继端点家族（Claude/Gemini/Responses）与 New API 系一致。
type UpstreamKind string

const (
	UpstreamKindAuto             UpstreamKind = ""
	UpstreamKindNewAPI           UpstreamKind = "newapi"
	UpstreamKindSub2API          UpstreamKind = "sub2api"
	UpstreamKindOpenAICompatible UpstreamKind = "openai"
)

// UpstreamChannelType 把上游类型映射为渠道类型编号。
func UpstreamChannelType(kind UpstreamKind) int {
	switch kind {
	case UpstreamKindNewAPI:
		return constant.ChannelTypeNewAPI
	case UpstreamKindSub2API:
		return constant.ChannelTypeSub2API
	default:
		return constant.ChannelTypeOpenAI
	}
}

// ParseUpstreamKind 解析前端/申请单传入的上游类型，未知值按自动处理。
func ParseUpstreamKind(raw string) UpstreamKind {
	switch UpstreamKind(strings.ToLower(strings.TrimSpace(raw))) {
	case UpstreamKindNewAPI:
		return UpstreamKindNewAPI
	case UpstreamKindSub2API:
		return UpstreamKindSub2API
	case UpstreamKindOpenAICompatible, "openai-compatible":
		return UpstreamKindOpenAICompatible
	default:
		return UpstreamKindAuto
	}
}

// UpstreamProbe 一次上游探测的结果，用于审核时判定渠道类型与同步能力。
type UpstreamProbe struct {
	Kind             UpstreamKind `json:"kind"`
	ChannelType      int          `json:"channel_type"`
	DeclaredKind     UpstreamKind `json:"declared_kind,omitempty"`
	NewAPIPricing    bool         `json:"new_api_pricing"`
	ModelsEndpointOK bool         `json:"models_endpoint_ok"`
	Models           []string     `json:"models,omitempty"`
	ModelCount       int          `json:"model_count"`
	PriceSyncCapable bool         `json:"price_sync_capable"`
	Warnings         []string     `json:"warnings,omitempty"`
}

const upstreamProbeTimeout = 8 * time.Second
const upstreamProbeMaxBody = 2 << 20
const upstreamProbeModelLimit = 500

// ProbeUpstream 探测上游网关：验证可达性、密钥可用性、模型清单与价格接口能力。
// declared 为申请单声明的类型，用于消歧（自动模式下不依赖启发式猜测成 Sub2API）。
func ProbeUpstream(ctx context.Context, baseURL string, apiKey string, declared UpstreamKind) *UpstreamProbe {
	probe := &UpstreamProbe{DeclaredKind: declared, Kind: declared}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		probe.Warnings = append(probe.Warnings, "站址必须是 http/https")
		probe.Kind = normalizeProbeKind(probe)
		probe.ChannelType = UpstreamChannelType(probe.Kind)
		return probe
	}
	if err := ValidateSSRFProtectedFetchURL(baseURL); err != nil {
		probe.Warnings = append(probe.Warnings,
			"站址被网络获取策略拒绝（可在“系统设置 → 网络获取”放行该域名/网段）: "+err.Error())
		probe.Kind = normalizeProbeKind(probe)
		probe.ChannelType = UpstreamChannelType(probe.Kind)
		return probe
	}

	// 1) New API 系公开价格接口（无鉴权）
	probe.NewAPIPricing = probeNewAPIPricing(ctx, baseURL, &probe.Warnings)

	// 2) OpenAI 兼容模型清单（带商家密钥）
	if strings.TrimSpace(apiKey) != "" {
		models, status, ok, err := probeModelsEndpoint(ctx, baseURL, apiKey)
		probe.ModelsEndpointOK = ok
		switch {
		case ok:
			probe.Models = models
			probe.ModelCount = len(models)
		case err != nil:
			probe.Warnings = append(probe.Warnings, "/v1/models 探测失败: "+err.Error())
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			probe.Warnings = append(probe.Warnings, "/v1/models 拒绝该密钥（HTTP "+strconv.Itoa(status)+"），渠道可能不可用")
		default:
			probe.Warnings = append(probe.Warnings,
				"/v1/models 不可用（HTTP "+strconv.Itoa(status)+"），无法自动补全模型清单")
		}
	} else {
		probe.Warnings = append(probe.Warnings, "未提供密钥，跳过 /v1/models 校验")
	}

	// 3) Sub2API 自报信号（仅在未声明类型时参与判定）
	if declared == UpstreamKindAuto && !probe.NewAPIPricing && probeLooksLikeSub2API(ctx, baseURL) {
		probe.Kind = UpstreamKindSub2API
	}

	probe.Kind = normalizeProbeKind(probe)
	probe.ChannelType = UpstreamChannelType(probe.Kind)
	probe.PriceSyncCapable = probe.NewAPIPricing
	if !probe.NewAPIPricing {
		probe.Warnings = append(probe.Warnings, "上游不提供 New API 风格价格接口，报价需手填")
	}
	return probe
}

// normalizeProbeKind 收敛最终类型：显式声明的类型优先（它决定中继端点家族），
// 探测结果只用于判断价格同步能力与给出告警；自动模式才依据探测事实判定。
func normalizeProbeKind(probe *UpstreamProbe) UpstreamKind {
	if probe.DeclaredKind != UpstreamKindAuto {
		return probe.DeclaredKind
	}
	if probe.NewAPIPricing {
		return UpstreamKindNewAPI
	}
	if probe.Kind == UpstreamKindSub2API {
		return UpstreamKindSub2API
	}
	return UpstreamKindOpenAICompatible
}

// probeNewAPIPricing 检查上游是否暴露 New API 系的公开价格接口。
func probeNewAPIPricing(ctx context.Context, baseURL string, warnings *[]string) bool {
	for _, endpoint := range []string{"/api/pricing", "/api/ratio_config"} {
		body, status, err := probeGet(ctx, baseURL+endpoint, "")
		if err != nil {
			continue
		}
		if status != http.StatusOK {
			continue
		}
		text := string(body)
		if strings.Contains(text, "model_ratio") || strings.Contains(text, "model_price") {
			return true
		}
	}
	*warnings = append(*warnings, "未发现 /api/pricing 或 /api/ratio_config 价格数据")
	return false
}

// probeModelsEndpoint 用商家密钥读取 OpenAI 兼容模型清单。
func probeModelsEndpoint(ctx context.Context, baseURL string, apiKey string) ([]string, int, bool, error) {
	body, status, err := probeGet(ctx, baseURL+"/v1/models", apiKey)
	if err != nil {
		return nil, status, false, err
	}
	if status != http.StatusOK {
		return nil, status, false, nil
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &resp); err != nil {
		return nil, status, false, err
	}
	if len(resp.Data) == 0 {
		return nil, status, false, nil
	}
	models := make([]string, 0, len(resp.Data))
	for _, item := range resp.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			models = append(models, id)
			if len(models) >= upstreamProbeModelLimit {
				break
			}
		}
	}
	return models, status, len(models) > 0, nil
}

// probeLooksLikeSub2API 读取 /api/status 作为 Sub2API 的弱信号。
func probeLooksLikeSub2API(ctx context.Context, baseURL string) bool {
	body, status, err := probeGet(ctx, baseURL+"/api/status", "")
	if err != nil || status != http.StatusOK {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "sub2api")
}

func probeGet(ctx context.Context, target string, apiKey string) ([]byte, int, error) {
	if err := ValidateSSRFProtectedFetchURL(target); err != nil {
		return nil, 0, err
	}
	client := GetSSRFProtectedHTTPClient()
	if client == nil {
		return nil, 0, nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamProbeMaxBody))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
