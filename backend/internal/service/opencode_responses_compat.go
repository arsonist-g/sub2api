package service

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// sanitizeOpenCodeResponsesRequest 剥除 OpenCode Zen/Go Responses 端点不接受的
// 请求字段。OpenCode 官方客户端（@ai-sdk/openai）发往自家网关的请求不带这些
// 字段；它们是 Anthropic→Responses 桥接为对齐 ChatGPT/Codex 后端形态注入的，
// OpenCode 网关按严格 schema 校验，携带会触发确定性 400（与 reasoning.effort
// 档位问题同类，对齐 cc-switch 对第三方 Responses 上游的出站清理思路）：
//   - include: reasoning.encrypted_content 为 OpenAI 官方后端特性
//   - text.verbosity: gpt-5 系特性，OpenCode 模型目录无此声明
//   - reasoning.summary: 要求上游产出推理摘要，OpenCode 模型不支持
//
// reasoning.effort 由 clampOpenCodeResponsesReasoningEffort 单独管理，此处
// 不动。调用顺序须为先本函数后钳制：删掉 summary 后 reasoning 若只剩 effort，
// 钳制的无档位分支会把空对象整体删除，不会留下 {}。
func sanitizeOpenCodeResponsesRequest(account *Account, body []byte) []byte {
	if !isOpenCodeAccount(account) || len(body) == 0 {
		return body
	}
	for _, path := range []string{"include", "text", "reasoning.summary"} {
		if !gjson.GetBytes(body, path).Exists() {
			continue
		}
		if deleted, err := sjson.DeleteBytes(body, path); err == nil {
			body = deleted
		}
	}
	return body
}
