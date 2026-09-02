package service

import "net/http"

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// SetTLSFingerprintProfileService 注入 TLS 指纹模板服务，供 zhipu 账号伪装
// 决策解析 profile 使用（handler 层 setter 注入，走 wire provider）。
func (s *OpenAIGatewayService) SetTLSFingerprintProfileService(service *TLSFingerprintProfileService) {
	s.tlsFPProfileService = service
}

// SetIdentityService 注入身份服务，供 zhipu anthropic 协议透传路径做会话
// ID 伪装使用（handler 层 setter 注入，走 wire provider）。
func (s *OpenAIGatewayService) SetIdentityService(service *IdentityService) {
	s.identityService = service
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
// zhipu 账号带伪装决策（ctx 中 TLSProfile 非 nil）时改走 DoWithTLS。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if d := zhipuSpoofDecisionFromContext(request.Context()); d != nil && d.TLSProfile != nil {
		return s.httpUpstream.DoWithTLS(request, proxyURL, account.ID, account.Concurrency, d.TLSProfile)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}
