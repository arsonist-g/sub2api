package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// OpenCode 上游（opencode.ai/zen[/go]）要求对话请求携带 X-Opencode-Session
// 头，值为稳定的按会话 ID；自 2026-09-06 起缺失该头的请求可能被上游拒绝。
// 官方客户端（OpenCode CLI）按会话生成 UUID，网关侧按请求的会话信号派生
// 稳定 UUID，保证同一会话的多轮请求携带同一 ID。
//
// 会话信号优先级（与网关既有粘性会话信号对齐，见
// explicitOpenAIHeaderSessionID / deriveOpenAIContentSessionSeed）：
//  1. 客户端显式会话头（x-opencode-session / session-id / session_id /
//     conversation_id / x-session-affinity / x-session-id / x-conversation-id）
//  2. Claude Code 客户端的 x-claude-code-session-id
//  3. OpenAI 请求体 prompt_cache_key
//  4. Anthropic 请求体 metadata.user_id 的 session 段
//  5. 内容派生种子（model + tools/system 前缀 + 首条用户消息）
//
// 种子经 deriveStableUUIDv4 单向映射并混入 apiKeyID：客户端内部会话 ID 不
// 透传给上游，多租户下同名的客户端会话 ID 也不会在上游碰撞。所有信号均
// 缺失时（探测类请求）退化为账号级固定 ID，保证该头始终存在。
func resolveOpenCodeUpstreamSessionSeed(c *gin.Context, body []byte) string {
	if seed := explicitOpenAIHeaderSessionID(c); seed != "" {
		return seed
	}
	if c != nil {
		if seed := strings.TrimSpace(c.GetHeader("X-Claude-Code-Session-Id")); seed != "" {
			return seed
		}
	}
	if len(body) > 0 {
		if seed := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); seed != "" {
			return seed
		}
		if parsed := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String()); parsed != nil && parsed.SessionID != "" {
			return parsed.SessionID
		}
	}
	return deriveOpenAIContentSessionSeed(body)
}

// applyOpenCodeUpstreamSessionHeader 为 OpenCode 平台账号的出站请求设置
// X-Opencode-Session（其他平台 no-op）。已存在同名头时不覆盖，调用点须位于
// 账号级 header 覆写（ApplyHeaderOverrides）之前，使管理员显式配置拥有
// 最终决定权。
func applyOpenCodeUpstreamSessionHeader(header http.Header, account *Account, c *gin.Context, body []byte) {
	if header == nil || !isOpenCodeAccount(account) {
		return
	}
	if header.Get(openCodeNativeSessionHeader) != "" {
		return
	}
	seed := resolveOpenCodeUpstreamSessionSeed(c, body)
	if seed == "" {
		seed = "account:" + strconv.FormatInt(account.ID, 10)
	}
	header.Set(openCodeNativeSessionHeader, deriveStableUUIDv4(
		"sub2api:opencode-session:v1:"+strconv.FormatInt(getAPIKeyIDFromContext(c), 10)+":"+seed))
}
