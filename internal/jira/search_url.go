package jira

import (
	"fmt"
	"net/url"
	"strings"
)

func buildSearchStatusURL(jiraBaseURL string, keys []string, startAt int) (string, error) {
	base := normalizeBaseURL(jiraBaseURL)
	if base == "" || len(keys) == 0 {
		return "", fmt.Errorf("url для jira-статуса неполный")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("разобрать базовый url jira: %w", err)
	}

	jqlParts := make([]string, 0, len(keys))
	for _, key := range keys {
		normalized := strings.TrimSpace(key)
		if normalized == "" || normalized == "-" {
			continue
		}
		jqlParts = append(jqlParts, "\""+strings.ReplaceAll(normalized, "\"", "\\\"")+"\"")
	}
	if len(jqlParts) == 0 {
		return "", fmt.Errorf("url для jira-статуса неполный")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/rest/api/2/search"
	query := parsed.Query()
	query.Set("jql", "key in ("+strings.Join(jqlParts, ", ")+")")
	query.Set("fields", "status")
	query.Set("maxResults", fmt.Sprintf("%d", jiraSearchBatchSize))
	if startAt > 0 {
		query.Set("startAt", fmt.Sprintf("%d", startAt))
	}
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func jiraBaseFromTicketURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	path := parsed.Path
	idx := strings.Index(path, "/browse/")
	if idx >= 0 {
		path = path[:idx]
	}

	path = strings.TrimRight(path, "/")
	if path == "" {
		return parsed.Scheme + "://" + parsed.Host
	}

	return parsed.Scheme + "://" + parsed.Host + path
}
