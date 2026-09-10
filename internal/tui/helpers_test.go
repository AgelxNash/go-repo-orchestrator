package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agelxnash/go-repo-orchestrator/internal/git"
)

// TestWrapTextKeepsLongGitStderrReadable проверяет, что длинный stderr git переносится без потери причины.
func TestWrapTextKeepsLongGitStderrReadable(t *testing.T) {
	t.Parallel()

	src := "ошибка fetch --prune origin: ошибка git fetch: kex_exchange_identification: read: Connection reset by peer\nfatal: Не удалось прочитать из внешнего репозитория."
	lines := wrapText(src, 40)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "kex_exchange_identification") {
		t.Fatalf("expected handshake cause to remain, got %q", joined)
	}
	if !strings.Contains(joined, "внешнего") || !strings.Contains(joined, "репозитория") {
		t.Fatalf("expected fatal line to remain, got %q", joined)
	}
	for _, line := range lines {
		if len([]rune(line)) > 40 {
			t.Fatalf("wrapped line exceeded width: %q", line)
		}
	}
}

// TestUserFacingErrorKeepsTransientSSHDetails сохраняет kex_exchange_identification в тексте для TUI.
func TestUserFacingErrorKeepsTransientSSHDetails(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: ошибка git fetch: kex_exchange_identification: read: Connection reset by peer\nfatal: Не удалось прочитать из внешнего репозитория.", git.ErrTransientNetwork)
	got := userFacingError(err)
	if got == nil {
		t.Fatal("expected user-facing error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "лимит одновременных сессий") {
		t.Fatalf("expected network-limit hint, got %q", msg)
	}
	if !strings.Contains(msg, "kex_exchange_identification") {
		t.Fatalf("expected original git stderr, got %q", msg)
	}
	if !strings.Contains(msg, "внешнего репозитория") {
		t.Fatalf("expected fatal line, got %q", msg)
	}
}

// TestUserFacingErrorKeepsEmptyClone сохраняет ErrEmptyClone для отображения оболочки без checkout.
func TestUserFacingErrorKeepsEmptyClone(t *testing.T) {
	t.Parallel()

	got := userFacingError(fmt.Errorf("получить статус репозитория: %w", git.ErrEmptyClone))
	if !errors.Is(got, git.ErrEmptyClone) {
		t.Fatalf("expected ErrEmptyClone to be preserved, got %v", got)
	}
}
