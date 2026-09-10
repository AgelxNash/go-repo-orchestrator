package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeLookup(env map[string]string) envLookup {
	return func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}
}

func TestExpandEnvString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "без доллара не меняется", in: "plain value", want: "plain value"},
		{name: "подстановка в середине строки", in: "abc-${VAR}-def", env: map[string]string{"VAR": "X"}, want: "abc-X-def"},
		{name: "несколько плейсхолдеров", in: "${A}${B}", env: map[string]string{"A": "1", "B": "2"}, want: "12"},
		{name: "дефолт при незаданной", in: "${VAR:-fallback}", env: map[string]string{}, want: "fallback"},
		{name: "дефолт при пустом значении", in: "${VAR:-fallback}", env: map[string]string{"VAR": ""}, want: "fallback"},
		{name: "значение приоритетнее дефолта", in: "${VAR:-fallback}", env: map[string]string{"VAR": "real"}, want: "real"},
		{name: "пустой дефолт легализует пустоту", in: "${VAR:-}", env: map[string]string{}, want: ""},
		{name: "заданная пустая без дефолта подставляется", in: "[${VAR}]", env: map[string]string{"VAR": ""}, want: "[]"},
		{name: "дефолт с двоеточиями (URL)", in: "${CDP:-http://127.0.0.1:9222}", env: map[string]string{}, want: "http://127.0.0.1:9222"},
		{name: "незаданная без дефолта — ошибка", in: "${MISSING_VAR}", env: map[string]string{}, wantErr: "MISSING_VAR не задана"},
		{name: "незакрытый плейсхолдер — ошибка", in: "prefix ${OPEN", wantErr: "незакрытый плейсхолдер"},
		{name: "некорректное имя — ошибка", in: "${1BAD}", wantErr: "некорректное имя переменной"},
		{name: "пустое имя — ошибка", in: "${}", wantErr: "некорректное имя переменной"},
		{name: "$$ экранирует литеральный доллар", in: "cost$$", want: "cost$"},
		{name: "$$ перед литеральной скобкой", in: "$${not-a-var}", want: "${not-a-var}"},
		{name: "одиночный $ в конце остаётся (regex-якорь)", in: "^feature/\\d+$", want: "^feature/\\d+$"},
		{name: "имена с подчёркиванием и цифрами", in: "${A_1_b}", env: map[string]string{"A_1_b": "ok"}, want: "ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := expandEnvString(tt.in, "test.key", fakeLookup(tt.env))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got value %q", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestExpandYAMLEnvNestedPaths(t *testing.T) {
	t.Parallel()

	raw := []byte("jira:\n  - group: G\n    token: ${TOKEN}\n    login:\n      password: ${PASS:-}\nbrowser:\n  cdp_url: ${CDP:-http://127.0.0.1:9222}\nrepos:\n  - name: r\n    path: ${HOME_DIR}/repo\n")
	expanded, err := ExpandYAMLEnv(raw, fakeLookup(map[string]string{
		"TOKEN":    "secret-token",
		"HOME_DIR": "/home/dev",
	}))
	if err != nil {
		t.Fatalf("expand: %v", err)
	}

	wantFragments := []string{
		"token: secret-token",
		"password: \"\"",
		"cdp_url: http://127.0.0.1:9222",
		"path: /home/dev/repo",
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(string(expanded), fragment) {
			t.Fatalf("expected expanded yaml to contain %q, got:\n%s", fragment, expanded)
		}
	}
}

func TestExpandYAMLEnvReportsKeyPath(t *testing.T) {
	t.Parallel()

	raw := []byte("jira:\n  - group: G\n    token: ${MISSING_TOKEN}\n")
	_, err := ExpandYAMLEnv(raw, fakeLookup(nil))
	if err == nil {
		t.Fatal("expected missing env error")
	}
	if !strings.Contains(err.Error(), "jira[0].token") {
		t.Fatalf("expected key path jira[0].token in error, got %q", err)
	}
	if !strings.Contains(err.Error(), "MISSING_TOKEN") {
		t.Fatalf("expected variable name in error, got %q", err)
	}
}

func TestExpandYAMLEnvKeepsNonStringsAndEmptyDoc(t *testing.T) {
	t.Parallel()

	raw := []byte("browser:\n  timeout: 30\n  enabled: true\nrepos:\n  - name: r\n    url: git@example.com:a/b.git\n")
	expanded, err := ExpandYAMLEnv(raw, fakeLookup(nil))
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if !strings.Contains(string(expanded), "timeout: 30") || !strings.Contains(string(expanded), "enabled: true") {
		t.Fatalf("non-string values must be preserved, got:\n%s", expanded)
	}

	empty, err := ExpandYAMLEnv([]byte("# only comment\n"), fakeLookup(nil))
	if err != nil {
		t.Fatalf("expand empty doc: %v", err)
	}
	if string(empty) != "# only comment\n" {
		t.Fatalf("empty document must pass through unchanged, got %q", empty)
	}
}

func TestLoadExpandsEnvPlaceholders(t *testing.T) {
	raw := "repos:\n  - name: test\n    path: ./svc\njira:\n  - group: G\n    url: https://jira.example.com\n    token: ${GRO_TEST_TOKEN}\nbrowser:\n  cdp_url: ${GRO_TEST_CDP:-http://127.0.0.1:9222}\n"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GRO_TEST_TOKEN", "token-from-env")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Jira[0].Token != "token-from-env" {
		t.Fatalf("expected token from env, got %q", cfg.Jira[0].Token)
	}
	if cfg.Browser.CDPURL != "http://127.0.0.1:9222" {
		t.Fatalf("expected cdp_url default, got %q", cfg.Browser.CDPURL)
	}
}

func TestLoadFailsOnMissingEnvWithoutDefault(t *testing.T) {
	raw := "repos:\n  - name: test\n    path: ./svc\njira:\n  - group: G\n    url: https://jira.example.com\n    token: ${GRO_TEST_TOKEN_MISSING}\n"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected load failure on missing env variable")
	}
	for _, fragment := range []string{"GRO_TEST_TOKEN_MISSING", "jira[0].token"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("expected error to contain %q, got %q", fragment, err)
		}
	}
}
