//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// opencodeAccount 构造 OpenCode 账号（coding = Go 订阅，payg = 按量 Zen）。
func opencodeAccount(mode string) *Account {
	return &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":      "ocsk-test",
			"account_mode": mode,
		},
	}
}

// TestOpenCodeDefaultBaseURLs 验证按 account_mode 选择 Go 订阅 / 按量 Zen 默认端点。
func TestOpenCodeDefaultBaseURLs(t *testing.T) {
	t.Parallel()

	coding := opencodeAccount(AccountModeCoding)
	require.Equal(t, "https://opencode.ai/zen/go/v1", coding.GetOpenAIBaseURL())
	require.True(t, coding.SupportsNativeCNResponses())

	// 前端建号默认写 adaptive；显式 responses / adaptive 才走原生 Responses 转发。
	coding.Credentials["api_protocol"] = APIProtocolAdaptive
	require.True(t, coding.UsesNativeCNResponses())
	coding.Credentials["api_protocol"] = APIProtocolChatCompletions
	require.False(t, coding.UsesNativeCNResponses())

	payg := opencodeAccount(AccountModePayG)
	require.Equal(t, "https://opencode.ai/zen/v1", payg.GetOpenAIBaseURL())

	anthropic := opencodeAccount(AccountModeCoding)
	anthropic.Credentials["api_protocol"] = APIProtocolAnthropic
	require.Equal(t, "https://opencode.ai/zen/go", anthropic.GetAnthropicProtocolBaseURL())
	// anthropic 协议账号的 models 同步等协议族路径回退 chat base。
	require.Equal(t, "https://opencode.ai/zen/go/v1", anthropic.GetOpenAIFormatBaseURL())

	paygAnthropic := opencodeAccount(AccountModePayG)
	paygAnthropic.Credentials["api_protocol"] = APIProtocolAnthropic
	require.Equal(t, "https://opencode.ai/zen", paygAnthropic.GetAnthropicProtocolBaseURL())
}

// TestOpenCodeCodingPlanProvider 识别规则与 cc-switch detect_provider 对齐：
// opencode.ai/zen/go 命中 Go 订阅；按量 /zen/v1 刻意不命中（无用量 API）。
func TestOpenCodeCodingPlanProvider(t *testing.T) {
	t.Parallel()

	// 显式 base_url（含 anthropic 协议 base）也按子串识别。
	anthropicBase := opencodeAccount(AccountModeCoding)
	anthropicBase.Credentials["base_url"] = "https://opencode.ai/zen/go"
	require.Equal(t, PlatformOpenCode, anthropicBase.GetCodingPlanProvider())

	payg := opencodeAccount(AccountModePayG)
	payg.Credentials["base_url"] = "https://opencode.ai/zen/v1"
	require.Equal(t, "", payg.GetCodingPlanProvider()) // payg 模式直接短路

	zenPaygCodingMode := opencodeAccount(AccountModeCoding)
	zenPaygCodingMode.Credentials["base_url"] = "https://opencode.ai/zen/v1"
	require.Equal(t, "", zenPaygCodingMode.GetCodingPlanProvider()) // 子串不含 /zen/go
}

// TestOpenCodeUsageURL 统一剥尾部 /v1 后拼回 /v1/usage，协议切换不影响端点。
func TestOpenCodeUsageURL(t *testing.T) {
	t.Parallel()
	require.Equal(t, "https://opencode.ai/zen/go/v1/usage", opencodeUsageURL("https://opencode.ai/zen/go/v1"))
	require.Equal(t, "https://opencode.ai/zen/go/v1/usage", opencodeUsageURL("https://opencode.ai/zen/go"))
	require.Equal(t, "https://opencode.ai/zen/go/v1/usage", opencodeUsageURL("https://opencode.ai/zen/go/v1/"))
}

// TestParseOpenCodeUsageTiers 三窗口防御式解析。
func TestParseOpenCodeUsageTiers(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"usage": {
			"rolling":  {"status": "ok",            "percent": 42,  "resetsAt": "2026-09-04T15:00:00Z"},
			"weekly":   {"status": "rate-limited",   "percent": 100, "resetsAt": "2026-09-07T00:00:00Z"},
			"monthly":  {"status": "ok",            "percent": 73,  "resetsAt": "2026-09-30T00:00:00Z"}
		}
	}`)
	tiers := parseOpenCodeUsageTiers(body)
	require.Len(t, tiers, 3)
	require.Equal(t, "5h", tiers[0].Window)
	require.InDelta(t, 42.0, tiers[0].UsedPercent, 1e-9)
	require.Equal(t, "2026-09-04T15:00:00Z", tiers[0].ResetAt)
	require.Equal(t, "weekly", tiers[1].Window)
	require.InDelta(t, 100.0, tiers[1].UsedPercent, 1e-9)
	require.Equal(t, "monthly", tiers[2].Window)
	require.InDelta(t, 73.0, tiers[2].UsedPercent, 1e-9)
}

// TestParseOpenCodeUsageTiers_PercentZeroDropsPlaceholderReset percent=0 时
// resetsAt 是「now+窗口时长」占位值，必须丢弃。
func TestParseOpenCodeUsageTiers_PercentZeroDropsPlaceholderReset(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"usage": {
			"rolling": {"status": "ok", "percent": 0, "resetsAt": "2026-09-04T05:00:00Z"}
		}
	}`)
	tiers := parseOpenCodeUsageTiers(body)
	require.Len(t, tiers, 1)
	require.Empty(t, tiers[0].ResetAt)
}

// TestParseOpenCodeUsageTiers_DefensiveSkip 缺失 / 不可解析窗口跳过；全部缺失 → nil。
func TestParseOpenCodeUsageTiers_DefensiveSkip(t *testing.T) {
	t.Parallel()
	partial := parseOpenCodeUsageTiers([]byte(`{"usage":{"rolling":{"status":"ok"},"weekly":{"percent":55}}}`))
	require.Len(t, partial, 1)
	require.Equal(t, "weekly", partial[0].Window)

	require.Nil(t, parseOpenCodeUsageTiers([]byte(`{}`)))
	require.Nil(t, parseOpenCodeUsageTiers([]byte(`{"usage":"not-an-object"}`)))
}

// TestOpenCodeThresholdCandidates 含 monthly 快照窗口。
func TestOpenCodeThresholdCandidates(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCode,
		Extra: map[string]any{
			"opencode_5h_used_percent":      95.0,
			"opencode_5h_reset_at":          "2099-01-01T00:00:00Z",
			"opencode_monthly_used_percent": 60.0,
			"opencode_monthly_reset_at":     "2099-02-01T00:00:00Z",
		},
	}
	cands := cnProviderThresholdCandidates(account, PlatformOpenCode)
	var present []string
	for _, c := range cands {
		if c != nil {
			present = append(present, c.window)
		}
	}
	require.Equal(t, []string{"5h", "monthly"}, present)

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformOpenCode: 90}, time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC))
	require.True(t, decision.ShouldPause)
	require.Equal(t, "5h", decision.Window)
}

// TestOpenCodeModelAPIProtocol 模型→协议端点组映射（官方文档分组）。
func TestOpenCodeModelAPIProtocol(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model string
		want  string
	}{
		{"muse-spark-1.3-contributor", APIProtocolResponses},
		{"muse-spark-1.2-contributor", APIProtocolResponses},
		{"gpt-5.6-luna", APIProtocolResponses},
		{"grok-4.6", APIProtocolResponses},
		{"minimax-m3", APIProtocolAnthropic},
		{"qwen3.8-flash", APIProtocolAnthropic},
		{"qwen3.6-plus", APIProtocolAnthropic},
		{"deepseek-v4-flash", APIProtocolChatCompletions},
		{"glm-5.3-flash", APIProtocolChatCompletions},
		{"GPT-5.6-LUNA", APIProtocolResponses}, // 大小写不敏感
		{" unknown-model ", APIProtocolChatCompletions},
		{"", APIProtocolChatCompletions},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, OpenCodeModelAPIProtocol(tc.model))
		})
	}
}

// TestClampOpenCodeEffortLevel 钳制规则：钳到不小于请求值的最近合法档，
// 请求超出最高档取最高档，无表 / 未知值返回空串。
func TestClampOpenCodeEffortLevel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		requested string
		model     string
		want      string
	}{
		{"glm-5.2 medium 钳上到 high", "medium", "glm-5.2", "high"},
		{"glm-5.2 low 钳上到 high", "low", "glm-5.2", "high"},
		{"glm-5.2 high 保持", "high", "glm-5.2", "high"},
		{"glm-5.2 ultra 钳到最高档 max", "ultra", "glm-5.2", "max"},
		{"glm-5.2 大小写不敏感", "Medium", "GLM-5.2", "high"},
		{"deepseek-v4-flash low 保持", "low", "deepseek-v4-flash", "low"},
		{"deepseek-v4-flash minimal 钳到 low", "minimal", "deepseek-v4-flash", "low"},
		{"kimi-k3 仅 max", "low", "kimi-k3", "max"},
		{"kimi-k3 max 保持", "max", "kimi-k3", "max"},
		{"无表模型删字段", "high", "glm-5.1", ""},
		{"未知模型删字段", "high", "qwen3.7-max", ""},
		{"请求值无法识别删字段", "bogus", "glm-5.2", ""},
		{"空模型删字段", "high", "", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, clampOpenCodeEffortLevel(tc.requested, tc.model))
		})
	}
}

// TestClampOpenCodeResponsesReasoningEffort Responses 体按档位表改写 / 删除。
func TestClampOpenCodeResponsesReasoningEffort(t *testing.T) {
	t.Parallel()
	account := opencodeAccount(AccountModeCoding)

	clamped := clampOpenCodeResponsesReasoningEffort(account,
		[]byte(`{"model":"glm-5.2","reasoning":{"effort":"medium"}}`))
	require.Equal(t, "high", gjson.GetBytes(clamped, "reasoning.effort").String())

	// reasoning 对象只剩 effort 时整对象删除（等价于客户端未声明）。
	deleted := clampOpenCodeResponsesReasoningEffort(account,
		[]byte(`{"model":"glm-5.1","reasoning":{"effort":"high"}}`))
	require.False(t, gjson.GetBytes(deleted, "reasoning").Exists())

	// reasoning 对象含其他键（如 summary）时仅删 effort。
	keepObject := clampOpenCodeResponsesReasoningEffort(account,
		[]byte(`{"model":"glm-5.1","reasoning":{"effort":"high","summary":"auto"}}`))
	require.False(t, gjson.GetBytes(keepObject, "reasoning.effort").Exists())
	require.Equal(t, "auto", gjson.GetBytes(keepObject, "reasoning.summary").String())

	// 非 opencode 平台原样返回。
	other := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	unchanged := clampOpenCodeResponsesReasoningEffort(other,
		[]byte(`{"model":"glm-5.2","reasoning":{"effort":"medium"}}`))
	require.Equal(t, "medium", gjson.GetBytes(unchanged, "reasoning.effort").String())

	// 无 effort 字段时不动 body。
	noEffort := clampOpenCodeResponsesReasoningEffort(account, []byte(`{"model":"glm-5.2"}`))
	require.Equal(t, `{"model":"glm-5.2"}`, string(noEffort))
}

// TestClampOpenCodeChatReasoningEffort CC 体顶层 reasoning_effort 改写 / 删除。
func TestClampOpenCodeChatReasoningEffort(t *testing.T) {
	t.Parallel()
	account := opencodeAccount(AccountModeCoding)

	clamped := clampOpenCodeChatReasoningEffort(account,
		[]byte(`{"model":"deepseek-v4-pro","reasoning_effort":"medium"}`))
	require.Equal(t, "high", gjson.GetBytes(clamped, "reasoning_effort").String())

	deleted := clampOpenCodeChatReasoningEffort(account,
		[]byte(`{"model":"kimi-k2.7-code","reasoning_effort":"high"}`))
	require.False(t, gjson.GetBytes(deleted, "reasoning_effort").Exists())

	other := &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey}
	unchanged := clampOpenCodeChatReasoningEffort(other,
		[]byte(`{"model":"deepseek-v4-pro","reasoning_effort":"medium"}`))
	require.Equal(t, "medium", gjson.GetBytes(unchanged, "reasoning_effort").String())
}

// TestOpenCodeExclusiveModelIDsCoverOfficialCatalog 锁定独占名单与官方清单的
// 自洽：名单 ⊆ 官方清单；被其他平台前缀认领的同名模型不得进名单（条目永不
// 生效）；官方清单中未被认领的模型必须全部进名单——否则官方目录更新后新增
// 的独占模型在 composite 分组会退回入口 400。
func TestOpenCodeExclusiveModelIDsCoverOfficialCatalog(t *testing.T) {
	t.Parallel()

	official := make(map[string]struct{}, len(openCodeDefaultModelIDs))
	for _, id := range OpenCodeDefaultModelIDs() {
		official[id] = struct{}{}
	}
	for id := range openCodeExclusiveModelIDs {
		require.Contains(t, official, id, "exclusive model not in official catalog: %s", id)
	}
	for id := range official {
		platform, detected := DetectModelPlatform(id)
		if detected && platform != PlatformOpenCode {
			require.NotContains(t, openCodeExclusiveModelIDs, id,
				"same-name model claimed by %s must stay out of the exclusive list: %s", platform, id)
			continue
		}
		require.Contains(t, openCodeExclusiveModelIDs, id,
			"official catalog model missing from exclusive list: %s", id)
	}
}
