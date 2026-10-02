//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// clineStreamFinalChunkFixture 取自生产环境一条真实流式响应的最后一条内容帧
// （只裁掉了 fallbacksAvailable 的长清单），用来钉住回显位置。
const clineStreamFinalChunkFixture = `{"choices":[{"delta":{"provider_metadata":{` +
	`"deepseek":{"choiceIndex":0,"messageRole":"assistant","promptCacheHitTokens":0,"promptCacheMissTokens":32},` +
	`"gateway":{"cost":"0.000015","generationId":"gen_01M3YD8MQPHGYHSMDRCMG098KM","routing":{` +
	`"affinity":{"displacedProvider":"alibaba","outcome":"promoted","pinnedProvider":"deepseek"},` +
	`"canonicalSlug":"deepseek/deepseek-v4.1-flash",` +
	`"fallbacksAvailable":["fireworks","alibaba","togetherai","deepseek"],` +
	`"finalProvider":"deepseek","modelAttemptCount":1,"resolvedProvider":"deepseek",` +
	`"totalProviderAttemptCount":1},"surchargeCost":"0"}}},"finish_reason":"stop","index":0}],` +
	`"created":1790948168,"id":"gen_01M3YD8MQPHGYHSMDRCMG098KM","model":"deepseek/deepseek-v4.1-flash",` +
	`"object":"chat.completion.chunk","system_fingerprint":"fp_3gnosp2g9g",` +
	`"usage":{"completion_tokens":17,"prompt_tokens":32,"total_tokens":49}}`

func TestUpstreamProviderFromPayload(t *testing.T) {
	t.Run("流式最后一条内容帧", func(t *testing.T) {
		require.Equal(t, "deepseek", upstreamProviderFromPayload([]byte(clineStreamFinalChunkFixture)))
	})

	t.Run("非流式形态落在 message 上", func(t *testing.T) {
		payload := `{"choices":[{"message":{"provider_metadata":{"gateway":{"routing":{"finalProvider":"baseten"}}}}}]}`
		require.Equal(t, "baseten", upstreamProviderFromPayload([]byte(payload)))
	})

	t.Run("顶层 provider_metadata", func(t *testing.T) {
		payload := `{"provider_metadata":{"gateway":{"routing":{"finalProvider":"togetherai"}}}}`
		require.Equal(t, "togetherai", upstreamProviderFromPayload([]byte(payload)))
	})

	t.Run("普通 chunk 不产生值", func(t *testing.T) {
		payload := `{"id":"gen_1","object":"chat.completion.chunk","model":"deepseek/deepseek-v4.1-flash",` +
			`"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
		require.Empty(t, upstreamProviderFromPayload([]byte(payload)))
	})

	t.Run("同名字段但值非字符串时忽略", func(t *testing.T) {
		payload := `{"choices":[{"delta":{"provider_metadata":{"gateway":{"routing":{"finalProvider":null}}}}}]}`
		require.Empty(t, upstreamProviderFromPayload([]byte(payload)))
	})

	t.Run("非 JSON 载荷不 panic", func(t *testing.T) {
		require.Empty(t, upstreamProviderFromPayload([]byte("data: [DONE]")))
		require.Empty(t, upstreamProviderFromPayload([]byte(`{"finalProvider":`)))
	})
}

// 观测器只在真正回显时记录，且后出现的声明覆盖先出现的（cline 只在最后一帧给）。
func TestUpstreamResponseModelObserverObserveProvider(t *testing.T) {
	observer := &upstreamResponseModelObserver{}

	observer.ObserveOpenAI([]byte(`{"choices":[{"delta":{"role":"assistant"}}]}`), "")
	require.Empty(t, observer.Provider())

	observer.ObserveOpenAI([]byte(clineStreamFinalChunkFixture), "")
	require.Equal(t, "deepseek", observer.Provider())

	observer.ObserveOpenAI([]byte(`{"choices":[{"delta":{"provider_metadata":{"gateway":{"routing":{"finalProvider":"baseten"}}}}}]}`), "")
	require.Equal(t, "baseten", observer.Provider())

	var nilObserver *upstreamResponseModelObserver
	require.Empty(t, nilObserver.Provider())
}
