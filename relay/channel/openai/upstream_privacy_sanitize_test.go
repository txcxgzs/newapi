package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenaiHandlerSanitizesModelAndFingerprintPerChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevTimeout := appconstant.StreamingTimeout
	if appconstant.StreamingTimeout <= 0 {
		appconstant.StreamingTimeout = 30
	}
	t.Cleanup(func() {
		appconstant.StreamingTimeout = prevTimeout
	})

	t.Run("non-stream chat completion enabled", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, appconstant.ContextKeyChannelSetting, dto.ChannelSettings{UpstreamPrivacySanitize: true})

		info := &relaycommon.RelayInfo{
			OriginModelName: "gpt-4o",
			RelayFormat:     types.RelayFormatOpenAI,
			RelayMode:       relayconstant.RelayModeChatCompletions,
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "m7/vendor-gpt-4o",
				ChannelSetting:    dto.ChannelSettings{UpstreamPrivacySanitize: true},
			},
		}
		respBody := `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"vendor-actual-model","system_fingerprint":"fp_secret_vendor","provider":"secret-vendor","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "Openai-Organization": []string{"org-secret"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}

		usage, apiErr := OpenaiHandler(c, info, resp)
		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		assert.Equal(t, 5, usage.TotalTokens)

		// Admin observability still captures the true returned upstream model.
		require.NotNil(t, info.ResponseModel)
		assert.Equal(t, "vendor-actual-model", info.ResponseModel.ReturnedModel)

		// Client receives sanitized body & headers.
		assert.Empty(t, recorder.Header().Get("Openai-Organization"))
		var out map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &out))
		assert.Equal(t, "gpt-4o", out["model"])
		_, hasFP := out["system_fingerprint"]
		assert.False(t, hasFP)
		_, hasProvider := out["provider"]
		assert.False(t, hasProvider)
	})

	t.Run("non-stream chat completion disabled stays untouched", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, appconstant.ContextKeyChannelSetting, dto.ChannelSettings{UpstreamPrivacySanitize: false})

		info := &relaycommon.RelayInfo{
			OriginModelName: "gpt-4o",
			RelayFormat:     types.RelayFormatOpenAI,
			RelayMode:       relayconstant.RelayModeChatCompletions,
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "m7/vendor-gpt-4o",
				ChannelSetting:    dto.ChannelSettings{UpstreamPrivacySanitize: false},
			},
		}
		respBody := `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"vendor-actual-model","system_fingerprint":"fp_secret_vendor","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "Openai-Organization": []string{"org-secret"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}

		usage, apiErr := OpenaiHandler(c, info, resp)
		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		assert.Equal(t, "org-secret", recorder.Header().Get("Openai-Organization"))
		var out map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &out))
		assert.Equal(t, "vendor-actual-model", out["model"])
	})

	t.Run("stream chat completion enabled", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, appconstant.ContextKeyChannelSetting, dto.ChannelSettings{UpstreamPrivacySanitize: true})

		info := &relaycommon.RelayInfo{
			OriginModelName: "gpt-4o",
			RelayFormat:     types.RelayFormatOpenAI,
			RelayMode:       relayconstant.RelayModeChatCompletions,
			IsStream:        true,
			DisablePing:     true,
			StartTime:       time.Now(),
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "m7/vendor-gpt-4o",
				ChannelSetting:    dto.ChannelSettings{UpstreamPrivacySanitize: true},
			},
		}
		streamBody := strings.Join([]string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"vendor-actual-model","system_fingerprint":"fp_secret","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
			"",
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"vendor-actual-model","system_fingerprint":"fp_secret","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
			"",
			`data: [DONE]`,
			"",
		}, "\n")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"up-req-secret"}},
			Body:       io.NopCloser(strings.NewReader(streamBody)),
		}

		usage, apiErr := OaiStreamHandler(c, info, resp)
		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		require.NotNil(t, info.ResponseModel)
		assert.Equal(t, "vendor-actual-model", info.ResponseModel.ReturnedModel)

		body := recorder.Body.String()
		assert.Empty(t, recorder.Header().Get("X-Request-Id"))
		assert.Contains(t, body, `"model":"gpt-4o"`)
		assert.NotContains(t, body, "vendor-actual-model")
		assert.NotContains(t, body, "fp_secret")
	})

	t.Run("non-stream responses api enabled", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		common.SetContextKey(c, appconstant.ContextKeyChannelSetting, dto.ChannelSettings{UpstreamPrivacySanitize: true})

		info := &relaycommon.RelayInfo{
			OriginModelName: "gpt-4.1",
			RelayFormat:     types.RelayFormatOpenAIResponses,
			RelayMode:       relayconstant.RelayModeResponses,
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "mapped-4.1",
				ChannelSetting:    dto.ChannelSettings{UpstreamPrivacySanitize: true},
			},
		}
		respBody := `{"id":"resp_1","object":"response","created_at":1700000000,"model":"mapped-4.1","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}

		usage, apiErr := OaiResponsesHandler(c, info, resp)
		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		require.NotNil(t, info.ResponseModel)
		assert.Equal(t, "mapped-4.1", info.ResponseModel.ReturnedModel)

		var out map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &out))
		assert.Equal(t, "gpt-4.1", out["model"])
	})
}
