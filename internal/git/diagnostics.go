package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepoDiagnostics — диагностический срез состояния локального репозитория
// для команды doctor repo (issue #68). Значения только для чтения, секретов
// не содержат.
type RepoDiagnostics struct {
	Path string

	Exists       bool
	IsGit        bool
	RootMismatch bool
	RepoRoot     string

	HEADValid     bool
	CurrentBranch string
	LocalBranches []string
	RemoteRefs    int
	Upstream      string
	DirtyFiles    int

	FetchAttempted bool
	FetchErr       string
}

// DiagnoseRepo собирает состояние репозитория по пути, используя те же
// git-примитивы, что и боевая загрузка (runGit/fetchPrune). probeFetch
// разрешает пробу fetch --prune origin с полной ошибкой (без мутаций,
// кроме обновления remote-refs).
func (c *Client) DiagnoseRepo(ctx context.Context, repoPath string, probeFetch bool) RepoDiagnostics {
	diag := RepoDiagnostics{Path: repoPath}

	info, err := os.Stat(repoPath)
	if err != nil || !info.IsDir() {
		return diag
	}
	diag.Exists = true

	if out, err := c.runGit(ctx, repoPath, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return diag
	}
	diag.IsGit = true

	absPath, err := filepath.Abs(repoPath)
	if err == nil {
		root, err := c.runGit(ctx, repoPath, "rev-parse", "--show-toplevel")
		if err == nil {
			diag.RepoRoot = strings.TrimSpace(root)
			diag.RootMismatch = diag.RepoRoot != filepath.Clean(absPath)
		}
	}

	if _, err := c.runGit(ctx, repoPath, "rev-parse", "--quiet", "--verify", "HEAD"); err == nil {
		diag.HEADValid = true
		if branch, err := c.runGit(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			diag.CurrentBranch = strings.TrimSpace(branch)
		}
		if upstream, err := c.runGit(ctx, repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
			diag.Upstream = strings.TrimSpace(upstream)
		}
	}

	if branches, err := c.runGit(ctx, repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads"); err == nil {
		for _, branch := range strings.Split(strings.TrimSpace(branches), "\n") {
			if branch = strings.TrimSpace(branch); branch != "" {
				diag.LocalBranches = append(diag.LocalBranches, branch)
			}
		}
	}
	if refs, err := c.runGit(ctx, repoPath, "for-each-ref", "--format=%(refname)", "refs/remotes"); err == nil {
		diag.RemoteRefs = len(nonEmptyLines(refs))
	}
	if status, err := c.runGit(ctx, repoPath, "status", "--porcelain"); err == nil {
		diag.DirtyFiles = len(nonEmptyLines(status))
	}

	if probeFetch && diag.IsGit {
		hasOrigin := false
		if remotes, err := c.runGit(ctx, repoPath, "remote"); err == nil {
			for _, r := range strings.Split(remotes, "\n") {
				if strings.TrimSpace(r) == "origin" {
					hasOrigin = true
					break
				}
			}
		}
		if hasOrigin {
			diag.FetchAttempted = true
			if err := c.fetchPrune(ctx, repoPath); err != nil {
				diag.FetchErr = err.Error()
			}
		}
	}

	return diag
}

func nonEmptyLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// VerdictText возвращает человекочитаемый вердикт по диагностике: что именно
// сломано и как чинить.
func (d RepoDiagnostics) VerdictText() string {
	switch {
	case !d.Exists:
		return fmt.Sprintf("каталог %q не существует", d.Path)
	case !d.IsGit:
		return "каталог не является git-репозиторием (нет .git)"
	case d.RootMismatch:
		return fmt.Sprintf("путь — вложенная папка репозитория, а не корень (корень: %s); укажите корень в path", d.RepoRoot)
	case !d.HEADValid && d.RemoteRefs > 0:
		return "клон-обрывок: remote-ссылки есть, но HEAD указывает на ветку без единого коммита (сбой первичного clone/fetch) — повторите синхронизацию или пересоздайте клон"
	case !d.HEADValid:
		return "HEAD невалиден и remote-ссылок нет — пустой git-каталог (git init без коммитов); повторите синхронизацию или пересоздайте клон"
	case d.FetchAttempted && d.FetchErr != "":
		return "репозиторий читается локально, но синхронизация remote не удалась (см. ошибку пробы выше)"
	case d.Upstream == "":
		return "репозиторий загрузится; у текущей ветки не настроен upstream"
	default:
		return fmt.Sprintf("репозиторий загрузится: ветка %s (upstream %s), локальных веток %d", d.CurrentBranch, d.Upstream, len(d.LocalBranches))
	}
}
