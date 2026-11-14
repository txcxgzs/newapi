package controller

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type financeTimeWindow struct {
	Start int64
	End   int64
}

func parseFinanceWindow(c *gin.Context, defaultDays int) financeTimeWindow {
	now := time.Now().Unix()
	startTs, _ := strconv.ParseInt(strings.TrimSpace(c.Query("start_timestamp")), 10, 64)
	endTs, _ := strconv.ParseInt(strings.TrimSpace(c.Query("end_timestamp")), 10, 64)
	if startTs <= 0 && defaultDays > 0 {
		startTs = now - int64(defaultDays)*86400
	}
	if endTs <= 0 {
		endTs = now
	}
	return financeTimeWindow{Start: startTs, End: endTs}
}

func loadUsernamesByIds(userIds []int) map[int]string {
	result := make(map[int]string)
	unique := make([]int, 0, len(userIds))
	seen := make(map[int]bool, len(userIds))
	for _, id := range userIds {
		if id > 0 && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return result
	}
	var users []model.User
	if err := model.DB.Unscoped().Select("id, username").Where("id IN ?", unique).Find(&users).Error; err == nil {
		for _, u := range users {
			result[u.Id] = u.Username
		}
	}
	return result
}

// GetFinanceOverview 管理后台财务中心总览：充值实收、订阅营收、兑换码、用户消耗、商家结算与抽佣毛利及日趋势。
func GetFinanceOverview(c *gin.Context) {
	win := parseFinanceWindow(c, 30)
	now := time.Now()
	_, zoneOffset := now.Zone()

	// 1. 充值订单统计
	type topupAgg struct {
		TotalOrders   int64   `json:"total_orders"`
		SuccessOrders int64   `json:"success_orders"`
		PendingOrders int64   `json:"pending_orders"`
		Money         float64 `json:"money"`
		AmountUSD     float64 `json:"amount_usd"`
	}
	var topupAll topupAgg
	model.DB.Model(&model.TopUp{}).
		Select("COUNT(*) AS total_orders, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS success_orders, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending_orders, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN money ELSE 0 END), 0) AS money, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN amount ELSE 0 END), 0) AS amount_usd",
			common.TopUpStatusSuccess, common.TopUpStatusPending, common.TopUpStatusSuccess, common.TopUpStatusSuccess).
		Scan(&topupAll)

	var topupPeriod struct {
		SuccessOrders int64   `json:"success_orders"`
		Money         float64 `json:"money"`
		AmountUSD     float64 `json:"amount_usd"`
	}
	model.DB.Model(&model.TopUp{}).
		Select("COUNT(*) AS success_orders, COALESCE(SUM(money),0) AS money, COALESCE(SUM(amount),0) AS amount_usd").
		Where("status = ? AND complete_time >= ? AND complete_time <= ?", common.TopUpStatusSuccess, win.Start, win.End).
		Scan(&topupPeriod)

	type providerStat struct {
		Provider string  `json:"provider"`
		Orders   int64   `json:"orders"`
		Money    float64 `json:"money"`
	}
	var providerBreakdown []providerStat
	model.DB.Model(&model.TopUp{}).
		Select("CASE WHEN payment_provider = '' THEN payment_method ELSE payment_provider END AS provider, COUNT(*) AS orders, COALESCE(SUM(money),0) AS money").
		Where("status = ?", common.TopUpStatusSuccess).
		Group("provider").
		Order("money DESC").
		Scan(&providerBreakdown)

	// 2. 订阅订单与有效订阅统计
	var subStats struct {
		TotalPaidOrders int64   `json:"total_paid_orders"`
		TotalRevenue    float64 `json:"total_revenue"`
		PeriodRevenue   float64 `json:"period_revenue"`
		ActiveUsers     int64   `json:"active_users"`
	}
	model.DB.Model(&model.SubscriptionOrder{}).
		Select("COUNT(*) AS total_paid_orders, COALESCE(SUM(money),0) AS total_revenue").
		Where("status = ?", common.TopUpStatusSuccess).
		Scan(&subStats)
	model.DB.Model(&model.SubscriptionOrder{}).
		Select("COALESCE(SUM(money),0)").
		Where("status = ? AND complete_time >= ? AND complete_time <= ?", common.TopUpStatusSuccess, win.Start, win.End).
		Scan(&subStats.PeriodRevenue)
	model.DB.Model(&model.UserSubscription{}).
		Where("status = ? AND end_time > ?", "active", now.Unix()).
		Count(&subStats.ActiveUsers)

	// 3. 兑换码统计
	var redempRaw struct {
		TotalCodes   int64 `json:"total_codes"`
		UsedCodes    int64 `json:"used_codes"`
		UnusedCodes  int64 `json:"unused_codes"`
		TotalQuota   int64 `json:"total_quota"`
		UsedQuota    int64 `json:"used_quota"`
		UnusedQuota  int64 `json:"unused_quota"`
	}
	model.DB.Model(&model.Redemption{}).
		Select("COUNT(*) AS total_codes, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS used_codes, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS unused_codes, "+
			"COALESCE(SUM(quota), 0) AS total_quota, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN quota ELSE 0 END), 0) AS used_quota, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN quota ELSE 0 END), 0) AS unused_quota",
			common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusEnabled,
			common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusEnabled).
		Scan(&redempRaw)

	// 4. 平台用户消费日志统计
	var consumePeriod struct {
		Calls int64 `json:"calls"`
		Quota int64 `json:"quota"`
	}
	if model.LOG_DB != nil {
		model.LOG_DB.Model(&model.Log{}).
			Select("COUNT(*) AS calls, COALESCE(SUM(quota),0) AS quota").
			Where("type = ? AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, win.Start, win.End).
			Scan(&consumePeriod)
	}

	// 5. 平台用户当前总余额负债与总消耗
	var userTotals struct {
		TotalRemainQuota int64 `json:"total_remain_quota"`
		TotalUsedQuota   int64 `json:"total_used_quota"`
		TotalUsers       int64 `json:"total_users"`
	}
	model.DB.Model(&model.User{}).
		Select("COUNT(*) AS total_users, COALESCE(SUM(quota),0) AS total_remain_quota, COALESCE(SUM(used_quota),0) AS total_used_quota").
		Scan(&userTotals)

	// 6. 商家分润、抽佣与提现盘子
	var merchantWalletTotals struct {
		AvailableBalance float64 `json:"available_balance"`
		PendingBalance   float64 `json:"pending_balance"`
		FrozenBalance    float64 `json:"frozen_balance"`
		TotalEarned      float64 `json:"total_earned"`
		TotalWithdrawn   float64 `json:"total_withdrawn"`
		TotalCommission  float64 `json:"total_commission"`
	}
	model.DB.Model(&model.MerchantWallet{}).
		Select("COALESCE(SUM(balance),0) AS available_balance, "+
			"COALESCE(SUM(pending_balance),0) AS pending_balance, "+
			"COALESCE(SUM(frozen_balance),0) AS frozen_balance, "+
			"COALESCE(SUM(total_earned),0) AS total_earned, "+
			"COALESCE(SUM(total_withdrawn),0) AS total_withdrawn, "+
			"COALESCE(SUM(total_commission),0) AS total_commission").
		Scan(&merchantWalletTotals)

	var periodEarningTotals struct {
		GrossAmount      float64 `json:"gross_amount"`
		MerchantAmount   float64 `json:"merchant_amount"`
		CommissionAmount float64 `json:"commission_amount"`
	}
	model.DB.Model(&model.MerchantEarning{}).
		Select("COALESCE(SUM(gross_amount),0) AS gross_amount, COALESCE(SUM(amount),0) AS merchant_amount, COALESCE(SUM(commission_amount),0) AS commission_amount").
		Where("status <> ? AND created_at >= ? AND created_at <= ?", model.MerchantEarningReversed, win.Start, win.End).
		Scan(&periodEarningTotals)

	var withdrawPending struct {
		Count  int64   `json:"count"`
		Amount float64 `json:"amount"`
	}
	model.DB.Model(&model.MerchantWithdrawal{}).
		Select("COUNT(*) AS count, COALESCE(SUM(amount),0) AS amount").
		Where("status IN ?", []int{model.WithdrawalPending, model.WithdrawalApproved}).
		Scan(&withdrawPending)

	// 7. 窗口每日财务趋势
	daily := buildFinanceDailySeries(win.Start, win.End, zoneOffset)

	common.ApiSuccess(c, gin.H{
		"window": gin.H{
			"start_timestamp": win.Start,
			"end_timestamp":   win.End,
		},
		"topup_stats": gin.H{
			"total_orders":       topupAll.TotalOrders,
			"success_orders":     topupAll.SuccessOrders,
			"pending_orders":     topupAll.PendingOrders,
			"total_money":        topupAll.Money,
			"total_amount_usd":   topupAll.AmountUSD,
			"period_orders":      topupPeriod.SuccessOrders,
			"period_money":       topupPeriod.Money,
			"period_amount_usd":  topupPeriod.AmountUSD,
			"provider_breakdown": providerBreakdown,
		},
		"subscription_stats": subStats,
		"redemption_stats": gin.H{
			"total_codes":   redempRaw.TotalCodes,
			"used_codes":    redempRaw.UsedCodes,
			"unused_codes":  redempRaw.UnusedCodes,
			"total_usd":     float64(redempRaw.TotalQuota) / common.QuotaPerUnit,
			"used_usd":      float64(redempRaw.UsedQuota) / common.QuotaPerUnit,
			"unused_usd":    float64(redempRaw.UnusedQuota) / common.QuotaPerUnit,
		},
		"consume_stats": gin.H{
			"period_calls":     consumePeriod.Calls,
			"period_spend_usd": float64(consumePeriod.Quota) / common.QuotaPerUnit,
			"all_time_usd":     float64(userTotals.TotalUsedQuota) / common.QuotaPerUnit,
		},
		"user_wallet_stats": gin.H{
			"total_users":       userTotals.TotalUsers,
			"total_remain_usd":  float64(userTotals.TotalRemainQuota) / common.QuotaPerUnit,
			"total_used_usd":    float64(userTotals.TotalUsedQuota) / common.QuotaPerUnit,
		},
		"merchant_stats": gin.H{
			"available_balance":         merchantWalletTotals.AvailableBalance,
			"pending_balance":           merchantWalletTotals.PendingBalance,
			"frozen_balance":            merchantWalletTotals.FrozenBalance,
			"total_earned":              merchantWalletTotals.TotalEarned,
			"total_withdrawn":           merchantWalletTotals.TotalWithdrawn,
			"total_commission":          merchantWalletTotals.TotalCommission,
			"period_gross_usd":          periodEarningTotals.GrossAmount,
			"period_merchant_earning":   periodEarningTotals.MerchantAmount,
			"period_platform_commission": periodEarningTotals.CommissionAmount,
			"pending_withdrawal_count":  withdrawPending.Count,
			"pending_withdrawal_amount": withdrawPending.Amount,
		},
		"daily_series": daily,
	})
}

func buildFinanceDailySeries(startTs, endTs int64, zoneOffset int) []gin.H {
	if endTs < startTs {
		return []gin.H{}
	}
	// 最长展示 90 天趋势避免过长循环
	if endTs-startTs > 90*86400 {
		startTs = endTs - 90*86400
	}
	type dayVal struct {
		DayIdx float64 `json:"day_idx"`
		Val    float64 `json:"val"`
		Extra  float64 `json:"extra"`
	}
	topupMap := make(map[int64]float64)
	var topupRows []dayVal
	model.DB.Model(&model.TopUp{}).
		Select("((complete_time + ?) / 86400) AS day_idx, COALESCE(SUM(money),0) AS val", zoneOffset).
		Where("status = ? AND complete_time >= ? AND complete_time <= ?", common.TopUpStatusSuccess, startTs, endTs).
		Group("day_idx").Scan(&topupRows)
	for _, r := range topupRows {
		topupMap[int64(math.Floor(r.DayIdx))] += r.Val
	}

	subMap := make(map[int64]float64)
	var subRows []dayVal
	model.DB.Model(&model.SubscriptionOrder{}).
		Select("((complete_time + ?) / 86400) AS day_idx, COALESCE(SUM(money),0) AS val", zoneOffset).
		Where("status = ? AND complete_time >= ? AND complete_time <= ?", common.TopUpStatusSuccess, startTs, endTs).
		Group("day_idx").Scan(&subRows)
	for _, r := range subRows {
		subMap[int64(math.Floor(r.DayIdx))] += r.Val
	}

	earningMap := make(map[int64]float64)
	commMap := make(map[int64]float64)
	var earningRows []dayVal
	model.DB.Model(&model.MerchantEarning{}).
		Select("((created_at + ?) / 86400) AS day_idx, COALESCE(SUM(amount),0) AS val, COALESCE(SUM(commission_amount),0) AS extra", zoneOffset).
		Where("status <> ? AND created_at >= ? AND created_at <= ?", model.MerchantEarningReversed, startTs, endTs).
		Group("day_idx").Scan(&earningRows)
	for _, r := range earningRows {
		d := int64(math.Floor(r.DayIdx))
		earningMap[d] += r.Val
		commMap[d] += r.Extra
	}

	withdrawMap := make(map[int64]float64)
	var wRows []dayVal
	model.DB.Model(&model.MerchantWithdrawal{}).
		Select("((paid_at + ?) / 86400) AS day_idx, COALESCE(SUM(amount),0) AS val", zoneOffset).
		Where("status = ? AND paid_at >= ? AND paid_at <= ?", model.WithdrawalPaid, startTs, endTs).
		Group("day_idx").Scan(&wRows)
	for _, r := range wRows {
		withdrawMap[int64(math.Floor(r.DayIdx))] += r.Val
	}

	startDay := (startTs + int64(zoneOffset)) / 86400
	endDay := (endTs + int64(zoneOffset)) / 86400
	out := make([]gin.H, 0, endDay-startDay+1)
	for day := startDay; day <= endDay; day++ {
		out = append(out, gin.H{
			"day":                 day*86400 - int64(zoneOffset),
			"topup_money":         topupMap[day],
			"subscription_money":  subMap[day],
			"merchant_earning":    earningMap[day],
			"platform_commission": commMap[day],
			"withdrawn_usd":       withdrawMap[day],
		})
	}
	return out
}

// GetFinanceTopUps 财务中心充值订单筛选查询。
func GetFinanceTopUps(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	status := strings.TrimSpace(c.Query("status"))
	provider := strings.TrimSpace(c.Query("provider"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	query := model.DB.Model(&model.TopUp{})
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	if provider != "" && provider != "all" {
		query = query.Where("(payment_provider = ? OR payment_method = ?)", provider, provider)
	}
	if keyword != "" {
		if uid, err := strconv.Atoi(keyword); err == nil && uid > 0 {
			query = query.Where("(trade_no LIKE ? OR user_id = ?)", "%"+keyword+"%", uid)
		} else {
			query = query.Where("trade_no LIKE ?", "%"+keyword+"%")
		}
	}
	if startTs > 0 {
		query = query.Where("create_time >= ?", startTs)
	}
	if endTs > 0 {
		query = query.Where("create_time <= ?", endTs)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var items []model.TopUp
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(items))
	for _, it := range items {
		userIds = append(userIds, it.UserId)
	}
	usernames := loadUsernamesByIds(userIds)

	type topupRow struct {
		model.TopUp
		Username string `json:"username"`
	}
	rows := make([]topupRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, topupRow{TopUp: it, Username: usernames[it.UserId]})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// GetFinanceSubscriptions 财务中心订阅订单与营收查询。
func GetFinanceSubscriptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	status := strings.TrimSpace(c.Query("status"))
	provider := strings.TrimSpace(c.Query("provider"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	query := model.DB.Model(&model.SubscriptionOrder{})
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	if provider != "" && provider != "all" {
		query = query.Where("(payment_provider = ? OR payment_method = ?)", provider, provider)
	}
	if keyword != "" {
		if uid, err := strconv.Atoi(keyword); err == nil && uid > 0 {
			query = query.Where("(trade_no LIKE ? OR user_id = ?)", "%"+keyword+"%", uid)
		} else {
			query = query.Where("trade_no LIKE ?", "%"+keyword+"%")
		}
	}
	if startTs > 0 {
		query = query.Where("create_time >= ?", startTs)
	}
	if endTs > 0 {
		query = query.Where("create_time <= ?", endTs)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var orders []model.SubscriptionOrder
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&orders).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(orders))
	planIds := make([]int, 0, len(orders))
	for _, o := range orders {
		userIds = append(userIds, o.UserId)
		planIds = append(planIds, o.PlanId)
	}
	usernames := loadUsernamesByIds(userIds)
	planTitles := make(map[int]string)
	if len(planIds) > 0 {
		var plans []model.SubscriptionPlan
		if err := model.DB.Select("id, title").Where("id IN ?", planIds).Find(&plans).Error; err == nil {
			for _, p := range plans {
				planTitles[p.Id] = p.Title
			}
		}
	}
	type subOrderRow struct {
		model.SubscriptionOrder
		Username  string `json:"username"`
		PlanTitle string `json:"plan_title"`
	}
	rows := make([]subOrderRow, 0, len(orders))
	for _, o := range orders {
		rows = append(rows, subOrderRow{
			SubscriptionOrder: o,
			Username:          usernames[o.UserId],
			PlanTitle:         planTitles[o.PlanId],
		})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// GetFinanceRedemptionSummary 财务中心兑换码账务与最近核销明细。
func GetFinanceRedemptionSummary(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	statusStr := strings.TrimSpace(c.Query("status"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	query := model.DB.Model(&model.Redemption{})
	if statusStr != "" && statusStr != "all" {
		if statusVal, err := strconv.Atoi(statusStr); err == nil {
			query = query.Where("status = ?", statusVal)
		}
	}
	if keyword != "" {
		query = query.Where("(name LIKE ? OR key LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var items []model.Redemption
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(items)*2)
	for _, r := range items {
		userIds = append(userIds, r.UserId, r.UsedUserId)
	}
	usernames := loadUsernamesByIds(userIds)
	type redemptionRow struct {
		Id           int     `json:"id"`
		Name         string  `json:"name"`
		Status       int     `json:"status"`
		Quota        int     `json:"quota"`
		AmountUSD    float64 `json:"amount_usd"`
		UserId       int     `json:"user_id"`
		CreatorName  string  `json:"creator_name"`
		UsedUserId   int     `json:"used_user_id"`
		UsedUsername string  `json:"used_username"`
		CreatedTime  int64   `json:"created_time"`
		RedeemedTime int64   `json:"redeemed_time"`
		ExpiredTime  int64   `json:"expired_time"`
	}
	rows := make([]redemptionRow, 0, len(items))
	for _, r := range items {
		rows = append(rows, redemptionRow{
			Id:           r.Id,
			Name:         r.Name,
			Status:       r.Status,
			Quota:        r.Quota,
			AmountUSD:    float64(r.Quota) / common.QuotaPerUnit,
			UserId:       r.UserId,
			CreatorName:  usernames[r.UserId],
			UsedUserId:   r.UsedUserId,
			UsedUsername: usernames[r.UsedUserId],
			CreatedTime:  r.CreatedTime,
			RedeemedTime: r.RedeemedTime,
			ExpiredTime:  r.ExpiredTime,
		})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// GetFinanceMerchantEarnings 全平台商家分润与平台抽佣明细查询。
func GetFinanceMerchantEarnings(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	merchantId, _ := strconv.Atoi(strings.TrimSpace(c.Query("merchant_user_id")))
	statusVal, statusErr := strconv.Atoi(strings.TrimSpace(c.Query("status")))
	modelName := strings.TrimSpace(c.Query("model_name"))
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	query := model.DB.Model(&model.MerchantEarning{})
	if merchantId > 0 {
		query = query.Where("merchant_user_id = ?", merchantId)
	}
	if statusErr == nil && statusVal > 0 {
		query = query.Where("status = ?", statusVal)
	}
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
	var items []model.MerchantEarning
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(items))
	for _, it := range items {
		userIds = append(userIds, it.MerchantUserId)
	}
	usernames := loadUsernamesByIds(userIds)

	siteNames := make(map[int]string)
	var profiles []model.MerchantProfile
	if len(userIds) > 0 {
		if err := model.DB.Select("user_id, site_name").Where("user_id IN ?", userIds).Find(&profiles).Error; err == nil {
			for _, p := range profiles {
				siteNames[p.UserId] = p.SiteName
			}
		}
	}

	type earningRow struct {
		model.MerchantEarning
		Username string `json:"username"`
		SiteName string `json:"site_name"`
	}
	rows := make([]earningRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, earningRow{
			MerchantEarning: it,
			Username:        usernames[it.MerchantUserId],
			SiteName:        siteNames[it.MerchantUserId],
		})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// PostFinanceSettleDueEarnings 手动触发到期 T+N 商家分润结算。
func PostFinanceSettleDueEarnings(c *gin.Context) {
	count, amount, err := model.SettleDueMerchantEarnings(time.Now().Unix())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"settled_count":  count,
		"settled_amount": amount,
	})
}

// PostFinanceReverseEarning 管理员对问题分润单进行冲正扣回。
func PostFinanceReverseEarning(c *gin.Context) {
	earningId, err := strconv.Atoi(c.Param("id"))
	if err != nil || earningId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "管理员财务冲正"
	}
	if err := model.ReverseMerchantEarning(earningId, c.GetInt("id"), reason); err != nil {
		if err == model.ErrInsufficientBalance {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "商家可用余额不足，无法全额冲正该笔已结算分润"})
			return
		}
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "分润流水不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, nil)
}

func buildFinanceLedgerQuery(c *gin.Context) *gorm.DB {
	domain := strings.TrimSpace(c.Query("domain"))
	bizType := strings.TrimSpace(c.Query("biz_type"))
	direction := strings.TrimSpace(c.Query("direction"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	userId, _ := strconv.Atoi(strings.TrimSpace(c.Query("user_id")))
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	query := model.DB.Model(&model.FinanceLedger{})
	if domain != "" && domain != "all" {
		query = query.Where("domain = ?", domain)
	}
	if bizType != "" && bizType != "all" {
		query = query.Where("biz_type = ?", bizType)
	}
	if direction != "" && direction != "all" {
		query = query.Where("direction = ?", direction)
	}
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("(ledger_no LIKE ? OR ref_id LIKE ? OR remark LIKE ?)", like, like, like)
	}
	if startTs > 0 {
		query = query.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		query = query.Where("created_at <= ?", endTs)
	}
	return query
}

// GetFinanceLedgers 统一资金总账分页查询。
func GetFinanceLedgers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := buildFinanceLedgerQuery(c)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var items []model.FinanceLedger
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(items))
	for _, it := range items {
		userIds = append(userIds, it.UserId, it.OperatorId)
	}
	usernames := loadUsernamesByIds(userIds)

	type ledgerRow struct {
		model.FinanceLedger
		Username     string `json:"username"`
		OperatorName string `json:"operator_name"`
	}
	rows := make([]ledgerRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, ledgerRow{
			FinanceLedger: it,
			Username:      usernames[it.UserId],
			OperatorName:  usernames[it.OperatorId],
		})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func csvSafeCell(v string) string {
	if v != "" {
		switch v[0] {
		case '=', '+', '-', '@', '\t', '\r':
			return "'" + v
		}
	}
	return v
}

// ExportFinanceLedgersCSV 导出资金总账为 UTF-8 BOM CSV（对公式前缀做防注入转义）。
func ExportFinanceLedgersCSV(c *gin.Context) {
	var items []model.FinanceLedger
	if err := buildFinanceLedgerQuery(c).Order("id DESC").Limit(10000).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(items))
	for _, it := range items {
		userIds = append(userIds, it.UserId)
	}
	usernames := loadUsernamesByIds(userIds)

	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		"ID", "LedgerNo", "Time", "Domain", "BizType", "Direction",
		"UserID", "Username", "AmountUSD", "Money", "QuotaDelta",
		"BeforeBalance", "AfterBalance", "RefType", "RefID", "OperatorID", "Remark",
	})
	for _, it := range items {
		_ = w.Write([]string{
			strconv.Itoa(it.Id),
			csvSafeCell(it.LedgerNo),
			time.Unix(it.CreatedAt, 0).Format("2006-01-02 15:04:05"),
			csvSafeCell(it.Domain),
			csvSafeCell(it.BizType),
			csvSafeCell(it.Direction),
			strconv.Itoa(it.UserId),
			csvSafeCell(usernames[it.UserId]),
			fmt.Sprintf("%.6f", it.AmountUSD),
			fmt.Sprintf("%.2f", it.Money),
			strconv.FormatInt(it.QuotaDelta, 10),
			fmt.Sprintf("%.6f", it.BeforeBalance),
			fmt.Sprintf("%.6f", it.AfterBalance),
			csvSafeCell(it.RefType),
			csvSafeCell(it.RefId),
			strconv.Itoa(it.OperatorId),
			csvSafeCell(it.Remark),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"finance-ledger-%s.csv\"", time.Now().Format("20060102-150405")))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// GetFinanceReconciliation 财务自动平账巡检：校验商家钱包账实一致、待解冻到期队列与滞留充值单。
func GetFinanceReconciliation(c *gin.Context) {
	now := time.Now().Unix()

	var wallets []model.MerchantWallet
	if err := model.DB.Find(&wallets).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type earningSum struct {
		MerchantUserId int     `json:"merchant_user_id"`
		SettledSum     float64 `json:"settled_sum"`
		UnsettledSum   float64 `json:"unsettled_sum"`
		ActiveTotal    float64 `json:"active_total"`
	}
	var sums []earningSum
	model.DB.Model(&model.MerchantEarning{}).
		Select("merchant_user_id, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN amount ELSE 0 END), 0) AS settled_sum, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN amount ELSE 0 END), 0) AS unsettled_sum, "+
			"COALESCE(SUM(CASE WHEN status <> ? THEN amount ELSE 0 END), 0) AS active_total",
			model.MerchantEarningSettled, model.MerchantEarningUnsettled, model.MerchantEarningReversed).
		Group("merchant_user_id").
		Scan(&sums)

	sumByMerchant := make(map[int]earningSum, len(sums))
	for _, s := range sums {
		sumByMerchant[s.MerchantUserId] = s
	}

	userIds := make([]int, 0, len(wallets))
	for _, w := range wallets {
		userIds = append(userIds, w.MerchantUserId)
	}
	usernames := loadUsernamesByIds(userIds)

	type mismatchRow struct {
		MerchantUserId  int     `json:"merchant_user_id"`
		Username        string  `json:"username"`
		WalletTotal     float64 `json:"wallet_total"`
		EarningTotal    float64 `json:"earning_total"`
		Diff            float64 `json:"diff"`
		PendingBalance  float64 `json:"pending_balance"`
		ExpectedPending float64 `json:"expected_pending"`
	}
	mismatches := make([]mismatchRow, 0)
	for _, w := range wallets {
		s := sumByMerchant[w.MerchantUserId]
		walletTotal := w.Balance + w.PendingBalance + w.FrozenBalance + w.TotalWithdrawn
		diff := walletTotal - s.ActiveTotal
		pendingDiff := w.PendingBalance - s.UnsettledSum
		if math.Abs(diff) > 1e-3 || math.Abs(pendingDiff) > 1e-3 {
			mismatches = append(mismatches, mismatchRow{
				MerchantUserId:  w.MerchantUserId,
				Username:        usernames[w.MerchantUserId],
				WalletTotal:     walletTotal,
				EarningTotal:    s.ActiveTotal,
				Diff:            diff,
				PendingBalance:  w.PendingBalance,
				ExpectedPending: s.UnsettledSum,
			})
		}
	}

	var dueSettle struct {
		Count  int64   `json:"count"`
		Amount float64 `json:"amount"`
	}
	model.DB.Model(&model.MerchantEarning{}).
		Select("COUNT(*) AS count, COALESCE(SUM(amount),0) AS amount").
		Where("status = ? AND settle_at <= ?", model.MerchantEarningUnsettled, now).
		Scan(&dueSettle)

	var stalePendingTopups int64
	model.DB.Model(&model.TopUp{}).
		Where("status = ? AND create_time < ?", common.TopUpStatusPending, now-86400).
		Count(&stalePendingTopups)

	var ledgerCount int64
	model.DB.Model(&model.FinanceLedger{}).Count(&ledgerCount)

	common.ApiSuccess(c, gin.H{
		"checked_at":             now,
		"checked_wallets":        len(wallets),
		"wallet_mismatch_count":  len(mismatches),
		"wallet_mismatches":      mismatches,
		"due_unsettled_count":    dueSettle.Count,
		"due_unsettled_amount":   dueSettle.Amount,
		"stale_pending_topups":   stalePendingTopups,
		"finance_ledger_records": ledgerCount,
		"healthy":                len(mismatches) == 0,
	})
}
