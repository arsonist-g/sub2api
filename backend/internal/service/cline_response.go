package service

import "github.com/tidwall/gjson"

// Cline 上游把非流式 Chat Completions 响应包在一层
// {"data":{...},"success":true} 外壳里。该外壳不是 OpenAI 形态：直接透传给
// 客户端会拿到非标准 JSON，按 CC 结构解析则 choices / usage 全部落空
// （输出为空、计费用量为 0）。因此在读到响应体后立刻解包。
//
// 判定同时要求「顶层没有 choices」与「data.choices 存在」，对未被包裹的响应
// 是纯 no-op，不会误伤其他平台，也能容忍上游将来去掉外壳。
func unwrapClineSuccessEnvelope(account *Account, body []byte) []byte {
	if account == nil || account.Platform != PlatformCline || len(body) == 0 {
		return body
	}
	if gjson.GetBytes(body, "choices").Exists() {
		return body
	}
	inner := gjson.GetBytes(body, "data")
	if !inner.IsObject() || !inner.Get("choices").Exists() {
		return body
	}
	return []byte(inner.Raw)
}
