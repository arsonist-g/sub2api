package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ClineHandler 暴露 Cline 订阅网关（api.cline.bot）的管理端查询端点：
//
//   - POST /admin/cline/models          实时拉取模型目录（订阅分组 + 按量目录）
//   - POST /admin/cline/provider-probe  探测逐模型可用的上游供应商
//
// 两个端点共用同一套凭据解析：传 account_id 走已落库账号（沿用其代理与请求头
// 覆写），否则用请求体里的 api_key 直连。供应商探测每个模型会发 1-2 次真实推理
// 请求，因此只由管理员显式触发，不做自动轮询。
type ClineHandler struct {
	catalogService *service.ClineCatalogService
	probeService   *service.ClineProviderProbeService
}

func NewClineHandler(
	catalogService *service.ClineCatalogService,
	probeService *service.ClineProviderProbeService,
) *ClineHandler {
	return &ClineHandler{
		catalogService: catalogService,
		probeService:   probeService,
	}
}

// clineRequest 是模型目录与供应商探测共用的入参。
type clineRequest struct {
	AccountID   *int64   `json:"account_id"`
	APIKey      string   `json:"api_key"`
	BaseURL     string   `json:"base_url"`
	AccountMode string   `json:"account_mode"`
	ModelIDs    []string `json:"model_ids"`
}

// LoadModels 拉取实时模型目录。
func (h *ClineHandler) LoadModels(c *gin.Context) {
	var req clineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if h == nil || h.catalogService == nil {
		response.BadRequest(c, "cline catalog service is not enabled")
		return
	}
	ctx := c.Request.Context()
	account, apiKey, baseURL, mode, err := h.catalogService.ResolveTarget(ctx, req.AccountID, req.APIKey, req.BaseURL, req.AccountMode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	catalog, err := h.catalogService.FetchModels(ctx, account, apiKey, baseURL, mode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, catalog)
}

// ProbeProviders 探测指定模型的上游供应商，返回逐模型的管道与可选供应商清单。
func (h *ClineHandler) ProbeProviders(c *gin.Context) {
	var req clineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if h == nil || h.probeService == nil || h.catalogService == nil {
		response.BadRequest(c, "cline provider probe service is not enabled")
		return
	}
	ctx := c.Request.Context()
	account, apiKey, baseURL, _, err := h.catalogService.ResolveTarget(ctx, req.AccountID, req.APIKey, req.BaseURL, req.AccountMode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	results, err := h.probeService.ProbeModels(ctx, account, apiKey, baseURL, req.ModelIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"models": results})
}
