package jira

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func detectAuthReason(statusCode int, location, finalURL, contentType string, body []byte) (StatusReason, bool) {
	switch statusCode {
	case http.StatusUnauthorized:
		return StatusReasonAuthRequired, true
	case http.StatusForbidden:
		return StatusReasonForbidden, true
	}

	if isLikelyLoginRedirect(statusCode, location) {
		return StatusReasonLoginRequired, true
	}

	if isLikelyLoginURL(finalURL) {
		return StatusReasonLoginRequired, true
	}

	if isUnexpectedHTMLResponse(contentType, body) {
		return StatusReasonLoginRequired, true
	}

	return StatusReasonNone, false
}

func isLikelyLoginRedirect(statusCode int, location string) bool {
	if statusCode < 300 || statusCode >= 400 {
		return false
	}
	return isLikelyLoginURL(location)
}

func isLikelyLoginURL(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return false
	}

	markers := []string{"/login", "/signin", "/auth", "sso", "oauth", "atlassian"}
	for _, marker := range markers {
		if strings.Contains(raw, marker) {
			return true
		}
	}

	return false
}

func isUnexpectedHTMLResponse(contentType string, body []byte) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(contentType, "text/html") {
		return true
	}

	snippet := strings.ToLower(strings.TrimSpace(string(body)))
	if strings.HasPrefix(snippet, "<!doctype html") || strings.HasPrefix(snippet, "<html") {
		return true
	}

	return false
}

func parseSearchStatuses(body []byte) (map[string]string, int, int, error) {
	var payload struct {
		Total  int `json:"total"`
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Status struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, 0, fmt.Errorf("декодировать ответ jira issue: %w", err)
	}

	statusByKey := make(map[string]string, len(payload.Issues))
	for _, issue := range payload.Issues {
		key := strings.ToUpper(strings.TrimSpace(issue.Key))
		if key == "" {
			continue
		}
		name := strings.TrimSpace(issue.Fields.Status.Name)
		if name == "" {
			continue
		}
		statusByKey[key] = name
	}

	return statusByKey, payload.Total, len(payload.Issues), nil
}
