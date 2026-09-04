package service

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// OpenCode Zen 网关对 reasoning_effort 的接受档位是逐模型不同的：
// glm-5.2 / deepseek-v4-pro 仅 high|max，deepseek-v4-flash 为 low|high|max，
// kimi-k3 仅 max；glm-5.1 / kimi-k2.7-code / mimo-v2.5-pro 等为 toggle 型
// （模型自身开关推理，不接受 effort 字段）。网关不认厂商原生 thinking 形状，
// 把不合法档位（如 Codex 默认 medium）发给仅支持 high|max 的模型会得到
// 确定性 400。档位表镜像 models.dev 的 reasoning_options 声明。
//
// 钳制规则（对齐 cc-switch transform_codex_chat.rs 的 zen 模式）：
//   - 模型在表内 → 钳到「不小于请求值的最近合法档」；请求超出最高档则取最高档
//   - 模型不在表内（未收录 / toggle 型）→ 完全不发该字段
//   - 请求值本身无法识别 → 同样不发该字段
//
// 档位序：minimal < low < medium < high < xhigh < max < ultra。
var openCodeReasoningEffortLevels = map[string][]string{
	"glm-5.2":           {"high", "max"},
	"deepseek-v4-pro":   {"high", "max"},
	"deepseek-v4-flash": {"low", "high", "max"},
	"kimi-k3":           {"max"},
}

func openCodeEffortRank(effort string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal":
		return 0, true
	case "low":
		return 1, true
	case "medium":
		return 2, true
	case "high":
		return 3, true
	case "xhigh":
		return 4, true
	case "max":
		return 5, true
	case "ultra":
		return 6, true
	default:
		return 0, false
	}
}

// clampOpenCodeEffortLevel 按模型档位表钳制请求档位；返回空串表示不应发送该字段。
func clampOpenCodeEffortLevel(requested, model string) string {
	levels, ok := openCodeReasoningEffortLevels[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		return ""
	}
	requestedRank, ok := openCodeEffortRank(requested)
	if !ok {
		return ""
	}
	best := ""
	bestRank := -1
	// 先找不小于请求值的最近合法档；找不到（请求超出全部合法档）再取最高合法档。
	for _, level := range levels {
		rank, ok := openCodeEffortRank(level)
		if !ok {
			continue
		}
		if rank >= requestedRank && (bestRank < 0 || rank < bestRank) {
			best, bestRank = level, rank
		}
	}
	if bestRank >= 0 {
		return best
	}
	for _, level := range levels {
		if rank, ok := openCodeEffortRank(level); ok && rank > bestRank {
			best, bestRank = level, rank
		}
	}
	return best
}

func isOpenCodeAccount(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenCode
}

// clampOpenCodeResponsesReasoningEffort 钳制 Responses 请求体的 reasoning.effort。
// 无合法档位时：reasoning 对象若还剩其他键则仅删 effort，否则整对象删除
// （等价于客户端从未声明 reasoning）。
func clampOpenCodeResponsesReasoningEffort(account *Account, body []byte) []byte {
	if !isOpenCodeAccount(account) || len(body) == 0 {
		return body
	}
	effort := strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String())
	if effort == "" {
		return body
	}
	clamped := clampOpenCodeEffortLevel(effort, gjson.GetBytes(body, "model").String())
	if clamped == effort {
		return body
	}
	if clamped != "" {
		if set, err := sjson.SetBytes(body, "reasoning.effort", clamped); err == nil {
			return set
		}
		return body
	}
	if reasoning := gjson.GetBytes(body, "reasoning"); reasoning.IsObject() {
		hasOtherKeys := false
		reasoning.ForEach(func(key, _ gjson.Result) bool {
			if key.String() != "effort" {
				hasOtherKeys = true
				return false
			}
			return true
		})
		if !hasOtherKeys {
			if deleted, err := sjson.DeleteBytes(body, "reasoning"); err == nil {
				return deleted
			}
			return body
		}
	}
	if deleted, err := sjson.DeleteBytes(body, "reasoning.effort"); err == nil {
		return deleted
	}
	return body
}

// clampOpenCodeChatReasoningEffort 钳制 Chat Completions 请求体的顶层
// reasoning_effort；无合法档位时整字段删除。
func clampOpenCodeChatReasoningEffort(account *Account, body []byte) []byte {
	if !isOpenCodeAccount(account) || len(body) == 0 {
		return body
	}
	effort := strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String())
	if effort == "" {
		return body
	}
	clamped := clampOpenCodeEffortLevel(effort, gjson.GetBytes(body, "model").String())
	if clamped == effort {
		return body
	}
	if clamped != "" {
		if set, err := sjson.SetBytes(body, "reasoning_effort", clamped); err == nil {
			return set
		}
		return body
	}
	if deleted, err := sjson.DeleteBytes(body, "reasoning_effort"); err == nil {
		return deleted
	}
	return body
}
