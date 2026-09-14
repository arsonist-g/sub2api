package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// clineProbeErrorFixture 还原上游真实报错形态：内部报错的 JSON 被当作字符串嵌在
// 外层 error 字段里，因此引号是转义过的。手工写转义容易把层数写错，这里用
// json.Marshal 生成，保证与线上响应一致。
func clineProbeErrorFixture(upstream string, inner string) string {
	errText := "inference request failed: failed to invoke model 'x/y' from " + upstream +
		": request failed with status 400: " + inner
	quoted, err := json.Marshal(errText)
	if err != nil {
		panic(err)
	}
	return `{"error":` + string(quoted) + `,"success":false}`
}

const clinePlannerInner = `{"error":{"message":"No available providers match the 'only' filter: __probe__. Available providers are: alibaba, baseten, deepinfra, deepseek, fireworks, gmicloud, modal, morph, novita, parasail, particle, relace, togetherai, wafer","type":"invalid_request_error"}}`

const clineDirectInner = `{"error":{"message":"No allowed providers are available for the selected model. Providers serving z-ai/glm-5.3-flash-20260826: deepinfra, relace, morph, wafer, but your request's provider.only preference permits only: __probe__.","code":404,"metadata":{"available_providers":["deepinfra","relace","morph","wafer"]}}}`

// clineDirectInnerWithoutStructuredList 去掉结构化字段，只留自然语言清单。
const clineDirectInnerTextOnly = `{"error":{"message":"No allowed providers are available for the selected model. Providers serving z-ai/glm-5.3-flash-20260826: deepinfra, relace, morph, wafer, but your request's provider.only preference permits only: __probe__.","code":404}}`

func TestExtractClineProbeProviders(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "planner 管道的自然语言清单",
			body: clineProbeErrorFixture("Vercel", clinePlannerInner),
			want: []string{"alibaba", "baseten", "deepinfra", "deepseek", "fireworks", "gmicloud", "modal", "morph", "novita", "parasail", "particle", "relace", "togetherai", "wafer"},
		},
		{
			name: "direct 管道的结构化清单",
			body: clineProbeErrorFixture("Openrouter", clineDirectInner),
			want: []string{"deepinfra", "relace", "morph", "wafer"},
		},
		{
			name: "direct 管道结构化字段缺失时回退到自然语言清单",
			body: clineProbeErrorFixture("Openrouter", clineDirectInnerTextOnly),
			want: []string{"deepinfra", "relace", "morph", "wafer"},
		},
		{
			name: "正常成功响应没有清单",
			body: `{"data":{"choices":[{"message":{"content":"OK"}}]},"success":true}`,
			want: nil,
		},
		{
			name: "空响应没有清单",
			body: "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, extractClineProbeProviders([]byte(tt.body)))
		})
	}
}

func clineTestAccount(prefs map[string]any) *Account {
	credentials := map[string]any{"api_key": "sk-test", "account_mode": AccountModeCoding}
	if prefs != nil {
		credentials["model_providers"] = prefs
	}
	return &Account{ID: 1, Platform: PlatformCline, Type: AccountTypeAPIKey, Credentials: credentials}
}

func TestApplyClineProviderPreference(t *testing.T) {
	body := []byte(`{"model":"cline-pass/deepseek-v4.1-flash","messages":[]}`)

	t.Run("planner 管道 only 只写 gateway", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/deepseek-v4.1-flash": map[string]any{"pipeline": ClinePipelinePlanner, "mode": "only", "providers": []any{"togetherai"}},
		})
		out := applyClineProviderPreference(account, body)
		require.Equal(t, []any{"togetherai"}, gjson.GetBytes(out, "providerOptions.gateway.only").Value())
		require.False(t, gjson.GetBytes(out, "provider").Exists())
	})

	t.Run("direct 管道 only 只写顶层 provider", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/deepseek-v4.1-flash": map[string]any{"pipeline": ClinePipelineDirect, "mode": "only", "providers": []any{"deepinfra"}},
		})
		out := applyClineProviderPreference(account, body)
		require.Equal(t, []any{"deepinfra"}, gjson.GetBytes(out, "provider.only").Value())
		require.False(t, gjson.GetBytes(out, "providerOptions").Exists())
	})

	t.Run("order 模式写 order 而非 only", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/deepseek-v4.1-flash": map[string]any{"pipeline": ClinePipelinePlanner, "mode": "order", "providers": []any{"baseten", "togetherai"}},
		})
		out := applyClineProviderPreference(account, body)
		require.Equal(t, []any{"baseten", "togetherai"}, gjson.GetBytes(out, "providerOptions.gateway.order").Value())
	})

	t.Run("管道未知时两种写法同时写", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/deepseek-v4.1-flash": map[string]any{"mode": "only", "providers": []any{"baseten"}},
		})
		out := applyClineProviderPreference(account, body)
		require.Equal(t, []any{"baseten"}, gjson.GetBytes(out, "providerOptions.gateway.only").Value())
		require.Equal(t, []any{"baseten"}, gjson.GetBytes(out, "provider.only").Value())
	})

	t.Run("auto 模式不注入", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/deepseek-v4.1-flash": map[string]any{"pipeline": ClinePipelinePlanner, "mode": "auto", "providers": []any{"baseten"}},
		})
		require.Equal(t, body, applyClineProviderPreference(account, body))
	})

	t.Run("未配置的模型不注入", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/glm-5.2": map[string]any{"mode": "only", "providers": []any{"zai"}},
		})
		require.Equal(t, body, applyClineProviderPreference(account, body))
	})

	t.Run("客户端已指定时不覆盖", func(t *testing.T) {
		account := clineTestAccount(map[string]any{
			"cline-pass/deepseek-v4.1-flash": map[string]any{"pipeline": ClinePipelinePlanner, "mode": "only", "providers": []any{"baseten"}},
		})
		clientBody := []byte(`{"model":"cline-pass/deepseek-v4.1-flash","providerOptions":{"gateway":{"only":["alibaba"]}}}`)
		require.Equal(t, clientBody, applyClineProviderPreference(account, clientBody))
	})

	t.Run("非 cline 账号不注入", func(t *testing.T) {
		account := &Account{ID: 2, Platform: PlatformOpenCode, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"model_providers": map[string]any{"cline-pass/deepseek-v4.1-flash": map[string]any{"mode": "only", "providers": []any{"baseten"}}},
		}}
		require.Equal(t, body, applyClineProviderPreference(account, body))
	})
}

func clineProbeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClineProviderProbeService_ClassifiesPipeline(t *testing.T) {
	successBody := `{"data":{"choices":[{"message":{"provider_metadata":{"gateway":{"routing":{"finalProvider":"openai-compatible-private"}}}}}]},"success":true}`

	tests := []struct {
		name         string
		responses    []*http.Response
		wantPipeline string
		wantCount    int
	}{
		{
			name: "首个请求就拿到 planner 清单",
			responses: []*http.Response{
				clineProbeResponse(http.StatusInternalServerError, clineProbeErrorFixture("Vercel", clinePlannerInner)),
			},
			wantPipeline: ClinePipelinePlanner,
			wantCount:    14,
		},
		{
			name: "gateway 形态被忽略后回落到 direct",
			responses: []*http.Response{
				clineProbeResponse(http.StatusOK, successBody),
				clineProbeResponse(http.StatusInternalServerError, clineProbeErrorFixture("Openrouter", clineDirectInner)),
			},
			wantPipeline: ClinePipelineDirect,
			wantCount:    4,
		},
		{
			name: "两种注入都被忽略即私有上游",
			responses: []*http.Response{
				clineProbeResponse(http.StatusOK, successBody),
				clineProbeResponse(http.StatusOK, successBody),
			},
			wantPipeline: ClinePipelinePrivate,
			wantCount:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: tt.responses}
			svc := &ClineProviderProbeService{httpUpstream: upstream, cfg: &config.Config{}}
			got, err := svc.ProbeModels(t.Context(), nil, "sk-test", DefaultClineBaseURL, []string{"cline-pass/deepseek-v4.1-flash"})
			require.NoError(t, err)
			probe := got["cline-pass/deepseek-v4.1-flash"]
			require.Equal(t, tt.wantPipeline, probe.Pipeline)
			require.Len(t, probe.Providers, tt.wantCount)
		})
	}
}
