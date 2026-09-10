package jira

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// maxDiagnoseBodySnippet — размер фрагмента тела ответа в диагностике
// (санитизированный, без бинарного мусора).
const maxDiagnoseBodySnippet = 512

// DiagnoseResult — полный трейс одного диагностического запроса статуса задачи.
// Секреты (значения token/паролей/кук) в трейс не попадают: только факт типа
// авторизации и метаданные запроса/ответа.
type DiagnoseResult struct {
	Group     string
	BaseURL   string
	Transport string // browser | http
	AuthKind  string // token (Bearer) | login/password (Basic) | нет

	SearchURL string

	// Ответ (если запрос дошёл до сервера).
	StatusCode  int
	ContentType string
	Location    string
	FinalURL    string
	BodySize    int
	BodySnippet string

	// Вердикт классификатора auth (auth_required/forbidden/login_required)
	// либо итог для 2xx-ответа.
	State             StatusState
	Reason            StatusReason
	Status            string // статус задачи из ответа (когда удалось распарсить)
	IssueFound        bool
	AuthDiagnosed     bool // true, если сработал detectAuthReason
	FallbackUsed      bool // browser → HTTP fallback
	BrowserError      string
	RequestErr        string
	NoGroupConfig     bool
	InvalidRequest    bool
	InvalidRequestMsg string
}

// DiagnoseIssue выполняет ровно один запрос статуса задачи по ключу тем же
// транспортным кодом, которым ходит TUI (resolveSearchWithContext +
// detectAuthReason), и возвращает полный трейс. Кэш статусов не затрагивается.
func (s *StatusService) DiagnoseIssue(ctx context.Context, group, key string) DiagnoseResult {
	if s == nil {
		return DiagnoseResult{Group: group, State: StatusStateError, Reason: StatusReasonTransportError, RequestErr: "сервис не инициализирован"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	key = strings.ToUpper(strings.TrimSpace(key))
	result := DiagnoseResult{Group: group, State: StatusStateError, Reason: StatusReasonTransportError}

	groupCfg, ok := s.groups[strings.TrimSpace(group)]
	if !ok {
		result.NoGroupConfig = true
		result.State = StatusStateUnmapped
		result.Reason = StatusReasonNoGroupConfig
		return result
	}

	result.BaseURL = groupCfg.baseURL
	result.Transport = string(groupCfg.transport)
	result.AuthKind = authKindLabel(groupCfg.auth)

	searchURL, err := buildSearchStatusURL(groupCfg.baseURL, []string{key}, 0)
	if err != nil {
		result.InvalidRequest = true
		result.InvalidRequestMsg = err.Error()
		result.Reason = StatusReasonInvalidRequest
		return result
	}
	result.SearchURL = searchURL

	headers := buildRequestHeaders(groupCfg.auth)
	response, usedFallback, browserFallbackErr, requestErr := s.resolveSearchWithContext(ctx, group, groupCfg.transport, searchURL, headers)
	result.FallbackUsed = usedFallback
	if usedFallback {
		result.BrowserError = "browser-транспорт недоступен (" + browserFallbackErr + "), выполнен HTTP fallback"
	}

	if requestErr != nil {
		result.RequestErr = requestErr.Error()
		return result
	}

	result.StatusCode = response.statusCode
	result.ContentType = response.contentType
	result.Location = response.location
	result.FinalURL = response.finalURL
	result.BodySize = len(response.body)
	result.BodySnippet = sanitizeDiagnoseSnippet(response.body)

	if reason, auth := detectAuthReason(response.statusCode, response.location, response.finalURL, response.contentType, response.body); auth {
		result.AuthDiagnosed = true
		result.State = StatusStateAuth
		result.Reason = reason
		return result
	}

	if response.statusCode != 200 {
		result.State = StatusStateError
		result.Reason = StatusReasonHTTPError
		switch {
		case response.statusCode == 404:
			result.Reason = StatusReasonIssueNotFound
		case response.statusCode >= 400 && response.statusCode <= 499:
			result.Reason = StatusReasonClientError
		case usedFallback:
			result.Reason = StatusReasonBrowserUnavailableHTTPError
		}
		return result
	}

	statusByKey, _, _, parseErr := parseSearchStatuses(response.body)
	if parseErr != nil {
		result.State = StatusStateError
		result.Reason = StatusReasonResponseParseErr
		result.RequestErr = parseErr.Error()
		return result
	}

	if status := strings.TrimSpace(statusByKey[key]); status != "" {
		result.Status = status
		result.IssueFound = true
		result.State = StatusStateReady
		result.Reason = StatusReasonNone
		return result
	}

	result.State = StatusStateError
	result.Reason = StatusReasonIssueNotFound
	return result
}

func authKindLabel(auth groupAuth) string {
	if strings.TrimSpace(auth.token) != "" {
		return "token (Bearer)"
	}
	if strings.TrimSpace(auth.username) != "" && auth.password != "" {
		return "login/password (Basic)"
	}
	return "нет (ожидается SSO-сессия браузера/cookie)"
}

// sanitizeDiagnoseSnippet возвращает первые maxDiagnoseBodySnippet байт тела
// ответа, оставляя только печатаемые символы, табуляции и переводы строк.
func sanitizeDiagnoseSnippet(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	snippet := body
	if len(snippet) > maxDiagnoseBodySnippet {
		snippet = snippet[:maxDiagnoseBodySnippet]
	}

	var out strings.Builder
	for _, r := range strings.ToValidUTF8(string(snippet), "") {
		switch {
		case r == '\n' || r == '\t':
			out.WriteRune(r)
		case unicode.IsPrint(r):
			out.WriteRune(r)
		default:
			out.WriteByte('.')
		}
	}

	return out.String()
}

// DiagnoseIssueText возвращает человекочитаемое пояснение вердикта диагностики
// (переиспользует формулировки причин из TUI).
func (r DiagnoseResult) VerdictText() string {
	switch {
	case r.NoGroupConfig:
		return fmt.Sprintf("группа %q не настроена в секции jira конфига", r.Group)
	case r.InvalidRequest:
		return "некорректный запрос: " + r.InvalidRequestMsg
	case r.RequestErr != "" && r.BrowserError != "":
		return "запрос не выполнен: browser-транспорт недоступен, HTTP fallback завершился ошибкой"
	case r.RequestErr != "":
		return "ошибка транспорта: " + r.RequestErr
	case r.AuthDiagnosed:
		switch r.Reason {
		case StatusReasonAuthRequired:
			return "требуется авторизация (HTTP 401)"
		case StatusReasonForbidden:
			return "доступ запрещен (HTTP 403)"
		case StatusReasonLoginRequired:
			return "нужен вход (redirect на login / login-URL / HTML-ответ)"
		default:
			return "ошибка авторизации: " + string(r.Reason)
		}
	case r.IssueFound:
		return "задача найдена, статус: " + r.Status
	case r.Reason == StatusReasonIssueNotFound:
		return "тикет не найден в этой Jira-группе (в ответе нет ключа задачи)"
	case r.Reason == StatusReasonResponseParseErr:
		return "некорректный JSON-ответ Jira"
	default:
		return fmt.Sprintf("HTTP %d, состояние %s (%s)", r.StatusCode, r.State, r.Reason)
	}
}
