package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestShouldCopyUpstreamHeaderSanitizePerChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cEnabled, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(cEnabled, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		UpstreamPrivacySanitize: true,
	})

	assert.False(t, ShouldCopyUpstreamHeader(cEnabled, common.RequestIdKey, []string{"up-req-123"}))
	assert.Equal(t, "up-req-123", cEnabled.GetString(common.UpstreamRequestIdKey))
	assert.False(t, ShouldCopyUpstreamHeader(cEnabled, "Content-Length", []string{"42"}))

	// Safe headers stay allowed.
	for _, header := range []string{"Content-Type", "Cache-Control", "Connection", "Transfer-Encoding", "X-Accel-Buffering", "Content-Disposition"} {
		assert.True(t, ShouldCopyUpstreamHeader(cEnabled, header, []string{"val"}), header)
	}

	// Upstream-identifying headers are blocked when channel enables sanitization.
	for _, header := range []string{"Openai-Organization", "Openai-Project", "X-Request-Id", "Server", "CF-Ray", "X-Upstream-Channel"} {
		assert.False(t, ShouldCopyUpstreamHeader(cEnabled, header, []string{"secret"}), header)
	}

	// A channel without sanitization keeps normal header pass-through.
	cDisabled, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(cDisabled, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		UpstreamPrivacySanitize: false,
	})
	assert.True(t, ShouldCopyUpstreamHeader(cDisabled, "Openai-Organization", []string{"org-secret"}))
	assert.True(t, ShouldCopyUpstreamHeader(cDisabled, "X-Request-Id", []string{"req-secret"}))
}
