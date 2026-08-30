package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

func (s *StatusService) PrefetchStatuses(ctx context.Context, requests []StatusBatchRequest) {
	_ = s.PrefetchStatusesWithProgress(ctx, requests, nil)
}

func (s *StatusService) PrefetchStatusesWithProgress(ctx context.Context, requests []StatusBatchRequest, onProgress PrefetchProgressCallback) []PrefetchBatchProgress {
	if s == nil || len(requests) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()

	buckets := make(map[string][]preparedStatusRequest)
	seenByBucket := make(map[string]map[string]struct{})

	for _, req := range requests {
		prepared, cacheErr, ok := s.prepareStatusRequest(req)
		if cacheErr != nil {
			if cacheErr.cacheKey != "" {
				s.store(cacheErr.cacheKey, cacheErr.result)
			}
			continue
		}
		if !ok {
			continue
		}

		if _, hit := s.cached(prepared.cacheKey); hit {
			continue
		}

		bucketKey := prepared.bucketKey()
		if _, exists := seenByBucket[bucketKey]; !exists {
			seenByBucket[bucketKey] = make(map[string]struct{})
		}
		if _, dup := seenByBucket[bucketKey][prepared.cacheKey]; dup {
			continue
		}

		seenByBucket[bucketKey][prepared.cacheKey] = struct{}{}
		buckets[bucketKey] = append(buckets[bucketKey], prepared)
	}

	batchTotal := 0
	total := 0
	for _, bucketRequests := range buckets {
		total += len(bucketRequests)
		batchTotal += (len(bucketRequests) + jiraSearchBatchSize - 1) / jiraSearchBatchSize
	}

	if total == 0 || batchTotal == 0 {
		return nil
	}

	progress := make([]PrefetchBatchProgress, 0, batchTotal)
	processed := 0
	batchIndex := 0

	for _, bucketRequests := range buckets {
		for start := 0; start < len(bucketRequests); start += jiraSearchBatchSize {
			end := min(start+jiraSearchBatchSize, len(bucketRequests))
			batch := bucketRequests[start:end]
			s.fetchAndStoreBatch(ctx, batch)

			batchIndex++
			processed += len(batch)
			progressItem := PrefetchBatchProgress{
				BatchIndex: batchIndex,
				BatchTotal: batchTotal,
				BatchSize:  len(batch),
				Processed:  processed,
				Total:      total,
			}
			if onProgress != nil {
				onProgress(progressItem)
			}
			progress = append(progress, progressItem)
		}
	}

	return progress
}

type preparedStatusRequest struct {
	group     string
	cacheKey  string
	key       string
	baseURL   string
	transport groupTransport
	headers   map[string]string
}

func (r preparedStatusRequest) bucketKey() string {
	prefix := r.key
	if idx := strings.Index(r.key, "-"); idx > 0 {
		prefix = r.key[:idx]
	}
	return r.group + "|" + r.baseURL + "|" + string(r.transport) + "|" + prefix
}

type statusCacheError struct {
	cacheKey string
	result   StatusResult
}

func (s *StatusService) cacheKeyForRequest(req StatusBatchRequest) (string, *statusCacheError) {
	group := strings.TrimSpace(req.Group)
	groupCfg, ok := s.groups[group]
	if !ok {
		return "", &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateUnmapped, Reason: StatusReasonNoGroupConfig}}
	}

	resolvedBaseURL := normalizeBaseURL(req.JiraBaseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = groupCfg.baseURL
	}

	cacheKey := buildCacheKey(group, req.TicketURL, resolvedBaseURL, req.Key)
	if cacheKey == "" {
		return "", &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonInvalidRequest}}
	}

	return cacheKey, nil
}

func (s *StatusService) prepareStatusRequest(req StatusBatchRequest) (preparedStatusRequest, *statusCacheError, bool) {
	group := strings.TrimSpace(req.Group)
	groupCfg, ok := s.groups[group]
	if !ok {
		return preparedStatusRequest{}, &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateUnmapped, Reason: StatusReasonNoGroupConfig}}, false
	}

	resolvedBaseURL := normalizeBaseURL(req.JiraBaseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = groupCfg.baseURL
	}

	cacheKey := buildCacheKey(group, req.TicketURL, resolvedBaseURL, req.Key)
	if cacheKey == "" {
		return preparedStatusRequest{}, &statusCacheError{result: StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonInvalidRequest}}, false
	}

	return preparedStatusRequest{
		group:     group,
		cacheKey:  cacheKey,
		key:       strings.ToUpper(strings.TrimSpace(req.Key)),
		baseURL:   resolvedBaseURL,
		transport: groupCfg.transport,
		headers:   buildRequestHeaders(groupCfg.auth),
	}, nil, true
}

func (s *StatusService) fetchAndStoreBatch(ctx context.Context, batch []preparedStatusRequest) {
	if len(batch) == 0 {
		return
	}

	startAt := 0
	allStatuses := make(map[string]string)
	var finalReason StatusReason
	var usedBrowserOverall bool

	for {
		searchURL, err := buildSearchStatusURL(batch[0].baseURL, extractKeys(batch), startAt)
		if err != nil {
			result := StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonInvalidRequest}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		s.logger.Debug("jira search request",
			zap.String("url", searchURL),
			zap.String("group", batch[0].group),
			zap.Int("keys", len(batch)),
			zap.Int("startAt", startAt),
		)

		response, usedBrowserFallback, requestErr := s.resolveSearchWithContext(ctx, batch[0].group, batch[0].transport, searchURL, batch[0].headers)
		if usedBrowserFallback {
			usedBrowserOverall = true
		}

		if requestErr != nil {
			s.logger.Warn("jira request failed",
				zap.String("url", searchURL),
				zap.String("group", batch[0].group),
				zap.Error(requestErr),
			)
			reason := StatusReasonTransportError
			if usedBrowserFallback {
				reason = StatusReasonBrowserUnavailableHTTPError
			}
			result := StatusResult{Status: unknownStatus, State: StatusStateTransient, Reason: reason}
			for _, req := range batch {
				s.storeTransient(req.cacheKey, result, defaultTransientStatusTTL)
			}
			return
		}

		if response.statusCode != http.StatusOK {
			s.logger.Warn("jira non-200 response",
				zap.String("url", searchURL),
				zap.String("group", batch[0].group),
				zap.Int("status_code", response.statusCode),
				zap.String("final_url", response.finalURL),
			)
		}

		if reason, ok := detectAuthReason(response.statusCode, response.location, response.finalURL, response.contentType, response.body); ok {
			result := StatusResult{Status: unknownStatus, State: StatusStateAuth, Reason: reason}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		if response.statusCode != http.StatusOK {
			if response.statusCode == http.StatusBadRequest {
				validBatch, invalidBatch := filterInvalidKeys(batch, response.body)
				if len(invalidBatch) > 0 {
					s.logger.Debug("jira 400 bad request: filtering invalid keys and retrying",
						zap.Int("invalid_count", len(invalidBatch)),
						zap.Int("valid_count", len(validBatch)),
					)
					for _, req := range invalidBatch {
						s.store(req.cacheKey, StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonIssueNotFound})
					}
					batch = validBatch
					if len(batch) == 0 {
						return
					}
					continue
				}
			}

			if ttl, ok := transientFailureTTL(response.statusCode, response.retryAfter); ok {
				result := StatusResult{Status: unknownStatus, State: StatusStateTransient, Reason: StatusReasonTemporarilyDown}
				for _, req := range batch {
					s.storeTransient(req.cacheKey, result, ttl)
				}
				return
			}

			reason := StatusReasonHTTPError
			if response.statusCode == http.StatusNotFound {
				reason = StatusReasonIssueNotFound
			} else if response.statusCode >= 400 && response.statusCode <= 499 {
				reason = StatusReasonClientError
			} else if usedBrowserFallback {
				reason = StatusReasonBrowserUnavailableHTTPError
			}
			result := StatusResult{Status: unknownStatus, State: StatusStateError, Reason: reason}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		statusByKey, total, received, parseErr := parseSearchStatuses(response.body)
		if parseErr != nil {
			result := StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonResponseParseErr}
			for _, req := range batch {
				s.store(req.cacheKey, result)
			}
			return
		}

		for k, v := range statusByKey {
			allStatuses[k] = v
		}

		startAt += received
		if startAt >= total || received == 0 {
			break
		}
	}

	finalReason = StatusReasonNone
	if usedBrowserOverall {
		finalReason = StatusReasonBrowserUnavailableHTTPFallback
	}

	for _, req := range batch {
		status := strings.TrimSpace(allStatuses[req.key])
		if status == "" {
			s.store(req.cacheKey, StatusResult{Status: unknownStatus, State: StatusStateError, Reason: StatusReasonIssueNotFound})
			continue
		}

		s.store(req.cacheKey, StatusResult{Status: status, State: StatusStateReady, Reason: finalReason})
	}
}

func extractKeys(batch []preparedStatusRequest) []string {
	keys := make([]string, 0, len(batch))
	for _, req := range batch {
		keys = append(keys, req.key)
	}
	return keys
}

func filterInvalidKeys(batch []preparedStatusRequest, body []byte) ([]preparedStatusRequest, []preparedStatusRequest) {
	var payload struct {
		ErrorMessages []string `json:"errorMessages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return batch, nil
	}

	invalidKeysMap := make(map[string]bool)
	for _, msg := range payload.ErrorMessages {
		for _, req := range batch {
			if strings.Contains(msg, fmt.Sprintf("'%s'", req.key)) {
				invalidKeysMap[req.key] = true
			}
		}
	}

	var valid []preparedStatusRequest
	var invalid []preparedStatusRequest
	for _, req := range batch {
		if invalidKeysMap[req.key] {
			invalid = append(invalid, req)
		} else {
			valid = append(valid, req)
		}
	}

	return valid, invalid
}
