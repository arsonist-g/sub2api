//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// opencodeSessionUUIDPattern 校验派生会话 ID 呈 UUIDv4 形态（与 OpenCode
// 官方客户端的取值格式对齐）。
var opencodeSessionUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func newOpenCodeSessionHeaderContext(t *testing.T, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func TestApplyOpenCodeUpstreamSessionHeader_NonOpenCodePlatformNoop(t *testing.T) {
	header := http.Header{}
	applyOpenCodeUpstreamSessionHeader(header, opencodeAccount(AccountModeCoding), newOpenCodeSessionHeaderContext(t, nil), []byte(`{"model":"grok-4.6"}`))
	require.NotEmpty(t, header.Get(openCodeNativeSessionHeader))

	foreign := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey}
	foreignHeader := http.Header{}
	applyOpenCodeUpstreamSessionHeader(foreignHeader, foreign, newOpenCodeSessionHeaderContext(t, nil), []byte(`{"model":"kimi-k3"}`))
	require.Empty(t, foreignHeader.Get(openCodeNativeSessionHeader))
}

func TestApplyOpenCodeUpstreamSessionHeader_StablePerConversation(t *testing.T) {
	account := opencodeAccount(AccountModeCoding)
	c := newOpenCodeSessionHeaderContext(t, map[string]string{"X-Opencode-Session": "sess-alpha"})
	c.Set("api_key", &APIKey{ID: 7})

	first := http.Header{}
	applyOpenCodeUpstreamSessionHeader(first, account, c, []byte(`{"model":"grok-4.6"}`))
	second := http.Header{}
	applyOpenCodeUpstreamSessionHeader(second, account, c, []byte(`{"model":"grok-4.6","input":[{"role":"user","content":"next turn"}]}`))

	require.NotEmpty(t, first.Get(openCodeNativeSessionHeader))
	require.Regexp(t, opencodeSessionUUIDPattern, first.Get(openCodeNativeSessionHeader))
	require.Equal(t, first.Get(openCodeNativeSessionHeader), second.Get(openCodeNativeSessionHeader),
		"同一会话的多轮请求必须派生同一会话 ID")
	// 客户端原始会话 ID 不透传（单向派生）。
	require.NotEqual(t, "sess-alpha", first.Get(openCodeNativeSessionHeader))
}

func TestApplyOpenCodeUpstreamSessionHeader_SeedSources(t *testing.T) {
	account := opencodeAccount(AccountModeCoding)

	fromNativeHeader := http.Header{}
	applyOpenCodeUpstreamSessionHeader(fromNativeHeader, account,
		newOpenCodeSessionHeaderContext(t, map[string]string{"X-Opencode-Session": "seed-1"}), nil)

	fromClaudeCodeHeader := http.Header{}
	applyOpenCodeUpstreamSessionHeader(fromClaudeCodeHeader, account,
		newOpenCodeSessionHeaderContext(t, map[string]string{"X-Claude-Code-Session-Id": "seed-2"}), nil)

	fromPromptCacheKey := http.Header{}
	applyOpenCodeUpstreamSessionHeader(fromPromptCacheKey, account,
		newOpenCodeSessionHeaderContext(t, nil), []byte(`{"prompt_cache_key":"seed-3"}`))

	fromMetadataUserID := http.Header{}
	legacyUserID := "user_" + strings.Repeat("ab", 32) + "_account__session_" + "0f14d0ab-9605-4a62-a9e4-52626a5a2c11"
	applyOpenCodeUpstreamSessionHeader(fromMetadataUserID, account,
		newOpenCodeSessionHeaderContext(t, nil), []byte(`{"metadata":{"user_id":"`+legacyUserID+`"}}`))

	fromContentSeed := http.Header{}
	applyOpenCodeUpstreamSessionHeader(fromContentSeed, account,
		newOpenCodeSessionHeaderContext(t, nil), []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}]}`))

	for name, header := range map[string]http.Header{
		"native header":      fromNativeHeader,
		"claude code header": fromClaudeCodeHeader,
		"prompt_cache_key":   fromPromptCacheKey,
		"metadata.user_id":   fromMetadataUserID,
		"content seed":       fromContentSeed,
	} {
		require.Regexp(t, opencodeSessionUUIDPattern, header.Get(openCodeNativeSessionHeader), name)
	}
	// 不同种子派生不同 ID；内容种子与显式种子也不相同。
	require.NotEqual(t, fromNativeHeader.Get(openCodeNativeSessionHeader), fromClaudeCodeHeader.Get(openCodeNativeSessionHeader))
	require.NotEqual(t, fromNativeHeader.Get(openCodeNativeSessionHeader), fromContentSeed.Get(openCodeNativeSessionHeader))
}

func TestApplyOpenCodeUpstreamSessionHeader_IsolatesAPIKeys(t *testing.T) {
	account := opencodeAccount(AccountModeCoding)

	keyA := newOpenCodeSessionHeaderContext(t, map[string]string{"X-Opencode-Session": "shared-session"})
	keyA.Set("api_key", &APIKey{ID: 1})
	keyB := newOpenCodeSessionHeaderContext(t, map[string]string{"X-Opencode-Session": "shared-session"})
	keyB.Set("api_key", &APIKey{ID: 2})

	headerA, headerB := http.Header{}, http.Header{}
	applyOpenCodeUpstreamSessionHeader(headerA, account, keyA, nil)
	applyOpenCodeUpstreamSessionHeader(headerB, account, keyB, nil)

	require.NotEqual(t, headerA.Get(openCodeNativeSessionHeader), headerB.Get(openCodeNativeSessionHeader),
		"多租户下同名客户端会话 ID 不得在上游碰撞")
}

func TestApplyOpenCodeUpstreamSessionHeader_FallbackAndExistingHeader(t *testing.T) {
	account := opencodeAccount(AccountModeCoding)
	account.ID = 4242

	// 所有信号缺失：退化为账号级固定 ID，头仍必须存在。
	fallback := http.Header{}
	applyOpenCodeUpstreamSessionHeader(fallback, account, newOpenCodeSessionHeaderContext(t, nil), nil)
	require.Regexp(t, opencodeSessionUUIDPattern, fallback.Get(openCodeNativeSessionHeader))

	// 已有同名头（管理员显式配置）不覆盖。
	overridden := http.Header{}
	overridden.Set(openCodeNativeSessionHeader, "operator-configured")
	applyOpenCodeUpstreamSessionHeader(overridden, account, newOpenCodeSessionHeaderContext(t, nil), nil)
	require.Equal(t, "operator-configured", overridden.Get(openCodeNativeSessionHeader))
}

// TestForwardAsAnthropic_OpenCodeResponsesCarriesSessionHeader 验证 OpenCode
// 账号（api_protocol=responses）的 Responses 出站链路注入 X-Opencode-Session
// （OpenAI 网关 buildUpstreamRequest）。
func TestForwardAsAnthropic_OpenCodeResponsesCarriesSessionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"grok-4.6","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_oc","object":"response","model":"grok-4.6","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_oc_session"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := opencodeAccount(AccountModeCoding)
	account.Credentials["api_protocol"] = APIProtocolResponses
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
		openai_compat.ExtraKeyResponsesSupported: true,
	}

	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/responses"))
	require.Regexp(t, opencodeSessionUUIDPattern, upstream.lastReq.Header.Get("X-Opencode-Session"),
		"OpenCode Responses 出站请求必须携带派生会话 ID")
}

// TestForwardAsAnthropic_OpenCodeNativeAnthropicCarriesSessionHeader 验证
// OpenCode adaptive 账号的 CN Anthropic 直通链路（Claude Code → /zen/go/v1/messages）
// 注入 X-Opencode-Session（buildNativeAnthropicUpstreamRequest）。
func TestForwardAsAnthropic_OpenCodeNativeAnthropicCarriesSessionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"minimax-m3","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamJSON := `{"id":"msg_oc","type":"message","role":"assistant","model":"minimax-m3","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_oc_native_session"}},
		Body:       io.NopCloser(strings.NewReader(upstreamJSON)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := opencodeAccount(AccountModeCoding)
	account.Credentials["api_protocol"] = APIProtocolAdaptive

	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/v1/messages"),
		"adaptive 账号 Anthropic 入站应直通 /zen/go/v1/messages，got %s", upstream.lastReq.URL.String())
	require.Regexp(t, opencodeSessionUUIDPattern, upstream.lastReq.Header.Get("X-Opencode-Session"),
		"OpenCode 原生 Anthropic 出站请求必须携带派生会话 ID")
}

// TestForwardAsAnthropic_OpenCodeChatCompletionsCarriesSessionHeader 验证
// OpenCode 账号的 CC 直转链路注入 X-Opencode-Session（sendCCUpstreamRequest）。
func TestForwardAsAnthropic_OpenCodeChatCompletionsCarriesSessionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"glm-5.3","max_tokens":8,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	ccStream := strings.Join([]string{
		`data: {"id":"chatcmpl_oc","object":"chat.completion.chunk","model":"glm-5.3","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_oc_cc_session"}},
		Body:       io.NopCloser(strings.NewReader(ccStream)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := opencodeAccount(AccountModeCoding)
	account.Credentials["base_url"] = "https://opencode.ai/zen/go/v1"
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
	}

	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/chat/completions"))
	require.Regexp(t, opencodeSessionUUIDPattern, upstream.lastReq.Header.Get("X-Opencode-Session"),
		"OpenCode Chat Completions 出站请求必须携带派生会话 ID")
}

// TestGatewayService_OpenCodeAnthropicCarriesSessionHeader 验证 Anthropic 网关
// 出站链路（/zen/go/v1/messages）注入 X-Opencode-Session。
func TestGatewayService_OpenCodeAnthropicCarriesSessionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	svc := &GatewayService{cfg: rawChatCompletionsTestConfig()}
	account := opencodeAccount(AccountModeCoding)
	account.Type = AccountTypeAPIKey
	account.Credentials["base_url"] = "https://opencode.ai/zen/go"
	body := []byte(`{"model":"minimax-m3","messages":[{"role":"user","content":"hi"}]}`)

	req, _, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "ocsk-test", "api_key", "minimax-m3", true, false)
	require.NoError(t, err)
	require.Contains(t, req.URL.String(), "opencode.ai/zen/go/v1/messages")
	require.Regexp(t, opencodeSessionUUIDPattern, req.Header.Get("X-Opencode-Session"),
		"OpenCode Anthropic 出站请求必须携带派生会话 ID")
}
