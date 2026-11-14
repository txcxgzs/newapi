package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeClientRelayError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctxEnabled, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctxEnabled, constant.ContextKeyChannelSetting, dto.ChannelSettings{UpstreamPrivacySanitize: true})
	assert.True(t, common.ShouldSanitizeUpstreamPrivacy(ctxEnabled))

	ctxDisabled, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctxDisabled, constant.ContextKeyChannelSetting, dto.ChannelSettings{UpstreamPrivacySanitize: false})
	assert.False(t, common.ShouldSanitizeUpstreamPrivacy(ctxDisabled))

	upstreamErr := types.NewOpenAIError(
		errors.New(`Post "https://secret-relay.example.com/v1/chat/completions?token=abc": unauthorized on channel_id=12`),
		types.ErrorCodeDoRequestFailed,
		http.StatusBadGateway,
	)

	sanitized := sanitizeClientRelayError(upstreamErr, "req-local-999")
	openAIErr := sanitized.ToOpenAIError("https://secret-relay.example.com")
	assert.Equal(t, http.StatusBadGateway, sanitized.StatusCode)
	assert.Equal(t, "Upstream service error (status 502) (request id: req-local-999)", openAIErr.Message)
	assert.NotContains(t, openAIErr.Message, "secret-relay")
	assert.NotContains(t, openAIErr.Message, ".com")
	assert.NotContains(t, openAIErr.Message, "token")
	assert.NotContains(t, openAIErr.Message, "channel_id")

	// Original error must remain intact for server & admin logs.
	assert.Contains(t, upstreamErr.Error(), "secret-relay.example.com")

	// Safe local quota error should retain helpful user message.
	quotaErr := types.NewErrorWithStatusCode(
		errors.New("insufficient quota"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)
	kept := sanitizeClientRelayError(quotaErr, "req-local-1000")
	assert.Equal(t, "insufficient quota (request id: req-local-1000)", kept.Error())
}
