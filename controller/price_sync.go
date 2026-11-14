package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// 报价折算基数：模型倍率 1.0 对应 $2/1M tokens（QuotaPerUnit=500000 quota = $1，
// 1M tokens × 倍率 1 = 1M quota = $2）。输入价 = 倍率 × priceRatioBase，
// 输出价 = 倍率 × 补全倍率 × priceRatioBase。
// 常量唯一来源在 model 包，避免上下游口径漂移。
const priceRatioBase = model.QuoteRatioBase

const priceSyncHTTPTimeout = 15 * time.Second
const priceSyncMaxBodyBytes = 10 << 20

// 公开价格源识别
const (
	openRouterHost       = "openrouter.ai"
	openRouterModelsPath = "/api/v1/models"
	// OpenAI /v1/models 报价协议：带鉴权的 /v1/models 响应里附带 USD/1M-token 价格。
	openAIModelsPricingPath = "/v1/models"
)

// channelQuoteDraft 同步折算后的一份模型报价草案。
type channelQuoteDraft struct {
	ModelName   string  `json:"model_name"`
	InputPrice  float64 `json:"input_price"`
	OutputPrice float64 `json:"output_price"`
}

// PriceSyncSummary 一次同步任务的汇总。
type PriceSyncSummary struct {
	TotalChannels   int      `json:"total_channels"`
	SyncedChannels  int      `json:"synced_channels"`
	FailedChannels  int      `json:"failed_channels"`
	SkippedChannels int      `json:"skipped_channels"` // 上游不提供兼容价格接口，已跳过并关闭自动同步
	WrittenQuotes   int64    `json:"written_quotes"`
	FailedDetails   []string `json:"failed_details,omitempty"`
	SkippedDetails  []string `json:"skipped_details,omitempty"`
}

// errPriceUnsupported 表示上游不提供可自动同步的价格接口（典型：Sub2API 等
// 只有 /v1/* 中继面的网关）。这类渠道不做自动同步，报价由商家/管理员手填。
var errPriceUnsupported = errors.New("上游不提供可自动同步的价格接口，报价需手填")

// fetchChannelQuoteDrafts 从渠道上游拉取价格并折算为 $/1M 报价草案。
// 支持以下价格源：
//   - New API 系 /api/pricing     → data 为 []Pricing（type2）
//   - New API 系 /api/ratio_config → data 为 map（type1，含 model_ratio 等 key）
//   - OpenAI /v1/models 报价协议   → 带 Bearer 鉴权，pricing 为 USD/1M-token
//   - OpenRouter /api/v1/models   → 公开按 token 计价，复用 ratio_sync 的转换
//   - models.dev /api.json        → 公开按 $/1M 计价，复用 ratio_sync 的转换
//
// 若上游只有中继面（典型是 Sub2API），两个 New API 端点都会 404/401，
// 此时返回 errPriceUnsupported，让调用方优雅降级为"仅手填报价"而不是反复报错。
func fetchChannelQuoteDrafts(ctx context.Context, channel *model.Channel) ([]channelQuoteDraft, error) {
	return fetchQuoteDraftsByURL(ctx, strings.TrimRight(channel.GetBaseURL(), "/"),
		priceSyncChannelKey(channel), channel.GetPriceSyncProtocol())
}

// priceSyncChannelKey 取渠道用于读取价格接口的密钥（多 Key 渠道取首个非空 Key）。
func priceSyncChannelKey(channel *model.Channel) string {
	for _, key := range channel.GetKeys() {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(channel.Key)
}

// fetchQuoteDraftsByURL 按上游站址（而非渠道记录）拉取价格草案，
// 供商家在填写入驻/扩容表单时自助检测「倍率自动获取」是否可用。
// protocol 为空时按站址自动选择价格源；openai_models_pricing 时使用带鉴权的 /v1/models。
func fetchQuoteDraftsByURL(ctx context.Context, baseURL string, apiKey string, protocol string) ([]channelQuoteDraft, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("站址必须是 http/https")
	}

	// 渠道站址（尤其商家自填）属用户可控目标：复用仓库统一的 SSRF 防护客户端，
	// 它在拨号时校验解析出的 IP，并对每一跳重定向复用同一校验。
	// 注意：该客户端不透传 TLSInsecureSkipVerify，自签名上游需改用受信任证书。
	protected := service.GetSSRFProtectedHTTPClient()
	if protected == nil {
		return nil, fmt.Errorf("SSRF 防护客户端未初始化")
	}
	client := *protected
	if client.Timeout <= 0 || client.Timeout > priceSyncHTTPTimeout {
		client.Timeout = priceSyncHTTPTimeout
	}

	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == string(service.UpstreamKindOpenAIModelsPricing) {
		return fetchOpenAIModelsPricingQuoteDrafts(ctx, &client, baseURL, apiKey)
	}

	if host := upstreamHostname(baseURL); host == modelsDevHost || strings.HasSuffix(host, openRouterHost) {
		return fetchPublicPricePreset(ctx, &client, baseURL, host)
	}

	var lastErr error
	unsupported := 0
	for _, endpoint := range []string{"/api/pricing", "/api/ratio_config"} {
		target := baseURL + endpoint
		if err := service.ValidateSSRFProtectedFetchURL(target); err != nil {
			// 典型场景：本机 DNS/CDN 伪 IP（如 fake-ip 返回 198.18.x.x）会被网络获取策略
			// 拦下；提示运维在系统设置里放行该域名或网段。
			lastErr = fmt.Errorf("站址被网络获取策略拒绝（可在“系统设置 → 网络获取”放行该域名/网段）: %w", err)
			continue
		}
		body, status, err := httpGetJSON(ctx, &client, target, "")
		if err != nil {
			lastErr = err
			if isUnsupportedPriceStatus(status) {
				unsupported++
			}
			continue
		}
		drafts, err := parseUpstreamQuotes(body)
		if err != nil {
			lastErr = err
			unsupported++ // 端点存在但格式不兼容，等价于不提供兼容价格接口
			continue
		}
		return drafts, nil
	}
	if unsupported >= 2 {
		return nil, errPriceUnsupported
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("上游无可识别的价格数据")
	}
	return nil, lastErr
}

// fetchOpenAIModelsPricingQuoteDrafts 用渠道密钥读取上游 /v1/models 价格表。
func fetchOpenAIModelsPricingQuoteDrafts(ctx context.Context, client *http.Client, baseURL string, apiKey string) ([]channelQuoteDraft, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OpenAI /v1/models 报价协议上游缺少 API Key，无法读取 /v1/models 报价")
	}
	target := baseURL + openAIModelsPricingPath
	if err := service.ValidateSSRFProtectedFetchURL(target); err != nil {
		return nil, fmt.Errorf("站址被网络获取策略拒绝（可在“系统设置 → 网络获取”放行该域名/网段）: %w", err)
	}
	body, status, err := httpGetJSON(ctx, client, target, apiKey)
	if err != nil {
		// 401/403 是密钥问题而非"上游不支持"：保留为失败，避免定时任务
		// 因临时密钥失效就永久关闭自动同步。
		if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
			return nil, errPriceUnsupported
		}
		return nil, err
	}
	return parseOpenAIModelsPricingQuotes(body)
}

// parseOpenAIModelsPricingQuotes 解析 /v1/models 的内嵌定价：
// data[].pricing.{currency,input_per_m,cached_input_per_m,output_per_m}，
// 价格为 USD/1M-token，可直接作为盘口报价；缓存价当前盘口模型不区分，忽略。
func parseOpenAIModelsPricingQuotes(body []byte) ([]channelQuoteDraft, error) {
	var envelope struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing *struct {
				Currency   string   `json:"currency"`
				InputPerM  *float64 `json:"input_per_m"`
				OutputPerM *float64 `json:"output_per_m"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("响应不是合法 JSON: %w", err)
	}
	drafts := make([]channelQuoteDraft, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		modelName := strings.TrimSpace(item.ID)
		pricing := item.Pricing
		if modelName == "" || pricing == nil || pricing.InputPerM == nil || pricing.OutputPerM == nil {
			continue
		}
		currency := strings.TrimSpace(pricing.Currency)
		if currency != "" && !strings.EqualFold(currency, "USD") {
			continue
		}
		if *pricing.InputPerM <= 0 || *pricing.OutputPerM <= 0 {
			continue
		}
		drafts = append(drafts, channelQuoteDraft{
			ModelName:   modelName,
			InputPrice:  *pricing.InputPerM,
			OutputPrice: *pricing.OutputPerM,
		})
	}
	if len(drafts) == 0 {
		return nil, errPriceUnsupported
	}
	return drafts, nil
}

// upstreamHostname 取 base_url 的主机名（小写），解析失败返回空串。
func upstreamHostname(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// fetchPublicPricePreset 拉取 OpenRouter / models.dev 的公开价格表。
// 复用 ratio_sync 的转换器：它们输出 model_ratio/completion_ratio 结构，
// 与 draftsFromRatioMaps 的折算口径一致（ratio 1.0 = $2/1M tokens）。
func fetchPublicPricePreset(ctx context.Context, client *http.Client, baseURL string, host string) ([]channelQuoteDraft, error) {
	endpoint := modelsDevPath
	if strings.HasSuffix(host, openRouterHost) {
		endpoint = openRouterModelsPath
	}
	target := baseURL + endpoint
	if err := service.ValidateSSRFProtectedFetchURL(target); err != nil {
		return nil, err
	}
	body, _, err := httpGetJSON(ctx, client, target, "")
	if err != nil {
		return nil, err
	}
	return draftsFromPublicPriceBody(host, body)
}

// draftsFromPublicPriceBody 把公开价格表响应折算为报价草案（按 host 选择转换器）。
func draftsFromPublicPriceBody(host string, body []byte) ([]channelQuoteDraft, error) {
	var (
		ratioData map[string]any
		err       error
	)
	if strings.HasSuffix(host, openRouterHost) {
		ratioData, err = convertOpenRouterToRatioData(bytes.NewReader(body))
	} else {
		ratioData, err = convertModelsDevToRatioData(bytes.NewReader(body))
	}
	if err != nil {
		return nil, err
	}
	drafts := draftsFromRatioMaps(ratioData)
	if len(drafts) == 0 {
		return nil, errPriceUnsupported
	}
	return drafts, nil
}

// isUnsupportedPriceStatus 判断响应是否说明"该上游没有这个价格接口"。
func isUnsupportedPriceStatus(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	default:
		return false
	}
}

// httpGetJSON 带退避重试的 GET，返回响应体与状态码（状态码在错误时也带回，
// 便于上层区分"上游没有这个接口"与"网络/上游故障"）。apiKey 非空时按
// Bearer 发送，供 OpenAI /v1/models 报价协议鉴权。
func httpGetJSON(ctx context.Context, client *http.Client, url string, apiKey string) ([]byte, int, error) {
	var lastErr error
	lastStatus := 0
	for attempt := range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			lastStatus = resp.StatusCode
			if resp.StatusCode == http.StatusOK {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, priceSyncMaxBodyBytes))
				resp.Body.Close()
				if readErr == nil {
					return body, resp.StatusCode, nil
				}
				lastErr = readErr
			} else {
				resp.Body.Close()
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
				if resp.StatusCode >= 400 && resp.StatusCode < 500 {
					return nil, resp.StatusCode, lastErr // 客户端错误不再重试
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, lastStatus, ctx.Err()
		case <-time.After(time.Duration(200*(1<<attempt)) * time.Millisecond):
		}
	}
	return nil, lastStatus, lastErr
}

// parseUpstreamQuotes 把上游返回体解析为报价草案，自动识别 type1/type2 格式。
func parseUpstreamQuotes(body []byte) ([]channelQuoteDraft, error) {
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := common.DecodeJson(bytes.NewReader(body), &envelope); err != nil {
		return nil, fmt.Errorf("响应不是合法 JSON: %w", err)
	}
	if len(envelope.Data) == 0 {
		return nil, fmt.Errorf("响应缺少 data 字段")
	}

	// type1: data 为 map，包含 model_ratio / model_price 等 key
	var type1Data map[string]any
	if err := common.Unmarshal(envelope.Data, &type1Data); err == nil {
		if _, ok := type1Data["model_ratio"].(map[string]any); ok {
			return draftsFromRatioMaps(type1Data), nil
		}
	}

	// type2: data 为 []Pricing
	var items []struct {
		ModelName       string   `json:"model_name"`
		QuotaType       int      `json:"quota_type"`
		ModelRatio      *float64 `json:"model_ratio"`
		ModelPrice      *float64 `json:"model_price"`
		CompletionRatio *float64 `json:"completion_ratio"`
	}
	if err := common.Unmarshal(envelope.Data, &items); err == nil && len(items) > 0 {
		var drafts []channelQuoteDraft
		for _, item := range items {
			// 按次计费模型无法折算为 token 单价；倍率非正数属上游配置错误，一并跳过
			if item.ModelName == "" || item.QuotaType != 0 || item.ModelRatio == nil || *item.ModelRatio <= 0 {
				continue
			}
			completion := 1.0
			if item.CompletionRatio != nil && *item.CompletionRatio > 0 {
				completion = *item.CompletionRatio
			}
			drafts = append(drafts, channelQuoteDraft{
				ModelName:   item.ModelName,
				InputPrice:  *item.ModelRatio * priceRatioBase,
				OutputPrice: *item.ModelRatio * completion * priceRatioBase,
			})
		}
		if len(drafts) > 0 {
			return drafts, nil
		}
	}

	return nil, fmt.Errorf("无法识别的上游价格格式")
}

// draftsFromRatioMaps 从 type1 map 折算报价。
func draftsFromRatioMaps(data map[string]any) []channelQuoteDraft {
	toFloatMap := func(key string) map[string]float64 {
		raw, ok := data[key].(map[string]any)
		if !ok {
			return nil
		}
		out := make(map[string]float64, len(raw))
		for k, v := range raw {
			if f, ok := v.(float64); ok {
				out[k] = f
			}
		}
		return out
	}
	modelRatio := toFloatMap("model_ratio")
	completionRatio := toFloatMap("completion_ratio")
	drafts := make([]channelQuoteDraft, 0, len(modelRatio))
	for name, ratio := range modelRatio {
		if name == "" || ratio <= 0 {
			continue
		}
		completion := 1.0
		if cr, ok := completionRatio[name]; ok && cr > 0 {
			completion = cr
		}
		drafts = append(drafts, channelQuoteDraft{
			ModelName:   name,
			InputPrice:  ratio * priceRatioBase,
			OutputPrice: ratio * completion * priceRatioBase,
		})
	}
	return drafts
}

// roundQuotePrice 统一盘口单价精度（USD/1M，保留 4 位小数），
// 避免成本折算后的浮点尾差写入数据库。
func roundQuotePrice(value float64) float64 {
	return math.Round(value*10000) / 10000
}

// syncQuotesForChannel 把报价草案落到盘口：与渠道声明的模型取交集，
// 每个所属号池各写一行。最低安全售价保护开启时，仅在上游涨破安全线
// 才上调价格；关闭时直接按最低安全售价覆盖。
func syncQuotesForChannel(channel *model.Channel, drafts []channelQuoteDraft) (int, error) {
	served := make(map[string]bool)
	for _, m := range strings.Split(channel.Models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			served[m] = true
		}
	}
	// 商家对接改名的模型：上游按 upstream 名报价，先反解回官方名再落盘，
	// 否则映射模型的 auto 报价永远匹配不上（model_mapping 官方名→上游名）。
	// 若某个 upstream 名本身也是直营官方名，则该名保留自己的报价、不重映射，
	// 避免重映射后与直营名的报价行撞键（PostgreSQL 会直接报错）。
	if mapping := channel.GetModelMapping(); mapping != "" {
		var m map[string]string
		if common.Unmarshal([]byte(mapping), &m) == nil {
			reverse := make(map[string]string, len(m))
			for official, upstream := range m {
				if upstream != "" {
					reverse[upstream] = official
				}
			}
			if len(reverse) > 0 {
				for i := range drafts {
					if official, ok := reverse[drafts[i].ModelName]; ok && !served[drafts[i].ModelName] {
						drafts[i].ModelName = official
					}
				}
			}
		}
	}

	pools := channel.GetGroups()
	if len(pools) == 0 {
		pools = []string{"default"}
	}

	// 当前盘口：最低安全售价保护仅针对 OpenAI /v1/models 报价协议生效，
	// 需要拿「已设定的对外售价」做比较，未涨价时保留原价（含管理员手填价），
	// 涨破安全线才自动上调。非该协议渠道保持原有 1.0 倍率直接同步语义。
	setting := operation_setting.GetMarketSetting()
	isOpenAIModelsPricing := strings.EqualFold(
		channel.GetPriceSyncProtocol(),
		string(service.UpstreamKindOpenAIModelsPricing),
	)
	costMult := 1.0
	if isOpenAIModelsPricing {
		costMult = setting.UpstreamUSDCNYRate * setting.FeeMultiplier * setting.MinProfitMult
	}
	existing := make(map[string]model.ChannelQuote)
	if isOpenAIModelsPricing {
		var existingRows []model.ChannelQuote
		if err := model.DB.Where("channel_id = ?", channel.Id).Find(&existingRows).Error; err == nil {
			for _, row := range existingRows {
				existing[row.PoolGroup+"|"+row.ModelName] = row
			}
		}
	}

	// 渠道没有声明任何模型（served 为空）时不能放行全部上游报价：那会给
	// 根本不是本渠道服务范围的模型铺满"幽灵盘口"。与 RebuildChannelQuotesWithTx
	// 的语义保持一致——没有模型即没有可挂的盘口，走下方清理分支清干净。
	quotes := make([]model.ChannelQuote, 0, len(drafts)*len(pools))
	kept := make(map[string]bool)
	for _, draft := range drafts {
		if !served[draft.ModelName] {
			continue
		}
		minInput := roundQuotePrice(draft.InputPrice * costMult)
		minOutput := roundQuotePrice(draft.OutputPrice * costMult)
		for _, pool := range pools {
			key := pool + "|" + draft.ModelName
			current, hasCurrent := existing[key]
			targetInput, targetOutput := minInput, minOutput
			if isOpenAIModelsPricing && setting.EnableFloorProtection && hasCurrent {
				targetInput = max(current.InputPrice, minInput)
				targetOutput = max(current.OutputPrice, minOutput)
				// 上游没有涨破安全线：保留当前对外售价（尤其是手填宣传价），
				// 不产生写操作，也不改变报价来源。
				if targetInput <= current.InputPrice && targetOutput <= current.OutputPrice {
					kept[key] = true
					continue
				}
			}
			quotes = append(quotes, model.ChannelQuote{
				ChannelId: channel.Id,
				PoolGroup: pool,
				ModelName: draft.ModelName,
				// 落库单价反推出有效倍率，供行情与比价统一按倍率口径展示
				RateMultiplier: targetInput / priceRatioBase,
				InputPrice:     targetInput,
				OutputPrice:    targetOutput,
				Currency:       "USD",
				Source:         model.QuoteSourceAuto,
				Status:         model.QuoteStatusEnabled,
			})
		}
	}
	if len(quotes) == 0 {
		// 本轮没有任何可写的报价（上游改名/无交集）：只清理超出渠道当前
		// 「分组 × 模型」范围的幽灵行；范围内 auto/manual 行保持原样，
		// 绝不能传空白名单把管理员锁定（manual）的报价整批删掉。
		valid := make(map[string]bool)
		for _, pool := range pools {
			for _, m := range strings.Split(channel.Models, ",") {
				if m = strings.TrimSpace(m); m != "" {
					valid[pool+"|"+m] = true
				}
			}
		}
		return 0, model.CleanupStaleAutoQuotes(model.DB, channel.Id, valid)
	}
	// 兜底去重：同一 (pool, model) 只保留一条，PostgreSQL 的 OnConflict
	// 不允许同一条行被影响两次（不同数据库行为不一致）。
	seenQuote := make(map[string]struct{}, len(quotes))
	deduped := quotes[:0]
	for _, q := range quotes {
		key := q.PoolGroup + "|" + q.ModelName
		if _, dup := seenQuote[key]; dup {
			continue
		}
		seenQuote[key] = struct{}{}
		deduped = append(deduped, q)
	}
	quotes = deduped
	if err := model.UpsertChannelQuotes(quotes); err != nil {
		return 0, err
	}
	// 清理本轮上游未再报价 / 已下架模型残留的 auto 行，防止幽灵盘口；
	// 因未涨破安全线而保留的行虽未重写，仍属渠道在售范围，一并加入白名单。
	written := make(map[string]bool, len(quotes)+len(kept))
	for _, q := range quotes {
		written[q.PoolGroup+"|"+q.ModelName] = true
	}
	for key := range kept {
		written[key] = true
	}
	if err := model.CleanupStaleAutoQuotes(model.DB, channel.Id, written); err != nil {
		return len(quotes), err
	}
	return len(quotes), nil
}

// RunPriceSync 执行一轮全量报价同步。manual 为 true 时忽略渠道级开关（管理员手动触发）。
func RunPriceSync(ctx context.Context, concurrency int, manual bool, reportProgress func(processed, total int)) *PriceSyncSummary {
	setting := operation_setting.GetMarketSetting()
	if concurrency <= 0 {
		concurrency = setting.PriceSyncConcurrency
	}
	if concurrency <= 0 {
		concurrency = 8
	}

	var channels []*model.Channel
	if err := model.DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		logger.LogError(ctx, "price sync: query channels failed: "+err.Error())
		return &PriceSyncSummary{}
	}

	var targets []*model.Channel
	for _, ch := range channels {
		if ch.GetPriceSync() || manual {
			targets = append(targets, ch)
		}
	}

	summary := &PriceSyncSummary{TotalChannels: len(targets)}
	if len(targets) == 0 {
		return summary
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, ch := range targets {
		wg.Add(1)
		go func(idx int, channel *model.Channel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			drafts, err := fetchChannelQuoteDrafts(ctx, channel)
			if errors.Is(err, errPriceUnsupported) {
				mu.Lock()
				summary.SkippedChannels++
				if len(summary.SkippedDetails) < 50 {
					summary.SkippedDetails = append(summary.SkippedDetails, fmt.Sprintf("%s(#%d)", channel.Name, channel.Id))
				}
				mu.Unlock()
				// 定时任务里才自动关闭：管理员手动触发时保留其显式设置，只报告结果。
				if !manual {
					disableUnsupportedPriceSync(ctx, channel)
				}
			} else if err == nil {
				written, writeErr := syncQuotesForChannel(channel, drafts)
				if writeErr == nil {
					mu.Lock()
					summary.SyncedChannels++
					summary.WrittenQuotes += int64(written)
					mu.Unlock()
				} else {
					err = writeErr
				}
			}
			if err != nil && !errors.Is(err, errPriceUnsupported) {
				mu.Lock()
				summary.FailedChannels++
				name := fmt.Sprintf("%s(#%d): %s", channel.Name, channel.Id, err.Error())
				if len(summary.FailedDetails) < 50 {
					summary.FailedDetails = append(summary.FailedDetails, name)
				}
				mu.Unlock()
				logger.LogWarn(ctx, fmt.Sprintf("price sync failed on %s(#%d): %s", channel.Name, channel.Id, err.Error()))
			}
			if reportProgress != nil {
				reportProgress(idx+1, len(targets))
			}
		}(i, ch)
	}
	wg.Wait()
	return summary
}

// disableUnsupportedPriceSync 关闭该渠道的自动报价同步并说明原因：
// 上游没有兼容的价格接口时，每轮重试只会刷失败记录，报价改由手填维护。
func disableUnsupportedPriceSync(ctx context.Context, channel *model.Channel) {
	if !channel.GetPriceSync() {
		return
	}
	err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).
		Update("price_sync", false).Error
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("price sync: disable auto sync failed on %s(#%d): %s",
			channel.Name, channel.Id, err.Error()))
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("price sync unsupported upstream, auto sync disabled for %s(#%d)",
		channel.Name, channel.Id))
}

// SyncChannelQuotes 管理员手动触发同步（不传 channel_ids 则对全部开启同步的渠道执行）。
func SyncChannelQuotes(c *gin.Context) {
	var req struct {
		ChannelIds []int `json:"channel_ids"`
	}
	_ = c.ShouldBindJSON(&req)

	var summary *PriceSyncSummary
	if len(req.ChannelIds) > 0 {
		channels, err := model.GetChannelsByIds(req.ChannelIds)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "查询渠道失败: " + err.Error()})
			return
		}
		summary = runPriceSyncForChannels(c.Request.Context(), channels)
	} else {
		summary = RunPriceSync(c.Request.Context(), 0, true, nil)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": summary})
}

// runPriceSyncForChannels 对给定的一批渠道各执行一轮报价同步（并发受市场设置限制）。
// 管理员按渠道触发与商家门户"立即同步"共用这一条路径。
func runPriceSyncForChannels(ctx context.Context, channels []*model.Channel) *PriceSyncSummary {
	summary := &PriceSyncSummary{TotalChannels: len(channels)}
	if len(channels) == 0 {
		return summary
	}
	sem := make(chan struct{}, operation_setting.GetMarketSetting().PriceSyncConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, ch := range channels {
		wg.Add(1)
		go func(channel *model.Channel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			drafts, err := fetchChannelQuoteDrafts(ctx, channel)
			if errors.Is(err, errPriceUnsupported) {
				mu.Lock()
				summary.SkippedChannels++
				if len(summary.SkippedDetails) < 50 {
					summary.SkippedDetails = append(summary.SkippedDetails, fmt.Sprintf("%s(#%d)", channel.Name, channel.Id))
				}
				mu.Unlock()
			} else if err == nil {
				written, writeErr := syncQuotesForChannel(channel, drafts)
				if writeErr == nil {
					mu.Lock()
					summary.SyncedChannels++
					summary.WrittenQuotes += int64(written)
					mu.Unlock()
				} else {
					err = writeErr
				}
			}
			if err != nil && !errors.Is(err, errPriceUnsupported) {
				mu.Lock()
				summary.FailedChannels++
				if len(summary.FailedDetails) < 50 {
					summary.FailedDetails = append(summary.FailedDetails, fmt.Sprintf("%s(#%d): %s", channel.Name, channel.Id, err.Error()))
				}
				mu.Unlock()
			}
		}(ch)
	}
	wg.Wait()
	return summary
}
