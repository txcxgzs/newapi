package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// RecordMerchantEarningForConsume 在结算落账后调用：若该笔成交落在商家渠道上，
// 按商家分成比例给其钱包入账。任何错误只记日志，绝不影响主计费流程。
//
// 覆盖范围：三条结算路径——PostTextConsumeQuota、PostWssConsumeQuota（多轮共享
// request_id，无请求级兜底，见 quota.go 调用点注释）、PostAudioConsumeQuota；
// 以及异步任务成功终态（RecordMerchantEarningForTask）。
//
// 与消费日志解耦：分润不再以"日志落库"为前提。logId>0 时用 "log:<log_id>" 幂等；
// 日志未启用或写失败（logId=0）时用 dedupFallback（请求级兜底键，见各调用点）；
// 兜底键为空（如 WSS 多轮结算共享同一 request_id，无法做请求级幂等）时退化为
// 无幂等入账。
//
// 退款不变式：分润只挂在"最终结算"的路径上，而这些路径不会再产生
// 针对同一笔成交的退款日志；因此不存在"已分润但后续被退款"的流水，无需回冲。
// 异步任务（MJ/视频）由 RecordMerchantEarningForTask 在成功终态分润。
func RecordMerchantEarningForConsume(ctx context.Context, channelId int, logId int, modelName string, quota int, dedupFallback string) {
	if channelId <= 0 || quota <= 0 {
		return
	}
	creditMerchantEarning(ctx, channelId, logId, modelName, quota, earningDedupKey(logId, dedupFallback))
}

// RecordMerchantEarningForTask 在异步任务（MJ/视频等）到达成功终态后调用：
// 把该笔任务最终实扣额度（task.Quota，含差额结算后的调整）按商家分成比例入账。
// 终态流转由 CAS 赢家恰好触发一次，且成功任务不会再退款，故无需回冲；
// 失败任务走全额退款、不分润。
//
// taskKey 是幂等键，必须带任务表类型前缀（"mj:<id>" / "task:<id>"）：
// 不同任务表自增序列独立，裸数字会相撞导致后到的分润被幂等吞掉。
func RecordMerchantEarningForTask(ctx context.Context, channelId int, taskKey string, modelName string, quota int) {
	if channelId <= 0 || quota <= 0 || taskKey == "" {
		return
	}
	creditMerchantEarning(ctx, channelId, 0, modelName, quota, &taskKey)
}

// creditMerchantEarning 分润入账公共尾部：查渠道归属、校验商家状态、按分成比例
// 计算金额、以 dedupKey 幂等落账。dedupKey 为 nil 时退化为无幂等入账。
func creditMerchantEarning(ctx context.Context, channelId int, logId int, modelName string, quota int, dedupKey *string) {
	channel, err := model.CacheGetChannel(channelId)
	if err != nil || channel == nil {
		return
	}
	merchantId := channel.GetMerchantId()
	if merchantId <= 0 {
		return
	}

	profile, err := model.GetMerchantProfileByUserId(merchantId)
	if err != nil || profile == nil || profile.Status != model.MerchantStatusActive {
		return
	}
	if profile.ProfitRatio <= 0 {
		return
	}

	amount := float64(quota) / common.QuotaPerUnit * profile.ProfitRatio
	if amount <= 0 {
		return
	}

	earning := &model.MerchantEarning{
		MerchantUserId: merchantId,
		ChannelId:      channelId,
		LogId:          logId,
		Amount:         amount,
		ModelName:      modelName,
		DedupKey:       dedupKey,
	}
	if err := model.RecordMerchantEarning(nil, earning); err != nil {
		logger.LogError(ctx, "merchant earning record failed: "+err.Error())
	}
}

// requestDedupFallback 请求级幂等兜底键：仅在消费日志未落库（logId=0）时使用。
func requestDedupFallback(ctx context.Context) string {
	ginCtx, ok := ctx.(*gin.Context)
	if !ok {
		return ""
	}
	requestId := ginCtx.GetString(common.RequestIdKey)
	if requestId == "" {
		return ""
	}
	return "req:" + requestId
}

// earningDedupKey 生成分润流水的幂等键；两个来源都为空时返回 nil（无幂等保护）。
func earningDedupKey(logId int, dedupFallback string) *string {
	var key string
	if logId > 0 {
		key = fmt.Sprintf("log:%d", logId)
	} else if dedupFallback != "" {
		key = dedupFallback
	} else {
		return nil
	}
	return &key
}
