package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
)

func TestListReleasedFixVersionsReturnsSortedReleasedOnly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/project/TASKS/versions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "released" {
			t.Fatalf("expected released status filter, got %q", got)
		}
		_, _ = w.Write([]byte(`{"values":[{"id":"2","name":"v2","released":true,"releaseDate":"2026-04-10"},{"id":"1","name":"v1","released":true,"releaseDate":"2026-04-01"},{"id":"3","name":"draft","released":false,"releaseDate":"2026-04-20"}],"startAt":0,"maxResults":100,"total":2,"isLast":true}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	versions, err := svc.ListReleasedFixVersions(t.Context(), "TASKS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 released versions, got %d", len(versions))
	}
	if versions[0].ID != "2" || versions[1].ID != "1" {
		t.Fatalf("unexpected order: %#v", versions)
	}
}

func TestListDoneIssueKeysByReleaseBuildsJQLAndPaginates(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/rest/api/2/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		jql := r.URL.Query().Get("jql")
		if !strings.Contains(jql, "statusCategory = done") {
			t.Fatalf("unexpected jql without done clause: %q", jql)
		}
		if !strings.Contains(jql, "fixVersion = 42") {
			t.Fatalf("unexpected jql release clause: %q", jql)
		}

		startAt := r.URL.Query().Get("startAt")
		switch startAt {
		case "":
			_, _ = w.Write([]byte(`{"total":3,"issues":[{"key":"OPS-1"},{"key":"OPS-2"}]}`))
		case "2":
			_, _ = w.Write([]byte(`{"total":3,"issues":[{"key":"OPS-3"}]}`))
		default:
			t.Fatalf("unexpected startAt: %q", startAt)
		}
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	keys, err := svc.ListDoneIssueKeysByRelease(t.Context(), "TASKS", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 paginated requests, got %d", requests)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 issue keys, got %d (%v)", len(keys), keys)
	}
	assertContainsExactKeys(t, keys, []string{"OPS-1", "OPS-2", "OPS-3"})
}

func TestListReleasedFixVersionsParsesNumericIDWithoutScientificNotation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"values":[{"id":1234567890123456789,"name":"v-num","released":true,"releaseDate":"2026-04-10"}],"startAt":0,"maxResults":100,"total":1,"isLast":true}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	versions, err := svc.ListReleasedFixVersions(t.Context(), "TASKS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0].ID != "1234567890123456789" {
		t.Fatalf("expected stable numeric id string, got %q", versions[0].ID)
	}
	if strings.ContainsAny(strings.ToLower(versions[0].ID), "e+") {
		t.Fatalf("expected non-scientific id format, got %q", versions[0].ID)
	}
}

func TestListDoneIssueKeysByReleaseSafeNoopForUnknownGroup(t *testing.T) {
	t.Parallel()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: "https://jira.example.org"}}))

	keys, err := svc.ListDoneIssueKeysByRelease(t.Context(), "UNKNOWN", "101")
	if err != nil {
		t.Fatalf("expected safe noop error=nil, got %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no keys for unknown group, got %v", keys)
	}
}

func TestBuildDoneIssuesByReleaseURLEscapesStringReleaseID(t *testing.T) {
	t.Parallel()

	u, err := buildDoneIssuesByReleaseURL("https://jira.example.org", `release "Q1"`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	jql := parsed.Query().Get("jql")
	if !strings.Contains(jql, `statusCategory = done`) {
		t.Fatalf("unexpected jql: %q", jql)
	}
	if !strings.Contains(jql, `fixVersion = "release \"Q1\""`) {
		t.Fatalf("unexpected escaped release value in jql: %q", jql)
	}
}

func TestListReleasedFixVersionsRetriesOn429AndHonorsRetryAfter(t *testing.T) {
	origWait := jiraReleaseWaitFn
	t.Cleanup(func() { jiraReleaseWaitFn = origWait })
	delays := make([]time.Duration, 0, 2)
	jiraReleaseWaitFn = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"values":[{"id":"1","name":"v1","released":true,"releaseDate":"2026-04-01"}],"startAt":0,"maxResults":100,"total":1,"isLast":true}`))
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	versions, err := svc.ListReleasedFixVersions(t.Context(), "TASKS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected retry flow with 2 requests, got %d", requests)
	}
	if len(delays) != 1 {
		t.Fatalf("expected one retry delay, got %d", len(delays))
	}
	if delays[0] != time.Second {
		t.Fatalf("expected delay from Retry-After=1s, got %s", delays[0])
	}
	if len(versions) != 1 || versions[0].ID != "1" {
		t.Fatalf("unexpected versions: %#v", versions)
	}
}

func TestListDoneIssueKeysByReleaseReturnsErrorAfter429RetriesExhausted(t *testing.T) {
	origWait := jiraReleaseWaitFn
	t.Cleanup(func() { jiraReleaseWaitFn = origWait })
	countSleep := 0
	jiraReleaseWaitFn = func(_ context.Context, _ time.Duration) error {
		countSleep++
		return nil
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	_, err := svc.ListDoneIssueKeysByRelease(t.Context(), "TASKS", "42")
	if err == nil {
		t.Fatal("expected error after exhausting retries on 429")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected rate limit error, got %v", err)
	}
	if requests != jiraReleaseMaxRetries+1 {
		t.Fatalf("expected %d requests, got %d", jiraReleaseMaxRetries+1, requests)
	}
	if countSleep != jiraReleaseMaxRetries {
		t.Fatalf("expected %d sleeps, got %d", jiraReleaseMaxRetries, countSleep)
	}
}

func TestListDoneIssueKeysByReleaseCancelsDuringRetryWait(t *testing.T) {
	origWait := jiraReleaseWaitFn
	t.Cleanup(func() { jiraReleaseWaitFn = origWait })

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	jiraReleaseWaitFn = func(waitCtx context.Context, _ time.Duration) error {
		cancel()
		<-waitCtx.Done()
		return waitCtx.Err()
	}

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	_, err := svc.ListDoneIssueKeysByRelease(ctx, "TASKS", "42")
	if err == nil {
		t.Fatal("expected context cancellation error during retry wait")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected single request before cancel, got %d", requests)
	}
}

func TestListReleasedFixVersionsStopsPaginationOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			_, _ = w.Write([]byte(`{"values":[{"id":"1","name":"v1","released":true,"releaseDate":"2026-04-01"}],"startAt":0,"maxResults":1,"total":3,"isLast":false}`))
			cancel()
		case 2:
			t.Fatalf("unexpected second pagination request after cancellation")
		}
	}))
	defer server.Close()

	svc := NewStatusService(0, mustGroupConfigs(t, []config.JiraConfig{{Group: "TASKS", URL: server.URL}}))

	_, err := svc.ListReleasedFixVersions(ctx, "TASKS")
	if err == nil {
		t.Fatal("expected cancellation error during pagination")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected one request before cancel, got %d", requests)
	}
}
