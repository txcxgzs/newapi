package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func checkQuotaAdjustmentAuditReason(c *gin.Context, req ManageRequest, params model.AuditFields) bool {
	role := c.GetInt("role")
	if role >= common.RoleRootUser || req.Id <= 0 || (req.Mode != "add" && req.Mode != "subtract" && req.Mode != "override") {
		return true
	}
	var target model.User
	if err := model.DB.Select("id, role").First(&target, req.Id).Error; err != nil || target.Role >= role {
		return true
	}
	if _, ok := middleware.CheckAuditReason(c, authz.UserSensitiveWrite, middleware.DefaultAuditReasonMinLength); !ok {
		params["failure_reason"] = "audit_reason_required"
		return false
	}
	return true
}

func manageUserQuota(c *gin.Context, req ManageRequest) {
	action := "generic"
	params := model.AuditFields{
		"target_user_id":  req.Id,
		"mode":            req.Mode,
		"requested_quota": req.Value,
	}
	switch req.Mode {
	case "add":
		action = "user.quota_add"
	case "subtract":
		action = "user.quota_subtract"
	case "override":
		action = "user.quota_override"
	default:
		params["action"] = "add_quota"
		params["method"] = c.Request.Method
		params["route"] = c.FullPath()
	}
	success := false
	defer func() {
		content := auditContentEN(action, params)
		if !success {
			// Failed requests have no committed balance changes to render.
			content = "Failed user quota adjustment"
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params,
			auditOperatorInfo(c), &model.AuditRequestInfo{
				Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
			}, c)
		markAuditLogged(c)
	}()

	if !checkQuotaAdjustmentAuditReason(c, req, params) {
		return
	}

	adjustment, err := model.AdjustUserQuota(req.Id, c.GetInt("role"), req.Mode, req.Value)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrInvalidUserQuotaAdjustment):
			params["failure_reason"] = "invalid_parameters"
			if (req.Mode == "add" || req.Mode == "subtract") && req.Value <= 0 {
				common.ApiErrorT(c, "Quota change amount cannot be zero")
			} else {
				common.ApiErrorT(c, "Invalid parameters")
			}
		case errors.Is(err, model.ErrUserQuotaPermission):
			params["failure_reason"] = "permission_denied"
			common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		case errors.Is(err, gorm.ErrRecordNotFound):
			params["failure_reason"] = "target_not_found"
			common.ApiErrorT(c, "User does not exist")
		case errors.Is(err, model.ErrWalletQuotaLimitExceeded):
			params["failure_reason"] = "quota_limit_exceeded"
			common.ApiError(c, err)
		default:
			params["failure_reason"] = "database_error"
			common.ApiError(c, err)
		}
		return
	}

	params["target_username"] = adjustment.Username
	params["from"] = adjustment.Before
	params["to"] = adjustment.After
	if req.Mode != "override" {
		params["quota"] = req.Value
	}
	success = true
	operation := model.AuditOperation{Action: action, Params: params}
	model.RecordLogWithAdminInfo(adjustment.UserID, model.LogTypeTopup,
		auditContentEN(action, params), auditOperatorInfo(c), &operation, c)
	recordAdminQuotaLedger(c, action, adjustment.UserID, adjustment.Before, adjustment.After, false)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// manageUserGiftQuota 处理管理员对用户赠金的调整（add_gift_quota）。
// 与钱包额度调整同构：审计 + 余额日志，但作用于独立的赠金账本。
func manageUserGiftQuota(c *gin.Context, req ManageRequest) {
	action := "generic"
	params := model.AuditFields{
		"target_user_id":  req.Id,
		"mode":            req.Mode,
		"requested_quota": req.Value,
	}
	switch req.Mode {
	case "add":
		action = "user.gift_quota_add"
	case "subtract":
		action = "user.gift_quota_subtract"
	case "override":
		action = "user.gift_quota_override"
	default:
		params["action"] = "add_gift_quota"
		params["method"] = c.Request.Method
		params["route"] = c.FullPath()
	}
	success := false
	defer func() {
		content := auditContentEN(action, params)
		if !success {
			content = "Failed user gift quota adjustment"
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params,
			auditOperatorInfo(c), &model.AuditRequestInfo{
				Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
			}, c)
		markAuditLogged(c)
	}()

	if !checkQuotaAdjustmentAuditReason(c, req, params) {
		return
	}

	adjustment, err := model.AdjustUserGiftQuota(req.Id, c.GetInt("role"), req.Mode, req.Value)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrInvalidUserQuotaAdjustment):
			params["failure_reason"] = "invalid_parameters"
			if (req.Mode == "add" || req.Mode == "subtract") && req.Value <= 0 {
				common.ApiErrorT(c, "Quota change amount cannot be zero")
			} else {
				common.ApiErrorT(c, "Invalid parameters")
			}
		case errors.Is(err, model.ErrUserQuotaPermission):
			params["failure_reason"] = "permission_denied"
			common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		case errors.Is(err, gorm.ErrRecordNotFound):
			params["failure_reason"] = "target_not_found"
			common.ApiErrorT(c, "User does not exist")
		case errors.Is(err, model.ErrWalletQuotaLimitExceeded):
			params["failure_reason"] = "quota_limit_exceeded"
			common.ApiError(c, err)
		default:
			params["failure_reason"] = "database_error"
			common.ApiError(c, err)
		}
		return
	}

	params["target_username"] = adjustment.Username
	params["from"] = adjustment.Before
	params["to"] = adjustment.After
	if req.Mode != "override" {
		params["quota"] = req.Value
	}
	success = true
	operation := model.AuditOperation{Action: action, Params: params}
	model.RecordLogWithAdminInfo(adjustment.UserID, model.LogTypeTopup,
		auditContentEN(action, params), auditOperatorInfo(c), &operation, c)
	recordAdminQuotaLedger(c, action, adjustment.UserID, adjustment.Before, adjustment.After, true)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func recordAdminQuotaLedger(c *gin.Context, action string, targetUserId int, before int, after int, isGift bool) {
	delta := int64(after - before)
	dir := model.FinanceDirIn
	if delta < 0 {
		dir = model.FinanceDirOut
	}
	bizType := model.FinanceBizAdminQuotaAdjust
	refType := "admin_quota"
	if isGift {
		bizType = model.FinanceBizAdminGiftQuotaAdjust
		refType = "admin_gift_quota"
	}
	model.RecordFinanceLedgerBestEffort(&model.FinanceLedger{
		Domain:        model.FinanceDomainUserQuota,
		UserId:        targetUserId,
		BizType:       bizType,
		Direction:     dir,
		AmountUSD:     float64(delta) / common.QuotaPerUnit,
		QuotaDelta:    delta,
		BeforeBalance: float64(before) / common.QuotaPerUnit,
		AfterBalance:  float64(after) / common.QuotaPerUnit,
		RefType:       refType,
		RefId:         fmt.Sprintf("user:%d", targetUserId),
		OperatorId:    c.GetInt("id"),
		Remark:        action,
	})
}
