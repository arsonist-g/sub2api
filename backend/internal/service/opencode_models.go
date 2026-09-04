package service

// OpenCode Go 订阅的官方模型清单（2026-09 官方文档盘点，opencode.ai/docs/go）。
// 上游按协议分三组：responses（@ai-sdk/openai）、chat/completions
// （@ai-sdk/openai-compatible）、messages（@ai-sdk/anthropic）；网关侧三组
// 均可经对应协议端点访问，此处仅作分组模型白名单候选与 UI 展示，不参与路由。
// 官方声明模型列表会随时间变化，实际可用模型以上游 /v1/models 同步结果为准。
//
//nolint:gochecknoglobals // 静态清单，初始化后不变。
var openCodeDefaultModelIDs = []string{
	// responses 组
	"grok-4.5",
	"gpt-5.6-luna",
	"muse-spark-1.2-contributor",
	// chat/completions 组
	"glm-5.3",
	"glm-5.2",
	"glm-5.1",
	"kimi-k3",
	"kimi-k2.7-code",
	"kimi-k2.6",
	"deepseek-v4-pro",
	"deepseek-v4-flash",
	"mimo-v2.5",
	"mimo-v2.5-pro",
	"hy3",
	// messages 组
	"minimax-m3",
	"minimax-m2.7",
	"minimax-m2.5",
	"qwen3.8-max",
	"qwen3.7-max",
	"qwen3.7-plus",
	"qwen3.6-plus",
}

// OpenCodeDefaultModelIDs 返回 OpenCode Go 的默认模型候选（拷贝，调用方可安全修改）。
func OpenCodeDefaultModelIDs() []string {
	out := make([]string, len(openCodeDefaultModelIDs))
	copy(out, openCodeDefaultModelIDs)
	return out
}
