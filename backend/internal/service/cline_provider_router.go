package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Cline 上游供应商（provider）路由。
//
// Cline 网关后面挂着不止一个上游执行器，同一个订阅模型可能落到不同管道：
//
//   - planner：Vercel AI Gateway。请求体顶层 provider.{only,order,sort} 会被吞掉，
//     必须写在 providerOptions.gateway.{only,order} 才生效。
//   - direct：OpenRouter。顶层 provider.{only,order} 生效，providerOptions 被忽略。
//   - private：固定私有上游，任何 provider 偏好都被静默忽略，没有可选供应商。
//
// 每个模型属于哪条管道由探测得出，不硬编码：用一个不存在的供应商名触发上游报错，
// 报错文本/结构里就带着该模型当前可用的供应商清单。
//
// 注意这些字段属未公开实现细节，上游可随时改动；因此默认 auto（不注入），
// 只在管理员显式选择后才写入请求体。
const (
	ClinePipelinePlanner = "planner"
	ClinePipelineDirect  = "direct"
	ClinePipelinePrivate = "private"
	ClinePipelineUnknown = "unknown"

	ClineProviderModeAuto  = "auto"
	ClineProviderModeOnly  = "only"
	ClineProviderModeOrder = "order"

	clineProbeSentinel = "__probe__"
	// clineProbeMaxTokens 是探测请求的输出预算。上游对过小的 max_tokens 直接返回
	// 500 {"error":"empty response content"}：此时报错里既没有供应商清单、也拿不到
	// 成功响应，探测会退化成 unknown；而推理模型要先花掉思考 token 才会产出正文，
	// 预算太小必然撞上这个 500。因此起点取一个推理模型也能出正文的量级。
	clineProbeMaxTokens = 256
	// clineProbeEscalatedMaxTokens 是命中空正文后的升级预算，只对仍被截断的模型多发一次。
	clineProbeEscalatedMaxTokens = 1024
	clineProbeTimeout            = 60 * time.Second
	clineProbeConcurrency        = 4
	// 一次探测每个模型要发 1-2 个真实推理请求，上限防止误用把额度打空。
	clineProbeMaxModels = 40
)

// providerListPattern 匹配 planner 管道（Vercel AI Gateway）报错里的供应商清单。
var providerListPattern = regexp.MustCompile(`Available providers are:\s*([^."]+)`)

// availableProvidersPattern 匹配 direct 管道（OpenRouter）报错里的结构化清单。
var availableProvidersPattern = regexp.MustCompile(`"available_providers":\s*(\[[^\]]*\])`)

// servingProvidersPattern 匹配 direct 管道的自然语言清单，作为结构化字段缺失时的兜底。
var servingProvidersPattern = regexp.MustCompile(`(?s)Providers serving [^:]+:\s*(.*?),\s*but your request`)

// ClineModelProviderPreference 是单个模型的上游供应商偏好（存于账号 credentials）。
type ClineModelProviderPreference struct {
	Pipeline  string   `json:"pipeline,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Providers []string `json:"providers,omitempty"`
}

// ClineProviderProbe 是单个模型的探测结果。
type ClineProviderProbe struct {
	Pipeline  string   `json:"pipeline"`
	Providers []string `json:"providers"`
	ProbedAt  string   `json:"probed_at"`
}

// GetClineModelProviders 解析 credentials["model_providers"]。
// 非 cline 账号或未配置时返回 nil。
func (a *Account) GetClineModelProviders() map[string]ClineModelProviderPreference {
	if a == nil || a.Platform != PlatformCline {
		return nil
	}
	raw, ok := a.Credentials["model_providers"]
	if !ok || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out map[string]ClineModelProviderPreference
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetClineModelProvider 取指定模型的有效偏好：mode 为 auto 或供应商清单为空
// 都视为「不注入」。
func (a *Account) GetClineModelProvider(modelID string) (ClineModelProviderPreference, bool) {
	prefs := a.GetClineModelProviders()
	if len(prefs) == 0 {
		return ClineModelProviderPreference{}, false
	}
	pref, ok := prefs[strings.TrimSpace(modelID)]
	if !ok {
		return ClineModelProviderPreference{}, false
	}
	providers := normalizedClineProviders(pref.Providers)
	if len(providers) == 0 {
		return ClineModelProviderPreference{}, false
	}
	mode := strings.TrimSpace(pref.Mode)
	if mode != ClineProviderModeOnly && mode != ClineProviderModeOrder {
		return ClineModelProviderPreference{}, false
	}
	pref.Mode = mode
	pref.Providers = providers
	return pref, true
}

// applyClineProviderPreference 按请求体中的模型写入上游供应商偏好。
// 非 cline 账号、未命中配置、或客户端已自行指定同类字段时原样返回。
func applyClineProviderPreference(account *Account, body []byte) []byte {
	if account == nil || account.Platform != PlatformCline || len(body) == 0 {
		return body
	}
	modelID := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if modelID == "" {
		return body
	}
	pref, ok := account.GetClineModelProvider(modelID)
	if !ok {
		return body
	}

	field := "only"
	if pref.Mode == ClineProviderModeOrder {
		field = "order"
	}
	// 管道未知时两种写法同时写：两条管道各自取用自己认得的那个，互不干扰。
	useGateway := pref.Pipeline == ClinePipelinePlanner || pref.Pipeline == ClinePipelineUnknown || pref.Pipeline == ""
	useDirect := pref.Pipeline == ClinePipelineDirect || pref.Pipeline == ClinePipelineUnknown || pref.Pipeline == ""

	updated := body
	if useGateway && !gjson.GetBytes(updated, "providerOptions.gateway."+field).Exists() {
		if next, err := sjson.SetBytes(updated, "providerOptions.gateway."+field, pref.Providers); err == nil {
			updated = next
		}
	}
	if useDirect && !gjson.GetBytes(updated, "provider."+field).Exists() {
		if next, err := sjson.SetBytes(updated, "provider."+field, pref.Providers); err == nil {
			updated = next
		}
	}
	return updated
}

// normalizedClineProviders 清洗供应商名并保持原顺序去重。
func normalizedClineProviders(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// extractClineProbeProviders 从探测响应中提取供应商清单。
//
// 上游把内部报错的 JSON 作为字符串嵌在外层 error 字段里，引号是转义过的，
// 因此必须先去转义再匹配，否则会漏掉 direct 管道的结构化清单。
func extractClineProbeProviders(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	text := strings.ReplaceAll(string(raw), `\"`, `"`)
	if match := providerListPattern.FindStringSubmatch(text); len(match) == 2 {
		return normalizedClineProviders(strings.Split(match[1], ","))
	}
	if match := availableProvidersPattern.FindStringSubmatch(text); len(match) == 2 {
		var list []string
		if err := json.Unmarshal([]byte(match[1]), &list); err == nil {
			return normalizedClineProviders(list)
		}
	}
	if match := servingProvidersPattern.FindStringSubmatch(text); len(match) == 2 {
		return normalizedClineProviders(strings.Split(match[1], ","))
	}
	return nil
}

// ClineProviderProbeService 探测 Cline 各模型的可用上游供应商。
type ClineProviderProbeService struct {
	accountRepo  AccountRepository
	proxyRepo    ProxyRepository
	httpUpstream HTTPUpstream
	cfg          *config.Config
}

// NewClineProviderProbeService 构造供应商探测服务。
func NewClineProviderProbeService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *ClineProviderProbeService {
	return &ClineProviderProbeService{accountRepo: accountRepo, proxyRepo: proxyRepo, httpUpstream: httpUpstream, cfg: cfg}
}

// ProbeModels 并发探测给定模型的管道与可用供应商。
func (s *ClineProviderProbeService) ProbeModels(
	ctx context.Context,
	account *Account,
	apiKey string,
	baseURL string,
	modelIDs []string,
) (map[string]ClineProviderProbe, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "CLINE_PROBE_NOT_CONFIGURED", "cline provider probe service is not configured")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CLINE_PROBE_NO_APIKEY", "api_key is required")
	}
	models := normalizedClineProviders(modelIDs)
	if len(models) == 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "CLINE_PROBE_NO_MODELS", "model_ids is required")
	}
	if len(models) > clineProbeMaxModels {
		models = models[:clineProbeMaxModels]
	}
	baseURL = normalizeClineBaseURL(baseURL)
	targetURL, err := cnValidateProbeURL(s.cfg, baseURL+"/chat/completions")
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "CLINE_PROBE_URL_REJECTED", err.Error())
	}

	proxyURL := s.resolveProxyURL(ctx, account)
	accountID := int64(0)
	concurrency := 1
	if account != nil {
		accountID = account.ID
		concurrency = maxInt(account.Concurrency, 1)
	}

	results := make(map[string]ClineProviderProbe, len(models))
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, clineProbeConcurrency)
	for _, modelID := range models {
		wg.Add(1)
		go func(modelID string) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			probe := s.probeModel(ctx, targetURL, apiKey, proxyURL, accountID, concurrency, account, modelID)
			mu.Lock()
			results[modelID] = probe
			mu.Unlock()
		}(modelID)
	}
	wg.Wait()
	return results, nil
}

// probeModel 依次用两种注入位置探测同一个模型，返回先给出清单的那种管道。
func (s *ClineProviderProbeService) probeModel(
	ctx context.Context,
	targetURL string,
	apiKey string,
	proxyURL string,
	accountID int64,
	concurrency int,
	account *Account,
	modelID string,
) ClineProviderProbe {
	probe := ClineProviderProbe{Pipeline: ClinePipelineUnknown, Providers: []string{}, ProbedAt: time.Now().UTC().Format(time.RFC3339)}
	sawSuccess := false

	attempts := []struct {
		pipeline string
		build    func() []byte
	}{
		{pipeline: ClinePipelinePlanner, build: func() []byte {
			return clineProbeBody(modelID, "providerOptions.gateway.only", clineProbeMaxTokens)
		}},
		{pipeline: ClinePipelineDirect, build: func() []byte {
			return clineProbeBody(modelID, "provider.only", clineProbeMaxTokens)
		}},
	}

	for _, attempt := range attempts {
		status, body, err := s.postProbe(ctx, targetURL, apiKey, proxyURL, accountID, concurrency, account, attempt.build())
		if err != nil {
			continue
		}
		// 预算不足导致的空正文不是供应商信号：抬预算重试同一管道一次，
		// 避免把本可探测的模型误判为 unknown。
		if clineProbeBudgetExhausted(body) {
			retryBody, _ := sjson.SetBytes(attempt.build(), "max_tokens", clineProbeEscalatedMaxTokens)
			if retryStatus, retryResp, retryErr := s.postProbe(ctx, targetURL, apiKey, proxyURL, accountID, concurrency, account, retryBody); retryErr == nil {
				status, body = retryStatus, retryResp
			}
		}
		if providers := extractClineProbeProviders(body); len(providers) > 0 {
			probe.Pipeline = attempt.pipeline
			probe.Providers = providers
			return probe
		}
		if status >= 200 && status < 300 {
			// 注入被静默忽略：请求真的打到了模型本身。
			sawSuccess = true
		}
	}

	if sawSuccess {
		probe.Pipeline = ClinePipelinePrivate
		return probe
	}
	return probe
}

// postProbe 发送一次探测请求，返回状态码与响应体。
func (s *ClineProviderProbeService) postProbe(
	ctx context.Context,
	targetURL string,
	apiKey string,
	proxyURL string,
	accountID int64,
	concurrency int,
	account *Account,
	body []byte,
) (int, []byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, clineProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if account != nil {
		account.ApplyHeaderOverrides(req.Header)
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, accountID, concurrency)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, clineModelsMaxBytes))
	return resp.StatusCode, respBody, nil
}

// clineProbeBody 构造探测请求体：用一个不存在的供应商名把上游的可用清单逼出来。
func clineProbeBody(modelID, path string, maxTokens int) []byte {
	body, _ := sjson.SetBytes(nil, "model", modelID)
	body, _ = sjson.SetBytes(body, "stream", false)
	body, _ = sjson.SetBytes(body, "max_tokens", maxTokens)
	body, _ = sjson.SetBytes(body, "messages", []map[string]string{{"role": "user", "content": "hi"}})
	body, _ = sjson.SetBytes(body, path, []string{clineProbeSentinel})
	return body
}

// clineProbeEmptyContentMarker 是上游在输出预算不足时返回的报错标记。
const clineProbeEmptyContentMarker = "empty response content"

// clineProbeBudgetExhausted 判断响应是否为「预算不足导致空正文」。
func clineProbeBudgetExhausted(body []byte) bool {
	return bytes.Contains(body, []byte(clineProbeEmptyContentMarker))
}

func (s *ClineProviderProbeService) resolveProxyURL(ctx context.Context, account *Account) string {
	if account == nil || account.ProxyID == nil {
		return ""
	}
	if account.Proxy != nil {
		return account.Proxy.URL()
	}
	if s != nil && s.proxyRepo != nil {
		if proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID); err == nil && proxy != nil {
			return proxy.URL()
		}
	}
	return ""
}
