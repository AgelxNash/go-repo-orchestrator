package jira

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildSearchStatusURLHappyPath(t *testing.T) {
	t.Parallel()

	got, err := buildSearchStatusURL("https://jira.example.org", []string{"PROJ-1", "PROJ-2"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "/rest/api/2/search") {
		t.Fatalf("expected search path, got %q", got)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url not parseable: %v", err)
	}
	q := parsed.Query()
	if got := q.Get("jql"); got != `key in ("PROJ-1", "PROJ-2")` {
		t.Fatalf("unexpected jql: %q", got)
	}
	if got := q.Get("fields"); got != "status" {
		t.Fatalf("unexpected fields: %q", got)
	}
	if parsed.Query().Get("startAt") != "" {
		t.Fatalf("startAt should be empty for 0")
	}
}

func TestBuildSearchStatusURLWithStartAt(t *testing.T) {
	t.Parallel()

	got, err := buildSearchStatusURL("https://jira.example.org", []string{"PROJ-1"}, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := urlParse(t, got).Query().Get("startAt"); got != "50" {
		t.Fatalf("expected startAt=50, got %q", got)
	}
}

func TestBuildSearchStatusURLNormalizesBase(t *testing.T) {
	t.Parallel()

	got, err := buildSearchStatusURL("https://jira.example.org/", []string{"PROJ-1"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "//rest") {
		t.Fatalf("double-slash in path: %q", got)
	}
}

func TestBuildSearchStatusURLFiltersEmptyKeys(t *testing.T) {
	t.Parallel()

	got, err := buildSearchStatusURL("https://jira.example.org", []string{"", "  ", "-", "PROJ-1"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jql := urlParse(t, got).Query().Get("jql"); jql != `key in ("PROJ-1")` {
		t.Fatalf("unexpected jql: %q", jql)
	}
}

func TestBuildSearchStatusURLErrors(t *testing.T) {
	t.Parallel()

	if _, err := buildSearchStatusURL("", []string{"PROJ-1"}, 0); err == nil {
		t.Fatal("expected error for empty base")
	}
	if _, err := buildSearchStatusURL("https://jira.example.org", nil, 0); err == nil {
		t.Fatal("expected error for empty keys")
	}
	if _, err := buildSearchStatusURL("https://jira.example.org", []string{"", " "}, 0); err == nil {
		t.Fatal("expected error when all keys blank")
	}
}

func TestParseSearchStatusesParsesMixedValues(t *testing.T) {
	t.Parallel()

	body := []byte(`{"total":3,"issues":[
		{"key":"PROJ-1","fields":{"status":{"name":"In Progress"}}},
		{"key":"PROJ-2","fields":{"status":{"name":"Done"}}},
		{"key":"  ","fields":{"status":{"name":"Done"}}},
		{"key":"PROJ-4","fields":{"status":{"name":""}}},
		{"key":"PROJ-1","fields":{"status":{"name":"Reopened"}}}
	]}`)

	statuses, total, count, err := parseSearchStatuses(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if count != 5 {
		t.Fatalf("expected count=5, got %d", count)
	}
	if got := statuses["PROJ-1"]; got != "Reopened" {
		t.Fatalf("expected last-wins for PROJ-1, got %q", got)
	}
	if got := statuses["PROJ-2"]; got != "Done" {
		t.Fatalf("expected Done for PROJ-2, got %q", got)
	}
	if _, ok := statuses["PROJ-4"]; ok {
		t.Fatal("expected empty status to be skipped")
	}
}

func TestParseSearchStatusesMalformedJSON(t *testing.T) {
	t.Parallel()

	if _, _, _, err := parseSearchStatuses([]byte(`{not json`)); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestJiraBaseFromTicketURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://jira.example.org/browse/PROJ-1":         "https://jira.example.org",
		"https://jira.example.org/secure/Dashboard.jspa": "https://jira.example.org/secure/Dashboard.jspa",
		"https://jira.example.org/":                      "https://jira.example.org",
		"":                                               "",
		"-":                                              "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			got := jiraBaseFromTicketURL(in)
			if got != want {
				t.Fatalf("input %q: expected %q, got %q", in, want, got)
			}
		})
	}
}

func urlParse(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url parse: %v", err)
	}
	return parsed
}
