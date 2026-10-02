//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// clineMessagesFallbackTestAccount 构造走 /v1/messages → CC 回退的 cline 账号。
// ForceChatCompletions 让 ForwardAsAnthropic 走 CC 回退分支。
func clineMessagesFallbackTestAccount(extraCredentials map[string]any) *Account {
	credentials := map[string]any{
		"api_key":      "sk-cline-test",
		"base_url":     "http://upstream.example",
		"account_mode": "coding",
		"api_protocol": "chat_completions",
	}
	for key, value := range extraCredentials {
		credentials[key] = value
	}
	return &Account{
		ID:          303,
		Name:        "cline-apikey",
		Platform:    PlatformCline,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: credentials,
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
		},
	}
}

func clineEmptyStreamRequestBody() []byte {
	return []byte(`{"model":"cline-pass/deepseek-v4.1-flash","max_tokens":8,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
}

func clineStreamUpstreamResponse(contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{contentType},
			"x-request-id": []string{"rid_cline_empty"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func forwardClineMessages(t *testing.T, account *Account, upstream *http.Response) (*httptest.ResponseRecorder, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(""))
	c.Request.Header.Set("Content-Type", "application/json")

	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: &httpUpstreamRecorder{resp: upstream},
	}
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, clineEmptyStreamRequestBody(), "", "")
	return rec, err
}

// cline 上游返回 2xx 但整条流没有任何内容帧时，必须变成可重试的上游失败，
// 而不是把空的 Anthropic 消息补全后当成成功——后者会让客户端拿到空回复
// 自行重试，且 ops 错误记录里看不到任何痕迹。
func TestForwardAsAnthropic_ClineEmptyStreamBecomesRetryableFailover(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			// cline 的非流式响应带 {"data":...,"success":true} 外壳；流式请求如果
			// 收到这种非 SSE 的 JSON 体，解析不出任何 data: 帧。
			name:        "非 SSE 的错误 JSON 体",
			contentType: "application/json",
			body:        `{"error":"empty response content","success":false}`,
		},
		{
			name:        "data 帧里带着 error 对象",
			contentType: "text/event-stream",
			body:        "data: {\"error\":{\"message\":\"planner upstream rejected\"}}\n\ndata: [DONE]\n\n",
		},
		{
			name:        "干净的 [DONE] 空流",
			contentType: "text/event-stream",
			body:        "data: [DONE]\n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := forwardClineMessages(t, clineMessagesFallbackTestAccount(nil), clineStreamUpstreamResponse(tc.contentType, tc.body))

			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.True(t, errors.As(err, &failoverErr), "want *UpstreamFailoverError, got %T", err)

			require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
			require.Equal(t, http.StatusBadGateway, failoverErr.ClientStatusCode)
			require.Equal(t, clineEmptyStreamClientMessage, failoverErr.ClientMessage)
			require.True(t, failoverErr.RetryableOnSameAccount, "空响应必须在同一账号上重试")
			require.Equal(t, defaultClineEmptyStreamRetryCount, failoverErr.SameAccountRetryMax)
			require.True(t, failoverErr.RequestScopedTransient, "不应据此惩罚账号")
			// 原始证据留在 ResponseBody 上，供错误记录与错误透传规则使用。
			require.Contains(t, string(failoverErr.ResponseBody), strings.SplitN(tc.body, "\n", 2)[0])
			require.Zero(t, rec.Body.Len(), "判定失败时不应写出任何响应字节")
		})
	}
}

// 关掉开关时必须维持既有行为：补一条全帧的空消息，而不是报错。
func TestForwardAsAnthropic_ClineEmptyStreamKeepsFramedMessageWhenDisabled(t *testing.T) {
	account := clineMessagesFallbackTestAccount(map[string]any{"cline_empty_stream_retry": false})
	rec, err := forwardClineMessages(t, account, clineStreamUpstreamResponse("text/event-stream", "data: [DONE]\n\n"))

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "event: message_start")
	require.Contains(t, rec.Body.String(), "event: message_stop")
}

// 重试次数配成 0 表示不原地重试，此时不得再声明 RetryableOnSameAccount，
// 否则 failover 循环会回退到账号默认次数。
func TestForwardAsAnthropic_ClineEmptyStreamRetryCountZeroDisablesSameAccountRetry(t *testing.T) {
	account := clineMessagesFallbackTestAccount(map[string]any{"cline_empty_stream_retry_count": 0})
	_, err := forwardClineMessages(t, account, clineStreamUpstreamResponse("text/event-stream", "data: [DONE]\n\n"))

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.False(t, failoverErr.RetryableOnSameAccount)
}

// 有正常内容的 cline 流不受影响。
func TestForwardAsAnthropic_ClineContentStreamStillForwards(t *testing.T) {
	body := "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: [DONE]\n\n"
	account := clineMessagesFallbackTestAccount(nil)
	rec, err := forwardClineMessages(t, account, clineStreamUpstreamResponse("text/event-stream", body))

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"text":"hi"`)
}

func TestClineEmptyStreamUpstreamMessage(t *testing.T) {
	cases := []struct {
		name string
		scan ccStreamScanState
		want string
	}{
		{"帧里带 error", ccStreamScanState{SawDataFrame: true, SawDone: true, InBandError: "boom"}, "boom"},
		{"根本不是 SSE", ccStreamScanState{}, "upstream response was not an SSE stream"},
		{"SSE 空流带 [DONE]", ccStreamScanState{SawDataFrame: true, SawDone: true}, "upstream stream ended without any content"},
		{"SSE 空流无 [DONE]", ccStreamScanState{SawDataFrame: true}, "upstream stream ended without content or done sentinel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clineEmptyStreamUpstreamMessage(tc.scan))
		})
	}
}

func TestExtractCCInBandError(t *testing.T) {
	cases := []struct {
		payload string
		want    string
		wantOK  bool
	}{
		{`{"error":{"message":"boom","type":"upstream_error"}}`, "boom", true},
		{`{"error":"empty response content","success":false}`, "empty response content", true},
		{`{"error":"boom"}`, "boom", true},
		{`{"error":{"code":"invalid_api_key"}}`, "invalid_api_key", true},
		{`{"success":false,"message":"budget exhausted"}`, "budget exhausted", true},
		{`{"success":false}`, "upstream reported success=false", true},
		{`{"choices":[{"delta":{"content":"hi"}}]}`, "", false},
		// 空壳不算法错误，否则正常流量里的 error 字段会被误判成上游故障。
		{`{"error":null,"choices":[]}`, "", false},
		{`{"error":{}}`, "", false},
		{`{"error":""}`, "", false},
		{`{"error":false,"choices":[]}`, "", false},
		{`{"success":true,"data":{}}`, "", false},
	}
	for _, tc := range cases {
		got, ok := extractCCInBandError(tc.payload)
		require.Equal(t, tc.wantOK, ok, "payload=%s", tc.payload)
		require.Equal(t, tc.want, got, "payload=%s", tc.payload)
	}
}
