package jira

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type searchStatusResponse struct {
	statusCode  int
	retryAfter  string
	contentType string
	location    string
	finalURL    string
	body        []byte
}

type browserSearchResponse struct {
	statusCode  int
	retryAfter  string
	contentType string
	location    string
}

func (s *StatusService) resolveSearch(group string, transport groupTransport, requestURL string, headers map[string]string) (searchStatusResponse, bool, error) {
	return s.resolveSearchWithContext(context.Background(), group, transport, requestURL, headers)
}

func (s *StatusService) resolveSearchWithContext(ctx context.Context, group string, transport groupTransport, requestURL string, headers map[string]string) (searchStatusResponse, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if transport == groupTransportBrowser {
		responseHeaders, responseBody, browserErr := s.resolveStatusViaBrowser(ctx, requestURL, headers)
		if browserErr == nil {
			return searchStatusResponse{
				statusCode:  responseHeaders.statusCode,
				retryAfter:  responseHeaders.retryAfter,
				contentType: responseHeaders.contentType,
				location:    responseHeaders.location,
				body:        responseBody,
			}, false, nil
		}

		s.logBrowserFallback(group, browserErr)

		httpResponse, err := s.resolveStatusViaHTTP(ctx, group, requestURL, headers)
		if err != nil {
			return searchStatusResponse{}, true, err
		}
		return httpResponse, true, nil
	}

	httpResponse, err := s.resolveStatusViaHTTP(ctx, group, requestURL, headers)
	if err != nil {
		return searchStatusResponse{}, false, err
	}

	return httpResponse, false, nil
}

// groupHTTPClient возвращает http-клиент группы (mTLS/CA) или общий клиент сервиса.
func (s *StatusService) groupHTTPClient(group string) httpDoer {
	if gs, ok := s.groups[group]; ok && gs.httpClient != nil {
		return gs.httpClient
	}
	return s.httpClient
}

func (s *StatusService) resolveStatusViaHTTP(ctx context.Context, group string, requestURL string, headers map[string]string) (searchStatusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return searchStatusResponse{}, fmt.Errorf("собрать jira-запрос: %w", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := s.groupHTTPClient(group).Do(req)
	if err != nil {
		return searchStatusResponse{}, fmt.Errorf("ошибка http-запроса jira: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return searchStatusResponse{}, fmt.Errorf("прочитать тело ответа jira: %w", err)
	}

	return searchStatusResponse{
		statusCode:  resp.StatusCode,
		retryAfter:  resp.Header.Get("Retry-After"),
		contentType: resp.Header.Get("Content-Type"),
		location:    resp.Header.Get("Location"),
		finalURL:    finalResponseURL(resp),
		body:        body,
	}, nil
}

func (s *StatusService) resolveStatusViaBrowser(ctx context.Context, requestURL string, headers map[string]string) (browserSearchResponse, []byte, error) {
	if s.browser == nil {
		return browserSearchResponse{}, nil, fmt.Errorf("browser runtime для jira не настроен")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	statusCode, responseHeaders, body, err := s.browser.RequestGET(ctx, requestURL, headers)
	if err != nil {
		return browserSearchResponse{}, nil, fmt.Errorf("ошибка browser-запроса jira: %w", err)
	}

	return browserSearchResponse{
		statusCode:  statusCode,
		retryAfter:  headerValue(responseHeaders, "Retry-After"),
		contentType: headerValue(responseHeaders, "Content-Type"),
		location:    headerValue(responseHeaders, "Location"),
	}, body, nil
}

func (s *StatusService) logBrowserFallback(group string, browserErr error) {
	if s == nil || s.logger == nil {
		return
	}
	if !s.markBrowserFallbackWarned(group) {
		return
	}

	fields := []zap.Field{
		zap.String("group", strings.TrimSpace(group)),
		zap.String("fallback_transport", string(groupTransportHTTP)),
	}
	if browserErr != nil {
		fields = append(fields, zap.String("browser_error", browserErr.Error()))
	}

	s.logger.Warn("jira browser transport unavailable, using http fallback", fields...)
}

func (s *StatusService) markBrowserFallbackWarned(group string) bool {
	if s == nil {
		return false
	}

	group = strings.TrimSpace(group)
	if group == "" {
		group = "-"
	}

	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()

	if s.fallbackWarned[group] {
		return false
	}
	s.fallbackWarned[group] = true
	return true
}

func finalResponseURL(resp *http.Response) string {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return ""
	}
	return resp.Request.URL.String()
}

func buildRequestHeaders(auth groupAuth) map[string]string {
	headers := map[string]string{
		"Accept": "application/json",
	}

	token := strings.TrimSpace(auth.token)
	if token != "" {
		headers["Authorization"] = "Bearer " + token
		return headers
	}

	username := strings.TrimSpace(auth.username)
	if username == "" || auth.password == "" {
		return headers
	}

	req, err := http.NewRequest(http.MethodGet, "https://jira.local", nil)
	if err != nil {
		return headers
	}
	req.SetBasicAuth(username, auth.password)
	if authHeader := strings.TrimSpace(req.Header.Get("Authorization")); authHeader != "" {
		headers["Authorization"] = authHeader
	}

	return headers
}

func headerValue(headers map[string]string, key string) string {
	for headerKey, value := range headers {
		if strings.EqualFold(headerKey, key) {
			return value
		}
	}

	return ""
}
