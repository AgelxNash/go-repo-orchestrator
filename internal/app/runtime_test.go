package app

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewRuntimeUsesStateDirForPlaywrightDriver(t *testing.T) {
	t.Parallel()

	runtime, err := NewRuntime("/var/lib/gbc-state", "/var/lib/gbc-state/workspace", time.Second, "", nil, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runtime == nil || runtime.Playwright == nil {
		t.Fatal("runtime playwright is required")
	}

	driverDirectory := runtime.Playwright.DriverDirectory()
	if !strings.Contains(driverDirectory, "/var/lib/gbc-state/playwright/driver/") {
		t.Fatalf("unexpected driver directory: %q", driverDirectory)
	}
}
