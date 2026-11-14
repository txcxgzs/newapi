package model

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm/clause"
)

const (
	liveTtftAlpha    = 0.25
	liveTpsAlpha     = 0.25
	liveSuccessAlpha = 0.20
)

type channelModelMetricKey struct {
	ChannelId int
	ModelName string
}

// ChannelLiveMetric persists sliding EMA telemetry (TTFT, TPS, success rate) per (channel, model).
type ChannelLiveMetric struct {
	Id            int     `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId     int     `json:"channel_id" gorm:"not null;uniqueIndex:idx_live_metric_channel_model"`
	ModelName     string  `json:"model_name" gorm:"type:varchar(191);not null;uniqueIndex:idx_live_metric_channel_model"`
	TtftEmaMs     float64 `json:"ttft_ema_ms" gorm:"default:0"`
	TpsEma        float64 `json:"tps_ema" gorm:"default:0"`
	SuccessEma    float64 `json:"success_ema" gorm:"default:1"`
	SampleCount   int     `json:"sample_count" gorm:"default:0"`
	SuccessCount  int     `json:"success_count" gorm:"default:0"`
	FailCount     int     `json:"fail_count" gorm:"default:0"`
	LastSuccessAt int64   `json:"last_success_at" gorm:"bigint;default:0"`
	LastErrorAt   int64   `json:"last_error_at" gorm:"bigint;default:0"`
	UpdatedAt     int64   `json:"updated_at" gorm:"bigint;default:0;index"`
}

func (ChannelLiveMetric) TableName() string {
	return "channel_live_metrics"
}

// ChannelLiveMetricSnapshot holds live in-memory EMA inference telemetry for a (channel, model) pair.
type ChannelLiveMetricSnapshot struct {
	ChannelId     int     `json:"channel_id"`
	ModelName     string  `json:"model_name"`
	TtftEmaMs     float64 `json:"ttft_ema_ms"`
	TpsEma        float64 `json:"tps_ema"`
	SuccessEma    float64 `json:"success_ema"`
	SampleCount   int     `json:"sample_count"`
	SuccessCount  int     `json:"success_count"`
	FailCount     int     `json:"fail_count"`
	LastSuccessAt int64   `json:"last_success_at"`
	LastErrorAt   int64   `json:"last_error_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

var (
	channelLiveMetricsMu    sync.RWMutex
	channelLiveMetrics      = make(map[channelModelMetricKey]*ChannelLiveMetricSnapshot)
	channelLiveMetricsDirty = make(map[channelModelMetricKey]struct{})
)

func updateLiveMetricEMA(prev float64, cur float64, alpha float64, hasPrev bool) float64 {
	if !hasPrev || prev <= 0 {
		return cur
	}
	return alpha*cur + (1-alpha)*prev
}

// RecordChannelLiveOutcome updates the sliding EMA telemetry (TTFT, TPS, success rate)
// for a single relay attempt on (channelId, modelName).
func RecordChannelLiveOutcome(channelId int, modelName string, success bool, ttftMs float64, tps float64) {
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" {
		return
	}
	now := time.Now().Unix()

	channelLiveMetricsMu.Lock()
	defer channelLiveMetricsMu.Unlock()

	key := channelModelMetricKey{ChannelId: channelId, ModelName: modelName}
	snap, exists := channelLiveMetrics[key]
	if !exists {
		snap = &ChannelLiveMetricSnapshot{
			ChannelId:  channelId,
			ModelName:  modelName,
			SuccessEma: 1.0,
		}
		channelLiveMetrics[key] = snap
	}

	snap.SampleCount++
	snap.UpdatedAt = now
	if success {
		snap.SuccessCount++
		snap.LastSuccessAt = now
		snap.SuccessEma = liveSuccessAlpha*1.0 + (1-liveSuccessAlpha)*snap.SuccessEma
		if !math.IsNaN(ttftMs) && !math.IsInf(ttftMs, 0) && ttftMs > 0 {
			snap.TtftEmaMs = updateLiveMetricEMA(snap.TtftEmaMs, ttftMs, liveTtftAlpha, snap.TtftEmaMs > 0)
		}
		if !math.IsNaN(tps) && !math.IsInf(tps, 0) && tps > 0 {
			snap.TpsEma = updateLiveMetricEMA(snap.TpsEma, tps, liveTpsAlpha, snap.TpsEma > 0)
		}
	} else {
		snap.FailCount++
		snap.LastErrorAt = now
		snap.SuccessEma = liveSuccessAlpha*0.0 + (1-liveSuccessAlpha)*snap.SuccessEma
	}
	channelLiveMetricsDirty[key] = struct{}{}
}

// GetChannelLiveMetric returns the cached live telemetry snapshot for (channelId, modelName),
// falling back to the normalized model name if no exact match exists.
func GetChannelLiveMetric(channelId int, modelName string) (ChannelLiveMetricSnapshot, bool) {
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" {
		return ChannelLiveMetricSnapshot{}, false
	}

	channelLiveMetricsMu.RLock()
	defer channelLiveMetricsMu.RUnlock()

	if snap, ok := channelLiveMetrics[channelModelMetricKey{ChannelId: channelId, ModelName: modelName}]; ok && snap != nil {
		return *snap, true
	}
	if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != "" && normalized != modelName {
		if snap, ok := channelLiveMetrics[channelModelMetricKey{ChannelId: channelId, ModelName: normalized}]; ok && snap != nil {
			return *snap, true
		}
	}
	return ChannelLiveMetricSnapshot{}, false
}

// GetChannelLiveMetricsForModel batch-loads live telemetry snapshots keyed by channelId for modelName.
func GetChannelLiveMetricsForModel(channelIds []int, modelName string) map[int]ChannelLiveMetricSnapshot {
	modelName = strings.TrimSpace(modelName)
	if len(channelIds) == 0 || modelName == "" {
		return nil
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)

	channelLiveMetricsMu.RLock()
	defer channelLiveMetricsMu.RUnlock()

	result := make(map[int]ChannelLiveMetricSnapshot, len(channelIds))
	for _, cid := range channelIds {
		if cid <= 0 {
			continue
		}
		if snap, ok := channelLiveMetrics[channelModelMetricKey{ChannelId: cid, ModelName: modelName}]; ok && snap != nil {
			result[cid] = *snap
			continue
		}
		if normalized != "" && normalized != modelName {
			if snap, ok := channelLiveMetrics[channelModelMetricKey{ChannelId: cid, ModelName: normalized}]; ok && snap != nil {
				result[cid] = *snap
			}
		}
	}
	return result
}

// GetChannelLiveMetricsByChannel returns all cached per-model telemetry snapshots for a channel.
func GetChannelLiveMetricsByChannel(channelId int) []ChannelLiveMetricSnapshot {
	if channelId <= 0 {
		return nil
	}
	channelLiveMetricsMu.RLock()
	defer channelLiveMetricsMu.RUnlock()

	out := make([]ChannelLiveMetricSnapshot, 0)
	for key, snap := range channelLiveMetrics {
		if key.ChannelId == channelId && snap != nil {
			out = append(out, *snap)
		}
	}
	return out
}

// GetAllChannelLiveMetrics returns a copy of all cached live telemetry snapshots.
func GetAllChannelLiveMetrics() []ChannelLiveMetricSnapshot {
	channelLiveMetricsMu.RLock()
	defer channelLiveMetricsMu.RUnlock()

	out := make([]ChannelLiveMetricSnapshot, 0, len(channelLiveMetrics))
	for _, snap := range channelLiveMetrics {
		if snap != nil {
			out = append(out, *snap)
		}
	}
	return out
}

// LoadChannelLiveMetricsFromDB restores persisted per-(channel, model) telemetry from the database into memory.
func LoadChannelLiveMetricsFromDB() error {
	if DB == nil {
		return nil
	}
	var rows []ChannelLiveMetric
	if err := DB.Order("updated_at DESC").Limit(50000).Find(&rows).Error; err != nil {
		return err
	}

	channelLiveMetricsMu.Lock()
	defer channelLiveMetricsMu.Unlock()

	for _, r := range rows {
		name := strings.TrimSpace(r.ModelName)
		if r.ChannelId <= 0 || name == "" {
			continue
		}
		key := channelModelMetricKey{ChannelId: r.ChannelId, ModelName: name}
		if existing, ok := channelLiveMetrics[key]; ok && existing != nil && existing.UpdatedAt >= r.UpdatedAt {
			continue
		}
		channelLiveMetrics[key] = &ChannelLiveMetricSnapshot{
			ChannelId:     r.ChannelId,
			ModelName:     name,
			TtftEmaMs:     r.TtftEmaMs,
			TpsEma:        r.TpsEma,
			SuccessEma:    r.SuccessEma,
			SampleCount:   r.SampleCount,
			SuccessCount:  r.SuccessCount,
			FailCount:     r.FailCount,
			LastSuccessAt: r.LastSuccessAt,
			LastErrorAt:   r.LastErrorAt,
			UpdatedAt:     r.UpdatedAt,
		}
	}
	return nil
}

// FlushChannelLiveMetrics persists any dirty in-memory live telemetry entries to the database.
func FlushChannelLiveMetrics() error {
	if DB == nil {
		return nil
	}

	channelLiveMetricsMu.Lock()
	if len(channelLiveMetricsDirty) == 0 {
		channelLiveMetricsMu.Unlock()
		return nil
	}
	dirtyKeys := channelLiveMetricsDirty
	channelLiveMetricsDirty = make(map[channelModelMetricKey]struct{})
	rows := make([]ChannelLiveMetric, 0, len(dirtyKeys))
	for key := range dirtyKeys {
		if snap, ok := channelLiveMetrics[key]; ok && snap != nil {
			rows = append(rows, ChannelLiveMetric{
				ChannelId:     snap.ChannelId,
				ModelName:     snap.ModelName,
				TtftEmaMs:     snap.TtftEmaMs,
				TpsEma:        snap.TpsEma,
				SuccessEma:    snap.SuccessEma,
				SampleCount:   snap.SampleCount,
				SuccessCount:  snap.SuccessCount,
				FailCount:     snap.FailCount,
				LastSuccessAt: snap.LastSuccessAt,
				LastErrorAt:   snap.LastErrorAt,
				UpdatedAt:     snap.UpdatedAt,
			})
		}
	}
	channelLiveMetricsMu.Unlock()

	if len(rows) == 0 {
		return nil
	}
	err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "model_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"ttft_ema_ms",
			"tps_ema",
			"success_ema",
			"sample_count",
			"success_count",
			"fail_count",
			"last_success_at",
			"last_error_at",
			"updated_at",
		}),
	}).CreateInBatches(rows, 100).Error
	if err != nil {
		channelLiveMetricsMu.Lock()
		for _, key := range func() []channelModelMetricKey {
			out := make([]channelModelMetricKey, 0, len(rows))
			for _, r := range rows {
				out = append(out, channelModelMetricKey{ChannelId: r.ChannelId, ModelName: r.ModelName})
			}
			return out
		}() {
			channelLiveMetricsDirty[key] = struct{}{}
		}
		channelLiveMetricsMu.Unlock()
		return err
	}
	return nil
}

// StartChannelLiveMetricSyncLoop periodically flushes dirty live telemetry metrics to the database.
func StartChannelLiveMetricSyncLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := FlushChannelLiveMetrics(); err != nil {
			common.SysError("failed to flush channel live metrics: " + err.Error())
		}
	}
}

// ResetChannelLiveMetrics clears in-memory live telemetry (primarily for tests).
func ResetChannelLiveMetrics() {
	channelLiveMetricsMu.Lock()
	defer channelLiveMetricsMu.Unlock()
	channelLiveMetrics = make(map[channelModelMetricKey]*ChannelLiveMetricSnapshot)
	channelLiveMetricsDirty = make(map[channelModelMetricKey]struct{})
}
