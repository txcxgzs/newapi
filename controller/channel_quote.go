package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetChannelQuotes 管理端盘口报价列表，支持分页与筛选。
// 查询参数：p, page_size, channel_id, pool_group, model_name, source
func GetChannelQuotes(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	query := model.DB.Model(&model.ChannelQuote{})
	if channelId, err := strconv.Atoi(c.Query("channel_id")); err == nil && channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if pool := c.Query("pool_group"); pool != "" {
		query = query.Where("pool_group = ?", pool)
	}
	if modelName := c.Query("model_name"); modelName != "" {
		query = query.Where("model_name LIKE ?", "%"+modelName+"%")
	}
	if source := c.Query("source"); source != "" {
		query = query.Where("source = ?", source)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var quotes []model.ChannelQuote
	if err := query.Order("model_name ASC, input_price ASC").
		Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&quotes).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(quotes)
	common.ApiSuccess(c, pageInfo)
}

// UpsertManualQuote 管理员手动设置/修改一条报价（source=manual，自动同步不会覆盖）。
func UpsertManualQuote(c *gin.Context) {
	var req struct {
		ChannelId   int     `json:"channel_id" binding:"required"`
		PoolGroup   string  `json:"pool_group" binding:"required"`
		ModelName   string  `json:"model_name" binding:"required"`
		InputPrice  float64 `json:"input_price"`
		OutputPrice float64 `json:"output_price"`
		Status      int     `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if req.InputPrice <= 0 && req.OutputPrice <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "输入价与输出价不能同时为空"})
		return
	}
	status := req.Status
	if status != model.QuoteStatusDisabled {
		status = model.QuoteStatusEnabled
	}
	now := time.Now().Unix()
	quote := model.ChannelQuote{
		ChannelId: req.ChannelId,
		PoolGroup: req.PoolGroup,
		ModelName: req.ModelName,
		// 手工锁定价同步反推有效倍率，保证行情/比价仍能按倍率口径统一展示
		RateMultiplier: req.InputPrice / priceRatioBase,
		InputPrice:     req.InputPrice,
		OutputPrice:    req.OutputPrice,
		Currency:       "USD",
		Source:         model.QuoteSourceManual,
		Status:         status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := model.UpsertChannelQuotes([]model.ChannelQuote{quote}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, quote)
}

// DeleteChannelQuote 管理员删除一条报价。
func DeleteChannelQuote(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if err := model.DB.Delete(&model.ChannelQuote{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
