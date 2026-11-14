package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserLogsSanitizePerChannelWhileAdminLogsPreserveThem(t *testing.T) {
	prevDB, prevLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Log{}))
	DB, LOG_DB = db, db
	t.Cleanup(func() {
		DB, LOG_DB = prevDB, prevLogDB
		require.NoError(t, sqlDB.Close())
	})

	enabledSetting := `{"upstream_privacy_sanitize":true}`
	disabledSetting := `{"upstream_privacy_sanitize":false}`
	require.NoError(t, db.Create(&Channel{Id: 42, Name: "secret-upstream-channel", Setting: &enabledSetting}).Error)
	require.NoError(t, db.Create(&Channel{Id: 43, Name: "public-channel", Setting: &disabledSetting}).Error)
	// Even after soft deletion of a sanitized channel, user logs for it must stay sanitized.
	require.NoError(t, db.Delete(&Channel{}, 42).Error)

	rawOtherSanitized := `{
		"request_path": "/v1/chat/completions",
		"is_model_mapped": true,
		"upstream_model_name": "m9/secret-custom",
		"response_model": {"requested_model":"gpt-4o","upstream_model":"m9/secret-custom","returned_model":"secret-upstream"},
		"po": {"fields": ["temperature"]},
		"request_conversion_chain": ["openai", "claude"],
		"final_request_format": "claude",
		"request_plugins": ["plugin-a"],
		"admin_info": {"use_channel": ["42"], "upstream_privacy_sanitize": true},
		"root_info": {"upstream_request_id": "up-root-1"}
	}`
	rawOtherLiveChannel := `{
		"request_path": "/v1/chat/completions",
		"is_model_mapped": true,
		"upstream_model_name": "m9/secret-custom",
		"response_model": {"requested_model":"gpt-4o","upstream_model":"m9/secret-custom","returned_model":"secret-upstream"},
		"po": {"fields": ["temperature"]},
		"request_conversion_chain": ["openai", "claude"],
		"final_request_format": "claude",
		"request_plugins": ["plugin-a"],
		"admin_info": {"use_channel": ["44"]},
		"root_info": {"upstream_request_id": "up-root-2"}
	}`
	enabledLiveSetting := `{"upstream_privacy_sanitize":true}`
	require.NoError(t, db.Create(&Channel{Id: 44, Name: "live-sanitized-channel", Setting: &enabledLiveSetting}).Error)

	userSanitizedLog := &Log{
		Type:              LogTypeError,
		ChannelId:         42,
		ChannelName:       "secret-upstream-channel",
		UpstreamRequestId: "up-req-42",
		RequestId:         "req-user-42",
		Content:           "Post https://secret-vendor.example.com/v1 failed",
		Other:             rawOtherSanitized,
	}
	userLiveSanitizedLog := &Log{
		Type:              LogTypeError,
		ChannelId:         44,
		ChannelName:       "live-sanitized-channel",
		UpstreamRequestId: "up-req-44",
		RequestId:         "req-user-44",
		Content:           "Post https://live-secret.example.com/v1 failed",
		Other:             rawOtherLiveChannel,
	}
	userRegularLog := &Log{
		Type:              LogTypeError,
		ChannelId:         43,
		ChannelName:       "public-channel",
		UpstreamRequestId: "up-req-43",
		RequestId:         "req-user-43",
		Content:           "normal error detail",
		Other:             rawOtherLiveChannel,
	}
	adminSanitizedLog := &Log{
		Type:              LogTypeError,
		ChannelId:         42,
		ChannelName:       "secret-upstream-channel",
		UpstreamRequestId: "up-req-42",
		RequestId:         "req-user-42",
		Content:           "Post https://secret-vendor.example.com/v1 failed",
		Other:             rawOtherSanitized,
	}

	formatUserLogs([]*Log{userSanitizedLog, userLiveSanitizedLog, userRegularLog}, 0)
	FormatAdminLogs([]*Log{adminSanitizedLog})

	// Live channel toggled ON retroactively sanitizes logs even without admin_info flag.
	assert.Equal(t, 0, userLiveSanitizedLog.ChannelId)
	assert.Equal(t, "", userLiveSanitizedLog.UpstreamRequestId)
	assert.Equal(t, "Upstream service error (request id: req-user-44)", userLiveSanitizedLog.Content)

	// Deleted sanitized channel hides all upstream & routing details via admin_info fallback.
	assert.Equal(t, 0, userSanitizedLog.ChannelId)
	assert.Equal(t, "", userSanitizedLog.ChannelName)
	assert.Equal(t, "", userSanitizedLog.UpstreamRequestId)
	assert.Equal(t, "Upstream service error (request id: req-user-42)", userSanitizedLog.Content)
	userOther, err := common.StrToMap(userSanitizedLog.Other)
	require.NoError(t, err)
	assert.Equal(t, "/v1/chat/completions", userOther["request_path"])
	for _, hiddenKey := range []string{
		"is_model_mapped",
		"upstream_model_name",
		"response_model",
		"po",
		"request_conversion_chain",
		"final_request_format",
		"request_plugins",
		"admin_info",
		"root_info",
	} {
		_, exists := userOther[hiddenKey]
		assert.False(t, exists, hiddenKey)
	}

	// Non-sanitized channel only performs standard baseline redaction (ChannelName & admin/root/audit_info).
	assert.Equal(t, 43, userRegularLog.ChannelId)
	assert.Equal(t, "", userRegularLog.ChannelName)
	assert.Equal(t, "normal error detail", userRegularLog.Content)
	regularOther, err := common.StrToMap(userRegularLog.Other)
	require.NoError(t, err)
	assert.Equal(t, true, regularOther["is_model_mapped"])
	assert.Contains(t, regularOther, "response_model")
	assert.NotContains(t, regularOther, "admin_info")

	// Admin view retains full routing & diagnostic details even for sanitized channels.
	assert.Equal(t, 42, adminSanitizedLog.ChannelId)
	assert.Equal(t, "secret-upstream-channel", adminSanitizedLog.ChannelName)
	assert.Equal(t, "up-req-42", adminSanitizedLog.UpstreamRequestId)
	assert.Contains(t, adminSanitizedLog.Content, "secret-vendor.example.com")
	adminOther, err := common.StrToMap(adminSanitizedLog.Other)
	require.NoError(t, err)
	assert.Equal(t, true, adminOther["is_model_mapped"])
	assert.Equal(t, "m9/secret-custom", adminOther["upstream_model_name"])
	assert.Contains(t, adminOther, "response_model")
	assert.Contains(t, adminOther, "admin_info")
	assert.NotContains(t, adminOther, "root_info")
}
