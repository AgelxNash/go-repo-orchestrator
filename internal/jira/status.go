package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/browser"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"

	"go.uber.org/zap"
)

const defaultStatusTimeout = 5 * time.Second
const defaultTransientStatusTTL = 15 * time.Second
const maxTransientStatusTTL = 2 * time.Minute
const jiraSearchBatchSize = 500

type StatusBatchRequest struct {
	Group       string
	TicketURL   string
	JiraBaseURL string
	Key         string
}

// PrefetchBatchProgress описывает прогресс пакетной проверки Jira-статусов.
type PrefetchBatchProgress struct {
	BatchIndex int
	BatchTotal int
	BatchSize  int
	Processed  int
	Total      int
}

type PrefetchProgressCallback func(PrefetchBatchProgress)

type cacheEntry struct {
	result    StatusResult
	expiresAt time.Time
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type browserRequester interface {
	RequestGET(ctx context.Context, requestURL string, headers map[string]string) (int, map[string]string, []byte, error)
}

type groupTransport string

const (
	groupTransportHTTP    groupTransport = "http"
	groupTransportBrowser groupTransport = "browser"
)

type groupAuth struct {
	token    string
	username string
	password string
}

type groupSettings struct {
	baseURL    string
	transport  groupTransport
	auth       groupAuth
	httpClient httpDoer // per-group клиент (mTLS/CA); nil — использовать общий
}

type StatusService struct {
	httpClient httpDoer
	browser    browserRequester
	groups     map[string]groupSettings
	logger     *zap.Logger

	fallbackMu     sync.Mutex
	fallbackWarned map[string]bool

	mu    sync.RWMutex
	cache map[string]cacheEntry

	fetchMu sync.Mutex
}

type StatusServiceOption func(*StatusService)

func NewStatusService(timeout time.Duration, opts ...StatusServiceOption) *StatusService {
	if timeout <= 0 {
		timeout = defaultStatusTimeout
	}

	svc := &StatusService{
		httpClient:     &http.Client{Timeout: timeout},
		groups:         make(map[string]groupSettings),
		cache:          make(map[string]cacheEntry),
		fallbackWarned: make(map[string]bool),
		logger:         zap.NewNop(),
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(svc)
	}

	return svc
}

func WithGroupConfigs(timeout time.Duration, groups []config.JiraConfig) (StatusServiceOption, error) {
	option := func(s *StatusService) {
		if s == nil {
			return
		}

		next := make(map[string]groupSettings, len(groups))
		for _, group := range groups {
			name := strings.TrimSpace(group.Group)
			if name == "" {
				continue
			}

			transport := groupTransportHTTP
			if group.Playwright {
				transport = groupTransportBrowser
			}

			token := strings.TrimSpace(group.Token)
			if strings.EqualFold(strings.TrimSpace(group.Type), "token") {
				token = strings.TrimSpace(group.Token)
			}

			next[name] = groupSettings{
				baseURL:   normalizeBaseURL(group.URL),
				transport: transport,
				auth: groupAuth{
					token:    token,
					username: strings.TrimSpace(group.Login.Username),
					password: group.Login.Password,
				},
			}
		}

		s.groups = next
	}

	// Fail fast: сертификаты mTLS-групп проверяются и загружаются один раз при старте.
	perGroup := make(map[string]httpDoer, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Group)
		if name == "" || group.SSL.IsZero() {
			continue
		}
		client, err := buildGroupHTTPClient(timeout, group.SSL)
		if err != nil {
			return nil, fmt.Errorf("jira-группа %q: %w", name, err)
		}
		if client != nil {
			perGroup[name] = client
		}
	}
	if len(perGroup) > 0 {
		inner := option
		option = func(s *StatusService) {
			inner(s)
			if s == nil {
				return
			}
			for name, client := range perGroup {
				if gs, ok := s.groups[name]; ok {
					gs.httpClient = client
					s.groups[name] = gs
				}
			}
		}
	}

	return option, nil
}

func WithBrowserRuntime(runtime *browser.PlaywrightRuntime) StatusServiceOption {
	return func(s *StatusService) {
		if s == nil {
			return
		}

		s.browser = runtime
	}
}

func WithLogger(logger *zap.Logger) StatusServiceOption {
	return func(s *StatusService) {
		if s == nil || logger == nil {
			return
		}

		s.logger = logger
	}
}

func (s *StatusService) ResolveStatus(group, ticketURL, jiraBaseURL, key string) StatusResult {
	if s == nil {
		return StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonTransportError}
	}

	req := StatusBatchRequest{
		Group:       group,
		TicketURL:   ticketURL,
		JiraBaseURL: jiraBaseURL,
		Key:         key,
	}
	s.PrefetchStatuses([]StatusBatchRequest{req})

	cacheKey, cacheErr := s.cacheKeyForRequest(req)
	if cacheErr != nil {
		return cacheErr.result
	}
	if result, ok := s.cached(cacheKey); ok {
		return result
	}

	return StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonTransportError}
}

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

func (s *StatusService) PrefetchStatuses(requests []StatusBatchRequest) {
	_ = s.PrefetchStatusesWithProgress(requests, nil)
}

func (s *StatusService) PrefetchStatusesWithProgress(requests []StatusBatchRequest, onProgress PrefetchProgressCallback) []PrefetchBatchProgress {
	if s == nil || len(requests) == 0 {
		return nil
	}

	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()

	buckets := make(map[string][]preparedStatusRequest)
	seenByBucket := make(map[string]map[string]struct{})

	for _, req := range requests {
		prepared, cacheErr, ok := s.prepareStatusRequest(req)
		if cacheErr != nil {
			if cacheErr.cacheKey != "" {
				s.store(cacheErr.cacheKey, cacheErr.result)
			}
			continue
		}
		if !ok {
			continue
		}

		if _, hit := s.cached(prepared.cacheKey); hit {
			continue
		}

		bucketKey := prepared.bucketKey()
		if _, exists := seenByBucket[bucketKey]; !exists {
			seenByBucket[bucketKey] = make(map[string]struct{})
		}
		if _, dup := seenByBucket[bucketKey][prepared.cacheKey]; dup {
			continue
		}

		seenByBucket[bucketKey][prepared.cacheKey] = struct{}{}
		buckets[bucketKey] = append(buckets[bucketKey], prepared)
	}

	batchTotal := 0
	total := 0
	for _, bucketRequests := range buckets {
		total += len(bucketRequests)
		batchTotal += (len(bucketRequests) + jiraSearchBatchSize - 1) / jiraSearchBatchSize
	}

	if total == 0 || batchTotal == 0 {
		return nil
	}

	progress := make([]PrefetchBatchProgress, 0, batchTotal)
	processed := 0
	batchIndex := 0

	for _, bucketRequests := range buckets {
		for start := 0; start < len(bucketRequests); start += jiraSearchBatchSize {
			end := min(start+jiraSearchBatchSize, len(bucketRequests))
			batch := bucketRequests[start:end]
			s.fetchAndStoreBatch(batch)

			batchIndex++
			processed += len(batch)
			progressItem := PrefetchBatchProgress{
				BatchIndex: batchIndex,
				BatchTotal: batchTotal,
				BatchSize:  len(batch),
				Processed:  processed,
				Total:      total,
			}
			if onProgress != nil {
				onProgress(progressItem)
			}
			progress = append(progress, progressItem)
		}
	}

	return progress
}

type preparedStatusRequest struct {
	group     string
	cacheKey  string
	key       string
	baseURL   string
	transport groupTransport
	headers   map[string]string
}

func (r preparedStatusRequest) bucketKey() string {
	prefix := r.key
	if idx := strings.Index(r.key, "-"); idx > 0 {
		prefix = r.key[:idx]
	}
	return r.group + "|" + r.baseURL + "|" + string(r.transport) + "|" + prefix
}

type statusCacheError struct {
	cacheKey string
	result   StatusResult
}

func (s *StatusService) cacheKeyForRequest(req StatusBatchRequest) (string, *statusCacheError) {
	group := strings.TrimSpace(req.Group)
	groupCfg, ok := s.groups[group]
	if !ok {
		return "", &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateUnmapped, Reason: StatusReasonNoGroupConfig}}
	}

	resolvedBaseURL := normalizeBaseURL(req.JiraBaseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = groupCfg.baseURL
	}

	cacheKey := buildCacheKey(group, req.TicketURL, resolvedBaseURL, req.Key)
	if cacheKey == "" {
		return "", &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonInvalidRequest}}
	}

	return cacheKey, nil
}

func (s *StatusService) prepareStatusRequest(req StatusBatchRequest) (preparedStatusRequest, *statusCacheError, bool) {
	group := strings.TrimSpace(req.Group)
	groupCfg, ok := s.groups[group]
	if !ok {
		return preparedStatusRequest{}, &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateUnmapped, Reason: StatusReasonNoGroupConfig}}, false
	}

	resolvedBaseURL := normalizeBaseURL(req.JiraBaseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = groupCfg.baseURL
	}

	cacheKey := buildCacheKey(group, req.TicketURL, resolvedBaseURL, req.Key)
	if cacheKey == "" {
		return preparedStatusRequest{}, &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonInvalidRequest}}, false
	}

	return preparedStatusRequest{
		group:     group,
		cacheKey:  cacheKey,
		key:       strings.ToUpper(strings.TrimSpace(req.Key)),
		baseURL:   resolvedBaseURL,
		transport: groupCfg.transport,
		headers:   buildRequestHeaders(groupCfg.auth),
	}, nil, true
}

func (s *StatusService) fetchAndStoreBatch(batch []preparedStatusRequest) {
	if len(batch) == 0 {
		return
	}

	startAt := 0
	allStatuses := make(map[string]string)
	var finalReason StatusReason
	var usedBrowserOverall bool

	for {
		searchURL, err := buildSearchStatusURL(batch[0].baseURL, extractKeys(batch), startAt)
		if err != nil {
			result := StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonInvalidRequest}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		s.logger.Debug("jira search request",
			zap.String("url", searchURL),
			zap.String("group", batch[0].group),
			zap.Int("keys", len(batch)),
			zap.Int("startAt", startAt),
		)

		response, usedBrowserFallback, requestErr := s.resolveSearch(batch[0].group, batch[0].transport, searchURL, batch[0].headers)
		if usedBrowserFallback {
			usedBrowserOverall = true
		}

		if requestErr != nil {
			s.logger.Warn("jira request failed",
				zap.String("url", searchURL),
				zap.String("group", batch[0].group),
				zap.Error(requestErr),
			)
			reason := StatusReasonTransportError
			if usedBrowserFallback {
				reason = StatusReasonBrowserUnavailableHTTPError
			}
			result := StatusResult{Status: unknownStatus, State: StatusStateTransient, Reason: reason}
			for _, req := range batch {
				s.storeTransient(req.cacheKey, result, defaultTransientStatusTTL)
			}
			return
		}

		if response.statusCode != http.StatusOK {
			s.logger.Warn("jira non-200 response",
				zap.String("url", searchURL),
				zap.String("group", batch[0].group),
				zap.Int("status_code", response.statusCode),
				zap.String("final_url", response.finalURL),
			)
		}

		if reason, ok := detectAuthReason(response.statusCode, response.location, response.finalURL, response.contentType, response.body); ok {
			result := StatusResult{Status: unknownStatus, State: StatusStateAuth, Reason: reason}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		if response.statusCode != http.StatusOK {
			if response.statusCode == http.StatusBadRequest {
				validBatch, invalidBatch := filterInvalidKeys(batch, response.body)
				if len(invalidBatch) > 0 {
					s.logger.Debug("jira 400 bad request: filtering invalid keys and retrying",
						zap.Int("invalid_count", len(invalidBatch)),
						zap.Int("valid_count", len(validBatch)),
					)
					for _, req := range invalidBatch {
						s.store(req.cacheKey, StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonIssueNotFound})
					}
					batch = validBatch
					if len(batch) == 0 {
						return
					}
					continue
				}
			}

			if ttl, ok := transientFailureTTL(response.statusCode, response.retryAfter); ok {
				result := StatusResult{Status: unknownStatus, State: StatusStateTransient, Reason: StatusReasonTemporarilyDown}
				for _, req := range batch {
					s.storeTransient(req.cacheKey, result, ttl)
				}
				return
			}

			reason := StatusReasonHTTPError
			if response.statusCode == http.StatusNotFound {
				reason = StatusReasonIssueNotFound
			} else if response.statusCode >= 400 && response.statusCode <= 499 {
				reason = StatusReasonClientError
			} else if usedBrowserFallback {
				reason = StatusReasonBrowserUnavailableHTTPError
			}
			result := StatusResult{Status: unknownStatus, State: StatusStateError, Reason: reason}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		statusByKey, total, received, parseErr := parseSearchStatuses(response.body)
		if parseErr != nil {
			result := StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonResponseParseErr}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		for k, v := range statusByKey {
			allStatuses[k] = v
		}

		startAt += received
		if startAt >= total || received == 0 {
			break
		}
	}

	finalReason = StatusReasonNone
	if usedBrowserOverall {
		finalReason = StatusReasonBrowserUnavailableHTTPFallback
	}

	for _, req := range batch {
		status := strings.TrimSpace(allStatuses[req.key])
		if status == "" {
			s.store(req.cacheKey, StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonIssueNotFound})
			continue
		}

		s.store(req.cacheKey, StatusResult{Status: status, State: StatusStateReady, Reason: finalReason})
	}
}

func extractKeys(batch []preparedStatusRequest) []string {
	keys := make([]string, 0, len(batch))
	for _, req := range batch {
		keys = append(keys, req.key)
	}
	return keys
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

func detectAuthReason(statusCode int, location, finalURL, contentType string, body []byte) (StatusReason, bool) {
	switch statusCode {
	case http.StatusUnauthorized:
		return StatusReasonAuthRequired, true
	case http.StatusForbidden:
		return StatusReasonForbidden, true
	}

	if isLikelyLoginRedirect(statusCode, location) {
		return StatusReasonLoginRequired, true
	}

	if isLikelyLoginURL(finalURL) {
		return StatusReasonLoginRequired, true
	}

	if isUnexpectedHTMLResponse(contentType, body) {
		return StatusReasonLoginRequired, true
	}

	return StatusReasonNone, false
}

func isLikelyLoginRedirect(statusCode int, location string) bool {
	if statusCode < 300 || statusCode >= 400 {
		return false
	}
	return isLikelyLoginURL(location)
}

func isLikelyLoginURL(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return false
	}

	markers := []string{"/login", "/signin", "/auth", "sso", "oauth", "atlassian"}
	for _, marker := range markers {
		if strings.Contains(raw, marker) {
			return true
		}
	}

	return false
}

func isUnexpectedHTMLResponse(contentType string, body []byte) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(contentType, "text/html") {
		return true
	}

	snippet := strings.ToLower(strings.TrimSpace(string(body)))
	if strings.HasPrefix(snippet, "<!doctype html") || strings.HasPrefix(snippet, "<html") {
		return true
	}

	return false
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

func (s *StatusService) cached(cacheKey string) (StatusResult, bool) {
	s.mu.RLock()
	entry, ok := s.cache[cacheKey]
	s.mu.RUnlock()
	if !ok {
		return StatusResult{}, false
	}

	if entry.expiresAt.IsZero() || time.Now().Before(entry.expiresAt) {
		return entry.result, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok = s.cache[cacheKey]
	if !ok {
		return StatusResult{}, false
	}

	if entry.expiresAt.IsZero() || time.Now().Before(entry.expiresAt) {
		return entry.result, true
	}

	delete(s.cache, cacheKey)
	return StatusResult{}, false
}

func (s *StatusService) store(cacheKey string, result StatusResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[cacheKey] = cacheEntry{result: result}
}

func (s *StatusService) storeTransient(cacheKey string, result StatusResult, ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultTransientStatusTTL
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[cacheKey] = cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
}

func transientFailureTTL(statusCode int, retryAfter string) (time.Duration, bool) {
	switch {
	case statusCode == http.StatusTooManyRequests:
		if ttl, ok := parseRetryAfter(retryAfter); ok {
			return ttl, true
		}
		return defaultTransientStatusTTL, true
	case statusCode >= 500 && statusCode <= 599:
		return defaultTransientStatusTTL, true
	default:
		return 0, false
	}
}

func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		if seconds <= 0 {
			return defaultTransientStatusTTL, true
		}
		if seconds > maxTransientStatusTTL {
			return maxTransientStatusTTL, true
		}
		return seconds, true
	}

	deadline, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}

	ttl := time.Until(deadline)
	if ttl <= 0 {
		return defaultTransientStatusTTL, true
	}
	if ttl > maxTransientStatusTTL {
		return maxTransientStatusTTL, true
	}

	return ttl, true
}

func buildCacheKey(group, ticketURL, jiraBaseURL, key string) string {
	group = strings.TrimSpace(group)
	base := normalizeBaseURL(jiraBaseURL)
	if base == "" {
		base = jiraBaseFromTicketURL(ticketURL)
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if group == "" || base == "" || key == "" || key == "-" {
		return ""
	}

	return group + "|" + base + "|" + key
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return ""
	}

	return strings.TrimRight(raw, "/")
}

func buildSearchStatusURL(jiraBaseURL string, keys []string, startAt int) (string, error) {
	base := normalizeBaseURL(jiraBaseURL)
	if base == "" || len(keys) == 0 {
		return "", fmt.Errorf("url для jira-статуса неполный")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("разобрать базовый url jira: %w", err)
	}

	jqlParts := make([]string, 0, len(keys))
	for _, key := range keys {
		normalized := strings.TrimSpace(key)
		if normalized == "" || normalized == "-" {
			continue
		}
		jqlParts = append(jqlParts, "\""+strings.ReplaceAll(normalized, "\"", "\\\"")+"\"")
	}
	if len(jqlParts) == 0 {
		return "", fmt.Errorf("url для jira-статуса неполный")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/rest/api/2/search"
	query := parsed.Query()
	query.Set("jql", "key in ("+strings.Join(jqlParts, ", ")+")")
	query.Set("fields", "status")
	query.Set("maxResults", fmt.Sprintf("%d", jiraSearchBatchSize))
	if startAt > 0 {
		query.Set("startAt", fmt.Sprintf("%d", startAt))
	}
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func jiraBaseFromTicketURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	path := parsed.Path
	idx := strings.Index(path, "/browse/")
	if idx >= 0 {
		path = path[:idx]
	}

	path = strings.TrimRight(path, "/")
	if path == "" {
		return parsed.Scheme + "://" + parsed.Host
	}

	return parsed.Scheme + "://" + parsed.Host + path
}

func parseSearchStatuses(body []byte) (map[string]string, int, int, error) {
	var payload struct {
		Total  int `json:"total"`
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Status struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, 0, fmt.Errorf("декодировать ответ jira issue: %w", err)
	}

	statusByKey := make(map[string]string, len(payload.Issues))
	for _, issue := range payload.Issues {
		key := strings.ToUpper(strings.TrimSpace(issue.Key))
		if key == "" {
			continue
		}
		name := strings.TrimSpace(issue.Fields.Status.Name)
		if name == "" {
			continue
		}
		statusByKey[key] = name
	}

	return statusByKey, payload.Total, len(payload.Issues), nil
}

func filterInvalidKeys(batch []preparedStatusRequest, body []byte) ([]preparedStatusRequest, []preparedStatusRequest) {
	var payload struct {
		ErrorMessages []string `json:"errorMessages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return batch, nil
	}

	invalidKeysMap := make(map[string]bool)
	for _, msg := range payload.ErrorMessages {
		for _, req := range batch {
			if strings.Contains(msg, fmt.Sprintf("'%s'", req.key)) {
				invalidKeysMap[req.key] = true
			}
		}
	}

	var valid []preparedStatusRequest
	var invalid []preparedStatusRequest
	for _, req := range batch {
		if invalidKeysMap[req.key] {
			invalid = append(invalid, req)
		} else {
			valid = append(valid, req)
		}
	}

	return valid, invalid
}
