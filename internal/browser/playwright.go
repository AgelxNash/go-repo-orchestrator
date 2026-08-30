package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
	"go.uber.org/zap"
)

type startPlaywrightFunc func(cdpURL string, runOptions *playwright.RunOptions) (playwrightSession, error)
type installPlaywrightFunc func(runOptions *playwright.RunOptions) error

type playwrightSession struct {
	closeFn func() error
	mode    runtimeMode
	browser playwright.Browser
}

type runtimeMode string

const (
	runtimeModeLaunch runtimeMode = "launch"
	runtimeModeCDP    runtimeMode = "cdp"
)

// PlaywrightRuntime управляет жизненным циклом фонового браузера Playwright.
type PlaywrightRuntime struct {
	mu              sync.Mutex
	startFn         startPlaywrightFunc
	installFn       installPlaywrightFunc
	cdpURL          string
	driverDirectory string
	logger          *zap.Logger
	closeFn         func() error
	browser         playwright.Browser
	started         bool
	mode            runtimeMode
}

type PlaywrightRuntimeOption func(*PlaywrightRuntime)

func NewPlaywrightRuntime(cdpURL string, opts ...PlaywrightRuntimeOption) *PlaywrightRuntime {
	runtime := &PlaywrightRuntime{
		startFn:   defaultStartPlaywright,
		installFn: defaultInstallPlaywright,
		cdpURL:    strings.TrimSpace(cdpURL),
		logger:    zap.NewNop(),
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(runtime)
	}

	if runtime.logger == nil {
		runtime.logger = zap.NewNop()
	}

	return runtime
}

func newPlaywrightRuntimeWithStartFn(cdpURL string, startFn startPlaywrightFunc, opts ...PlaywrightRuntimeOption) *PlaywrightRuntime {
	if startFn == nil {
		startFn = defaultStartPlaywright
	}

	runtime := &PlaywrightRuntime{
		startFn:   startFn,
		installFn: defaultInstallPlaywright,
		cdpURL:    strings.TrimSpace(cdpURL),
		logger:    zap.NewNop(),
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(runtime)
	}

	if runtime.logger == nil {
		runtime.logger = zap.NewNop()
	}

	return runtime
}

func WithLogger(logger *zap.Logger) PlaywrightRuntimeOption {
	return func(runtime *PlaywrightRuntime) {
		if runtime == nil {
			return
		}
		runtime.logger = logger
	}
}

func WithStateDir(stateDir string) PlaywrightRuntimeOption {
	return func(runtime *PlaywrightRuntime) {
		if runtime == nil {
			return
		}

		runtime.driverDirectory = defaultPlaywrightDriverDirectory(stateDir)
	}
}

func WithDriverDirectory(driverDirectory string) PlaywrightRuntimeOption {
	return func(runtime *PlaywrightRuntime) {
		if runtime == nil {
			return
		}

		runtime.driverDirectory = strings.TrimSpace(driverDirectory)
	}
}

func WithInstaller(installer installPlaywrightFunc) PlaywrightRuntimeOption {
	return func(runtime *PlaywrightRuntime) {
		if runtime == nil || installer == nil {
			return
		}

		runtime.installFn = installer
	}
}

func (r *PlaywrightRuntime) Start() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	preflight := cdpPreflightResult{}
	if strings.TrimSpace(r.cdpURL) != "" {
		preflight = runCDPPreflight(r.cdpURL)
	}
	if preflight.step != "" && isDebugLogger(r.logger) {
		fields := []zap.Field{
			zap.String("endpoint", sanitizeCDPURL(r.cdpURL)),
			zap.String("step", preflight.step),
			zap.String("class", preflight.class),
		}
		if preflight.message != "" {
			fields = append(fields, zap.String("details", preflight.message))
		}
		r.logger.Debug("cdp preflight", fields...)
	}

	runOptions := r.runOptions()
	session, err := r.startFn(r.cdpURL, runOptions)
	if err != nil && isPlaywrightRuntimeMissingError(err.Error()) {
		if bootstrapErr := r.bootstrap(runOptions); bootstrapErr != nil {
			return fmt.Errorf("не удалось автоматически подготовить локальный Playwright runtime: %w", bootstrapErr)
		}

		session, err = r.startFn(r.cdpURL, runOptions)
	}
	if err != nil {
		r.logCDPConnectError(err, preflight)
		return wrapPlaywrightStartError(err, r.cdpURL, preflight)
	}

	r.closeFn = session.closeFn
	r.browser = session.browser
	r.started = true
	r.mode = session.mode

	return nil
}

func (r *PlaywrightRuntime) runOptions() *playwright.RunOptions {
	options := &playwright.RunOptions{
		Verbose:  false,
		Browsers: []string{"chromium"},
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	}

	if r == nil {
		return options
	}

	if driverDirectory := strings.TrimSpace(r.driverDirectory); driverDirectory != "" {
		options.DriverDirectory = driverDirectory
	}

	return options
}

func (r *PlaywrightRuntime) bootstrap(runOptions *playwright.RunOptions) error {
	if r == nil {
		return errors.New("playwright runtime равен nil")
	}
	if r.installFn == nil {
		return errors.New("playwright installer не настроен")
	}

	driverDirectory := "default cache"
	if runOptions != nil && strings.TrimSpace(runOptions.DriverDirectory) != "" {
		driverDirectory = runOptions.DriverDirectory
	}

	r.logger.Warn("playwright runtime отсутствует, выполняю авто-bootstrap", zap.String("driver_dir", driverDirectory))
	if err := r.installFn(runOptions); err != nil {
		return fmt.Errorf("авто-bootstrap playwright runtime: %w", err)
	}
	r.logger.Info("playwright runtime подготовлен", zap.String("driver_dir", driverDirectory))

	return nil
}

func (r *PlaywrightRuntime) logCDPConnectError(err error, preflight cdpPreflightResult) {
	if r == nil || strings.TrimSpace(r.cdpURL) == "" || !isDebugLogger(r.logger) {
		return
	}

	fields := []zap.Field{
		zap.String("endpoint", sanitizeCDPURL(r.cdpURL)),
		zap.String("error_class", classifyCDPError(err)),
		zap.String("message", sanitizeCDPErrorMessage(err.Error())),
	}
	if preflight.step != "" {
		fields = append(fields, zap.String("preflight_step", preflight.step))
		fields = append(fields, zap.String("preflight_class", preflight.class))
		if preflight.message != "" {
			fields = append(fields, zap.String("preflight_message", preflight.message))
		}
	}

	r.logger.Debug("cdp connect handshake failed", fields...)
}

func isDebugLogger(logger *zap.Logger) bool {
	if logger == nil {
		return false
	}

	return logger.Core().Enabled(zap.DebugLevel)
}

func (r *PlaywrightRuntime) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	r.started = false
	if r.closeFn == nil {
		return nil
	}

	err := r.closeFn()
	r.closeFn = nil
	r.browser = nil
	r.mode = ""

	return err
}

func (r *PlaywrightRuntime) RequestGET(ctx context.Context, requestURL string, headers map[string]string) (int, map[string]string, []byte, error) {
	if r == nil {
		return 0, nil, nil, errors.New("playwright runtime равен nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	started := r.started
	browser := r.browser
	r.mu.Unlock()

	if !started || browser == nil {
		return 0, nil, nil, errors.New("playwright runtime не запущен")
	}

	browserContext, mustClose, err := selectContextForRequest(browser, requestURL)
	if err != nil {
		return 0, nil, nil, err
	}
	if mustClose {
		defer func() {
			_ = browserContext.Close()
		}()
	}

	requestCtx := browserContext.Request()
	options := playwright.APIRequestContextGetOptions{
		Headers: headers,
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout > 0 {
			options.Timeout = playwright.Float(timeout.Seconds() * 1000)
		}
	}

	response, err := requestCtx.Get(requestURL, options)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ошибка get-запроса playwright: %w", err)
	}
	defer func() {
		_ = response.Dispose()
	}()

	body, err := response.Body()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("прочитать тело ответа playwright: %w", err)
	}

	return response.Status(), response.Headers(), body, nil
}

func selectContextForRequest(browser playwright.Browser, requestURL string) (playwright.BrowserContext, bool, error) {
	contexts := browser.Contexts()
	for _, ctx := range contexts {
		cookies, err := ctx.Cookies(requestURL)
		if err != nil {
			continue
		}
		if len(cookies) > 0 {
			return ctx, false, nil
		}
	}

	if len(contexts) > 0 {
		return contexts[0], false, nil
	}

	ctx, err := browser.NewContext()
	if err != nil {
		return nil, false, fmt.Errorf("создать browser context playwright: %w", err)
	}

	return ctx, true, nil
}

func (r *PlaywrightRuntime) Started() bool {
	if r == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.started
}

func (r *PlaywrightRuntime) Mode() string {
	if r == nil {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return string(r.mode)
}

func (r *PlaywrightRuntime) DriverDirectory() string {
	if r == nil {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return strings.TrimSpace(r.driverDirectory)
}

func defaultStartPlaywright(cdpURL string, runOptions *playwright.RunOptions) (playwrightSession, error) {
	cdpURL = strings.TrimSpace(cdpURL)

	pw, err := playwright.Run(runOptions)
	if err != nil {
		return playwrightSession{}, err
	}

	if cdpURL != "" {
		browser, err := pw.Chromium.ConnectOverCDP(cdpURL, playwright.BrowserTypeConnectOverCDPOptions{
			Timeout: playwright.Float(15000),
		})
		if err != nil {
			_ = pw.Stop()
			return playwrightSession{}, err
		}

		return playwrightSession{closeFn: func() error {
			if err := pw.Stop(); err != nil {
				return fmt.Errorf("остановить playwright runtime: %w", err)
			}

			return nil
		}, mode: runtimeModeCDP, browser: browser}, nil
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		_ = pw.Stop()
		return playwrightSession{}, err
	}

	return playwrightSession{closeFn: func() error {
		var errs []error
		if err := browser.Close(); err != nil {
			errs = append(errs, fmt.Errorf("закрыть браузер playwright: %w", err))
		}
		if err := pw.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("остановить playwright runtime: %w", err))
		}

		return errors.Join(errs...)
	}, mode: runtimeModeLaunch, browser: browser}, nil
}

func defaultInstallPlaywright(runOptions *playwright.RunOptions) error {
	if err := playwright.Install(runOptions); err != nil {
		return err
	}

	return nil
}

func defaultPlaywrightDriverDirectory(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}

	version := strings.TrimSpace(playwrightDriverVersion())
	if version == "" {
		return filepath.Join(stateDir, "playwright", "driver")
	}

	return filepath.Join(stateDir, "playwright", "driver", version)
}

func playwrightDriverVersion() string {
	driver, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: "."})
	if err != nil {
		return ""
	}

	return strings.TrimSpace(driver.Version)
}

type cdpPreflightResult struct {
	step    string
	class   string
	message string
}

func runCDPPreflight(rawCDPURL string) cdpPreflightResult {
	rawCDPURL = strings.TrimSpace(rawCDPURL)
	if rawCDPURL == "" {
		return cdpPreflightResult{}
	}

	parsed, err := url.Parse(rawCDPURL)
	if err != nil {
		return cdpPreflightResult{step: "parse", class: "invalid_url", message: "parse cdp url failed"}
	}

	switch parsed.Scheme {
	case "http", "https":
		return probeHTTPCDPVersion(parsed)
	case "ws", "wss":
		return cdpPreflightResult{step: "preflight", class: "skipped_ws_endpoint", message: "ws/wss endpoint preflight skipped"}
	default:
		return cdpPreflightResult{step: "preflight", class: "unsupported_scheme", message: "unsupported cdp scheme"}
	}
}

func probeHTTPCDPVersion(parsed *url.URL) cdpPreflightResult {
	if parsed == nil {
		return cdpPreflightResult{step: "http_probe", class: "invalid_url", message: "nil parsed cdp url"}
	}

	probeURL := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/json/version"}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, probeURL.String(), nil)
	if err != nil {
		return cdpPreflightResult{step: "http_probe", class: "request_build_failed", message: "unable to create cdp probe request"}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return cdpPreflightResult{step: "http_probe", class: "endpoint_unreachable", message: sanitizeCDPErrorMessage(err.Error())}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return cdpPreflightResult{step: "http_probe", class: "read_failed", message: "unable to read cdp probe response"}
	}

	if resp.StatusCode != http.StatusOK {
		return cdpPreflightResult{step: "http_probe", class: "unexpected_status", message: fmt.Sprintf("unexpected status: %d", resp.StatusCode)}
	}

	var payload struct {
		Browser              string `json:"Browser"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return cdpPreflightResult{step: "http_probe", class: "non_cdp_response", message: "endpoint responds but is not chromium cdp"}
	}

	if strings.TrimSpace(payload.WebSocketDebuggerURL) == "" {
		return cdpPreflightResult{step: "http_probe", class: "non_cdp_response", message: "missing webSocketDebuggerUrl in cdp response"}
	}

	if strings.TrimSpace(payload.Browser) == "" {
		return cdpPreflightResult{step: "http_probe", class: "cdp_endpoint_detected", message: "cdp endpoint is reachable"}
	}

	return cdpPreflightResult{step: "http_probe", class: "cdp_endpoint_detected", message: "chromium cdp endpoint is reachable"}
}

var urlInErrorRE = regexp.MustCompile(`(?i)(?:https?|wss?)://[^\s"')]+`)

func sanitizeCDPErrorMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	return urlInErrorRE.ReplaceAllStringFunc(raw, func(candidate string) string {
		sanitized := sanitizeCDPURL(candidate)
		if sanitized == "" {
			return "<redacted-url>"
		}
		return sanitized
	})
}

func classifyCDPError(err error) string {
	if err == nil {
		return "unknown"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case isPlaywrightDriverMissingError(msg):
		return "driver_missing"
	case strings.Contains(msg, "connection refused"):
		return "connection_refused"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "not found"):
		return "not_found"
	case strings.Contains(msg, "handshake"):
		return "handshake_error"
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") || strings.Contains(msg, "401") || strings.Contains(msg, "403"):
		return "auth_error"
	default:
		return "connect_error"
	}
}

const playwrightInstallCommand = "go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps"

func wrapPlaywrightStartError(err error, cdpURL string, preflight cdpPreflightResult) error {
	_ = err
	driverMissing := isPlaywrightDriverMissingError(strings.ToLower(err.Error()))

	if strings.TrimSpace(cdpURL) != "" {
		sanitized := sanitizeCDPURL(cdpURL)
		if driverMissing {
			if preflight.class == "cdp_endpoint_detected" {
				if sanitized != "" {
					return fmt.Errorf("CDP endpoint доступен (%s), но локальный Playwright driver не установлен. Установите driver/runtime: %s", sanitized, playwrightInstallCommand)
				}
				return fmt.Errorf("CDP endpoint доступен, но локальный Playwright driver не установлен. Установите driver/runtime: %s", playwrightInstallCommand)
			}
			if sanitized != "" {
				return fmt.Errorf("не удалось подключиться к внешнему браузеру по CDP (%s): локальный Playwright driver не установлен. Установите driver/runtime: %s", sanitized, playwrightInstallCommand)
			}
			return fmt.Errorf("не удалось подключиться к внешнему браузеру по CDP: локальный Playwright driver не установлен. Установите driver/runtime: %s", playwrightInstallCommand)
		}
		if sanitized != "" {
			return fmt.Errorf("не удалось подключиться к внешнему браузеру по CDP (%s)", sanitized)
		}
		return fmt.Errorf("не удалось подключиться к внешнему браузеру по CDP")
	}

	return fmt.Errorf("не удалось запустить Playwright браузер. Установите драйвер и браузеры: %s", playwrightInstallCommand)
}

func isPlaywrightDriverMissingError(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}

	return strings.Contains(message, "please install the driver") ||
		strings.Contains(message, "driver executable doesn't exist") ||
		(strings.Contains(message, "playwright") && strings.Contains(message, "install"))
}

func isPlaywrightRuntimeMissingError(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}

	if isPlaywrightDriverMissingError(message) {
		return true
	}

	return strings.Contains(message, "browser executable") ||
		strings.Contains(message, "executable doesn't exist") ||
		strings.Contains(message, "download new browsers")
}

func sanitizeCDPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host
}
