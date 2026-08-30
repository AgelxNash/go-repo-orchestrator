package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func TestSanitizeBranchNameAcceptsSafe(t *testing.T) {
	t.Parallel()

	safe := []string{
		"main",
		"feature/TASK-123-add-sso",
		"fix/edge-case",
		"release_2026.04.0",
		"hotfix.dot.branch",
	}
	for _, name := range safe {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := sanitizeBranchName(name)
			if err != nil {
				t.Fatalf("safe name %q rejected: %v", name, err)
			}
			if got == "" {
				t.Fatalf("safe name %q normalized to empty", name)
			}
		})
	}
}

func TestSanitizeBranchNameRejectsUnsafe(t *testing.T) {
	t.Parallel()

	unsafe := map[string]string{
		"empty":             "",
		"whitespace":        "   ",
		"cr_injection":      "feature\r\nrm -rf /",
		"lf_injection":      "feature\nrm -rf /",
		"null_byte":         "feature\x00branch",
		"tab_char":          "feature\tbranch",
		"del_char":          "feature\x7fbranch",
		"cmd_percent":       "feature%PATH%",
		"backtick":          "feature`whoami`",
		"too_long":          strings.Repeat("a", maxBranchNameLength+1),
		"control_vert_tab":  "feature\vbranch",
		"control_form_feed": "feature\fbranch",
	}
	for name, value := range unsafe {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := sanitizeBranchName(value)
			if err == nil {
				t.Fatalf("expected error for unsafe name %q", value)
			}
			if !errors.Is(err, ErrInvalidBranchName) {
				t.Fatalf("expected ErrInvalidBranchName, got %v", err)
			}
		})
	}
}

func TestSanitizeBranchNameTrimsWhitespace(t *testing.T) {
	t.Parallel()

	got, err := sanitizeBranchName("  feature/trim-me  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feature/trim-me" {
		t.Fatalf("expected trim, got %q", got)
	}
}

func TestEscapeForBatNeutralizesInjection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		mustNot  []string
		expected string
	}{
		{
			name:     "double_quote_doubled",
			input:    `branch"name`,
			expected: `branch""name`,
		},
		{
			name:     "caret_doubled",
			input:    "a^b",
			expected: "a^^b",
		},
		{
			name:     "percent_doubled",
			input:    "%PATH%",
			expected: "%%PATH%%",
		},
		{
			name:     "exclamation_escaped",
			input:    "!var!",
			expected: "^!var^!",
		},
		{
			name:    "ampersand_escaped",
			input:   "a&whoami",
			mustNot: []string{"a&whoami"},
		},
		{
			name:    "pipe_escaped",
			input:   "a|type",
			mustNot: []string{"a|type"},
		},
		{
			name:    "redirect_escaped",
			input:   "a>file",
			mustNot: []string{"a>file"},
		},
		{
			name:  "combo_injection",
			input: `a"b%c&d|e<f>g^h!i`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := escapeForBat(tc.input)
			if tc.expected != "" && got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
			for _, fragment := range tc.mustNot {
				if strings.Contains(got, fragment) {
					t.Fatalf("escaped value %q still contains unsafe fragment %q", got, fragment)
				}
			}
			// Гарантия, что в экранированной строке не появится новый разрыв,
			// способный сломать кавычную обёртку.
			if strings.ContainsRune(got, '\n') || strings.ContainsRune(got, '\r') {
				t.Fatalf("escaped value contains CR/LF: %q", got)
			}
		})
	}
}

func TestQuoteForBatWrapsAndEscapes(t *testing.T) {
	t.Parallel()

	got := quoteForBat(`a"b`)
	if got != `"a""b"` {
		t.Fatalf("expected wrapped+escaped value, got %q", got)
	}
}

func TestBuildDeleteCommandBATAvoidsUnsafeBranches(t *testing.T) {
	t.Parallel()

	unsafeName := "feature\r\n%PATH%"
	branch := model.BranchInfo{Name: unsafeName, Scope: model.BranchScopeLocal}
	if got := buildDeleteCommandBAT(branch); got != "" {
		t.Fatalf("expected empty string for unsafe local branch, got %q", got)
	}

	unsafeRemote := "feature%PATH%"
	branchRemote := model.BranchInfo{
		Name:          unsafeRemote,
		Scope:         model.BranchScopeRemote,
		RemoteName:    "origin",
		QualifiedName: "origin/feature%PATH%",
	}
	if got := buildDeleteCommandBAT(branchRemote); got != "" {
		t.Fatalf("expected empty string for unsafe remote branch, got %q", got)
	}
}
