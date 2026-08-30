package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// moduleRoot returns the absolute path to the repository root (where go.mod lives).
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file lives at cmd/go-repo-orchestrator/gomod_test.go,
	// so two parent directories up is the module root.
	return filepath.Join(filepath.Dir(filename), "..", "..")
}

// TestGoModVerify runs "go mod verify" to confirm that all modules on disk
// match their expected hashes in go.sum.
func TestGoModVerify(t *testing.T) {
	root := moduleRoot(t)

	cmd := exec.Command("go", "mod", "verify")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go mod verify failed: %v\n%s", err, out)
	}
}

// TestGoModTidy verifies the module graph is tidy — that is, running "go mod tidy"
// would produce no diff. It relies on "go mod tidy" exiting non-zero when the
// module is not tidy in recent Go toolchains.
func TestGoModTidy(t *testing.T) {
	root := moduleRoot(t)

	// Capture current go.mod and go.sum content before attempting tidy.
	goModPath := filepath.Join(root, "go.mod")
	goSumPath := filepath.Join(root, "go.sum")

	originalGoMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod before tidy: %v", err)
	}
	originalGoSum, err := os.ReadFile(goSumPath)
	if err != nil {
		t.Fatalf("read go.sum before tidy: %v", err)
	}

	// Run go mod tidy.
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	// Read updated files.
	newGoMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod after tidy: %v", err)
	}
	newGoSum, err := os.ReadFile(goSumPath)
	if err != nil {
		t.Fatalf("read go.sum after tidy: %v", err)
	}

	// Restore originals regardless of outcome.
	t.Cleanup(func() {
		_ = os.WriteFile(goModPath, originalGoMod, 0o644) //nolint:gosec // восстановление собственного go.mod, путь вычислен от корня модуля
		_ = os.WriteFile(goSumPath, originalGoSum, 0o644) //nolint:gosec // восстановление собственного go.sum, путь вычислен от корня модуля
	})

	if !bytes.Equal(originalGoMod, newGoMod) {
		t.Error("go.mod changed after go mod tidy — module is not tidy")
	}
	if !bytes.Equal(originalGoSum, newGoSum) {
		t.Error("go.sum changed after go mod tidy — module is not tidy")
	}
}
