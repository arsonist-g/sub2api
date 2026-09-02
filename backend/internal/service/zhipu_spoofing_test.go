package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func zhipuSpoofTestAccount(extra map[string]any) *Account {
	return &Account{
		Platform: PlatformZhipu,
		Type:     AccountTypeAPIKey,
		Extra:    extra,
	}
}

func TestDetectZhipuClientPlatform(t *testing.T) {
	cases := []struct {
		ua       string
		expected string
	}{
		{"claude-cli/2.1.220 (external, cli)", "claude-code"},
		{"claude-vscode/2.1.0", "claude-ide"},
		{"codex_cli_rs/0.42.0", "codex"},
		{"opencode/1.0.0", "opencode"},
		{"crush/0.1.0", "crush"},
		{"goose/1.0.0", "goose"},
		{"cursor/1.0.0", "cursor"},
		{"roo-cline/3.0.0 VSCode", "roo"},
		{"roocode/3.0.0", "roo"},
		{"kilo-code/1.0.0", "kilo"},
		{"cline/2.0.0 VSCode", "cline"},
		{"droid/1.0.0", "droid"},
		{"openclaw/1.0.0", "openclaw"},
		{"CherryStudio/1.0.0", "cherry-studio"},
		{"trae/1.0.0", "trae"},
		{"qoder/1.0.0", "qoder"},
		{"lingma/1.0.0", "lingma"},
		{"codebuddy/1.0.0", "codebuddy"},
		{"monkeycode/1.0.0", "monkeycode"},
		{"zcode/1.0.0", "zcode"},
		{"hermes-agent/1.0.0", "hermes-agent"},
		{"somepi/1.0.0", ""}, // 无空格 " pi/" 前缀不误命中
		{"python-requests/2.0", ""},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.expected, DetectZhipuClientPlatform(tc.ua), "ua=%q", tc.ua)
	}
}

func TestDetectZhipuClientPlatform_RooBeforeCline(t *testing.T) {
	// roo-cline 同时包含 "cline" 子串，必须优先命中 roo
	require.Equal(t, "roo", DetectZhipuClientPlatform("roo-cline/3.0.0"))
}

func TestZhipuAccountSpoofAccessors(t *testing.T) {
	// zhipu 账号支持 TLS 指纹与会话伪装开关
	acc := zhipuSpoofTestAccount(map[string]any{
		"enable_tls_fingerprint":     true,
		"session_id_masking_enabled": true,
	})
	require.True(t, acc.IsTLSFingerprintEnabled())
	require.True(t, acc.IsSessionIDMaskingEnabled())

	off := zhipuSpoofTestAccount(map[string]any{
		"enable_tls_fingerprint":     false,
		"session_id_masking_enabled": false,
	})
	require.False(t, off.IsTLSFingerprintEnabled())
	require.False(t, off.IsSessionIDMaskingEnabled())

	// 排除平台列表解析：过滤非字符串与空串
	acc.Extra["spoof_excluded_platforms"] = []any{"claude-code", 42, "", "codex"}
	require.Equal(t, []string{"claude-code", "codex"}, acc.GetSpoofExcludedPlatforms())

	acc.Extra["spoof_excluded_platforms"] = "not-an-array"
	require.Nil(t, acc.GetSpoofExcludedPlatforms())

	acc.Extra["spoof_excluded_platforms"] = []any{}
	require.Nil(t, acc.GetSpoofExcludedPlatforms())

	// 非 zhipu 账号一律不支持
	anthropic := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{
		"enable_tls_fingerprint":     true,
		"session_id_masking_enabled": true,
	}}
	require.False(t, anthropic.IsTLSFingerprintEnabled())
	require.False(t, anthropic.IsSessionIDMaskingEnabled())
}

func TestComputeZhipuSpoofDecision(t *testing.T) {
	s := &OpenAIGatewayService{}

	t.Run("开关全关不生成决策", func(t *testing.T) {
		acc := zhipuSpoofTestAccount(nil)
		require.Nil(t, s.computeZhipuSpoofDecision("opencode/1.0.0", acc))
	})

	t.Run("命中排除平台不生成决策", func(t *testing.T) {
		acc := zhipuSpoofTestAccount(map[string]any{
			"enable_tls_fingerprint":     true,
			"session_id_masking_enabled": true,
			"spoof_excluded_platforms":   []any{"claude-code"},
		})
		require.Nil(t, s.computeZhipuSpoofDecision("claude-cli/2.1.220", acc))
		// 未命中的已识别平台照常伪装，但不强制 Anthropic
		d := s.computeZhipuSpoofDecision("opencode/1.0.0", acc)
		require.NotNil(t, d)
		require.Equal(t, "opencode", d.Platform)
		require.False(t, d.ForceAnthropic)
		require.True(t, d.MaskSession)
	})

	t.Run("未识别平台强制 Anthropic 且照常伪装", func(t *testing.T) {
		acc := zhipuSpoofTestAccount(map[string]any{
			"enable_tls_fingerprint":     true,
			"session_id_masking_enabled": true,
		})
		d := s.computeZhipuSpoofDecision("python-requests/2.0", acc)
		require.NotNil(t, d)
		require.Equal(t, "", d.Platform)
		require.True(t, d.ForceAnthropic)
		require.True(t, d.MaskSession)
	})

	t.Run("仅开启会话伪装", func(t *testing.T) {
		acc := zhipuSpoofTestAccount(map[string]any{
			"session_id_masking_enabled": true,
		})
		d := s.computeZhipuSpoofDecision("some-client/1.0", acc)
		require.NotNil(t, d)
		require.True(t, d.MaskSession)
		require.Nil(t, d.TLSProfile)
	})
}

func TestResolveZhipuSpoofDecision_NonZhipuUntouched(t *testing.T) {
	s := &OpenAIGatewayService{}
	ctx := context.Background()
	acc := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{
		"enable_tls_fingerprint": true,
	}}
	out := s.resolveZhipuSpoofDecision(ctx, "opencode/1.0.0", acc)
	require.Equal(t, ctx, out)
}

func TestIdentityService_MaskSessionIDOnly(t *testing.T) {
	acc := zhipuSpoofTestAccount(map[string]any{"session_id_masking_enabled": true})
	svc := NewIdentityService(&identityCacheStub{})

	ua := "claude-cli/2.1.220 (external, cli)"
	originalUserID := FormatMetadataUserID(
		"d61f76d0730d2b920763648949bad5c79742155c27037fc77ac3f9805cb90169",
		"",
		"7578cf37-aaca-46e4-a45c-71285d9dbb83",
		"2.1.78",
	)
	body := []byte(`{"model":"glm-5","messages":[],"metadata":{"user_id":` + strconv.Quote(originalUserID) + `}}`)

	masked := svc.MaskSessionIDOnly(context.Background(), body, acc, ua)
	require.NotEqual(t, string(body), string(masked))
	require.Contains(t, string(masked), "d61f76d0730d2b920763648949bad5c79742155c27037fc77ac3f9805cb90169")
	require.NotContains(t, string(masked), "7578cf37-aaca-46e4-a45c-71285d9dbb83")

	// 无 metadata 的 body 原样返回
	noMeta := []byte(`{"model":"glm-5","messages":[]}`)
	require.Equal(t, string(noMeta), string(svc.MaskSessionIDOnly(context.Background(), noMeta, acc, ua)))

	// 未开启伪装的账号原样返回
	off := zhipuSpoofTestAccount(nil)
	require.Equal(t, string(body), string(svc.MaskSessionIDOnly(context.Background(), body, off, ua)))
}
