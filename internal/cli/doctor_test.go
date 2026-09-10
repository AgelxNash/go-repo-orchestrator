package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func writeDoctorConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDoctorJiraPrintsFullTrace(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	configPath := writeDoctorConfig(t, "repos:\n  - name: svc\n    path: ./tmp-svc\n    branch:\n      jira:\n        - '(?P<OPS>OPS-\\d+)'\njira:\n  - group: 'OPS'\n    url: "+server.URL+"\n    token: secret-token-value\n    ssl:\n      verify: false\n")

	var out bytes.Buffer
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "jira", "ops-7", "--config", configPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor jira: %v", err)
	}

	rendered := out.String()
	for _, fragment := range []string{
		"Jira-диагностика ключа OPS-7",
		"Группа: OPS",
		"named-group mapping",
		"Транспорт: http | Авторизация: token (Bearer)",
		"/rest/api/2/search",
		"HTTP 401",
		"требуется авторизация (HTTP 401)",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, rendered)
		}
	}
	if strings.Contains(rendered, "secret-token-value") {
		t.Fatalf("doctor output must not leak token value:\n%s", rendered)
	}
}

func TestDoctorJiraUnmappedKeyChecksAllGroups(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>login page</html>"))
	}))
	defer server.Close()

	configPath := writeDoctorConfig(t, "repos:\n  - name: svc\n    path: ./tmp-svc\njira:\n  - group: 'OPS'\n    url: "+server.URL+"\n    ssl:\n      verify: false\n")

	var out bytes.Buffer
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "jira", "PROJ-123", "--config", configPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor jira: %v", err)
	}

	rendered := out.String()
	for _, fragment := range []string{
		"ключ не замаплен ни одним branch.jira regex — проверяю все группы",
		"нужен вход",
		"<html>login page</html>",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, rendered)
		}
	}
}

func TestDoctorJiraBrowserUnavailableFailsWithTrace(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	configPath := writeDoctorConfig(t, "repos:\n  - name: svc\n    path: ./tmp-svc\njira:\n  - group: 'OPS'\n    url: "+server.URL+"\n    playwright: true\n    ssl:\n      verify: false\nbrowser:\n  cdp_url: 'http://127.0.0.1:1'\n")

	var out bytes.Buffer
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "jira", "OPS-8", "--config", configPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit when browser transport is unavailable")
	}
	if !strings.Contains(err.Error(), "playwright runtime недоступен") {
		t.Fatalf("unexpected error: %v", err)
	}

	rendered := out.String()
	for _, fragment := range []string{
		"Browser-транспорт",
		"недоступен",
		"browser-транспорт недоступен",
		"HTTP fallback",
		"HTTP 401",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, rendered)
		}
	}
}

func TestDoctorJiraRequiresExactlyOneKey(t *testing.T) {
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"doctor", "jira", "--config", "./config.example.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected args validation error")
	}
	if !strings.Contains(err.Error(), "ровно один ключ задачи") {
		t.Fatalf("unexpected error: %v", err)
	}
}
