package jira

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/browser"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"

	"go.uber.org/zap"
)

const defaultStatusTimeout = 5 * time.Second
const defaultTransientStatusTTL = 15 * time.Second
const maxTransientStatusTTL = 2 * time.Minute
const jiraSearchBatchSize = 500

type StatusBatchRequest struct {
	Group       string
	TicketURL   string
	JiraBaseURL string
	Key         string
}

// PrefetchBatchProgress описывает прогресс пакетной проверки Jira-статусов.
type PrefetchBatchProgress struct {
	BatchIndex int
	BatchTotal int
	BatchSize  int
	Processed  int
	Total      int
}

type PrefetchProgressCallback func(PrefetchBatchProgress)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type browserRequester interface {
	RequestGET(ctx context.Context, requestURL string, headers map[string]string) (int, map[string]string, []byte, error)
}

type groupTransport string

const (
	groupTransportHTTP    groupTransport = "http"
	groupTransportBrowser groupTransport = "browser"
)

type groupAuth struct {
	token    string
	username string
	password string
}

type groupSettings struct {
	baseURL    string
	transport  groupTransport
	auth       groupAuth
	httpClient httpDoer // per-group клиент (mTLS/CA); nil — использовать общий
}

type StatusService struct {
	httpClient httpDoer
	browser    browserRequester
	groups     map[string]groupSettings
	logger     *zap.Logger

	fallbackMu     sync.Mutex
	fallbackWarned map[string]bool

	mu    sync.RWMutex
	cache map[string]cacheEntry

	fetchMu sync.Mutex
}

type StatusServiceOption func(*StatusService)

func NewStatusService(timeout time.Duration, opts ...StatusServiceOption) *StatusService {
	if timeout <= 0 {
		timeout = defaultStatusTimeout
	}

	svc := &StatusService{
		httpClient:     &http.Client{Timeout: timeout},
		groups:         make(map[string]groupSettings),
		cache:          make(map[string]cacheEntry),
		fallbackWarned: make(map[string]bool),
		logger:         zap.NewNop(),
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(svc)
	}

	return svc
}

func WithGroupConfigs(timeout time.Duration, groups []config.JiraConfig) (StatusServiceOption, error) {
	option := func(s *StatusService) {
		if s == nil {
			return
		}

		next := make(map[string]groupSettings, len(groups))
		for _, group := range groups {
			name := strings.TrimSpace(group.Group)
			if name == "" {
				continue
			}

			transport := groupTransportHTTP
			if group.Playwright {
				transport = groupTransportBrowser
			}

			token := strings.TrimSpace(group.Token)
			if strings.EqualFold(strings.TrimSpace(group.Type), "token") {
				token = strings.TrimSpace(group.Token)
			}

			next[name] = groupSettings{
				baseURL:   normalizeBaseURL(group.URL),
				transport: transport,
				auth: groupAuth{
					token:    token,
					username: strings.TrimSpace(group.Login.Username),
					password: group.Login.Password,
				},
			}
		}

		s.groups = next
	}

	// Fail fast: сертификаты mTLS-групп проверяются и загружаются один раз при старте.
	perGroup := make(map[string]httpDoer, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Group)
		if name == "" || group.SSL.IsZero() {
			continue
		}
		client, err := buildGroupHTTPClient(timeout, group.SSL)
		if err != nil {
			return nil, fmt.Errorf("jira-группа %q: %w", name, err)
		}
		if client != nil {
			perGroup[name] = client
		}
	}
	if len(perGroup) > 0 {
		inner := option
		option = func(s *StatusService) {
			inner(s)
			if s == nil {
				return
			}
			for name, client := range perGroup {
				if gs, ok := s.groups[name]; ok {
					gs.httpClient = client
					s.groups[name] = gs
				}
			}
		}
	}

	return option, nil
}

func WithBrowserRuntime(runtime *browser.PlaywrightRuntime) StatusServiceOption {
	return func(s *StatusService) {
		if s == nil {
			return
		}

		s.browser = runtime
	}
}

func WithLogger(logger *zap.Logger) StatusServiceOption {
	return func(s *StatusService) {
		if s == nil || logger == nil {
			return
		}

		s.logger = logger
	}
}

func (s *StatusService) ResolveStatus(group, ticketURL, jiraBaseURL, key string) StatusResult {
	if s == nil {
		return StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonTransportError}
	}

	req := StatusBatchRequest{
		Group:       group,
		TicketURL:   ticketURL,
		JiraBaseURL: jiraBaseURL,
		Key:         key,
	}
	s.PrefetchStatuses([]StatusBatchRequest{req})

	cacheKey, cacheErr := s.cacheKeyForRequest(req)
	if cacheErr != nil {
		return cacheErr.result
	}
	if result, ok := s.cached(cacheKey); ok {
		return result
	}

	return StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonTransportError}
}
