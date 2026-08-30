package config

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLoadAndRules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: test\n    url: git@gitlab.com:anHome/git-branch-cleaner-test.git\n    branch:\n      keep: ['^(main|master)$', '^release/.*$']\n      jira: ['^(?:feature|bugfix)/(?P<JIRA>[A-Z]+-\\d+)$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	repo := cfg.Repos[0]
	if !repo.IsProtected("main") {
		t.Fatalf("main must be protected")
	}
	reason, ok := repo.ProtectedReason("release/1.0.0")
	if !ok || reason == "" {
		t.Fatalf("release branch must be protected by keep pattern")
	}

	if _, ok := repo.ProtectedReason("feature/task"); ok {
		t.Fatalf("feature/task should not be protected")
	}

	jiraKey, found := repo.ExtractJiraKey("feature/OPS-123")
	if !found || jiraKey != "OPS-123" {
		t.Fatalf("jira key extraction failed, got %q found=%v", jiraKey, found)
	}
}

func TestLoadAndRulesWithNewBranchSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "jira:\n" +
		"  - group: EXAMPLE\n" +
		"    url: https://jira.example.com\n" +
		"    playwright: false\n" +
		"    type: token\n" +
		"    token: demo\n" +
		"repos:\n" +
		"  - name: test\n" +
		"    url: git@gitlab.com:anHome/git-branch-cleaner-test.git\n" +
		"    branch:\n" +
		"      autoswitch: develop\n" +
		"      keep: ['^(main|master)$', '^release/.*$']\n" +
		"      jira: ['^(?:feature|bugfix)/(?P<JIRA>[A-Z]+-\\d+)$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(cfg.Jira) != 1 {
		t.Fatalf("expected top-level jira section to be parsed")
	}
	if cfg.Jira[0].Group != "EXAMPLE" {
		t.Fatalf("expected jira group EXAMPLE, got %q", cfg.Jira[0].Group)
	}
	if cfg.Jira[0].URL != "https://jira.example.com" {
		t.Fatalf("expected normalized jira url, got %q", cfg.Jira[0].URL)
	}

	repo := cfg.Repos[0]
	if repo.Branch.Autoswitch != "develop" {
		t.Fatalf("expected autoswitch branch from new schema, got %q", repo.Branch.Autoswitch)
	}
	if len(repo.Branch.Keep) != 2 {
		t.Fatalf("expected keep patterns from new schema")
	}
	if !repo.IsProtected("main") {
		t.Fatalf("main must be protected")
	}
	jiraKey, found := repo.ExtractJiraKey("feature/OPS-123")
	if !found || jiraKey != "OPS-123" {
		t.Fatalf("jira key extraction failed, got %q found=%v", jiraKey, found)
	}
}

func TestExtractJiraMatchMapsNamedGroupToTopLevelJiraGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "jira:\n" +
		"  - group: TASKS\n" +
		"    url: https://tasks.example.org/\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n" +
		"    branch:\n" +
		"      jira: ['^feature/(?P<TASKS>[A-Z]+-\\d+)$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	match, ok := cfg.Repos[0].ExtractJiraMatch("feature/OPS-42")
	if !ok {
		t.Fatal("expected jira match")
	}
	if match.Key != "OPS-42" {
		t.Fatalf("expected ticket key OPS-42, got %q", match.Key)
	}
	if match.Group != "TASKS" {
		t.Fatalf("expected jira group TASKS, got %q", match.Group)
	}
	if match.URL != "https://tasks.example.org" {
		t.Fatalf("expected normalized group url, got %q", match.URL)
	}
	if match.TicketURL != "https://tasks.example.org/browse/OPS-42" {
		t.Fatalf("unexpected ticket url: %q", match.TicketURL)
	}
}

func TestExtractJiraMatchDoesNotMapWhenNamedGroupDoesNotMatchJiraGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "jira:\n" +
		"  - group: TASKS\n" +
		"    url: https://tasks.example.org\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n" +
		"    branch:\n" +
		"      jira: ['^feature/(?P<JIRA>[A-Z]+-\\d+)$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	match, ok := cfg.Repos[0].ExtractJiraMatch("feature/OPS-99")
	if !ok {
		t.Fatal("expected jira key extraction")
	}
	if match.Key != "OPS-99" {
		t.Fatalf("expected key OPS-99, got %q", match.Key)
	}
	if match.Group != "" || match.URL != "" || match.TicketURL != "" {
		t.Fatalf("expected mapping to stay empty when group mismatch, got %#v", match)
	}
}

func TestExtractJiraMatchDetailedReportsNoRegexMatch(t *testing.T) {
	t.Parallel()

	repo := RepoConfig{}
	repo.jira = []*compiledPattern{{raw: "^feature/(?P<JIRA>[A-Z]+-\\d+)$", re: regexp.MustCompile(`^feature/(?P<JIRA>[A-Z]+-\d+)$`)}}

	match, ok, diag := repo.ExtractJiraMatchDetailed("hotfix/OPS-99")
	if ok {
		t.Fatalf("expected no match, got %#v", match)
	}
	if diag.Reason != JiraMatchReasonNoRegexMatch {
		t.Fatalf("expected reason %q, got %q", JiraMatchReasonNoRegexMatch, diag.Reason)
	}
}

func TestExtractJiraMatchDetailedReportsNamedGroupWithoutGroupConfig(t *testing.T) {
	t.Parallel()

	repo := RepoConfig{}
	repo.jira = []*compiledPattern{{raw: `^(?P<SIMPLEWINE>(WEB|MOBI|BFF)-\d+)$`, re: regexp.MustCompile(`^(?P<SIMPLEWINE>(WEB|MOBI|BFF)-\d+)$`)}}
	repo.jiraGroups = map[string]JiraConfig{}

	match, ok, diag := repo.ExtractJiraMatchDetailed("BFF-1004")
	if !ok {
		t.Fatal("expected key extraction")
	}
	if match.Key != "BFF-1004" {
		t.Fatalf("expected key BFF-1004, got %q", match.Key)
	}
	if diag.Reason != JiraMatchReasonNamedGroupNoGroup {
		t.Fatalf("expected reason %q, got %q", JiraMatchReasonNamedGroupNoGroup, diag.Reason)
	}
	if diag.Group != "SIMPLEWINE" {
		t.Fatalf("expected unmatched group SIMPLEWINE, got %q", diag.Group)
	}
}

func TestExtractJiraMatchDetailedReportsFallbackJIRA(t *testing.T) {
	t.Parallel()

	repo := RepoConfig{}
	repo.jira = []*compiledPattern{{raw: "^feature/(?P<JIRA>[A-Z]+-\\d+)$", re: regexp.MustCompile(`^feature/(?P<JIRA>[A-Z]+-\d+)$`)}}
	repo.jiraGroups = map[string]JiraConfig{}

	match, ok, diag := repo.ExtractJiraMatchDetailed("feature/OPS-77")
	if !ok {
		t.Fatal("expected fallback key extraction")
	}
	if match.Key != "OPS-77" {
		t.Fatalf("expected key OPS-77, got %q", match.Key)
	}
	if diag.Reason != JiraMatchReasonFallbackJIRA {
		t.Fatalf("expected reason %q, got %q", JiraMatchReasonFallbackJIRA, diag.Reason)
	}
}

func TestPlaywrightEnabled(t *testing.T) {
	t.Parallel()

	cfg := &Config{Jira: []JiraConfig{{Group: "A", Playwright: false}, {Group: "B", Playwright: true}}}
	if !cfg.PlaywrightEnabled() {
		t.Fatal("expected playwright startup to be enabled")
	}

	cfg = &Config{Jira: []JiraConfig{{Group: "A", Playwright: false}}}
	if cfg.PlaywrightEnabled() {
		t.Fatal("expected playwright startup to be disabled")
	}
}

func writeTestConfig(t *testing.T, raw string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	return configPath
}

func TestLoadParsesJiraSSLBlock(t *testing.T) {
	t.Parallel()

	certPath := filepath.Join(t.TempDir(), "client.pem")
	if err := os.WriteFile(certPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw := "jira:\n" +
		"  - group: MYPROJ\n" +
		"    url: https://jira.corp.local\n" +
		"    ssl:\n" +
		"      client_cert: " + certPath + "\n" +
		"      ca_cert: " + caPath + "\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"

	cfg, err := Load(writeTestConfig(t, raw))
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	ssl := cfg.Jira[0].SSL
	if ssl.ClientCert != certPath || ssl.CACert != caPath {
		t.Fatalf("expected ssl paths to be parsed, got %+v", ssl)
	}
	if !ssl.IsZero() && ssl.ClientKey != "" {
		t.Fatalf("client_key must stay empty, got %q", ssl.ClientKey)
	}
}

func TestLoadFailsOnJiraSSLKeyWithoutCert(t *testing.T) {
	t.Parallel()

	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw := "jira:\n" +
		"  - group: MYPROJ\n" +
		"    url: https://jira.corp.local\n" +
		"    ssl:\n" +
		"      client_key: " + keyPath + "\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"

	_, err := Load(writeTestConfig(t, raw))
	if err == nil {
		t.Fatal("expected client_key without client_cert validation error")
	}
	if !strings.Contains(err.Error(), "client_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnJiraSSLPasswordWithoutCert(t *testing.T) {
	t.Parallel()

	raw := "jira:\n" +
		"  - group: MYPROJ\n" +
		"    url: https://jira.corp.local\n" +
		"    ssl:\n" +
		"      client_key_password: secret\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"

	_, err := Load(writeTestConfig(t, raw))
	if err == nil {
		t.Fatal("expected client_key_password without client_cert validation error")
	}
	if !strings.Contains(err.Error(), "client_key_password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnJiraSSLVerifyFalseWithCACert(t *testing.T) {
	t.Parallel()

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw := "jira:\n" +
		"  - group: MYPROJ\n" +
		"    url: https://jira.corp.local\n" +
		"    ssl:\n" +
		"      ca_cert: " + caPath + "\n" +
		"      verify: false\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"

	_, err := Load(writeTestConfig(t, raw))
	if err == nil {
		t.Fatal("expected verify=false with ca_cert validation error")
	}
	if !strings.Contains(err.Error(), "взаимоисключаются") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnJiraSSLMissingCertFile(t *testing.T) {
	t.Parallel()

	raw := "jira:\n" +
		"  - group: MYPROJ\n" +
		"    url: https://jira.corp.local\n" +
		"    ssl:\n" +
		"      client_cert: /nonexistent/path/client.pem\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"

	_, err := Load(writeTestConfig(t, raw))
	if err == nil {
		t.Fatal("expected missing ssl file validation error")
	}
	if !strings.Contains(err.Error(), "client_cert") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadParsesBrowserCDPURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "browser:\n" +
		"  cdp_url: http://127.0.0.1:9222\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Browser.CDPURL != "http://127.0.0.1:9222" {
		t.Fatalf("expected cdp_url to be parsed, got %q", cfg.Browser.CDPURL)
	}
}

func TestLoadFailsOnInvalidBrowserCDPURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "browser:\n" +
		"  cdp_url: ftp://127.0.0.1:9222\n" +
		"repos:\n" +
		"  - name: demo\n" +
		"    path: ./tmp/repo\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected invalid cdp_url validation error")
	}
	if !strings.Contains(err.Error(), "browser.cdp_url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnUnknownRootConfigKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "unexpected_root: true\n" +
		"repos:\n" +
		"  - name: test\n" +
		"    path: ./tmp/repo\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected unknown root config key validation error")
	}
	if !strings.Contains(err.Error(), "unexpected_root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnUnknownConfigKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: test\n" +
		"    path: ./tmp/repo\n" +
		"    branch:\n" +
		"      keep: ['^main$']\n" +
		"      typo_keep: ['^master$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected unknown config key validation error")
	}
	if !strings.Contains(err.Error(), "typo_keep") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnInvalidRegex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: test\n    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n    branch:\n      keep: ['[invalid']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatalf("expected regex validation error")
	}
	if !strings.Contains(err.Error(), "branch.keep") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnInvalidRepoURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: test\n    url: definitely-not-a-git-url\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatalf("expected repo url validation error")
	}
	if !strings.Contains(err.Error(), "некорректный url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadWithPathSource(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: local\n    path: " + repoPath + "\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	repo := cfg.Repos[0]
	if repo.SourceType() != "path" {
		t.Fatalf("expected path source type")
	}
	if repo.Path == "" {
		t.Fatalf("path source should keep non-empty path")
	}
}

func TestLoadAllowsBothURLAndPathForOpensource(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: test\n    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n    path: " + repoPath + "\n    branch:\n      autoswitch: develop\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("expected successful load, got error: %v", err)
	}
	repo := cfg.Repos[0]
	if repo.SourceType() != "opensource" {
		t.Fatalf("expected opensource source type, got %s", repo.SourceType())
	}
	if repo.Branch.Autoswitch != "develop" {
		t.Fatalf("expected develop branch, got %s", repo.Branch.Autoswitch)
	}
}

func TestLoadFailsWhenSourceMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: test\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatalf("expected source required validation error")
	}
	if !strings.Contains(err.Error(), "требуется указать url или path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractJiraKeyFallbackWithoutNamedGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n  - name: test\n    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n    branch:\n      jira: ['[A-Z]+-\\d+']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	jiraKey, found := cfg.Repos[0].ExtractJiraKey("bugfix/OPS-321-fix")
	if !found || jiraKey != "OPS-321" {
		t.Fatalf("fallback jira extraction failed, got %q found=%v", jiraKey, found)
	}
}

func TestLoadFailsOnDuplicateRepoName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: same\n" +
		"    path: ./repo-a\n" +
		"  - name: same\n" +
		"    path: ./repo-b\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatalf("expected duplicate name validation error")
	}

	errText := err.Error()
	if !strings.Contains(errText, "конфликт имени") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errText, "repos[0]") || !strings.Contains(errText, "repos[1]") {
		t.Fatalf("expected conflicting repo indexes in error: %v", err)
	}
}

func TestLoadFailsOnDuplicateManagedWorkdirKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	repoURL := "https://gitlab.com/group/repo.git"
	raw := "repos:\n" +
		"  - name: repo!\n" +
		"    url: " + repoURL + "\n" +
		"  - name: repo?\n" +
		"    url: " + repoURL + "\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatalf("expected duplicate managed workdir key validation error")
	}

	errText := err.Error()
	if !strings.Contains(errText, "конфликт рабочего ключа") {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedKey := "managed:" + managedRepoDirKey("repo!", repoURL)
	if !strings.Contains(errText, expectedKey) {
		t.Fatalf("expected managed key %q in error: %v", expectedKey, err)
	}
}

func TestLoadFailsOnDuplicatePathWorkdirKey(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: repo-a\n" +
		"    path: " + repoPath + "\n" +
		"  - name: repo-b\n" +
		"    path: " + repoPath + "\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatalf("expected duplicate path workdir key validation error")
	}

	errText := err.Error()
	if !strings.Contains(errText, "конфликт рабочего ключа") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errText, "path:"+repoPath) {
		t.Fatalf("expected path workdir key in error: %v", err)
	}
}

func TestLoadFailsOnLegacyFallbackFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: test\n" +
		"    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n" +
		"    autoswitch_branch: develop\n" +
		"    keep_patterns: ['^release/.*$']\n" +
		"    jira: ['(?P<JIRA>OPS-\\d+)']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected legacy fallback fields validation error")
	}
	errText := err.Error()
	if !strings.Contains(errText, "autoswitch_branch") || !strings.Contains(errText, "keep_patterns") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanDirectoryIncludesPathAndOriginURLWhenAvailable(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	root := t.TempDir()
	remotePath := filepath.Join(root, "remote.git")
	repoWithOrigin := filepath.Join(root, "with-origin")
	repoWithoutOrigin := filepath.Join(root, "without-origin")

	runCommand(t, root, "git", "init", "--bare", remotePath)
	runCommand(t, root, "git", "init", repoWithOrigin)
	runCommand(t, repoWithOrigin, "git", "remote", "add", "origin", remotePath)
	runCommand(t, root, "git", "init", repoWithoutOrigin)

	cfg, err := ScanDirectory(context.Background(), root)
	if err != nil {
		t.Fatalf("scan directory failed: %v", err)
	}

	if len(cfg.Repos) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(cfg.Repos))
	}

	byName := make(map[string]RepoConfig, len(cfg.Repos))
	for _, repo := range cfg.Repos {
		byName[repo.Name] = repo
	}

	withOrigin, ok := byName["with-origin"]
	if !ok {
		t.Fatalf("expected with-origin repository in scan result")
	}
	if withOrigin.Path != repoWithOrigin {
		t.Fatalf("expected path %q, got %q", repoWithOrigin, withOrigin.Path)
	}
	if withOrigin.URL != remotePath {
		t.Fatalf("expected origin url %q, got %q", remotePath, withOrigin.URL)
	}

	withoutOrigin, ok := byName["without-origin"]
	if !ok {
		t.Fatalf("expected without-origin repository in scan result")
	}
	if withoutOrigin.Path != repoWithoutOrigin {
		t.Fatalf("expected path %q, got %q", repoWithoutOrigin, withoutOrigin.Path)
	}
	if withoutOrigin.URL != "" {
		t.Fatalf("expected empty url for repo without origin, got %q", withoutOrigin.URL)
	}
}

func TestScanDirectoryReturnsContextErrorWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ScanDirectory(ctx, t.TempDir())
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got %v", err)
	}
}

func runCommand(t *testing.T, workdir string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %v\n%s\n%v", name, args, string(out), err)
	}
}

func TestLoadParsesBranchAutocheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: test\n" +
		"    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n" +
		"    branch:\n" +
		"      keep: ['^(main|master)$']\n" +
		"      autocheck: ['^feature/.*$', '^bugfix/.*$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	repo := cfg.Repos[0]
	if len(repo.Branch.Autocheck) != 2 {
		t.Fatalf("expected 2 autocheck patterns, got %d", len(repo.Branch.Autocheck))
	}
	if !repo.MatchesAutocheck("feature/login") {
		t.Fatal("feature/login must match autocheck")
	}
	if !repo.MatchesAutocheck("bugfix/crash") {
		t.Fatal("bugfix/crash must match autocheck")
	}
	if repo.MatchesAutocheck("main") {
		t.Fatal("main must not match autocheck")
	}
	if repo.MatchesAutocheck("hotfix/urgent") {
		t.Fatal("hotfix/urgent must not match autocheck")
	}
}

func TestAutocheckIsNoOpWhenEmpty(t *testing.T) {
	t.Parallel()

	repo := RepoConfig{}
	repo.autocheck = nil

	if repo.MatchesAutocheck("anything") {
		t.Fatal("empty autocheck must not match any branch")
	}
}

func TestLoadFailsOnInvalidAutocheckRegex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: test\n" +
		"    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n" +
		"    branch:\n" +
		"      autocheck: ['[invalid']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected regex validation error for branch.autocheck")
	}
	if !strings.Contains(err.Error(), "branch.autocheck") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAutocheckDoesNotMatchProtectedBranches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := "repos:\n" +
		"  - name: test\n" +
		"    url: https://gitlab.com/anHome/git-branch-cleaner-test.git\n" +
		"    branch:\n" +
		"      keep: ['^release/.*$']\n" +
		"      autocheck: ['^release/.*$', '^feature/.*$']\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	repo := cfg.Repos[0]
	// autocheck regex совпадает, но ветка защищена через keep
	if !repo.IsProtected("release/1.0") {
		t.Fatal("release/1.0 must be protected")
	}
	// MatchesAutocheck проверяет только regex, не защиту
	if !repo.MatchesAutocheck("release/1.0") {
		t.Fatal("release/1.0 must match autocheck regex")
	}
	if !repo.MatchesAutocheck("feature/task") {
		t.Fatal("feature/task must match autocheck regex")
	}
}
