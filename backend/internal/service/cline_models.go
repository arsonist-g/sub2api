package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

// Cline 订阅网关的模型目录服务。
//
// 上游模型清单有两条来源，均在推理端点之外：
//   - GET {base}/ai/cline/recommended-models：官方推荐模型，免鉴权，
//     clinePass[] 即订阅可用模型（coding 模式的权威来源）。
//   - GET {base}/models：OpenAI 兼容全量目录，不含 cline-pass/*，
//     是 payg（Credits 余额）模式的可用模型来源。
//
// 静态清单仅在上游接口不可用时兜底，避免模型候选整个消失。
const (
	clineUpstreamTimeout = 30 * time.Second
	clineModelsMaxBytes  = 8 << 20
)

// ClineModelOption 是模型目录条目（管理端 UI 消费）。
type ClineModelOption struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	OwnedBy     string   `json:"owned_by,omitempty"`
}

// ClineModelGroup 是按来源分组的模型清单。
type ClineModelGroup struct {
	Key    string             `json:"key"`
	Label  string             `json:"label"`
	Models []ClineModelOption `json:"models"`
}

// ClineModelCatalog 是一次模型拉取的完整结果。
type ClineModelCatalog struct {
	Mode      string             `json:"mode"`
	Sources   []string           `json:"sources"`
	Groups    []ClineModelGroup  `json:"groups"`
	Catalog   []ClineModelOption `json:"catalog,omitempty"`
	FetchedAt string             `json:"fetched_at"`
}

// ClineCatalogService 拉取 Cline 的实时模型清单。
type ClineCatalogService struct {
	accountRepo  AccountRepository
	proxyRepo    ProxyRepository
	httpUpstream HTTPUpstream
	cfg          *config.Config
}

// NewClineCatalogService 构造 Cline 模型目录服务。
func NewClineCatalogService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *ClineCatalogService {
	return &ClineCatalogService{
		accountRepo:  accountRepo,
		proxyRepo:    proxyRepo,
		httpUpstream: httpUpstream,
		cfg:          cfg,
	}
}

// FetchModels 拉取指定凭据可见的模型清单。
//
// account 为空表示「尚未落库的新账号」：使用直传 apiKey + 无代理调用；
// account 非空则复用账号的代理与请求头覆写，保证与真实转发同一条出口。
func (s *ClineCatalogService) FetchModels(
	ctx context.Context,
	account *Account,
	apiKey string,
	baseURL string,
	mode string,
) (*ClineModelCatalog, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "CLINE_CATALOG_NOT_CONFIGURED", "cline catalog service is not configured")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CLINE_CATALOG_NO_APIKEY", "api_key is required")
	}
	baseURL = normalizeClineBaseURL(baseURL)
	if mode != AccountModePayG {
		mode = AccountModeCoding
	}

	catalog := &ClineModelCatalog{
		Mode:      mode,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	subscription, subErr := s.fetchSubscriptionModels(ctx, account, apiKey, baseURL)
	if subErr == nil && len(subscription) > 0 {
		catalog.Groups = append(catalog.Groups, ClineModelGroup{
			Key:    "clinePass",
			Label:  "Cline Pass",
			Models: subscription,
		})
		catalog.Sources = append(catalog.Sources, "cline.api")
	}

	if mode == AccountModePayG {
		models, err := s.fetchCatalogModels(ctx, account, apiKey, baseURL)
		if err == nil && len(models) > 0 {
			catalog.Catalog = models
			catalog.Sources = append(catalog.Sources, "cline.catalog")
		}
	}

	if len(catalog.Groups) == 0 && len(catalog.Catalog) == 0 {
		// 上游两个来源都不可用时用静态清单兜底，让模型候选不至于整个消失。
		if subErr != nil {
			return nil, subErr
		}
		catalog.Groups = append(catalog.Groups, ClineModelGroup{
			Key:    "clinePass",
			Label:  "Cline Pass",
			Models: clineStaticModelOptions(),
		})
		catalog.Sources = append(catalog.Sources, "static")
	}
	return catalog, nil
}

// fetchSubscriptionModels 读取官方推荐模型接口中的 clinePass 清单。
func (s *ClineCatalogService) fetchSubscriptionModels(
	ctx context.Context,
	account *Account,
	apiKey string,
	baseURL string,
) ([]ClineModelOption, error) {
	body, err := s.get(ctx, account, apiKey, baseURL, "/ai/cline/recommended-models")
	if err != nil {
		return nil, err
	}
	var out []ClineModelOption
	for _, item := range gjson.GetBytes(body, "clinePass").Array() {
		id := strings.TrimSpace(item.Get("id").String())
		if id == "" {
			continue
		}
		out = append(out, ClineModelOption{
			ID:          id,
			Name:        strings.TrimSpace(item.Get("name").String()),
			Description: strings.TrimSpace(item.Get("description").String()),
			Tags:        clineStringList(item.Get("tags")),
		})
	}
	return out, nil
}

// fetchCatalogModels 读取 OpenAI 兼容 /models 目录（payg 模式的可用模型）。
func (s *ClineCatalogService) fetchCatalogModels(
	ctx context.Context,
	account *Account,
	apiKey string,
	baseURL string,
) ([]ClineModelOption, error) {
	body, err := s.get(ctx, account, apiKey, baseURL, "/models")
	if err != nil {
		return nil, err
	}
	out := make([]ClineModelOption, 0, 256)
	for _, item := range gjson.GetBytes(body, "data").Array() {
		id := strings.TrimSpace(item.Get("id").String())
		if id == "" {
			continue
		}
		out = append(out, ClineModelOption{ID: id, OwnedBy: strings.TrimSpace(item.Get("owned_by").String())})
	}
	return out, nil
}

// get 执行一次带鉴权的只读 GET，出口与真实转发保持一致（代理 + 请求头覆写）。
func (s *ClineCatalogService) get(
	ctx context.Context,
	account *Account,
	apiKey string,
	baseURL string,
	path string,
) ([]byte, error) {
	targetURL := strings.TrimRight(baseURL, "/") + path
	validatedURL, err := cnValidateProbeURL(s.cfg, targetURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "CLINE_CATALOG_URL_REJECTED", err.Error())
	}
	proxyURL := s.resolveProxyURL(ctx, account)
	callCtx, cancel := context.WithTimeout(ctx, clineUpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, validatedURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "CLINE_CATALOG_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	accountID := int64(0)
	concurrency := 1
	if account != nil {
		account.ApplyHeaderOverrides(req.Header)
		accountID = account.ID
		concurrency = maxInt(account.Concurrency, 1)
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, accountID, concurrency)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CLINE_CATALOG_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, clineModelsMaxBytes))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, infraerrors.Newf(http.StatusUnauthorized, "CLINE_CATALOG_UNAUTHORIZED", "cline rejected the api key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CLINE_CATALOG_UPSTREAM_ERROR", "cline api error (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 240))
	}
	return body, nil
}

func (s *ClineCatalogService) resolveProxyURL(ctx context.Context, account *Account) string {
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

// normalizeClineBaseURL 归一化 base_url：空值回落到官方地址，并去掉尾部斜杠。
func normalizeClineBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return DefaultClineBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func clineStringList(result gjson.Result) []string {
	if !result.IsArray() {
		return nil
	}
	out := make([]string, 0, len(result.Array()))
	for _, item := range result.Array() {
		if v := strings.TrimSpace(item.String()); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// clineStaticModelOptions 是官方订阅模型的静态兜底清单。
func clineStaticModelOptions() []ClineModelOption {
	out := make([]ClineModelOption, 0, len(clineStaticModelIDs))
	for _, id := range clineStaticModelIDs {
		out = append(out, ClineModelOption{ID: id})
	}
	return out
}

// ClineDefaultModelIDs 返回 Cline 订阅模型的静态清单（接口不可用时的兜底）。
func ClineDefaultModelIDs() []string {
	return append([]string(nil), clineStaticModelIDs...)
}

var clineStaticModelIDs = []string{
	"cline-pass/qwen3.8-max",
	"cline-pass/qwen3.7-max",
	"cline-pass/qwen3.7-plus",
	"cline-pass/glm-5.3",
	"cline-pass/glm-5.3-flash",
	"cline-pass/glm-5.2",
	"cline-pass/kimi-k3",
	"cline-pass/kimi-k2.7-code",
	"cline-pass/kimi-k2.6",
	"cline-pass/minimax-m3",
	"cline-pass/mimo-v2.5",
	"cline-pass/mimo-v2.5-pro",
	"cline-pass/deepseek-v4-pro",
	"cline-pass/deepseek-v4.1-flash",
	"cline-pass/deepseek-v4-flash",
}
