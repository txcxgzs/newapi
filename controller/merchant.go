package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// errMerchantApplicationPending 用户已存在待审申请（事务内查重命中）。
var errMerchantApplicationPending = errors.New("merchant application pending")

// merchantModelQuote 入驻申请中要上架的模型条目。
// Model 是平台官方模型名（用户请求的名字）；Upstream 是上游实际模型名，
// 两者不同时审核通过会写入渠道的 model_mapping（官方名 → 上游名）。
// 商家不填价格：模型归属哪个号池由管理员审核时决定，价格由渠道倍率派生。
type merchantModelQuote struct {
	Model    string `json:"model"`
	Pool     string `json:"pool"`
	Upstream string `json:"upstream,omitempty"`
}

// merchantChannelModelLimit 审核通过时写入渠道的模型数上限（探测清单可能很长）。
const merchantChannelModelLimit = 200

// merchantRateMultiplierLimit 渠道倍率（相对平台标准价的折扣系数）上限。
// 商家门户调倍率已有 0（不含）~100 的校验，提交申请、编辑渠道与审核通过
// 同样必须拦截，否则可以绕过门户把天价倍率写进渠道的派生盘口。
const merchantRateMultiplierLimit = 100.0

// ---------- 用户侧：入驻申请 ----------

// PostMerchantApplication 登录用户提交商家入驻申请。
// 上游类型（newapi/sub2api/openai）决定中继端点家族与报价同步能力。
// 商家只填渠道倍率（相对平台标准价的折扣系数），不填任何价格；
// Sub2API 一类不暴露价格接口的网关可以不填模型清单，审核时由探测补全。
func PostMerchantApplication(c *gin.Context) {
	userId := c.GetInt("id")
	var req struct {
		SiteName       string               `json:"site_name" binding:"required"`
		SiteUrl        string               `json:"site_url" binding:"required"`
		SiteKey        string               `json:"site_key" binding:"required"`
		UpstreamKind   string               `json:"upstream_kind"`
		RateMultiplier float64              `json:"rate_multiplier"`
		Models         []merchantModelQuote `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误：站名、站址与密钥必填"})
		return
	}
	for i := range req.Models {
		name := strings.TrimSpace(req.Models[i].Model)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型清单存在空条目"})
			return
		}
		// 渠道模型以逗号拼接存储：含逗号的模型名会被拆成多个模型，
		// 等于绕过逐模型审核，必须在入口拒绝。
		if strings.ContainsAny(name, ", \t\r\n") {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型名不能包含逗号或空白字符：" + name})
			return
		}
		req.Models[i].Model = name
	}
	if req.RateMultiplier < 0 || req.RateMultiplier > merchantRateMultiplierLimit {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道倍率不能为负数，且不能超过 100"})
		return
	}

	// 已是商家的用户提交的是"扩容申请"（在已有分组下新增渠道），允许提交；
	// 未入驻的用户提交的是首次入驻申请。
	if _, err := model.GetMerchantProfileByUserId(userId); err == nil {
		if len(req.Models) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "扩容申请必须至少选择一个要上架的模型"})
			return
		}
	}
	// 已有待审申请则拒绝重复提交。查重与插入放进同一事务：锁定读防止并发下
	// 先查后插的重复提交；SQLite 单写者串行化，并发提交表现为 BUSY 失败，不会重复插入。
	modelsJson, err := common.Marshal(req.Models)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	app := &model.MerchantApplication{
		UserId:         userId,
		SiteName:       strings.TrimSpace(req.SiteName),
		SiteUrl:        strings.TrimSpace(req.SiteUrl),
		SiteKey:        req.SiteKey,
		Models:         string(modelsJson),
		RateMultiplier: normalizeRateMultiplier(req.RateMultiplier),
		UpstreamKind:   string(service.ParseUpstreamKind(req.UpstreamKind)),
		Status:         model.MerchantApplicationPending,
		CreatedAt:      time.Now().Unix(),
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		// 锁定读必须避免聚合函数：PG 禁止 count(*) 与 FOR UPDATE 同用，
		// 用 SELECT id ... LIMIT 1 判重；MySQL 下同时锁住 user_id 索引间隙
		//（依赖 InnoDB 默认 REPEATABLE READ，READ-COMMITTED 无间隙锁），
		// SQLite 跳过锁、靠单写者串行化。
		check := tx.Model(&model.MerchantApplication{}).
			Where("user_id = ? AND status = ?", userId, model.MerchantApplicationPending).
			Select("id").Limit(1)
		if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			check = check.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var pendingIds []int
		if err := check.Find(&pendingIds).Error; err != nil {
			return err
		}
		if len(pendingIds) > 0 {
			return errMerchantApplicationPending
		}
		return tx.Create(app).Error
	}); err != nil {
		if errors.Is(err, errMerchantApplicationPending) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "已有待审核的申请"})
			return
		}
		// 部分唯一索引兜底触发：PG 竞态窗口内的并发双插，后到的入库失败。
		// 注：gorm 未开启 TranslateError，ErrDuplicatedKey 当前恒不命中，
		// 实际靠下方 hasPendingMerchantApplication 复核；保留它作为将来
		// 开启错误翻译后的第一道快速路径。
		if errors.Is(err, gorm.ErrDuplicatedKey) || hasPendingMerchantApplication(userId) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "已有待审核的申请"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, app)
}

// hasPendingMerchantApplication 查重兜底：唯一索引冲突未被驱动翻译成
// gorm.ErrDuplicatedKey 时，用普通查询确认是否为并发重复提交。
func hasPendingMerchantApplication(userId int) bool {
	var pending int64
	if err := model.DB.Model(&model.MerchantApplication{}).
		Where("user_id = ? AND status = ?", userId, model.MerchantApplicationPending).
		Count(&pending).Error; err != nil {
		// 查询失败时不误报"已有申请"，让原始错误原样返回便于排查
		common.SysError("recheck pending merchant application failed: " + err.Error())
		return false
	}
	return pending > 0
}

// GetMyMerchantApplication 查看自己的入驻申请（最近一条）。
func GetMyMerchantApplication(c *gin.Context) {
	userId := c.GetInt("id")
	var app model.MerchantApplication
	err := model.DB.Where("user_id = ?", userId).Order("id DESC").First(&app).Error
	if err != nil {
		common.ApiSuccess(c, nil)
		return
	}
	common.ApiSuccess(c, app)
}

// merchantRatioPreviewLimit 倍率检测时返回给前端的样例报价条数。
const merchantRatioPreviewLimit = 8

// GetMerchantPlatformModels 平台在售的官方模型清单（全部分组）。
// 模型对接编辑器的下拉数据源：商家上架要对接的是平台全量模型，
// 不按商家当前可用分组过滤——商家申请加入的号池可能尚未开通。
func GetMerchantPlatformModels(c *gin.Context) {
	models := model.GetEnabledModels()
	sort.Strings(models)
	common.ApiSuccess(c, models)
}

// ProbeMerchantApplyUpstream 商家在入驻/扩容表单里自查上游：
// 校验站址与密钥、判定上游类型，并实测一次「倍率自动获取」——
// 真正从上游价格接口拉取数据折算成 $/1M 报价，返回可用性与样例报价，
// 而不是只判断接口是否存在。站址/密钥由请求体临时提供，不落库。
func ProbeMerchantApplyUpstream(c *gin.Context) {
	var req struct {
		SiteUrl      string `json:"site_url" binding:"required"`
		SiteKey      string `json:"site_key" binding:"required"`
		UpstreamKind string `json:"upstream_kind"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误：站址与密钥必填"})
		return
	}
	siteUrl := strings.TrimSpace(req.SiteUrl)
	if err := service.ValidateSSRFProtectedFetchURL(siteUrl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "站址不合法或不可访问（若为本地 DNS/CDN 伪 IP，请联系管理员在“系统设置 → 网络获取”放行）：" + err.Error(),
		})
		return
	}

	// 探测 + 抓价的网络耗时必须封顶，避免表单里的"检测"按钮长时间无响应。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	declared := service.ParseUpstreamKind(req.UpstreamKind)
	common.ApiSuccess(c, probeUpstreamWithPrices(ctx, siteUrl, req.SiteKey, declared))
}

// probeUpstreamWithPrices 探测上游并实测「倍率自动获取」，返回探测结果、
// 上游模型清单与样例报价。入驻/扩容表单与渠道编辑共用这一条口径。
func probeUpstreamWithPrices(ctx context.Context, siteUrl string, siteKey string, declared service.UpstreamKind) gin.H {
	probe := service.ProbeUpstream(ctx, siteUrl, siteKey, declared)
	result := gin.H{
		"probe":               probe,
		"kind":                probe.Kind,
		"price_sync_capable":  probe.PriceSyncCapable,
		"price_sync_protocol": probe.PriceSyncProtocol,
		"models_endpoint_ok":  probe.ModelsEndpointOK,
		"model_count":         probe.ModelCount,
		"warnings":            probe.Warnings,
	}
	if len(probe.Models) > 0 {
		// 上游模型清单：前端用于「上游模型名 ↔ 平台官方模型名」的对接编辑器
		result["models"] = probe.Models
	}
	// 商家显式声明 Sub2API / OpenAI 兼容且探测不到价格接口时不再实测抓价。
	// 若显式选择 OpenAI /v1/models 报价协议，协议非空，仍需带密钥实测。
	if probe.PriceSyncProtocol == "" &&
		(declared == service.UpstreamKindSub2API || declared == service.UpstreamKindOpenAICompatible) {
		result["price_fetch_ok"] = false
		result["price_fetch_message"] = "该上游类型不提供价格接口，报价将按平台标准价挂牌"
		return result
	}
	drafts, err := fetchQuoteDraftsByURL(ctx, siteUrl, siteKey, probe.PriceSyncProtocol)
	if err != nil || len(drafts) == 0 {
		result["price_fetch_ok"] = false
		if err != nil && !errors.Is(err, errPriceUnsupported) {
			result["price_fetch_message"] = err.Error()
		}
		return result
	}
	// 倍率合理性校验：上游配置错误（非正数倍率、异常天价）不能静默进入盘口。
	// 非正数直接剔除；异常偏大的保留但给出告警，商家与管理员都能看到。
	sane := make([]channelQuoteDraft, 0, len(drafts))
	var abnormal []string
	for _, d := range drafts {
		switch {
		case d.InputPrice <= 0:
			abnormal = append(abnormal, fmt.Sprintf("%s：上游倍率 ≤ 0，已忽略", d.ModelName))
		case d.InputPrice > priceRatioBase*1000:
			abnormal = append(abnormal, fmt.Sprintf("%s：上游倍率异常偏大（输入 $%.2f/1M），请核对", d.ModelName, d.InputPrice))
			sane = append(sane, d)
		default:
			sane = append(sane, d)
		}
	}
	if len(abnormal) > 0 {
		if len(abnormal) > 5 {
			abnormal = append(abnormal[:5], fmt.Sprintf("…共 %d 条异常", len(abnormal)))
		}
		result["warnings"] = append(probe.Warnings, abnormal...)
	}
	if len(sane) == 0 {
		result["price_fetch_ok"] = false
		result["price_fetch_message"] = "上游返回的倍率数据全部异常（非正数），无法用于自动同步"
		return result
	}
	sort.Slice(sane, func(i, j int) bool { return sane[i].ModelName < sane[j].ModelName })
	limit := min(len(sane), merchantRatioPreviewLimit)
	result["price_fetch_ok"] = true
	result["ratio_total"] = len(sane)
	result["ratio_preview"] = sane[:limit]
	return result
}

// upstreamKindFromChannel 把渠道类型与已保存的同步协议映射回探测声明口径。
func upstreamKindFromChannel(channel *model.Channel) service.UpstreamKind {
	if channel.GetPriceSyncProtocol() == string(service.UpstreamKindOpenAIModelsPricing) {
		return service.UpstreamKindOpenAIModelsPricing
	}
	switch channel.Type {
	case constant.ChannelTypeNewAPI:
		return service.UpstreamKindNewAPI
	case constant.ChannelTypeSub2API:
		return service.UpstreamKindSub2API
	default:
		return service.UpstreamKindOpenAICompatible
	}
}

// validateMappingAcyclic 沿 official→upstream 链走一遍，检测模型映射成环
// （如 A→B 且 B→A）。成环的映射会让中继解析模型名时直接报错，必须保存前拦截。
func validateMappingAcyclic(m map[string]string) error {
	for start := range m {
		visited := map[string]bool{start: true}
		current := start
		for {
			next, ok := m[current]
			if !ok || next == "" || next == current {
				break
			}
			if visited[next] {
				return fmt.Errorf("模型映射存在环（%s → %s），请修正对接关系", current, next)
			}
			visited[next] = true
			current = next
		}
	}
	return nil
}

// merchantOwnedChannel 校验渠道归属当前商家。
func merchantOwnedChannel(c *gin.Context, profile *model.MerchantProfile, channelId int) (*model.Channel, bool) {
	var channel model.Channel
	if err := model.DB.Where("id = ? AND merchant_id = ?", channelId, profile.UserId).
		First(&channel).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权操作该渠道"})
		return nil, false
	}
	return &channel, true
}

// ProbeMerchantChannel 商家探测自己渠道的上游：默认复用渠道已保存的站址与密钥，
// 请求体里给出新站址/密钥时按新值探测（编辑渠道时先校验再保存）。
func ProbeMerchantChannel(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	var req struct {
		ChannelId int    `json:"channel_id" binding:"required"`
		SiteUrl   string `json:"site_url"`
		SiteKey   string `json:"site_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	channel, ok := merchantOwnedChannel(c, profile, req.ChannelId)
	if !ok {
		return
	}
	siteUrl := strings.TrimSpace(req.SiteUrl)
	if siteUrl == "" {
		siteUrl = channel.GetBaseURL()
	}
	siteKey := strings.TrimSpace(req.SiteKey)
	if siteKey == "" {
		siteKey = channel.Key
	}
	if err := service.ValidateSSRFProtectedFetchURL(siteUrl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "站址不合法或不可访问（若为本地 DNS/CDN 伪 IP，请联系管理员在“系统设置 → 网络获取”放行）：" + err.Error(),
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	common.ApiSuccess(c, probeUpstreamWithPrices(ctx, siteUrl, siteKey, upstreamKindFromChannel(channel)))
}

// UpdateMerchantChannel 商家编辑自己的渠道：站点名称、站址、密钥、模型对接。
// 站址或密钥变更必须先通过上游探测（密钥要能读到模型清单），避免存下不可用的凭据；
// 模型对接变更会立即重建派生盘口（自动同步与手动锁定的报价不受影响）。
func UpdateMerchantChannel(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	var req struct {
		ChannelId int    `json:"channel_id" binding:"required"`
		SiteName  string `json:"site_name"`
		SiteUrl   string `json:"site_url"`
		SiteKey   string `json:"site_key"`
		// 指针区分「未传 models」与「显式提交空清单（清空全部模型）」
		Models *[]merchantModelQuote `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	channel, ok := merchantOwnedChannel(c, profile, req.ChannelId)
	if !ok {
		return
	}
	if req.Models != nil {
		for _, m := range *req.Models {
			name := strings.TrimSpace(m.Model)
			if name == "" {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型对接存在空条目"})
				return
			}
			// 与提交/审核入口同一套校验：逗号会被拆成多个模型，等于绕过
			// 逐模型审核；平台没定价的模型上架后客户请求会撞"价格未配置"。
			if strings.ContainsAny(name, ", \t\r\n") {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型名不能包含逗号或空白字符：" + name})
				return
			}
			if !isPlatformPriceableModel(name) {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型未在平台配置价格（固定价格/模型倍率/阶梯计费），请先在平台定价后再上架：" + name})
				return
			}
		}
	}

	siteName := strings.TrimSpace(req.SiteName)
	siteUrl := strings.TrimSpace(req.SiteUrl)
	siteKey := strings.TrimSpace(req.SiteKey)
	if siteUrl != "" {
		if err := service.ValidateSSRFProtectedFetchURL(siteUrl); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "站址不合法或不可访问（若为本地 DNS/CDN 伪 IP，请联系管理员在“系统设置 → 网络获取”放行）：" + err.Error(),
			})
			return
		}
	}
	urlChanged := siteUrl != "" && strings.TrimRight(siteUrl, "/") != strings.TrimRight(channel.GetBaseURL(), "/")
	keyChanged := siteKey != ""

	// 站址或密钥变更：先探测，密钥必须能读到模型清单，否则拒绝保存
	if urlChanged || keyChanged {
		probeURL := siteUrl
		if probeURL == "" {
			probeURL = channel.GetBaseURL()
		}
		probeKey := siteKey
		if probeKey == "" {
			probeKey = channel.Key
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
		defer cancel()
		probe := service.ProbeUpstream(ctx, probeURL, probeKey, upstreamKindFromChannel(channel))
		if !probe.ModelsEndpointOK {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "上游校验未通过：新站址/密钥无法读取模型清单，未保存修改。" + strings.Join(probe.Warnings, "；"),
			})
			return
		}
	}

	updates := map[string]any{}
	if siteName != "" && "商家:"+siteName != channel.Name {
		updates["name"] = "商家:" + siteName
	}
	if urlChanged {
		updates["base_url"] = siteUrl
	}
	if keyChanged {
		updates["key"] = siteKey
	}
	if req.Models != nil {
		newModels := *req.Models
		if len(newModels) > merchantChannelModelLimit {
			newModels = newModels[:merchantChannelModelLimit]
		}
		seen := make(map[string]bool)
		modelMapping := make(map[string]string)
		models := make([]string, 0, len(newModels))
		for _, q := range newModels {
			if q.Model == "" || seen[q.Model] {
				continue
			}
			seen[q.Model] = true
			models = append(models, q.Model)
			if q.Upstream != "" && q.Upstream != q.Model {
				modelMapping[q.Model] = q.Upstream
			}
		}
		if err := validateMappingAcyclic(modelMapping); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		updates["models"] = strings.Join(models, ",")
		if len(modelMapping) > 0 {
			if raw, mapErr := common.Marshal(modelMapping); mapErr == nil {
				mapping := string(raw)
				updates["model_mapping"] = &mapping
			} else {
				logger.LogWarn(c.Request.Context(), "merchant channel edit: marshal model mapping failed: "+mapErr.Error())
				updates["model_mapping"] = nil
			}
		} else {
			// 全部同名对接：清空映射
			updates["model_mapping"] = nil
		}
	}

	if len(updates) > 0 {
		// 渠道更新、路由索引重建、盘口重建必须在同一事务，
		// 避免探测通过但落库失败留下"models 已变、路由/盘口还是旧的"的半成品。
		txErr := model.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
				return err
			}
			var fresh model.Channel
			if err := tx.First(&fresh, "id = ?", channel.Id).Error; err != nil {
				return err
			}
			if err := fresh.UpdateAbilities(tx); err != nil {
				return fmt.Errorf("重建路由索引失败: %w", err)
			}
			if _, hasModels := updates["models"]; hasModels {
				if err := model.RebuildChannelQuotesWithTx(tx, &fresh); err != nil {
					return fmt.Errorf("重建渠道盘口失败: %w", err)
				}
			}
			return nil
		})
		if txErr != nil {
			common.ApiError(c, txErr)
			return
		}
		// 事务提交后刷新内存渠道缓存（未开缓存时为安全空操作）
		model.InitChannelCache()
	}
	// 商家资料里的站点信息保持一致
	profileUpdates := map[string]any{}
	if siteName != "" {
		profileUpdates["site_name"] = siteName
	}
	if urlChanged {
		profileUpdates["site_url"] = siteUrl
	}
	if len(profileUpdates) > 0 {
		profileUpdates["updated_at"] = time.Now().Unix()
		if err := model.DB.Model(&model.MerchantProfile{}).Where("user_id = ?", profile.UserId).
			Updates(profileUpdates).Error; err != nil {
			logger.LogWarn(c.Request.Context(), "merchant channel edit: update profile failed: "+err.Error())
		}
	}
	common.ApiSuccess(c, gin.H{
		"channel_id": channel.Id,
		"updated":    len(updates) > 0,
	})
}

// ---------- 商家门户（数据隔离：一律以当前用户ID过滤） ----------

// requireActiveMerchant 校验当前用户是有效商家，返回商家资料。
func requireActiveMerchant(c *gin.Context) (*model.MerchantProfile, bool) {
	userId := c.GetInt("id")
	profile, err := model.GetMerchantProfileByUserId(userId)
	if err != nil || profile == nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "商家资料不存在"})
		return nil, false
	}
	if profile.Status != model.MerchantStatusActive {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "商家账号已被停用"})
		return nil, false
	}
	return profile, true
}

// GetMerchantPortalSummary 商家门户总览：自己的调用量/成交额/成功率/钱包。
func GetMerchantPortalSummary(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	userId := profile.UserId
	channelIds, err := model.GetMerchantChannelIds(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	summary := gin.H{
		"profit_ratio":  profile.ProfitRatio,
		"site_name":     profile.SiteName,
		"site_url":      profile.SiteUrl,
		"channel_count": len(channelIds),
	}

	// 读接口只读钱包：钱包不存在等价于余额为 0，不得顺手建行。
	wallet, err := model.GetMerchantWalletByUserId(userId)
	if err != nil {
		wallet = &model.MerchantWallet{MerchantUserId: userId}
	}
	summary["wallet"] = wallet

	if len(channelIds) == 0 {
		summary["traffic_24h"] = gin.H{"calls": 0, "quota": 0, "fails": 0, "success_rate": 0}
		summary["groups_24h"] = []gin.H{}
		common.ApiSuccess(c, summary)
		return
	}

	dayAgo := time.Now().Add(-24 * time.Hour).Unix()
	type trafficRow struct {
		Calls int64 `json:"calls"`
		Quota int64 `json:"quota"`
		Fails int64 `json:"fails"`
	}
	var row trafficRow
	err = model.LOG_DB.Model(&model.Log{}).
		Select("SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS calls, "+
			"SUM(CASE WHEN type = ? THEN quota ELSE 0 END) AS quota, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS fails",
			model.LogTypeConsume, model.LogTypeConsume, model.LogTypeError).
		Where("channel_id IN ? AND created_at > ?", channelIds, dayAgo).
		Scan(&row).Error
	if err != nil {
		// 行情数据拿不到不应阻塞门户
		logger.LogWarn(c.Request.Context(), "merchant summary traffic failed: "+err.Error())
	}
	successRate := 0.0
	if row.Calls+row.Fails > 0 {
		successRate = float64(row.Calls) / float64(row.Calls+row.Fails) * 100
	}
	summary["traffic_24h"] = gin.H{
		"calls":        row.Calls,
		"quota":        row.Quota,
		"quota_usd":    float64(row.Quota) / common.QuotaPerUnit,
		"fails":        row.Fails,
		"success_rate": successRate,
	}

	// 按分组拆分近24h调用量：商家需要看到自己在每个号池的调用与成交情况。
	groups := make([]gin.H, 0)
	if rows, groupErr := model.GetMerchantChannelTrafficByGroup(channelIds, dayAgo); groupErr != nil {
		logger.LogWarn(c.Request.Context(), "merchant summary group traffic failed: "+groupErr.Error())
	} else {
		for _, r := range rows {
			groupRate := 0.0
			if r.Calls+r.Fails > 0 {
				groupRate = float64(r.Calls) / float64(r.Calls+r.Fails) * 100
			}
			groups = append(groups, gin.H{
				"group":        r.Group,
				"calls":        r.Calls,
				"quota":        r.Quota,
				"quota_usd":    float64(r.Quota) / common.QuotaPerUnit,
				"fails":        r.Fails,
				"success_rate": groupRate,
			})
		}
	}
	summary["groups_24h"] = groups

	// 平台按渠道累计的成交额（口径与渠道列表一致）
	var totalQuota int64
	model.DB.Model(&model.Channel{}).Where("id IN ?", channelIds).
		Select("COALESCE(SUM(used_quota),0)").Scan(&totalQuota)
	summary["total_quota_usd"] = float64(totalQuota) / common.QuotaPerUnit

	common.ApiSuccess(c, summary)
}

// MerchantPoolChannel 商家在某号池下的一个"池子"（渠道）及其健康度。
type MerchantPoolChannel struct {
	ChannelId      int      `json:"channel_id"`
	Name           string   `json:"name"`
	Status         int      `json:"status"`
	RateMultiplier float64  `json:"rate_multiplier"` // 渠道倍率（相对平台标准价的折扣系数）
	Models         []string `json:"models"`          // 该渠道上架的模型（平台官方名）
	BaseUrl        string   `json:"base_url"`        // 上游站址（商家自己的，可回显）
	// 官方名 → 上游名的对接映射（JSON 字符串；为空表示全部同名）
	ModelMapping string `json:"model_mapping,omitempty"`
	PriceSync    bool   `json:"price_sync"` // 是否开启了上游报价自动同步
	// 最近一次同步到上游真实报价的时间（auto 报价的最大更新时间；0 = 从未同步）
	PriceSyncedAt int64   `json:"price_synced_at"`
	Calls24h      int64   `json:"calls_24h"`
	Fails24h      int64   `json:"fails_24h"`
	QuotaUsd24h   float64 `json:"quota_usd_24h"`
	SuccessRate   float64 `json:"success_rate"` // 0..1
	AvgUseTime    float64 `json:"avg_use_time"` // 平均耗时（秒）
}

// MerchantPoolGroup 一个号池分组及其下商家的全部池子。
type MerchantPoolGroup struct {
	Pool        string                `json:"pool"`
	Channels    []MerchantPoolChannel `json:"channels"`
	Calls24h    int64                 `json:"calls_24h"`
	QuotaUsd24h float64               `json:"quota_usd_24h"`
	SuccessRate float64               `json:"success_rate"`
}

// GetMerchantPortalPools 商家门户的分组/池子视图。
// 返回平台全部可对接分组 + 商家在每个分组下已接入的池子（含倍率/耗时/健康度）。
func GetMerchantPortalPools(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	// 平台预设分组：商家需要看到所有可对接的分组，哪怕自己还没接入
	allPools := make([]string, 0)
	for name := range ratio_setting.GetGroupRatioCopy() {
		allPools = append(allPools, name)
	}
	sort.Strings(allPools)

	channelIds, err := model.GetMerchantChannelIds(profile.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups := make([]MerchantPoolGroup, 0)
	if len(channelIds) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{"all_pools": allPools, "groups": groups},
		})
		return
	}

	var channels []model.Channel
	if err := model.DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	// 近24h健康度：按渠道聚合成功/失败/耗时
	dayAgo := time.Now().Add(-24 * time.Hour).Unix()
	healthById := make(map[int]model.MerchantChannelHealth)
	if rows, healthErr := model.GetMerchantChannelHealth(channelIds, dayAgo); healthErr != nil {
		logger.LogWarn(c.Request.Context(), "merchant pools health failed: "+healthErr.Error())
	} else {
		for _, h := range rows {
			healthById[h.ChannelId] = h
		}
	}

	// 按渠道取最近一次自动同步报价的时间（auto 报价的最大更新时间）
	syncedAtBy := make(map[int]int64)
	var syncRows []struct {
		ChannelId int   `json:"channel_id"`
		LastSync  int64 `json:"last_sync"`
	}
	if err := model.DB.Model(&model.ChannelQuote{}).
		Select("channel_id, MAX(updated_at) AS last_sync").
		Where("channel_id IN ? AND source = ?", channelIds, model.QuoteSourceAuto).
		Group("channel_id").
		Scan(&syncRows).Error; err == nil {
		for _, r := range syncRows {
			syncedAtBy[r.ChannelId] = r.LastSync
		}
	}

	// 按分组归拢池子：一个渠道可同时属于多个分组
	index := make(map[string]int)
	for _, ch := range channels {
		h := healthById[ch.Id]
		item := MerchantPoolChannel{
			ChannelId:      ch.Id,
			Name:           ch.Name,
			Status:         ch.Status,
			RateMultiplier: ch.GetRateMultiplier(),
			Models:         splitModelList(ch.Models),
			BaseUrl:        ch.GetBaseURL(),
			ModelMapping:   ch.GetModelMapping(),
			PriceSync:      ch.GetPriceSync(),
			PriceSyncedAt:  syncedAtBy[ch.Id],
			Calls24h:       h.Calls,
			Fails24h:       h.Fails,
			QuotaUsd24h:    float64(h.Quota) / common.QuotaPerUnit,
			SuccessRate:    h.SuccessRate(),
			AvgUseTime:     h.AvgUseTime,
		}
		for _, pool := range splitModelList(ch.Group) {
			pos, exists := index[pool]
			if !exists {
				groups = append(groups, MerchantPoolGroup{Pool: pool, Channels: []MerchantPoolChannel{}})
				pos = len(groups) - 1
				index[pool] = pos
			}
			groups[pos].Channels = append(groups[pos].Channels, item)
			groups[pos].Calls24h += h.Calls
			groups[pos].QuotaUsd24h += item.QuotaUsd24h
		}
	}
	for i := range groups {
		total := groups[i].Calls24h
		for _, ch := range groups[i].Channels {
			total += ch.Fails24h
		}
		if total > 0 {
			groups[i].SuccessRate = float64(groups[i].Calls24h) / float64(total)
		}
	}

	common.ApiSuccess(c, gin.H{"all_pools": allPools, "groups": groups})
}

// splitModelList 拆分渠道的逗号分隔清单（分组名/模型名共用）。
func splitModelList(raw string) []string {
	items := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

// GetMerchantPortalEarnings 商家门户分润流水（仅自己）。
// 除分页明细外，还返回筛选内统计、今日/近7天/近30天/累计汇总与近30天每日趋势，
// 供门户渲染汇总卡片与趋势图；模型关键词筛选与调用记录同一口径。
func GetMerchantPortalEarnings(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	modelName := strings.TrimSpace(c.Query("model_name"))
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	query := model.DB.Model(&model.MerchantEarning{}).Where("merchant_user_id = ?", profile.UserId)
	if modelName != "" {
		query = query.Where("model_name LIKE ?", "%"+modelName+"%")
	}
	if startTs > 0 {
		query = query.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		query = query.Where("created_at <= ?", endTs)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var earnings []model.MerchantEarning
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&earnings).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// 本页合计：翻页时每页底部能直接看到这一页赚了多少
	var pageAmount float64
	for _, e := range earnings {
		pageAmount += e.Amount
	}

	// 汇总卡片：今日 / 近7天 / 近30天 / 累计（不受表格筛选影响）
	now := time.Now()
	_, zoneOffset := now.Zone()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	summary := summarizeMerchantEarnings(profile.UserId, gin.H{
		"today":    todayStart,
		"last_7d":  now.Add(-7 * 24 * time.Hour).Unix(),
		"last_30d": now.Add(-30 * 24 * time.Hour).Unix(),
	})

	// 近30天每日趋势：按本地日界聚合（created_at + 时区偏移) / 86400）
	daily := merchantDailyEarnings(profile.UserId, now.Add(-29*24*time.Hour).Unix(), zoneOffset)

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(earnings)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"page":        pageInfo.GetPage(),
			"page_size":   pageInfo.GetPageSize(),
			"total":       total,
			"items":       earnings,
			"page_amount": pageAmount,
			"summary":     summary,
			"daily":       daily,
		},
	})
}

// summarizeMerchantEarnings 按 windows 给出的时间下限聚合各时间窗的分润合计。
func summarizeMerchantEarnings(userId int, windows gin.H) gin.H {
	out := gin.H{}
	for name, from := range windows {
		var sum float64
		model.DB.Model(&model.MerchantEarning{}).
			Select("COALESCE(SUM(amount),0)").
			Where("merchant_user_id = ? AND created_at >= ?", userId, from).
			Scan(&sum)
		out[name] = sum
	}
	var total float64
	model.DB.Model(&model.MerchantEarning{}).
		Select("COALESCE(SUM(amount),0)").
		Where("merchant_user_id = ?", userId).
		Scan(&total)
	out["total"] = total
	return out
}

// merchantDailyEarnings 返回 [from, now] 内按本地日聚合的分润序列，缺失的天补 0。
func merchantDailyEarnings(userId int, from int64, zoneOffset int) []gin.H {
	type dayRow struct {
		DayIdx int64   `json:"day_idx"`
		Amount float64 `json:"amount"`
	}
	var rows []dayRow
	model.DB.Model(&model.MerchantEarning{}).
		Select("((created_at + ?) / 86400) AS day_idx, COALESCE(SUM(amount),0) AS amount", zoneOffset).
		Where("merchant_user_id = ? AND created_at >= ?", userId, from).
		Group("day_idx").Order("day_idx ASC").Scan(&rows)

	amountByDay := make(map[int64]float64, len(rows))
	for _, r := range rows {
		amountByDay[r.DayIdx] = r.Amount
	}
	startDay := (from + int64(zoneOffset)) / 86400
	endDay := (time.Now().Unix() + int64(zoneOffset)) / 86400
	daily := make([]gin.H, 0, endDay-startDay+1)
	for day := startDay; day <= endDay; day++ {
		// 桶起点换回本地日零点的 unix 时间，前端按天格式化
		daily = append(daily, gin.H{
			"day":    day*86400 - int64(zoneOffset),
			"amount": amountByDay[day],
		})
	}
	return daily
}

// normalizeRateMultiplier 归一化渠道倍率：<=0 视为未填写，按 1.0（原价）处理。
func normalizeRateMultiplier(v float64) float64 {
	if v <= 0 {
		return 1.0
	}
	return v
}

// GetMerchantPortalQuotes 商家门户盘口列表（自己渠道的派生盘口）。
// 每条记录同时带出该渠道的倍率与有效倍率，便于商家理解"自己挂了多少"。
func GetMerchantPortalQuotes(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	channelIds, err := model.GetMerchantChannelIds(profile.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var quotes []model.ChannelQuote
	if len(channelIds) == 0 {
		quotes = []model.ChannelQuote{}
	} else if err := model.DB.Where("channel_id IN ?", channelIds).
		Order("pool_group ASC, model_name ASC").Find(&quotes).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// 带出渠道名与渠道倍率，前端可直接展示"渠道 × 倍率 × 盘口价"
	var channels []model.Channel
	if len(channelIds) > 0 {
		if err := model.DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}
	type portalQuote struct {
		model.ChannelQuote
		ChannelName       string  `json:"channel_name"`
		ChannelRate       float64 `json:"channel_rate_multiplier"`
		PlatformModelRate float64 `json:"platform_model_ratio"`
	}
	channelById := make(map[int]model.Channel, len(channels))
	for _, ch := range channels {
		channelById[ch.Id] = ch
	}
	items := make([]portalQuote, 0, len(quotes))
	for _, q := range quotes {
		ch := channelById[q.ChannelId]
		platformRatio := 0.0
		if ch.GetRateMultiplier() > 0 {
			platformRatio = q.RateMultiplier / ch.GetRateMultiplier()
		}
		items = append(items, portalQuote{
			ChannelQuote:      q,
			ChannelName:       ch.Name,
			ChannelRate:       ch.GetRateMultiplier(),
			PlatformModelRate: platformRatio,
		})
	}
	common.ApiSuccess(c, items)
}

// UpdateMerchantPortalRate 商家调整自己渠道的倍率。
// 倍率是渠道唯一的定价输入（相对平台标准价的折扣系数），
// 修改后立即重算该渠道的派生盘口，行情与智能路由随之更新。
func UpdateMerchantPortalRate(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	var req struct {
		ChannelId      int     `json:"channel_id" binding:"required"`
		RateMultiplier float64 `json:"rate_multiplier" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if req.RateMultiplier <= 0 || req.RateMultiplier > merchantRateMultiplierLimit {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道倍率需在 0（不含）到 100 之间"})
		return
	}
	// 数据隔离：只能改自己名下的渠道
	var channel model.Channel
	if err := model.DB.Where("id = ? AND merchant_id = ?", req.ChannelId, profile.UserId).
		First(&channel).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权操作该渠道"})
		return
	}
	if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).
		Update("rate_multiplier", req.RateMultiplier).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	channel.RateMultiplier = req.RateMultiplier
	if err := model.RebuildChannelQuotes(channel.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	// 渠道倍率已接入计费：刷新内存渠道缓存，否则下一笔请求仍按旧倍率扣费。
	if common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	common.ApiSuccess(c, gin.H{
		"channel_id":      channel.Id,
		"rate_multiplier": req.RateMultiplier,
	})
}

// SyncMerchantPortalQuotes 商家对自己名下的渠道触发一轮报价同步。
// 不传 channel_ids 时同步名下全部渠道；传入时逐个校验归属，只同步自己的渠道。
// 平台每隔 PriceSyncMinutes 会自动同步一次，这里是商家改完上游价格后的即时刷新入口。
func SyncMerchantPortalQuotes(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	var req struct {
		ChannelIds []int `json:"channel_ids"`
	}
	_ = c.ShouldBindJSON(&req)

	query := model.DB.Where("merchant_id = ?", profile.UserId)
	if len(req.ChannelIds) > 0 {
		query = query.Where("id IN ?", req.ChannelIds)
	}
	var channels []*model.Channel
	if err := query.Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if len(channels) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权操作该渠道"})
		return
	}
	summary := runPriceSyncForChannels(c.Request.Context(), channels)
	common.ApiSuccess(c, summary)
}

// GetMerchantPortalLogs 商家门户调用记录：只返回自己渠道的日志，并做脱敏处理。
// 支持按分组、模型、类型、时间窗口过滤；分页参数复用统一的 p/page_size。
func GetMerchantPortalLogs(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	channelIds, err := model.GetMerchantChannelIds(profile.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	params := model.MerchantLogQueryParams{
		ChannelIds: channelIds,
		ModelName:  strings.TrimSpace(c.Query("model_name")),
		Group:      strings.TrimSpace(c.Query("group")),
		StartIdx:   pageInfo.GetStartIdx(),
		Num:        pageInfo.GetPageSize(),
	}
	if logType, convErr := strconv.Atoi(c.Query("type")); convErr == nil {
		params.LogType = logType
	}
	if ts, convErr := strconv.ParseInt(c.Query("start_timestamp"), 10, 64); convErr == nil {
		params.StartTimestamp = ts
	}
	if ts, convErr := strconv.ParseInt(c.Query("end_timestamp"), 10, 64); convErr == nil {
		params.EndTimestamp = ts
	}

	logs, total, err := model.GetMerchantChannelLogs(params)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 脱敏：剔除用户身份/令牌/IP/原始元数据，商家只能看到运营层面的调用信息。
	model.FormatMerchantLogs(logs)
	// 与表格同一筛选口径的统计：调用/失败/消耗/Token，供前端渲染汇总条。
	stats, err := model.GetMerchantChannelLogStats(params)
	if err != nil {
		logger.LogWarn(c.Request.Context(), "merchant log stats failed: "+err.Error())
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"page":      pageInfo.GetPage(),
			"page_size": pageInfo.GetPageSize(),
			"total":     total,
			"items":     logs,
			"stats":     stats,
		},
	})
}

// PostMerchantWithdrawal 商家发起提现：冻结余额并建单。
func PostMerchantWithdrawal(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	var req struct {
		Amount      float64 `json:"amount" binding:"required"`
		AccountInfo string  `json:"account_info" binding:"required"`
		Note        string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误：金额与收款方式必填"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "提现金额必须大于 0"})
		return
	}
	withdrawal := &model.MerchantWithdrawal{
		MerchantUserId: profile.UserId,
		Amount:         req.Amount,
		AccountInfo:    req.AccountInfo,
		Note:           req.Note,
		Status:         model.WithdrawalPending,
		CreatedAt:      time.Now().Unix(),
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.FreezeForWithdrawal(tx, profile.UserId, req.Amount); err != nil {
			return err
		}
		return tx.Create(withdrawal).Error
	})
	if err == model.ErrInsufficientBalance {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "钱包余额不足"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

// GetMerchantWithdrawals 商家自己的提现记录。
func GetMerchantWithdrawals(c *gin.Context) {
	profile, ok := requireActiveMerchant(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	query := model.DB.Model(&model.MerchantWithdrawal{}).Where("merchant_user_id = ?", profile.UserId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var withdrawals []model.MerchantWithdrawal
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&withdrawals).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(withdrawals)
	common.ApiSuccess(c, pageInfo)
}

// ---------- 管理端 ----------

// GetMerchantApplications 管理员查看入驻申请列表。
func GetMerchantApplications(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := model.DB.Model(&model.MerchantApplication{})
	if status, err := strconv.Atoi(c.Query("status")); err == nil && status >= 0 {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var apps []model.MerchantApplication
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&apps).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// 密钥脱敏：只回显长度提示
	for i := range apps {
		if apps[i].SiteKey != "" {
			apps[i].SiteKey = fmt.Sprintf("(已保存，长度 %d)", len(apps[i].SiteKey))
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(apps)
	common.ApiSuccess(c, pageInfo)
}

// isPlatformPriceableModel 判断模型是否平台可计费：固定价格、模型倍率或阶梯计费
// 配置任一存在即可。商家渠道价格由「平台标准倍率 × 渠道倍率」派生，平台无价的
// 模型进入渠道后客户请求会直接撞上"价格未配置"。
// 注意倍率检查必须用 HasConfiguredModelRatio：GetModelRatio 的 ok 位在自用模式下
// 对任意模型都返回 true（37.5 兜底），用它会把校验完全架空。
func isPlatformPriceableModel(name string) bool {
	if _, ok := ratio_setting.GetModelPrice(name, false); ok {
		return true
	}
	if ratio_setting.HasConfiguredModelRatio(name) {
		return true
	}
	if billing_setting.GetBillingMode(name) != billing_setting.BillingModeTieredExpr {
		return false
	}
	// 阶梯计费还必须有非空表达式，否则请求期会在 modelPriceHelperTiered 报错
	if expr, ok := billing_setting.GetBillingExpr(name); ok {
		return strings.TrimSpace(expr) != ""
	}
	return false
}

// ReviewMerchantApplication 管理员审核入驻申请。
// approve：创建商家渠道 + 写入手填报价 + 建资料/钱包 + 用户升级为商家角色。
func ReviewMerchantApplication(c *gin.Context) {
	adminId := c.GetInt("id")
	appId, err := strconv.Atoi(c.Param("id"))
	if err != nil || appId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	var req struct {
		Action      string   `json:"action" binding:"required"` // approve | reject
		ReviewNote  string   `json:"review_note"`
		Pools       []string `json:"pools"` // approve 时渠道加入的号池
		ProfitRatio *float64 `json:"profit_ratio"`
		Priority    *int64   `json:"priority"`
		Weight      *uint    `json:"weight"`
		// RateMultiplier 审核时可覆盖商家申请的渠道倍率（相对平台标准价的折扣系数）
		RateMultiplier *float64 `json:"rate_multiplier"`
		// 上游类型可在审核时覆盖申请声明；channel_type 直接指定渠道类型
		// （1=OpenAI 兼容 / 59=Sub2API / 60=New API），price_sync 覆盖报价同步开关。
		UpstreamKind string `json:"upstream_kind"`
		ChannelType  *int   `json:"channel_type"`
		PriceSync    *bool  `json:"price_sync"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}

	approve := req.Action == "approve"

	var preview model.MerchantApplication
	if err := model.DB.First(&preview, appId).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "申请不存在"})
		return
	}
	if preview.Status != model.MerchantApplicationPending {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该申请已处理"})
		return
	}

	// 默认分成比例：商家拿 95%、平台抽 5%；管理员可在审核时覆盖。
	profitRatio := 0.95
	if req.ProfitRatio != nil && *req.ProfitRatio > 0 && *req.ProfitRatio <= 1 {
		profitRatio = *req.ProfitRatio
	}

	// 渠道倍率：优先用管理员审核时填的值，否则沿用商家申请时声明的值。
	// 这是渠道唯一的定价输入，盘口由「平台标准倍率 × 本倍率」派生。
	// 上界与商家门户保持一致；申请若在提交校验修复前落库了超限倍率，
	// 审核时也必须拦截，不能把天价盘口放进渠道。
	rateMultiplier := normalizeRateMultiplier(preview.RateMultiplier)
	if req.RateMultiplier != nil {
		if *req.RateMultiplier < 0 || *req.RateMultiplier > merchantRateMultiplierLimit {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道倍率不能为负数，且不能超过 100"})
			return
		}
		if *req.RateMultiplier > 0 {
			rateMultiplier = *req.RateMultiplier
		}
	}
	if approve && rateMultiplier > merchantRateMultiplierLimit {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该申请声明的渠道倍率超过 100，请修正后再通过审核"})
		return
	}

	// 通过前的校验与探测放在事务之外：号池、站址（SSRF）、上游类型与模型清单。
	var quotes []merchantModelQuote
	models := make([]string, 0)
	var probe *service.UpstreamProbe
	channelType := constant.ChannelTypeOpenAI
	priceSyncEnabled := false
	var modelMappingJSON *string
	if approve {
		if len(req.Pools) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "审核通过时必须勾选至少一个号池"})
			return
		}
		// 号池即渠道分组：不存在的分组会让渠道挂进"死池"，没有用户组能匹配到它。
		knownPools := ratio_setting.GetGroupRatioCopy()
		for _, pool := range req.Pools {
			if _, ok := knownPools[pool]; !ok {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "号池（分组）不存在：" + pool})
				return
			}
		}
		if err := service.ValidateSSRFProtectedFetchURL(preview.SiteUrl); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "站址不合法或不可访问（若为本地 DNS/CDN 伪 IP，请在“系统设置 → 网络获取”放行该域名或网段）：" + err.Error(),
			})
			return
		}
		if err := common.UnmarshalJsonStr(preview.Models, &quotes); err != nil {
			quotes = nil // Sub2API 一类不提供报价时允许为空
		}
		declared := service.ParseUpstreamKind(req.UpstreamKind)
		if declared == service.UpstreamKindAuto {
			declared = service.ParseUpstreamKind(preview.UpstreamKind)
		}
		probe = service.ProbeUpstream(c.Request.Context(), preview.SiteUrl, preview.SiteKey, declared)
		channelType = probe.ChannelType
		if req.ChannelType != nil {
			switch *req.ChannelType {
			case constant.ChannelTypeOpenAI, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
				channelType = *req.ChannelType
			default:
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "不支持的渠道类型"})
				return
			}
		}
		// 官方模型名去重后写入渠道；商家对接的"官方名 → 上游名"生成 model_mapping，
		// 名称一致的条目不需要映射。
		seenModels := make(map[string]bool)
		modelMapping := make(map[string]string)
		for _, q := range quotes {
			if q.Model == "" || seenModels[q.Model] {
				continue
			}
			seenModels[q.Model] = true
			models = append(models, q.Model)
			if q.Upstream != "" && q.Upstream != q.Model {
				modelMapping[q.Model] = q.Upstream
			}
		}
		if len(models) == 0 {
			// Sub2API 等无价格接口的网关：用商家密钥探测出的模型清单作为渠道模型。
			models = probe.Models
		}
		if len(models) > merchantChannelModelLimit {
			models = models[:merchantChannelModelLimit]
		}
		if len(models) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "无法确定渠道模型：请填写至少一个模型报价，或确认密钥可读取 /v1/models",
			})
			return
		}
		// 渠道模型必须是平台可计费的（固定价格/模型倍率/阶梯计费任一），
		// 否则路由选中后客户请求会撞上"价格未配置"。逗号同样在此兜底拦截。
		for _, name := range models {
			if strings.ContainsAny(name, ", \t\r\n") {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"message": "模型名不能包含逗号或空白字符（会被拆成多个模型）：" + name,
				})
				return
			}
			if !isPlatformPriceableModel(name) {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"message": "模型未在平台配置价格（固定价格/模型倍率/阶梯计费），请先在平台定价后再通过审核：" + name,
				})
				return
			}
		}
		if len(modelMapping) > 0 {
			if err := validateMappingAcyclic(modelMapping); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
				return
			}
			if raw, mapErr := common.Marshal(modelMapping); mapErr == nil {
				mapping := string(raw)
				modelMappingJSON = &mapping
			} else {
				logger.LogWarn(c.Request.Context(), "merchant review: marshal model mapping failed: "+mapErr.Error())
			}
		}
		// 只有具备可自动读取价格接口的上游才默认开启自动报价同步：
		// New API 需落到 New API 渠道类型，OpenAI /v1/models 报价协议走 OpenAI 兼容中继。
		priceSyncEnabled = probe.PriceSyncCapable &&
			(channelType == constant.ChannelTypeNewAPI ||
				probe.PriceSyncProtocol == string(service.UpstreamKindOpenAIModelsPricing))
		if req.PriceSync != nil {
			priceSyncEnabled = *req.PriceSync
		}
	}

	// 分组归属天然对齐：盘口不再由商家填写，而是直接从渠道的 Group 字段派生，
	// 因此"显示的盘口"与"计费的分组"必然一致，不再需要人工对齐。

	// 审核与派生数据（渠道/盘口/资料/钱包/角色）必须同一事务，避免半成品商家。
	var channelId int
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		app, err := model.GetMerchantApplicationForUpdate(tx, appId)
		if err != nil {
			return err
		}
		if app.Status != model.MerchantApplicationPending {
			return model.ErrMerchantApplicationHandled
		}
		app.ReviewNote = req.ReviewNote
		app.ReviewedBy = adminId
		app.ReviewedAt = time.Now().Unix()
		if !approve {
			app.Status = model.MerchantApplicationRejected
			app.SiteKey = "" // 审核结束即抹除上游密钥，避免长期留存
			return tx.Save(app).Error
		}
		app.Status = model.MerchantApplicationApproved
		if probe != nil {
			app.UpstreamKind = string(probe.Kind)
		}

		priceSync := priceSyncEnabled
		autoRouting := true
		channel := &model.Channel{
			Type:              channelType,
			Key:               app.SiteKey,
			Name:              "商家:" + app.SiteName,
			Status:            common.ChannelStatusEnabled,
			CreatedTime:       time.Now().Unix(),
			BaseURL:           &app.SiteUrl,
			Models:            strings.Join(models, ","),
			Group:             strings.Join(req.Pools, ","),
			MerchantId:        app.UserId,
			PriceSync:         &priceSync,
			PriceSyncProtocol: probe.PriceSyncProtocol,
			AutoRouting:       &autoRouting,
			RateMultiplier:    rateMultiplier,
			Priority:          req.Priority,
			Weight:            req.Weight,
			ModelMapping:      modelMappingJSON,
		}
		if err := channel.InsertWithTx(tx); err != nil {
			return fmt.Errorf("创建渠道失败: %w", err)
		}
		channelId = channel.Id

		// 盘口由「平台标准倍率 × 渠道倍率」派生，商家不填价格。
		if err := model.RebuildChannelQuotesWithTx(tx, channel); err != nil {
			return fmt.Errorf("派生渠道盘口失败: %w", err)
		}

		now := time.Now().Unix()

		// 扩容申请：商家资料已存在时复用，不重复创建（唯一索引会冲突）
		if existing, profileErr := model.GetMerchantProfileByUserId(app.UserId); profileErr == nil && existing != nil {
			if err := tx.Model(&model.MerchantProfile{}).Where("id = ?", existing.Id).
				Update("profit_ratio", profitRatio).Error; err != nil {
				return fmt.Errorf("更新商家分成比例失败: %w", err)
			}
		} else {
			profile := &model.MerchantProfile{
				UserId:      app.UserId,
				SiteName:    app.SiteName,
				SiteUrl:     app.SiteUrl,
				ProfitRatio: profitRatio,
				Status:      model.MerchantStatusActive,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := tx.Create(profile).Error; err != nil {
				return fmt.Errorf("创建商家资料失败: %w", err)
			}
		}
		if _, err := model.EnsureMerchantWallet(tx, app.UserId); err != nil {
			return fmt.Errorf("创建商家钱包失败: %w", err)
		}
		if err := tx.Model(&model.User{}).Where("id = ? AND role < ?", app.UserId, common.RoleAdminUser).
			Update("role", common.RoleMerchant).Error; err != nil {
			return fmt.Errorf("升级商家角色失败: %w", err)
		}
		// 密钥已复制进渠道，申请单不再保留明文。
		app.SiteKey = ""
		return tx.Save(app).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "申请不存在"})
		return
	case errors.Is(err, model.ErrMerchantApplicationHandled):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该申请已处理"})
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	// 新商家渠道 + abilities 刚落库，立即刷新内存渠道缓存保证可路由
	model.InitChannelCache()
	common.ApiSuccess(c, gin.H{
		"channel_id":    channelId,
		"channel_type":  channelType,
		"price_sync":    priceSyncEnabled,
		"upstream_kind": appUpstreamKind(probe),
		"probe":         probe,
	})
}

// appUpstreamKind 返回探测出的最终上游类型（无探测时为空）。
func appUpstreamKind(probe *service.UpstreamProbe) string {
	if probe == nil {
		return ""
	}
	return string(probe.Kind)
}

// ProbeMerchantApplication 管理员对申请做一次上游探测，不改变申请状态。
func ProbeMerchantApplication(c *gin.Context) {
	appId, err := strconv.Atoi(c.Param("id"))
	if err != nil || appId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	var app model.MerchantApplication
	if err := model.DB.First(&app, appId).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "申请不存在"})
		return
	}
	if strings.TrimSpace(app.SiteKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该申请的密钥已清除，无法探测"})
		return
	}
	probe := service.ProbeUpstream(c.Request.Context(), app.SiteUrl, app.SiteKey,
		service.ParseUpstreamKind(app.UpstreamKind))
	common.ApiSuccess(c, probe)
}

// GetMerchants 管理员查看商家列表（含钱包）。
func GetMerchants(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	var total int64
	model.DB.Model(&model.MerchantProfile{}).Count(&total)
	var profiles []model.MerchantProfile
	if err := model.DB.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&profiles).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type merchantRow struct {
		model.MerchantProfile
		Wallet     *model.MerchantWallet `json:"wallet"`
		ChannelIds []int                 `json:"channel_ids"`
		Username   string                `json:"username"`
	}
	rows := make([]merchantRow, 0, len(profiles))
	for _, p := range profiles {
		row := merchantRow{MerchantProfile: p}
		if wallet, err := model.GetMerchantWalletByUserId(p.UserId); err == nil {
			row.Wallet = wallet
		}
		if ids, err := model.GetMerchantChannelIds(p.UserId); err == nil {
			row.ChannelIds = ids
		}
		if name, err := model.GetUsernameById(p.UserId, false); err == nil {
			row.Username = name
		}
		rows = append(rows, row)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// GetMerchantOverview 管理员总览：全部商家（含渠道与分组归属）、全部分组及商家索引。
// 一次请求返回「商家 → 渠道」与「分组 → 商家」两份视图数据，供管理页总览共用。
func GetMerchantOverview(c *gin.Context) {
	var profiles []model.MerchantProfile
	if err := model.DB.Order("user_id ASC").Find(&profiles).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type channelBrief struct {
		Id         int      `json:"id"`
		Name       string   `json:"name"`
		Pools      []string `json:"pools"`
		ModelCount int      `json:"model_count"`
		Status     int      `json:"status"`
		PriceSync  bool     `json:"price_sync"`
	}
	type merchantBrief struct {
		UserId      int            `json:"user_id"`
		Username    string         `json:"username"`
		SiteName    string         `json:"site_name"`
		SiteUrl     string         `json:"site_url"`
		Status      int            `json:"status"`
		ProfitRatio float64        `json:"profit_ratio"`
		Balance     float64        `json:"balance"`
		Channels    []channelBrief `json:"channels"`
	}
	type poolEntry struct {
		ChannelCount  int      `json:"channel_count"`
		MerchantCount int      `json:"merchant_count"`
		MerchantNames []string `json:"merchant_names"`
		seenMerchants map[int]bool
	}
	merchants := make([]merchantBrief, 0, len(profiles))
	poolIndex := make(map[string]*poolEntry)
	poolEntryFor := func(pool string) *poolEntry {
		entry, ok := poolIndex[pool]
		if !ok {
			entry = &poolEntry{MerchantNames: []string{}, seenMerchants: map[int]bool{}}
			poolIndex[pool] = entry
		}
		return entry
	}
	for _, p := range profiles {
		mb := merchantBrief{
			UserId:      p.UserId,
			SiteName:    p.SiteName,
			SiteUrl:     p.SiteUrl,
			Status:      p.Status,
			ProfitRatio: p.ProfitRatio,
		}
		if wallet, err := model.GetMerchantWalletByUserId(p.UserId); err == nil && wallet != nil {
			mb.Balance = wallet.Balance
		}
		if name, err := model.GetUsernameById(p.UserId, false); err == nil {
			mb.Username = name
		}
		if ids, err := model.GetMerchantChannelIds(p.UserId); err == nil && len(ids) > 0 {
			var channels []model.Channel
			if err := model.DB.Where("id IN ?", ids).Find(&channels).Error; err == nil {
				for _, ch := range channels {
					pools := splitModelList(ch.Group)
					mb.Channels = append(mb.Channels, channelBrief{
						Id:         ch.Id,
						Name:       ch.Name,
						Pools:      pools,
						ModelCount: len(splitModelList(ch.Models)),
						Status:     ch.Status,
						PriceSync:  ch.GetPriceSync(),
					})
					for _, pool := range pools {
						entry := poolEntryFor(pool)
						entry.ChannelCount++
						if !entry.seenMerchants[p.UserId] {
							entry.seenMerchants[p.UserId] = true
							entry.MerchantCount++
							name := mb.Username
							if name == "" {
								name = mb.SiteName
							}
							entry.MerchantNames = append(entry.MerchantNames, name)
						}
					}
				}
			}
		}
		merchants = append(merchants, mb)
	}
	// 平台预设分组（还没有商家接入的也展示，方便管理员看到空分组）
	for pool := range ratio_setting.GetGroupRatioCopy() {
		poolEntryFor(pool)
	}
	pools := make([]gin.H, 0, len(poolIndex))
	for name, entry := range poolIndex {
		pools = append(pools, gin.H{
			"pool":           name,
			"merchant_count": entry.MerchantCount,
			"channel_count":  entry.ChannelCount,
			"merchant_names": entry.MerchantNames,
		})
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i]["pool"].(string) < pools[j]["pool"].(string) })
	common.ApiSuccess(c, gin.H{"merchants": merchants, "pools": pools})
}

// UpdateMerchant 管理员调整商家分成比例或停用/启用。
func UpdateMerchant(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	var req struct {
		ProfitRatio *float64 `json:"profit_ratio"`
		Status      *int     `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if req.Status != nil &&
		*req.Status != model.MerchantStatusActive && *req.Status != model.MerchantStatusSuspended {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "未知的商家状态"})
		return
	}
	updates := map[string]any{"updated_at": time.Now().Unix()}
	if req.ProfitRatio != nil {
		if *req.ProfitRatio < 0 || *req.ProfitRatio > 1 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分成比例需在 0~1 之间"})
			return
		}
		updates["profit_ratio"] = *req.ProfitRatio
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	// 停用商家必须联动停用其名下渠道：分润按 profile 状态即时停止，
	// 渠道若继续启用会继续承接付费流量而收入全部归平台。
	// 重新启用时渠道不自动恢复，由管理员按需手动启用，避免复活被单独停用的渠道。
	var suspendedChannels int64
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var profile model.MerchantProfile
		if err := tx.First(&profile, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.MerchantProfile{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		if req.Status != nil && *req.Status != model.MerchantStatusActive {
			var err error
			suspendedChannels, err = model.DisableMerchantChannelsInTx(tx, profile.UserId)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "商家不存在"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if suspendedChannels > 0 && common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	logger.LogInfo(c, fmt.Sprintf("merchant #%d updated, suspended channels: %d", id, suspendedChannels))
	common.ApiSuccess(c, nil)
}

// AdminGetMerchantWithdrawals 管理员查看提现申请。
func AdminGetMerchantWithdrawals(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := model.DB.Model(&model.MerchantWithdrawal{})
	if status, err := strconv.Atoi(c.Query("status")); err == nil && status >= 0 {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var withdrawals []model.MerchantWithdrawal
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&withdrawals).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(withdrawals)
	common.ApiSuccess(c, pageInfo)
}

// ReviewMerchantWithdrawal 管理员处理提现：approve 通过 / reject 拒绝退回 / paid 标记已打款。
func ReviewMerchantWithdrawal(c *gin.Context) {
	adminId := c.GetInt("id")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	var req struct {
		Action     string `json:"action" binding:"required"` // approve | reject | paid
		ReviewNote string `json:"review_note"`
		PaidProof  string `json:"paid_proof"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	action := req.Action
	if action != "approve" && action != "reject" && action != "paid" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "未知操作"})
		return
	}

	// 状态流转与钱包变动必须同一事务并加行锁，避免并发重复退款/重复累计。
	var withdrawal model.MerchantWithdrawal
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := model.GetMerchantWithdrawalForUpdate(tx, id)
		if err != nil {
			return err
		}
		if locked.Status == model.WithdrawalRejected || locked.Status == model.WithdrawalPaid {
			return model.ErrMerchantWithdrawalHandled
		}
		switch action {
		case "approve":
			if locked.Status != model.WithdrawalPending {
				return model.ErrMerchantWithdrawalHandled
			}
			locked.Status = model.WithdrawalApproved
		case "reject":
			locked.Status = model.WithdrawalRejected
			// 退回冻结金额
			if err := model.RefundWithdrawal(tx, locked.MerchantUserId, locked.Amount); err != nil {
				return err
			}
		case "paid":
			locked.Status = model.WithdrawalPaid
			locked.PaidAt = time.Now().Unix()
			locked.PaidProof = req.PaidProof
			if err := model.ConfirmWithdrawalPaid(tx, locked.MerchantUserId, locked.Amount); err != nil {
				return err
			}
		}
		locked.ReviewNote = req.ReviewNote
		locked.ReviewedBy = adminId
		locked.ReviewedAt = time.Now().Unix()
		withdrawal = *locked
		return tx.Save(locked).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "提现单不存在"})
		return
	case errors.Is(err, model.ErrMerchantWithdrawalHandled):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该提现单已处理"})
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}
