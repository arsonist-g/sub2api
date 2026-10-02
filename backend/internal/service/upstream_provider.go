package service

import (
	"bytes"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// cline 的 Chat Completions 响应会在最后一条内容帧里回显本次真正服务的上游供应商，
// 位置是 choices[0].delta.provider_metadata.gateway.routing.finalProvider，同一个
// 对象里还有 resolvedProvider / pinnedProvider / displacedProvider / fallbacksAvailable
// 等路由明细。流式与非流式分别落在 delta 与 message 上（非流式响应在观察前已经过
// unwrapClineSuccessEnvelope 解壳）。
//
// 字段属于上游未公开实现细节，所以三条路径都试；都不命中时返回空串——绝大多数
// 平台不回显，此时不产生任何记录值。
var upstreamProviderPaths = []string{
	"choices.0.delta.provider_metadata.gateway.routing.finalProvider",
	"choices.0.message.provider_metadata.gateway.routing.finalProvider",
	"provider_metadata.gateway.routing.finalProvider",
}

// upstreamProviderPayloadMarker 用于在解析前挡掉不含该字段的载荷。
const upstreamProviderPayloadMarker = "finalProvider"

// upstreamProviderFromPayload 从一条上游载荷里取出回显的供应商名。
func upstreamProviderFromPayload(payload []byte) string {
	if !bytes.Contains(payload, []byte(upstreamProviderPayloadMarker)) {
		return ""
	}
	for _, path := range upstreamProviderPaths {
		if name := strings.TrimSpace(gjson.GetBytes(payload, path).String()); name != "" {
			return name
		}
	}
	return ""
}

// observedUpstreamProvider 返回本次请求上游回显的供应商；未回显时为空串。
func observedUpstreamProvider(c *gin.Context) string {
	return upstreamResponseModelObserverFromContext(c).Provider()
}
