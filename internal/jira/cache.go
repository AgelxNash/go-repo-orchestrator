package jira

import (
	"net/http"
	"strings"
	"time"
)

type cacheEntry struct {
	result    StatusResult
	expiresAt time.Time
}

func (s *StatusService) cached(cacheKey string) (StatusResult, bool) {
	s.mu.RLock()
	entry, ok := s.cache[cacheKey]
	s.mu.RUnlock()
	if !ok {
		return StatusResult{}, false
	}

	if entry.expiresAt.IsZero() || time.Now().Before(entry.expiresAt) {
		return entry.result, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok = s.cache[cacheKey]
	if !ok {
		return StatusResult{}, false
	}

	if entry.expiresAt.IsZero() || time.Now().Before(entry.expiresAt) {
		return entry.result, true
	}

	delete(s.cache, cacheKey)
	return StatusResult{}, false
}

func (s *StatusService) store(cacheKey string, result StatusResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[cacheKey] = cacheEntry{result: result}
}

func (s *StatusService) storeTransient(cacheKey string, result StatusResult, ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultTransientStatusTTL
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[cacheKey] = cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
}

func transientFailureTTL(statusCode int, retryAfter string) (time.Duration, bool) {
	switch {
	case statusCode == http.StatusTooManyRequests:
		if ttl, ok := parseRetryAfter(retryAfter); ok {
			return ttl, true
		}
		return defaultTransientStatusTTL, true
	case statusCode >= 500 && statusCode <= 599:
		return defaultTransientStatusTTL, true
	default:
		return 0, false
	}
}

func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		if seconds <= 0 {
			return defaultTransientStatusTTL, true
		}
		if seconds > maxTransientStatusTTL {
			return maxTransientStatusTTL, true
		}
		return seconds, true
	}

	deadline, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}

	ttl := time.Until(deadline)
	if ttl <= 0 {
		return defaultTransientStatusTTL, true
	}
	if ttl > maxTransientStatusTTL {
		return maxTransientStatusTTL, true
	}

	return ttl, true
}

func buildCacheKey(group, ticketURL, jiraBaseURL, key string) string {
	group = strings.TrimSpace(group)
	base := normalizeBaseURL(jiraBaseURL)
	if base == "" {
		base = jiraBaseFromTicketURL(ticketURL)
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if group == "" || base == "" || key == "" || key == "-" {
		return ""
	}

	return group + "|" + base + "|" + key
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return ""
	}

	return strings.TrimRight(raw, "/")
}
