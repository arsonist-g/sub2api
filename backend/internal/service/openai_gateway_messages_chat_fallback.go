package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// forwardAnthropicViaRawChatCompletions serves /v1/messages clients through
// an OpenAI-compatible upstream that only supports /v1/chat/completions.
//
// Conversion chain (direct, no Responses intermediary):
//
//	Request:  Anthropic Messages → Chat Completions (AnthropicToChatCompletionsRequest)
//	Response: CC chunk/response → Anthropic events/response (direct bridge)
//
// This is the /v1/messages counterpart of forwardResponsesViaRawChatCompletions
// (which serves /v1/responses clients). Unlike the Responses path, the direct
// bridge skips the Responses API intermediate representation entirely — every
// streaming token runs through a single state machine instead of two.
func (s *OpenAIGatewayService) forwardAnthropicViaRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse Anthropic request
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := anthropicReq.Model
	if strings.TrimSpace(originalModel) == "" {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	clientStream := anthropicReq.Stream

	// 2. Anthropic → Chat Completions (direct, no Responses intermediary)
	chatReq, err := apicompat.AnthropicToChatCompletionsRequest(&anthropicReq)
	if err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert anthropic to chat completions: %w", err)
	}

	billingModel := resolveOpenAIForwardModel(account, anthropicReq.Model, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	chatReq.Model = upstreamModel
	chatReq.ReasoningEffort = openAICompatAnthropicReasoningEffort(&anthropicReq, upstreamModel, chatReq.ReasoningEffort)
	chatReq.Stream = clientStream
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	convertedEffort := chatReq.ReasoningEffort
	reasoningEffort := &convertedEffort
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	serviceTier := extractOpenAIServiceTierFromBody(body)

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(chatBody, upstreamModel); normalized {
		chatBody = normalizedBody
	}
	// OpenCode Zen 网关的 reasoning_effort 合法档位逐模型不同，出站前统一钳制。
	chatBody = clampOpenCodeChatReasoningEffort(account, chatBody)
	if account.Platform == PlatformOpenAI {
		policyBody, changed, policyErr := ApplyOpenAIReasoningEffortPolicyFromContext(ctx, chatBody)
		if policyErr != nil {
			var overLimit *ReasoningEffortOverLimitError
			if errors.As(policyErr, &overLimit) {
				MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
				writeAnthropicError(c, http.StatusForbidden, "forbidden_error", overLimit.Error())
			}
			return nil, policyErr
		}
		if changed {
			chatBody = policyBody
			if effectiveEffort := strings.TrimSpace(gjson.GetBytes(chatBody, "reasoning_effort").String()); effectiveEffort != "" {
				reasoningEffort = &effectiveEffort
			}
		}
	}
	// Unlike forwardResponsesViaRawChatCompletions, applyOpenAIFastPolicyToBody
	// is intentionally skipped: Anthropic Messages bodies carry no service_tier,
	// so the converted Chat Completions body never contains one and the policy
	// would always be a no-op on this path.

	logger.L().Debug("openai messages: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 3. Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 4. Handle error responses
	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		// Non-failover error: return Anthropic-formatted error to client via the
		// shared compat handler (passthrough rules, ops recording, cyber_policy).
		return s.handleAnthropicErrorResponse(resp, c, account, billingModel)
	}

	// 5. Convert response
	if clientStream {
		return s.streamChatCompletionsAsAnthropic(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferChatCompletionsAsAnthropic(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsAnthropic(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	ccResp, usage, err := s.readCCUpstreamJSONResponse(c, account, resp, writeAnthropicError)
	if err != nil {
		return nil, err
	}
	anthropicResp := apicompat.ChatCompletionsResponseToAnthropic(ccResp, originalModel)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.JSON(http.StatusOK, anthropicResp)

	return &OpenAIForwardResult{
		RequestID:                   requestID,
		Usage:                       usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		ReasoningEffort:             reasoningEffort,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		UpstreamProvider:            observedUpstreamProvider(c),
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      false,
		Duration:                    time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsAnthropic(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)

	anthropicState := apicompat.NewChatCompletionsToAnthropicStreamState(originalModel)
	clientDisconnected := false

	// 与 responses 兄弟不同：客户端断开后仍继续做事件转换（喂 anthropicState），
	// 仅跳过写出，保证 finalize 阶段的 usage 汇总不受断开影响。
	emitChunk := func(chunk *apicompat.ChatCompletionsChunk) {
		// CC chunk → Anthropic events (direct, single state machine)
		anthropicEvents := apicompat.ChatCompletionsChunkToAnthropicEvents(chunk, anthropicState)
		if clientDisconnected {
			return
		}
		for _, aEvt := range anthropicEvents {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(aEvt)
			if err != nil {
				continue
			}
			writeStreamHeaders()
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				clientDisconnected = true
				break
			}
		}
		if !clientDisconnected && len(anthropicEvents) > 0 {
			c.Writer.Flush()
		}
	}

	scan := s.scanCCStream(c, resp, "openai messages chat fallback", requestID, startTime, emitChunk)
	usage := scan.Usage

	if scan.Err != nil {
		// Broken upstream read: skip finalization so no synthetic message_stop
		// masks the truncation, and surface the error to flag usage incomplete
		// (mirrors forwardResponsesViaRawChatCompletions).
		return &OpenAIForwardResult{
			RequestID:                   requestID,
			Usage:                       usage,
			Model:                       originalModel,
			BillingModel:                billingModel,
			UpstreamModel:               upstreamModel,
			ReasoningEffort:             reasoningEffort,
			UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
			ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			Stream:                      true,
			Duration:                    time.Since(startTime),
			FirstTokenMs:                scan.FirstTokenMs,
			ClientDisconnect:            clientDisconnected,
		}, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}

	if failoverErr := s.clineEmptyStreamFailoverError(account, resp, scan, requestID, originalModel, upstreamModel); failoverErr != nil {
		return nil, failoverErr
	}

	// Finalize: close open blocks + emit message_delta/message_stop.
	finalEvents := apicompat.FinalizeChatCompletionsAnthropicStream(anthropicState)
	if !clientDisconnected {
		for _, aEvt := range finalEvents {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(aEvt)
			if err != nil {
				continue
			}
			writeStreamHeaders()
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				clientDisconnected = true
				break
			}
		}
		c.Writer.Flush()
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai messages chat fallback", requestID)
	}

	return &OpenAIForwardResult{
		RequestID:                   requestID,
		Usage:                       usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		ReasoningEffort:             reasoningEffort,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		UpstreamProvider:            observedUpstreamProvider(c),
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      true,
		Duration:                    time.Since(startTime),
		FirstTokenMs:                scan.FirstTokenMs,
		ClientDisconnect:            clientDisconnected,
	}, nil
}

// clineEmptyStreamClientMessage 是空响应耗尽重试后回给客户端的说明。
const clineEmptyStreamClientMessage = "Upstream returned an empty response, please retry later"

// clineEmptyStreamFailoverError 判定上游是否「返回 2xx 但整条流没有任何内容帧」，
// 命中时返回一个同账号可重试的 failover 错误；未命中返回 nil。
//
// 上游（cline 这类聚合网关）可能回一个非 SSE 的 JSON 体、一个带 error 字段的
// data 帧，或者一条直接结束的空流。这几种形态目前无法与「合法但没有输出」区分，
// 而对客户端来说都表现为空回复——原先会被静默补成全帧空消息，客户端只能自己
// 重试，且运维侧看不到任何记录。所以统一按上游失败处理，交给 failover 循环重试，
// 重试用尽后落一条错误记录。
//
// 判定只对 cline 平台且账号开关打开时生效，其他平台维持既有行为，避免把合法的
// 空回复（例如 max_tokens 截断）误判成上游故障。
func (s *OpenAIGatewayService) clineEmptyStreamFailoverError(
	account *Account,
	resp *http.Response,
	scan ccStreamScanState,
	requestID string,
	originalModel string,
	upstreamModel string,
) *UpstreamFailoverError {
	if account == nil || account.Platform != PlatformCline {
		return nil
	}
	if !account.GetClineEmptyStreamRetryEnabled() {
		return nil
	}
	if scan.Err != nil || scan.UsableChunks > 0 {
		return nil
	}

	logger.L().Warn("cline.empty_stream_failover",
		zap.Int64("account_id", account.ID),
		zap.String("request_id", requestID),
		zap.String("model", originalModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("saw_data_frame", scan.SawDataFrame),
		zap.Bool("saw_done", scan.SawDone),
		zap.String("in_band_error", scan.InBandError),
		zap.String("upstream_content_type", resp.Header.Get("Content-Type")),
		zap.String("upstream_request_id", resp.Header.Get("x-request-id")),
		zap.String("body_head", s.clineEmptyStreamBodyForLog(scan.RawHead)),
	)

	retryCount := account.GetClineEmptyStreamRetryCount()
	failoverErr := newOpenAIUpstreamFailoverError(
		http.StatusBadGateway,
		resp.Header,
		scan.RawHead,
		clineEmptyStreamUpstreamMessage(scan),
		retryCount > 0,
	)
	if retryCount > 0 {
		// 用 Floor 而不是 Max：Max 只能把账号的重试预算往下压，抬不上去，
		// 而非池模式账号的预算固定为 3。
		failoverErr.SameAccountRetryFloor = retryCount
		// 固定间隔轮询，不做指数退避：名额可能在下一次轮询就空出来，
		// 退避会让重试恰好睡在名额空出的那一刻。
		failoverErr.SameAccountRetryDelay = account.GetClineEmptyStreamRetryInterval()
	}
	// 重试后大概率自愈；且 cline 分组通常只有单个账号，摘号会让整分钟不可用，
	// 因此明确不据此惩罚账号。
	failoverErr.RequestScopedTransient = true
	failoverErr.ClientStatusCode = http.StatusBadGateway
	failoverErr.ClientMessage = clineEmptyStreamClientMessage
	return failoverErr
}

// clineEmptyStreamUpstreamMessage 汇总空响应的可读原因，用于错误记录与日志。
func clineEmptyStreamUpstreamMessage(scan ccStreamScanState) string {
	if scan.InBandError != "" {
		return scan.InBandError
	}
	if !scan.SawDataFrame {
		return "upstream response was not an SSE stream"
	}
	if scan.SawDone {
		return "upstream stream ended without any content"
	}
	return "upstream stream ended without content or done sentinel"
}

// clineEmptyStreamBodyForLog 按配置决定是否把上游响应开头写进日志，默认不落盘，
// 避免把上游正文（可能含用户内容片段）写进日志。
func (s *OpenAIGatewayService) clineEmptyStreamBodyForLog(head []byte) string {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.LogUpstreamErrorBody || len(head) == 0 {
		return ""
	}
	maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	if maxBytes <= 0 || maxBytes > len(head) {
		maxBytes = len(head)
	}
	return string(head[:maxBytes])
}
