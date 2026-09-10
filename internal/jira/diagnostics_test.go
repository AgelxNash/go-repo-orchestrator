package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
)

func TestDiagnoseIssueAuthRequired(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "OPS", URL: server.URL}}))
	result := svc.DiagnoseIssue(t.Context(), "OPS", "ops-1")
	if !result.AuthDiagnosed || result.State != StatusStateAuth || result.Reason != StatusReasonAuthRequired {
		t.Fatalf("expected auth_required verdict, got %+v", result)
	}
	if result.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 status code, got %d", result.StatusCode)
	}
	if !strings.Contains(result.VerdictText(), "401") {
		t.Fatalf("expected human verdict with 401, got %q", result.VerdictText())
	}
}

func TestDiagnoseIssueHTMLLogin(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html><body>SSO login\x00page</body></html>"))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "OPS", URL: server.URL}}))
	result := svc.DiagnoseIssue(t.Context(), "OPS", "OPS-2")
	if !result.AuthDiagnosed || result.Reason != StatusReasonLoginRequired {
		t.Fatalf("expected login_required verdict, got %+v", result)
	}
	if !strings.Contains(result.BodySnippet, "SSO login") {
		t.Fatalf("expected sanitized snippet with login marker, got %q", result.BodySnippet)
	}
	if strings.ContainsRune(result.BodySnippet, 0) {
		t.Fatalf("snippet must not contain non-printable bytes: %q", result.BodySnippet)
	}
}

func TestDiagnoseIssueReady(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"issues":[{"key":"OPS-3","fields":{"status":{"name":"In Review"}}}]}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "OPS", URL: server.URL}}))
	result := svc.DiagnoseIssue(t.Context(), "OPS", "OPS-3")
	if result.State != StatusStateReady || !result.IssueFound || result.Status != "In Review" {
		t.Fatalf("expected ready verdict with status, got %+v", result)
	}
	if result.SearchURL == "" || !strings.Contains(result.SearchURL, "/rest/api/2/search") {
		t.Fatalf("expected search url in trace, got %q", result.SearchURL)
	}
}

func TestDiagnoseIssueNoGroupConfig(t *testing.T) {
	t.Parallel()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "OPS", URL: "https://jira.example.com"}}))
	result := svc.DiagnoseIssue(t.Context(), "UNKNOWN", "OPS-4")
	if !result.NoGroupConfig || result.State != StatusStateUnmapped {
		t.Fatalf("expected no_group_config, got %+v", result)
	}
}

func TestDiagnoseIssueBrowserFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group:      "OPS",
		URL:        server.URL,
		Playwright: true,
	}}))
	svc.browser = fakeBrowserRequester{requestGETFn: func(_ context.Context, _ string, _ map[string]string) (int, map[string]string, []byte, string, error) {
		return 0, nil, nil, "", errFakeBrowserDown
	}}

	result := svc.DiagnoseIssue(t.Context(), "OPS", "OPS-5")
	if !result.FallbackUsed || result.BrowserError == "" {
		t.Fatalf("expected fallback info in trace, got %+v", result)
	}
	if result.Transport != "browser" {
		t.Fatalf("expected browser transport group, got %q", result.Transport)
	}
	if !result.AuthDiagnosed || result.Reason != StatusReasonForbidden {
		t.Fatalf("expected forbidden verdict from http fallback, got %+v", result)
	}
	if !strings.Contains(result.VerdictText(), "403") {
		t.Fatalf("expected human verdict with 403, got %q", result.VerdictText())
	}
}

func TestDiagnoseIssueDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{
		Group: "OPS",
		URL:   server.URL,
		Token: "super-secret-token",
		Login: config.JiraLogin{Username: "user@example.com", Password: "super-secret-password"},
	}}))

	result := svc.DiagnoseIssue(t.Context(), "OPS", "OPS-6")
	rendered := result.VerdictText() + result.AuthKind + result.SearchURL + result.BodySnippet + result.BrowserError + result.RequestErr
	for _, secret := range []string{"super-secret-token", "super-secret-password"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("diagnose trace must not contain secret %q", secret)
		}
	}
	if result.AuthKind != "token (Bearer)" {
		t.Fatalf("expected token auth kind, got %q", result.AuthKind)
	}
}

func TestSanitizeDiagnoseSnippetLimitsSize(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", maxDiagnoseBodySnippet+100)
	snippet := sanitizeDiagnoseSnippet([]byte(body))
	if len(snippet) != maxDiagnoseBodySnippet {
		t.Fatalf("expected snippet capped at %d bytes, got %d", maxDiagnoseBodySnippet, len(snippet))
	}
}

// errFakeBrowserDown — маркерная ошибка фейкового браузер-транспорта.
var errFakeBrowserDown = errors.New("fake browser down")
