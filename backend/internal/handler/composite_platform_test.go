package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompositeTargetPlatformAllowedResolvesKnownAllowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.True(t, compositeTargetPlatformAllowed(c, nil, apiKey, "text-embedding-3-large", service.PlatformOpenAI))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformOpenAI, platform)
}

func TestOpenAICompatibleTextTargetAllowsCompositeProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providers := []struct {
		model    string
		platform string
	}{
		{model: "grok-4.3", platform: service.PlatformGrok},
		{model: "kimi-k2-thinking", platform: service.PlatformKimi},
		{model: "k3", platform: service.PlatformKimi},
		{model: "glm-5.2", platform: service.PlatformZhipu},
		{model: "deepseek-v3.2", platform: service.PlatformDeepseek},
	}
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/v1/responses/input_tokens", "/v1/messages/count_tokens"} {
		for _, provider := range providers {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", path, nil)
			apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

			require.True(t, openAICompatibleTextTargetAllowed(c, nil, apiKey, provider.model), "path=%s model=%s", path, provider.model)
			platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
			require.True(t, ok, "path=%s model=%s", path, provider.model)
			require.Equal(t, provider.platform, platform, "path=%s model=%s", path, provider.model)
		}
	}
}

// WS ingress 对 CN 账号既过不了 transport 过滤、HTTP 桥也没有 Responses 转换，
// 放行只会把明确的策略拒绝换成 "no available account"，因此 WS 白名单保持 openai+grok。
func TestResponsesWebSocketCompositePlatformGuardKeepsOpenAIAndGrokOnly(t *testing.T) {
	require.True(t, isResponsesWebSocketCompositePlatform(service.PlatformOpenAI))
	require.True(t, isResponsesWebSocketCompositePlatform(service.PlatformGrok))
	for _, platform := range []string{
		service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek,
		service.PlatformAnthropic, service.PlatformGemini,
	} {
		require.False(t, isResponsesWebSocketCompositePlatform(platform), "platform=%s", platform)
	}
}

func TestCompositeTargetPlatformAllowedRejectsWrongOrUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name  string
		model string
	}{
		{name: "wrong provider", model: "claude-sonnet-4-5"},
		{name: "unknown provider", model: "llama-4-maverick"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
			apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

			require.False(t, compositeTargetPlatformAllowed(c, nil, apiKey, tc.model, service.PlatformOpenAI))
		})
	}
}

func TestCompositeTargetPlatformResolvedRejectsUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.False(t, compositeTargetPlatformResolved(c, nil, apiKey, "llama-4-maverick"))
	_, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.False(t, ok)
}

func TestCompositeTargetPlatformResolvedAllowsConcreteGroupWithoutResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformAnthropic}}

	require.True(t, compositeTargetPlatformResolved(c, nil, apiKey, "llama-4-maverick"))
}

func TestOpenAIReasoningEffortPolicyForCompositeTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{
		Platform:           service.PlatformComposite,
		MaxReasoningEffort: "medium",
		ReasoningEffortMappings: []service.ReasoningEffortMapping{
			{From: "max", To: "xhigh"},
		},
	}
	apiKey := &service.APIKey{Group: group}
	body := []byte(`{"reasoning":{"effort":"max"}}`)

	openAICtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	openAICtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	openAICtx.Request = openAICtx.Request.WithContext(service.WithResolvedTargetPlatform(openAICtx.Request.Context(), service.PlatformOpenAI))
	got, changed, err := applyOpenAIReasoningEffortPolicyForRequest(openAICtx, apiKey, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"reasoning":{"effort":"medium"}}`, string(got))
	requested := service.RequestedReasoningEffortFromContext(openAICtx.Request.Context())
	require.NotNil(t, requested)
	require.Equal(t, "max", *requested)

	bindOpenAIReasoningEffortPolicyForMessagesRequest(openAICtx, apiKey, []byte(`{"output_config":{"effort":"max"}}`))
	bound, changed, err := service.ApplyOpenAIReasoningEffortPolicyFromContext(openAICtx.Request.Context(), body)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"reasoning":{"effort":"medium"}}`, string(bound))

	omittedCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	omittedCtx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	omittedCtx.Request = omittedCtx.Request.WithContext(service.WithResolvedTargetPlatform(omittedCtx.Request.Context(), service.PlatformOpenAI))
	bindOpenAIReasoningEffortPolicyForMessagesRequest(omittedCtx, apiKey, []byte(`{"model":"gpt-5"}`))
	omitted, changed, err := service.ApplyOpenAIReasoningEffortPolicyFromContext(omittedCtx.Request.Context(), body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, omitted)

	denyGroup := *group
	denyGroup.MaxReasoningEffortOverLimit = service.ReasoningEffortOverLimitDeny
	denyAPIKey := &service.APIKey{Group: &denyGroup}
	denyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	denyCtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	denyCtx.Request = denyCtx.Request.WithContext(service.WithResolvedTargetPlatform(denyCtx.Request.Context(), service.PlatformOpenAI))
	_, _, err = applyOpenAIReasoningEffortPolicyForRequest(denyCtx, denyAPIKey, body)
	require.Error(t, err)
	var overLimit *service.ReasoningEffortOverLimitError
	require.ErrorAs(t, err, &overLimit)

	grokCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	grokCtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	grokCtx.Request = grokCtx.Request.WithContext(service.WithResolvedTargetPlatform(grokCtx.Request.Context(), service.PlatformGrok))
	got, changed, err = applyOpenAIReasoningEffortPolicyForRequest(grokCtx, apiKey, body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, got)
}

// fakeCompositeEntryResolver 模拟入口侧 composite 路由解析（显式路由表 /
// 账号模型目录），锁定 handler 入口校验与调度决策链的一致性。
type fakeCompositeEntryResolver struct {
	decision service.CompositeRouteDecision
	ok       bool
}

func (f fakeCompositeEntryResolver) ResolveCompositeRouteDecisionForEntry(
	_ context.Context, _ *service.Group, _, _ string,
) (service.CompositeRouteDecision, bool) {
	return f.decision, f.ok
}

// composite 分组 + OpenAI 兼容端点 + OpenCode 目标：入口须放行（修复前
// detector 不识别 opencode 独占模型名 + 白名单缺 opencode，请求在协议转换
// 之前就被 400 拒绝）。
func TestOpenAICompatibleTextTargetAllowsOpenCodeViaEntryResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/openai/v1/chat/completions", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	resolver := fakeCompositeEntryResolver{ok: true, decision: service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceDetector,
		TargetPlatform: service.PlatformOpenCode,
		UpstreamModel:  "qwen3.8-max",
	}}

	require.True(t, openAICompatibleTextTargetAllowed(c, resolver, apiKey, "qwen3.8-max"))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformOpenCode, platform)
	upstream, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, "qwen3.8-max", upstream)
}

// 显式路由决策（路由表/账号目录）优先于 detector：同名模型（detector 会判
// zhipu 的 glm-5.3）被路由表指向 opencode 时，入口不得让 detector 抢先绑定
// 原生平台导致路由表被短路。
func TestCompositeEntryResolverOverridesDetectorForSameNameModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/openai/v1/responses", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	resolver := fakeCompositeEntryResolver{ok: true, decision: service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		TargetPlatform: service.PlatformOpenCode,
		UpstreamModel:  "glm-5.3",
	}}

	require.True(t, openAICompatibleTextTargetAllowed(c, resolver, apiKey, "glm-5.3"))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformOpenCode, platform)
}

// resolver 在位但未命中任何决策（路由表/账号目录/detector 全空）：保持
// fail-closed，不得因 resolver 缺席而放宽。
func TestCompositeEntryResolverMissKeepsFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/openai/v1/chat/completions", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.False(t, openAICompatibleTextTargetAllowed(c, fakeCompositeEntryResolver{}, apiKey, "llama-4-maverick"))
	_, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.False(t, ok)
}

func TestClientRequestedModelUsesCompositePublicModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "public-alias",
		TargetPlatform: service.PlatformOpenAI,
		UpstreamModel:  "gpt-5",
	}))

	input := buildContentModerationInput(c, nil, middleware2.AuthSubject{UserID: 42}, service.ContentModerationProtocolOpenAIChat, "gpt-5", nil)
	require.Equal(t, "public-alias", input.Model)
	require.Equal(t, service.PlatformOpenAI, input.Provider)

	fields := clientRequestedUsageFields(c, service.ChannelMappingResult{MappedModel: "gpt-5"}, "gpt-5", "gpt-5")
	require.Equal(t, "public-alias", fields.OriginalModel)
	require.Equal(t, "public-alias", fields.ChannelMappedModel)
	require.Equal(t, "public-alias\u2192gpt-5", fields.ModelMappingChain)
}
