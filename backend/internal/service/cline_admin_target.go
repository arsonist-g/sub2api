package service

import (
	"context"
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 管理端探测入口的凭据解析。
//
// 模型目录查询与供应商探测共用同一套入参：传 account_id 时加载已落库的 cline
// 账号并复用其 api_key / base_url / account_mode（以及代理与请求头覆写），否则
// 用请求体直传的 api_key + base_url 走「尚未保存的新账号」路径。

// ResolveTarget 解析管理端请求的目标凭据，返回归一化后的 base_url 与 mode。
func (s *ClineCatalogService) ResolveTarget(
	ctx context.Context,
	accountID *int64,
	apiKey string,
	baseURL string,
	mode string,
) (*Account, string, string, string, error) {
	apiKey = strings.TrimSpace(apiKey)
	mode = normalizeClineAccountMode(mode)
	if accountID != nil && *accountID > 0 {
		account, err := s.loadAccount(ctx, *accountID)
		if err != nil {
			return nil, "", "", "", err
		}
		credentialKey := strings.TrimSpace(account.GetCNAPIKey())
		if credentialKey == "" {
			return nil, "", "", "", infraerrors.New(http.StatusBadRequest, "CLINE_ACCOUNT_NO_APIKEY", "account api_key is empty")
		}
		credentialMode := normalizeClineAccountMode(account.GetAccountMode())
		return account, credentialKey, normalizeClineBaseURL(account.GetOpenAIBaseURL()), credentialMode, nil
	}
	if apiKey == "" {
		return nil, "", "", "", infraerrors.New(http.StatusBadRequest, "CLINE_CATALOG_NO_APIKEY", "api_key or account_id is required")
	}
	return nil, apiKey, normalizeClineBaseURL(baseURL), mode, nil
}

// loadAccount 加载并校验一个 cline 账号（管理端端点专用入口）。
func (s *ClineCatalogService) loadAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "CLINE_CATALOG_NOT_CONFIGURED", "cline catalog service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "CLINE_ACCOUNT_NOT_FOUND", "account not found: %d", accountID)
	}
	if account.Platform != PlatformCline {
		return nil, infraerrors.New(http.StatusBadRequest, "CLINE_ACCOUNT_INVALID_PLATFORM", "account is not a cline account")
	}
	return account, nil
}

// normalizeClineAccountMode 归一化账号模式：只有 payg 是显式值，其余（含空值）
// 一律按 coding 处理，与前端默认值一致。
func normalizeClineAccountMode(mode string) string {
	if strings.TrimSpace(mode) == AccountModePayG {
		return AccountModePayG
	}
	return AccountModeCoding
}
