package browser

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"
	"go.uber.org/zap"
)

func TestPlaywrightRuntimeStartAndClose(t *testing.T) {
	t.Parallel()

	startedCalls := 0
	closedCalls := 0

	runtime := newPlaywrightRuntimeWithStartFn("", func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		if cdpURL != "" {
			t.Fatalf("unexpected cdp url in launch mode test: %q", cdpURL)
		}
		startedCalls++
		return playwrightSession{
			closeFn: func() error {
				closedCalls++
				return nil
			},
			mode: runtimeModeLaunch,
		}, nil
	})

	if err := runtime.Start(); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	if !runtime.Started() {
		t.Fatal("expected runtime to be started")
	}

	if err := runtime.Start(); err != nil {
		t.Fatalf("second start must be idempotent: %v", err)
	}
	if startedCalls != 1 {
		t.Fatalf("expected one start call, got %d", startedCalls)
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	if runtime.Started() {
		t.Fatal("expected runtime to be stopped")
	}
	if closedCalls != 1 {
		t.Fatalf("expected one close call, got %d", closedCalls)
	}
	if runtime.Mode() != "" {
		t.Fatalf("expected mode reset after close, got %q", runtime.Mode())
	}
}

func TestPlaywrightRuntimeRequestGETWhenNotStarted(t *testing.T) {
	t.Parallel()

	runtime := newPlaywrightRuntimeWithStartFn("", nil)
	_, _, _, err := runtime.RequestGET(t.Context(), "https://jira.example.com/rest/api/2/issue/OPS-1?fields=status", nil)
	if err == nil {
		t.Fatal("expected request error when runtime is not started")
	}
	if !strings.Contains(err.Error(), "playwright runtime не запущен") {
		t.Fatalf("unexpected request error: %v", err)
	}
}

func TestPlaywrightRuntimeStartWrapsError(t *testing.T) {
	t.Parallel()

	runtime := newPlaywrightRuntimeWithStartFn("", func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("driver not found")
	})

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "не удалось запустить Playwright браузер") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if !strings.Contains(msg, "go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps") {
		t.Fatalf("expected install hint in error, got: %v", err)
	}
}

func TestPlaywrightRuntimeUsesCDPModeWhenConfigured(t *testing.T) {
	t.Parallel()

	const cdpURL = "http://127.0.0.1:9222"
	called := 0

	runtime := newPlaywrightRuntimeWithStartFn(cdpURL, func(receivedCDPURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		called++
		if receivedCDPURL != cdpURL {
			t.Fatalf("expected cdp url %q, got %q", cdpURL, receivedCDPURL)
		}
		return playwrightSession{closeFn: func() error { return nil }, mode: runtimeModeCDP}, nil
	})

	if err := runtime.Start(); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	if runtime.Mode() != string(runtimeModeCDP) {
		t.Fatalf("expected cdp mode, got %q", runtime.Mode())
	}
	if called != 1 {
		t.Fatalf("expected one start call, got %d", called)
	}
}

func TestPlaywrightRuntimeWrapsCDPError(t *testing.T) {
	t.Parallel()

	runtime := newPlaywrightRuntimeWithStartFn("ws://127.0.0.1:9222/devtools/browser/demo", func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("connection refused")
	})

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "не удалось подключиться к внешнему браузеру по CDP") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if strings.Contains(msg, "/devtools/browser/demo") {
		t.Fatalf("expected cdp path to be hidden, got: %v", err)
	}
	if !strings.Contains(msg, "ws://127.0.0.1:9222") {
		t.Fatalf("expected sanitized cdp endpoint in error, got: %v", err)
	}
}

func TestPlaywrightRuntimeWrapsCDPDriverMissingWhenEndpointReachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"Browser":"Chrome/125.0","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/demo"}`))
	}))
	defer srv.Close()

	runtime := newPlaywrightRuntimeWithStartFn(srv.URL, func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("please install the driver (v1.57.0) first")
	}, WithInstaller(func(runOptions *playwright.RunOptions) error {
		if runOptions == nil {
			t.Fatal("run options are required")
		}
		return nil
	}))

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "CDP endpoint доступен") {
		t.Fatalf("expected reachable cdp hint, got: %v", err)
	}
	if !strings.Contains(msg, "локальный Playwright driver не установлен") {
		t.Fatalf("expected driver missing hint, got: %v", err)
	}
	if !strings.Contains(msg, "go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps") {
		t.Fatalf("expected install command hint, got: %v", err)
	}
}

func TestPlaywrightRuntimeWrapsCDPErrorWithoutSecrets(t *testing.T) {
	t.Parallel()

	runtime := newPlaywrightRuntimeWithStartFn("wss://user:token@example.org/devtools/browser/id?token=secret", func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("auth failed")
	})

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	msg := err.Error()
	if strings.Contains(msg, "user") || strings.Contains(msg, "token") || strings.Contains(msg, "secret") {
		t.Fatalf("expected credentials and query to be hidden, got: %v", err)
	}
	if !strings.Contains(msg, "wss://example.org") {
		t.Fatalf("expected sanitized cdp endpoint in error, got: %v", err)
	}
}

func TestPlaywrightRuntimeWrapsCDPErrorWithoutRawURLOnParseFailure(t *testing.T) {
	t.Parallel()

	runtime := newPlaywrightRuntimeWithStartFn("::not-an-url::", func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("bad endpoint")
	})

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	msg := err.Error()
	if strings.Contains(msg, "::not-an-url::") {
		t.Fatalf("expected raw cdp url to be hidden on parse failure, got: %v", err)
	}
}

func TestPlaywrightRuntimeWrapsCDPErrorDoesNotLeakRawDriverErrorURL(t *testing.T) {
	t.Parallel()

	const rawCDPURL = "wss://user:super-secret@example.org:9222/devtools/browser/id?token=top-secret" //nolint:gosec // тестовая константа: проверка маскировки кредов в URL при ошибках

	runtime := newPlaywrightRuntimeWithStartFn(rawCDPURL, func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("driver connect failed for " + cdpURL)
	})

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	msg := err.Error()
	if strings.Contains(msg, rawCDPURL) {
		t.Fatalf("expected raw cdp url to be hidden, got: %v", err)
	}
	if strings.Contains(msg, "user") || strings.Contains(msg, "super-secret") || strings.Contains(msg, "token=top-secret") {
		t.Fatalf("expected credentials and query to be hidden, got: %v", err)
	}
	if strings.Contains(msg, "/devtools/browser/id") {
		t.Fatalf("expected cdp path from raw driver error to be hidden, got: %v", err)
	}
	if !strings.Contains(msg, "wss://example.org:9222") {
		t.Fatalf("expected sanitized cdp endpoint in error, got: %v", err)
	}
}

func TestRunCDPPreflightEndpointUnreachable(t *testing.T) {
	t.Parallel()

	result := runCDPPreflight("http://127.0.0.1:1")
	if result.class != "endpoint_unreachable" {
		t.Fatalf("expected endpoint_unreachable, got %q", result.class)
	}
}

func TestRunCDPPreflightDetectsNonCDPResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	result := runCDPPreflight(srv.URL)
	if result.class != "non_cdp_response" {
		t.Fatalf("expected non_cdp_response, got %q", result.class)
	}
}

func TestRunCDPPreflightDetectsCDPEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"Browser":"Chrome/125.0","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/demo"}`))
	}))
	defer srv.Close()

	result := runCDPPreflight(srv.URL)
	if result.class != "cdp_endpoint_detected" {
		t.Fatalf("expected cdp_endpoint_detected, got %q", result.class)
	}
}

func TestPlaywrightRuntimeSkipsPreflightWhenDebugDisabled(t *testing.T) {
	t.Parallel()

	called := 0
	runtime := newPlaywrightRuntimeWithStartFn("http://127.0.0.1:1", func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		called++
		return playwrightSession{}, errors.New("connection refused")
	})

	_ = runtime.Start()
	if called != 1 {
		t.Fatalf("expected start function call, got %d", called)
	}
}

func TestPlaywrightRuntimeRunsPreflightWhenDebugEnabled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"Browser":"Chrome/125.0","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/demo"}`))
	}))
	defer srv.Close()

	runtime := newPlaywrightRuntimeWithStartFn(srv.URL, func(cdpURL string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("handshake failed")
	}, WithLogger(zap.NewExample()))

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}
}

func TestPlaywrightRuntimeAutoBootstrapOnMissingRuntime(t *testing.T) {
	t.Parallel()

	startCalls := 0
	installCalls := 0
	runtime := newPlaywrightRuntimeWithStartFn("", func(cdpURL string, runOptions *playwright.RunOptions) (playwrightSession, error) {
		if cdpURL != "" {
			t.Fatalf("unexpected cdp url: %q", cdpURL)
		}
		if runOptions == nil {
			t.Fatal("run options are required")
		}
		startCalls++
		if startCalls == 1 {
			return playwrightSession{}, errors.New("please install the driver (v1.57.0) first")
		}

		return playwrightSession{closeFn: func() error { return nil }, mode: runtimeModeLaunch}, nil
	},
		WithStateDir("/var/lib/gbc-state"),
		WithInstaller(func(runOptions *playwright.RunOptions) error {
			if runOptions == nil {
				t.Fatal("run options are required")
			}
			if !strings.Contains(runOptions.DriverDirectory, "/var/lib/gbc-state/playwright/driver/") {
				t.Fatalf("unexpected driver directory: %q", runOptions.DriverDirectory)
			}
			installCalls++
			return nil
		}),
	)

	if err := runtime.Start(); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	if startCalls != 2 {
		t.Fatalf("expected two start attempts, got %d", startCalls)
	}
	if installCalls != 1 {
		t.Fatalf("expected one install call, got %d", installCalls)
	}
}

func TestPlaywrightRuntimeBootstrapFailure(t *testing.T) {
	t.Parallel()

	runtime := newPlaywrightRuntimeWithStartFn("", func(_ string, _ *playwright.RunOptions) (playwrightSession, error) {
		return playwrightSession{}, errors.New("please install the driver (v1.57.0) first")
	}, WithInstaller(func(runOptions *playwright.RunOptions) error {
		if runOptions == nil {
			t.Fatal("run options are required")
		}
		return errors.New("network is unavailable")
	}))

	err := runtime.Start()
	if err == nil {
		t.Fatal("expected start error")
	}

	if !strings.Contains(err.Error(), "не удалось автоматически подготовить локальный Playwright runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}
