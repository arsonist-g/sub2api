package service

// Zhipu GLM 账号的 TLS 指纹模拟与会话 ID 伪装决策。
//
// 官方文档（docs.bigmodel.cn coding plan 接入工具页）未记载各接入工具的
// User-Agent，客户端工具识别只能基于 UA 子串启发式匹配，且工具清单会随
// 官方调整变化。识别不出的平台按"未排除"处理：只要对应开关打开，照常伪装。
//
// 排除策略：账号 extra.spoof_excluded_platforms 列出不需要伪装的客户端工具；
// 命中排除列表（仅已识别平台可能命中）→ 本请求不做任何伪装。
//
// 强制 Anthropic 端点：未识别平台的非 Anthropic 入站请求（CC / Responses）
// 一律转换为 Anthropic 协议走 GLM 原生 /api/anthropic 端点，不使用自适应
// 或 Chat Completions 端点——这样会话 ID 伪装（改写 metadata.user_id）才有
// 作用对象，且两条伪装能力在同一端点内同时生效。

import (
	"context"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// zhipuClientPlatformPatterns 客户端工具 UA 子串匹配表（小写比较，先命中先得）。
// 顺序敏感：roo-cline 必须先于 cline。模式为保守子串，宁可漏识别（按未排除
// 处理、照常伪装）也不误识别。
var zhipuClientPlatformPatterns = []struct{ Platform, Pattern string }{
	{"claude-ide", "claude-vscode"},
	{"claude-ide", "claude-jetbrains"},
	{"claude-code", "claude-cli"},
	{"claude-code", "claude-code"},
	{"codex", "codex"},
	{"opencode", "opencode"},
	{"crush", "crush"},
	{"goose", "goose"},
	{"cursor", "cursor"},
	{"roo", "roo-cline"},
	{"roo", "roocode"},
	{"kilo", "kilo"},
	{"cline", "cline"},
	{"droid", "droid"},
	{"openclaw", "openclaw"},
	{"cherry-studio", "cherry"},
	{"trae", "trae"},
	{"qoder", "qoder"},
	{"lingma", "lingma"},
	{"codebuddy", "codebuddy"},
	{"monkeycode", "monkeycode"},
	{"zcode", "zcode"},
	{"hermes-agent", "hermes"},
	{"pi", " pi/"},
}

// DetectZhipuClientPlatform 从 User-Agent 识别 GLM Coding Plan 客户端工具，
// 返回平台 slug；识别不出返回 ""。
func DetectZhipuClientPlatform(userAgent string) string {
	ua := strings.ToLower(userAgent)
	if ua == "" {
		return ""
	}
	for _, p := range zhipuClientPlatformPatterns {
		if strings.Contains(ua, p.Pattern) {
			return p.Platform
		}
	}
	return ""
}

// zhipuSpoofDecision 单次请求的伪装决策。在网关入口（已知 account 与客户端
// UA）计算一次，随 request context 传递到上游发送点。nil 表示本请求不做
// 任何伪装、按原有路由转发。
type zhipuSpoofDecision struct {
	Platform       string                  // 检测到的客户端工具 slug，"" 表示未识别
	TLSProfile     *tlsfingerprint.Profile // 非 nil 时上游走 DoWithTLS（utls 指纹）
	MaskSession    bool                    // 改写 anthropic 协议请求 metadata.user_id 的会话段
	ForceAnthropic bool                    // 未识别平台 + 非 Anthropic 入站 → 强制走原生 Anthropic 端点
}

type zhipuSpoofDecisionContextKey struct{}

func withZhipuSpoofDecision(ctx context.Context, d *zhipuSpoofDecision) context.Context {
	return context.WithValue(ctx, zhipuSpoofDecisionContextKey{}, d)
}

// zhipuSpoofDecisionFromContext 读取请求的伪装决策；无决策返回 nil。
func zhipuSpoofDecisionFromContext(ctx context.Context) *zhipuSpoofDecision {
	d, _ := ctx.Value(zhipuSpoofDecisionContextKey{}).(*zhipuSpoofDecision)
	return d
}

// computeZhipuSpoofDecision 计算伪装决策；未启用任何开关或命中排除平台时
// 返回 nil。
func (s *OpenAIGatewayService) computeZhipuSpoofDecision(userAgent string, account *Account) *zhipuSpoofDecision {
	tlsEnabled := account.IsTLSFingerprintEnabled()
	maskEnabled := account.IsSessionIDMaskingEnabled()
	if !tlsEnabled && !maskEnabled {
		return nil
	}

	platform := DetectZhipuClientPlatform(userAgent)
	// 已识别且命中账号排除列表 → 不伪装
	if platform != "" && slices.Contains(account.GetSpoofExcludedPlatforms(), platform) {
		return nil
	}

	d := &zhipuSpoofDecision{Platform: platform}
	if tlsEnabled && s.tlsFPProfileService != nil {
		d.TLSProfile = s.tlsFPProfileService.ResolveTLSProfile(account)
	}
	d.MaskSession = maskEnabled
	// 未识别平台一律走 Anthropic 端点（对已是 anthropic 协议/入站的请求为无操作）
	d.ForceAnthropic = platform == ""
	return d
}

// resolveZhipuSpoofDecision 在网关入口调用：计算 zhipu 账号本次请求的伪装
// 决策并附加到 ctx，返回新 ctx。非 zhipu 账号原样返回。
func (s *OpenAIGatewayService) resolveZhipuSpoofDecision(ctx context.Context, userAgent string, account *Account) context.Context {
	if account == nil || !account.IsZhipu() {
		return ctx
	}
	d := s.computeZhipuSpoofDecision(userAgent, account)
	if d == nil {
		return ctx
	}
	return withZhipuSpoofDecision(ctx, d)
}
