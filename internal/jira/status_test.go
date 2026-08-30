package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
)

type fakeBrowserRequester struct {
	requestGETFn func(ctx context.Context, requestURL string, headers map[string]string) (int, map[string]string, []byte, error)
}

func (f fakeBrowserRequester) RequestGET(ctx context.Context, requestURL string, headers map[string]string) (int, map[string]string, []byte, error) {
	if f.requestGETFn != nil {
		return f.requestGETFn(ctx, requestURL, headers)
	}

	return 0, nil, nil, errors.New("requestGETFn is not configured")
}

type panicHTTPDoer struct{}

func (panicHTTPDoer) Do(*http.Request) (*http.Response, error) {
	panic("unexpected HTTP transport call")
}

type fakeHTTPDoer struct {
	doFn func(req *http.Request) (*http.Response, error)
}

func (f fakeHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if f.doFn != nil {
		return f.doFn(req)
	}

	return nil, errors.New("doFn is not configured")
}

// mustGroupConfigs оборачивает WithGroupConfigs для тестов: ssl-блоки в тестовых
// конфигурациях не задаются, поэтому ошибка здесь невозможна.
func mustGroupConfigs(t *testing.T, groups []config.JiraConfig) StatusServiceOption {
	t.Helper()

	opt, err := WithGroupConfigs(0, groups)
	if err != nil {
		t.Fatalf("WithGroupConfigs: %v", err)
	}

	return opt
}

func TestParseSearchStatuses(t *testing.T) {
	t.Parallel()

	statuses, _, _, err := parseSearchStatuses([]byte(`{"issues":[{"key":"OPS-1","fields":{"status":{"name":"In Progress"}}}]}`))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if statuses["OPS-1"] != "In Progress" {
		t.Fatalf("expected status name In Progress, got %q", statuses["OPS-1"])
	}
}

func TestBuildSearchStatusURL(t *testing.T) {
	t.Parallel()

	issueURL, err := buildSearchStatusURL("https://jira.example.com", []string{"OPS-123", "OPS-124"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := url.Parse(issueURL)
	if err != nil {
		t.Fatalf("parse issue url: %v", err)
	}
	if parsed.Path != "/rest/api/2/search" {
		t.Fatalf("unexpected path: %s", parsed.Path)
	}
	if parsed.Query().Get("fields") != "status" {
		t.Fatalf("unexpected fields query: %s", parsed.RawQuery)
	}
	jql := parsed.Query().Get("jql")
	if !strings.Contains(jql, "key in") || !strings.Contains(jql, `"OPS-123"`) || !strings.Contains(jql, `"OPS-124"`) {
		t.Fatalf("unexpected jql query: %q", jql)
	}
}

func TestResolveStatusUsesHTTPTransportAndCache(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assertSearchRequestHasKeys(t, r, []string{"OPS-7"})
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		_, _ = w.Write([]byte(`{"issues":[{"key":"OPS-7","fields":{"status":{"name":"Done"}}}]}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
		Type:  "token",
		Token: "test-token",
	}}))

	first := svc.ResolveStatus("TASKS", "", "", "OPS-7")
	if first.Status != "Done" {
		t.Fatalf("unexpected first status: %q", first.Status)
	}
	if first.State != StatusStateReady {
		t.Fatalf("expected ready state, got %q", first.State)
	}

	second := svc.ResolveStatus("TASKS", "", "", "OPS-7")
	if second.Status != "Done" {
		t.Fatalf("unexpected second status: %q", second.Status)
	}

	if requests != 1 {
		t.Fatalf("expected single upstream request due cache, got %d", requests)
	}
}

func TestResolveStatusReturnsDashForUnknownGroup(t *testing.T) {
	t.Parallel()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   "https://jira.example.com",
	}}))

	status := svc.ResolveStatus("UNKNOWN", "", "", "OPS-10")
	if status.Status != "-" {
		t.Fatalf("expected fallback status '-', got %q", status.Status)
	}
	if status.State != StatusStateUnmapped {
		t.Fatalf("expected unmapped state, got %q", status.State)
	}
}

func TestResolveStatusUsesBrowserTransportForPlaywrightGroup(t *testing.T) {
	t.Parallel()

	called := 0
	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group:      "IDEA",
		URL:        "https://idea.example.org",
		Playwright: true,
	}}))
	svc.browser = fakeBrowserRequester{requestGETFn: func(_ context.Context, requestURL string, headers map[string]string) (int, map[string]string, []byte, error) {
		called++
		if !strings.HasPrefix(requestURL, "https://idea.example.org/rest/api/2/search?") {
			t.Fatalf("unexpected browser request URL: %q", requestURL)
		}
		parsed, err := url.Parse(requestURL)
		if err != nil {
			t.Fatalf("parse browser request url: %v", err)
		}
		jql := parsed.Query().Get("jql")
		if !strings.Contains(jql, `"IDEA-1"`) {
			t.Fatalf("unexpected jql in browser request: %q", jql)
		}
		if headers["Accept"] != "application/json" {
			t.Fatalf("unexpected accept header: %q", headers["Accept"])
		}
		return http.StatusOK, map[string]string{}, []byte(`{"issues":[{"key":"IDEA-1","fields":{"status":{"name":"In Review"}}}]}`), nil
	}}
	svc.httpClient = panicHTTPDoer{}

	status := svc.ResolveStatus("IDEA", "", "", "IDEA-1")
	if status.Status != "In Review" {
		t.Fatalf("unexpected status from browser transport: %q", status.Status)
	}
	if status.State != StatusStateReady {
		t.Fatalf("expected ready state, got %q", status.State)
	}
	if called != 1 {
		t.Fatalf("expected browser transport to be called once, got %d", called)
	}
}

func TestResolveStatusFallsBackToHTTPWhenBrowserUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSearchRequestHasKeys(t, r, []string{"IDEA-2"})
		_, _ = w.Write([]byte(`{"issues":[{"key":"IDEA-2","fields":{"status":{"name":"Done"}}}]}`))
	}))
	defer server.Close()

	called := 0
	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group:      "IDEA",
		URL:        server.URL,
		Playwright: true,
	}}))
	svc.browser = fakeBrowserRequester{requestGETFn: func(_ context.Context, _ string, _ map[string]string) (int, map[string]string, []byte, error) {
		called++
		return 0, nil, nil, errors.New("playwright runtime is not started")
	}}

	status := svc.ResolveStatus("IDEA", "", "", "IDEA-2")
	if status.Status != "Done" {
		t.Fatalf("expected status from http fallback, got %q", status.Status)
	}
	if status.State != StatusStateReady {
		t.Fatalf("expected ready state after fallback, got %q", status.State)
	}
	if status.Reason != StatusReasonBrowserUnavailableHTTPFallback {
		t.Fatalf("expected browser fallback reason, got %q", status.Reason)
	}
	if called != 1 {
		t.Fatalf("expected browser request attempt before fallback, got %d", called)
	}
}

func TestResolveStatusFallbackToHTTPAuthRequired(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group:      "IDEA",
		URL:        server.URL,
		Playwright: true,
	}}))
	svc.browser = fakeBrowserRequester{requestGETFn: func(_ context.Context, _ string, _ map[string]string) (int, map[string]string, []byte, error) {
		return 0, nil, nil, errors.New("playwright runtime is not started")
	}}

	status := svc.ResolveStatus("IDEA", "", "", "IDEA-3")
	if status.State != StatusStateAuth {
		t.Fatalf("expected auth state for http fallback 401, got %q", status.State)
	}
	if status.Reason != StatusReasonAuthRequired {
		t.Fatalf("expected auth_required reason, got %q", status.Reason)
	}
}

func TestResolveStatusFallbackToHTTPNetworkError(t *testing.T) {
	t.Parallel()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group:      "IDEA",
		URL:        "https://jira.example.com",
		Playwright: true,
	}}))
	svc.browser = fakeBrowserRequester{requestGETFn: func(_ context.Context, _ string, _ map[string]string) (int, map[string]string, []byte, error) {
		return 0, nil, nil, errors.New("playwright runtime is not started")
	}}
	svc.httpClient = fakeHTTPDoer{doFn: func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: i/o timeout")
	}}

	status := svc.ResolveStatus("IDEA", "", "", "IDEA-4")
	if status.State != StatusStateTransient {
		t.Fatalf("expected transient state for network error, got %q", status.State)
	}
	if status.Reason != StatusReasonBrowserUnavailableHTTPError {
		t.Fatalf("expected browser+http error reason, got %q", status.Reason)
	}
}

func TestResolveStatusFallbackDashOnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))
	status := svc.ResolveStatus("TASKS", "", "", "OPS-1")
	if status.Status != "-" {
		t.Fatalf("expected fallback status '-', got %q", status.Status)
	}
	if status.State != StatusStateAuth {
		t.Fatalf("expected auth state for 401, got %q", status.State)
	}
}

func TestResolveStatusTransient429RecoversAfterRetryTTL(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assertSearchRequestHasKeys(t, r, []string{"OPS-429"})
		if requests == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		_, _ = w.Write([]byte(`{"issues":[{"key":"OPS-429","fields":{"status":{"name":"In Review"}}}]}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))
	first := svc.ResolveStatus("TASKS", "", "", "OPS-429")
	if first.Status != unknownStatus {
		t.Fatalf("expected transient fallback status '-', got %q", first.Status)
	}
	if first.State != StatusStateTransient {
		t.Fatalf("expected transient state, got %q", first.State)
	}

	cacheKey := buildCacheKey("TASKS", "", server.URL, "OPS-429")
	svc.mu.Lock()
	entry, ok := svc.cache[cacheKey]
	if !ok {
		svc.mu.Unlock()
		t.Fatalf("expected transient entry in cache")
	}
	if entry.expiresAt.IsZero() {
		svc.mu.Unlock()
		t.Fatalf("expected transient entry to have ttl")
	}

	remaining := time.Until(entry.expiresAt)
	if remaining < 2*time.Second || remaining > 4*time.Second {
		svc.mu.Unlock()
		t.Fatalf("expected retry-after ttl around 3s, got %s", remaining)
	}

	entry.expiresAt = time.Now().Add(-time.Second)
	svc.cache[cacheKey] = entry
	svc.mu.Unlock()

	second := svc.ResolveStatus("TASKS", "", "", "OPS-429")
	if second.Status != "In Review" {
		t.Fatalf("expected recovered status In Review, got %q", second.Status)
	}
	if second.State != StatusStateReady {
		t.Fatalf("expected ready state after retry, got %q", second.State)
	}

	if requests != 2 {
		t.Fatalf("expected 2 upstream requests (429 then success), got %d", requests)
	}
}

func TestResolveStatusMapsNonTransientHTTPErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))

	status := svc.ResolveStatus("TASKS", "", "", "OPS-404")
	if status.State != StatusStateError {
		t.Fatalf("expected error state for 404, got %q", status.State)
	}
	if status.Reason != StatusReasonIssueNotFound {
		t.Fatalf("expected issue_not_found reason for 404, got %q", status.Reason)
	}
}

func TestResolveStatusMapsForbiddenAsAuthState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))

	status := svc.ResolveStatus("TASKS", "", "", "OPS-403")
	if status.State != StatusStateAuth {
		t.Fatalf("expected auth state for 403, got %q", status.State)
	}
	if status.Reason != StatusReasonForbidden {
		t.Fatalf("expected forbidden reason for 403, got %q", status.Reason)
	}
}

func TestResolveStatusMapsClient4xxErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))

	status := svc.ResolveStatus("TASKS", "", "", "OPS-400")
	if status.State != StatusStateError {
		t.Fatalf("expected error state for 400, got %q", status.State)
	}
	if status.Reason != StatusReasonClientError {
		t.Fatalf("expected client_error reason for 400, got %q", status.Reason)
	}
}

func TestResolveStatusMapsLoginRedirectOrHTMLAsAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>login</body></html>"))
			return
		}

		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))

	status := svc.ResolveStatus("TASKS", "", "", "OPS-LOGIN")
	if status.State != StatusStateAuth {
		t.Fatalf("expected auth state for login redirect/html, got %q", status.State)
	}
	if status.Reason != StatusReasonLoginRequired {
		t.Fatalf("expected login_required reason for login redirect/html, got %q", status.Reason)
	}
}

func TestPrefetchStatusesBatchesBy500(t *testing.T) {
	t.Parallel()

	requests := 0
	batchSizes := make([]int, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		keys := extractKeysFromSearchRequest(t, r)
		batchSizes = append(batchSizes, len(keys))

		issues := make([]string, 0, len(keys))
		for _, key := range keys {
			issues = append(issues, fmt.Sprintf(`{"key":%q,"fields":{"status":{"name":"Done"}}}`, key))
		}
		_, _ = w.Write([]byte(`{"total": ` + fmt.Sprintf("%d", len(keys)) + `, "issues":[` + strings.Join(issues, ",") + `]}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))

	requestsIn := make([]StatusBatchRequest, 0, 501)
	for i := 1; i <= 501; i++ {
		requestsIn = append(requestsIn, StatusBatchRequest{Group: "TASKS", Key: fmt.Sprintf("WEB-%d", i)})
	}

	svc.PrefetchStatuses(context.Background(), requestsIn)

	if requests != 2 {
		t.Fatalf("expected 2 batched requests, got %d", requests)
	}
	if len(batchSizes) != 2 {
		t.Fatalf("expected two batch sizes, got %d", len(batchSizes))
	}
	if batchSizes[0] != 500 {
		t.Fatalf("expected first batch size 500, got %d", batchSizes[0])
	}
	if batchSizes[1] != 1 {
		t.Fatalf("expected second batch size 1, got %d", batchSizes[1])
	}

	first := svc.ResolveStatus("TASKS", "", "", "WEB-1")
	last := svc.ResolveStatus("TASKS", "", "", "WEB-501")
	if first.Status != "Done" || last.Status != "Done" {
		t.Fatalf("expected cached statuses Done/Done, got %q/%q", first.Status, last.Status)
	}
	if requests != 2 {
		t.Fatalf("expected no extra requests after cache hits, got %d", requests)
	}
}

func TestPrefetchStatusesSeparatesGroups(t *testing.T) {
	t.Parallel()

	tasksRequests := 0
	tasksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasksRequests++
		keys := extractKeysFromSearchRequest(t, r)
		assertContainsExactKeys(t, keys, []string{"OPS-1", "OPS-2"})
		_, _ = w.Write([]byte(`{"issues":[{"key":"OPS-1","fields":{"status":{"name":"Tasks Done"}}},{"key":"OPS-2","fields":{"status":{"name":"Tasks Done"}}}]}`))
	}))
	defer tasksServer.Close()

	ideaRequests := 0
	ideaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ideaRequests++
		keys := extractKeysFromSearchRequest(t, r)
		assertContainsExactKeys(t, keys, []string{"IDEA-1"})
		_, _ = w.Write([]byte(`{"issues":[{"key":"IDEA-1","fields":{"status":{"name":"Idea Done"}}}]}`))
	}))
	defer ideaServer.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{
		{Group: "TASKS", URL: tasksServer.URL},
		{Group: "IDEA", URL: ideaServer.URL},
	}))

	svc.PrefetchStatuses(context.Background(), []StatusBatchRequest{
		{Group: "TASKS", Key: "OPS-1"},
		{Group: "IDEA", Key: "IDEA-1"},
		{Group: "TASKS", Key: "OPS-2"},
	})

	if tasksRequests != 1 {
		t.Fatalf("expected TASKS to be fetched once, got %d", tasksRequests)
	}
	if ideaRequests != 1 {
		t.Fatalf("expected IDEA to be fetched once, got %d", ideaRequests)
	}

	if status := svc.ResolveStatus("TASKS", "", "", "OPS-1"); status.Status != "Tasks Done" {
		t.Fatalf("unexpected TASKS status: %q", status.Status)
	}
	if status := svc.ResolveStatus("IDEA", "", "", "IDEA-1"); status.Status != "Idea Done" {
		t.Fatalf("unexpected IDEA status: %q", status.Status)
	}
}

func TestResolveStatusFallsBackWhenIssueMissingInBatchResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSearchRequestHasKeys(t, r, []string{"OPS-404"})
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "TASKS",
		URL:   server.URL,
	}}))

	status := svc.ResolveStatus("TASKS", "", "", "OPS-404")
	if status.Status != "-" {
		t.Fatalf("expected fallback status '-', got %q", status.Status)
	}
	if status.State != StatusStateError {
		t.Fatalf("expected error state, got %q", status.State)
	}
	if status.Reason != StatusReasonIssueNotFound {
		t.Fatalf("expected issue_not_found reason, got %q", status.Reason)
	}
}

func TestBuildCacheKeyIncludesGroup(t *testing.T) {
	t.Parallel()

	left := buildCacheKey("TASKS", "", "https://jira.example.com", "OPS-1")
	right := buildCacheKey("IDEA", "", "https://jira.example.com", "OPS-1")
	if left == "" || right == "" {
		t.Fatal("expected non-empty cache keys")
	}
	if left == right {
		t.Fatalf("expected different cache keys for different jira groups, got left=%q right=%q", left, right)
	}
	if !strings.HasPrefix(left, "TASKS|") {
		t.Fatalf("expected TASKS cache key prefix, got %q", left)
	}
}

func assertSearchRequestHasKeys(t *testing.T, r *http.Request, keys []string) {
	t.Helper()
	if r.URL.Path != "/rest/api/2/search" {
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}
	if r.URL.Query().Get("fields") != "status" {
		t.Fatalf("unexpected fields query: %s", r.URL.RawQuery)
	}
	actualKeys := extractKeysFromSearchRequest(t, r)
	assertContainsExactKeys(t, actualKeys, keys)
}

func extractKeysFromSearchRequest(t *testing.T, r *http.Request) []string {
	t.Helper()
	jql := r.URL.Query().Get("jql")
	if !strings.HasPrefix(strings.TrimSpace(jql), "key in (") {
		t.Fatalf("unexpected jql: %q", jql)
	}
	matches := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(jql, -1)
	keys := make([]string, 0, len(matches))
	for _, match := range matches {
		keys = append(keys, match[1])
	}
	return keys
}

func assertContainsExactKeys(t *testing.T, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("unexpected key count: want %d got %d (%v)", len(expected), len(actual), actual)
	}
	want := make(map[string]struct{}, len(expected))
	for _, key := range expected {
		want[key] = struct{}{}
	}
	for _, key := range actual {
		if _, ok := want[key]; !ok {
			t.Fatalf("unexpected key in request: %q (all: %v)", key, actual)
		}
	}
}

func TestResolveStatusFallbackOn400BadRequest(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		keys := extractKeysFromSearchRequest(t, r)

		if requests == 1 {
			assertContainsExactKeys(t, keys, []string{"BFF-1", "BFF-2", "BFF-INVALID"})
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessages":["An issue with key 'BFF-INVALID' does not exist for field 'key'."],"errors":{}}`))
			return
		}

		assertContainsExactKeys(t, keys, []string{"BFF-1", "BFF-2"})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"issues":[{"key":"BFF-1","fields":{"status":{"name":"Done"}}},{"key":"BFF-2","fields":{"status":{"name":"In Progress"}}}]}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "BFF",
		URL:   server.URL,
	}}))

	svc.PrefetchStatuses(context.Background(), []StatusBatchRequest{
		{Group: "BFF", Key: "BFF-1"},
		{Group: "BFF", Key: "BFF-INVALID"},
		{Group: "BFF", Key: "BFF-2"},
	})

	if requests != 2 {
		t.Fatalf("expected 2 batched requests (initial + fallback retry), got %d", requests)
	}

	st1 := svc.ResolveStatus("BFF", "", "", "BFF-1")
	stInv := svc.ResolveStatus("BFF", "", "", "BFF-INVALID")
	st2 := svc.ResolveStatus("BFF", "", "", "BFF-2")

	if st1.Status != "Done" {
		t.Fatalf("expected BFF-1 Done, got %q", st1.Status)
	}
	if st2.Status != "In Progress" {
		t.Fatalf("expected BFF-2 In Progress, got %q", st2.Status)
	}
	if stInv.State != StatusStateError || stInv.Reason != StatusReasonIssueNotFound {
		t.Fatalf("expected BFF-INVALID IssueNotFound, got State=%v Reason=%v", stInv.State, stInv.Reason)
	}
}

func TestPrefetchStatusesWithProgressRespectsContextCancellation(t *testing.T) {
	block := make(chan struct{})
	defer close(block)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	groupOption, err := WithGroupConfigs(time.Second, []config.JiraConfig{
		{Group: "OPS", URL: server.URL},
	})
	if err != nil {
		t.Fatalf("build group configs: %v", err)
	}
	svc := NewStatusService(10*time.Second, groupOption)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.PrefetchStatusesWithProgress(ctx, []StatusBatchRequest{{Group: "OPS", Key: "OPS-1"}}, nil)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("prefetch did not respect context cancellation")
	}

	result := svc.ResolveStatus("OPS", "", server.URL, "OPS-1")
	if result.State != StatusStateTransient {
		t.Fatalf("expected transient state after cancellation, got %q", result.State)
	}
	if result.Reason != StatusReasonTransportError {
		t.Fatalf("expected transport error reason after cancellation, got %q", result.Reason)
	}
}
