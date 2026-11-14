package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelLiveMetricFlushAndReload(t *testing.T) {
	originalDB := DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelLiveMetric{}))

	DB = db
	ResetChannelLiveMetrics()
	t.Cleanup(func() {
		ResetChannelLiveMetrics()
		DB = originalDB
	})

	RecordChannelLiveOutcome(101, "gpt-4o", true, 320, 68.5)
	RecordChannelLiveOutcome(101, "gpt-4o", true, 280, 72.5)
	RecordChannelLiveOutcome(102, "gpt-4o", false, 1900, 0)

	require.NoError(t, FlushChannelLiveMetrics())

	var rows []ChannelLiveMetric
	require.NoError(t, DB.Order("channel_id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, 101, rows[0].ChannelId)
	assert.Equal(t, 2, rows[0].SuccessCount)

	// Simulate process restart: wipe in-memory map, then reload from DB
	ResetChannelLiveMetrics()
	_, okBefore := GetChannelLiveMetric(101, "gpt-4o")
	assert.False(t, okBefore)

	require.NoError(t, LoadChannelLiveMetricsFromDB())

	restored101, ok101 := GetChannelLiveMetric(101, "gpt-4o")
	require.True(t, ok101)
	assert.Equal(t, 2, restored101.SuccessCount)
	assert.InDelta(t, 310.0, restored101.TtftEmaMs, 0.01)
	assert.InDelta(t, 69.5, restored101.TpsEma, 0.01)

	restored102, ok102 := GetChannelLiveMetric(102, "gpt-4o")
	require.True(t, ok102)
	assert.Equal(t, 1, restored102.FailCount)
	assert.Less(t, restored102.SuccessEma, 1.0)
}
