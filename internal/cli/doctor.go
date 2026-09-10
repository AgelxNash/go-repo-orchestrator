package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/agelxnash/go-repo-orchestrator/internal/app"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
)

const diagnoseTimeout = 30 * time.Second

const diagnoseBodySnippetBytes = 512

// writef печатает в диагностический вывод; ошибка записи не критична для
// doctor-команды (буфер/stdio).
func writef(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}

type doctorGroupTarget struct {
	Group   string
	Repo    string
	Pattern string
	Mapped  bool
}

// newDoctorCommand собирает группу диагностических команд (без запуска TUI).
func newDoctorCommand(v *viper.Viper, logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Диагностика интеграций без запуска TUI",
	}
	cmd.AddCommand(newDoctorJiraCommand(v, logger))
	return cmd
}

func newDoctorJiraCommand(v *viper.Viper, logger *zap.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "jira <ISSUE-KEY>",
		Short: "Выполнить один запрос статуса Jira-задачи с полным трейсом",
		Long: "Диагностика Jira-интеграции по ключу задачи: выполняет ровно один запрос статуса тем же транспортным кодом,\n" +
			"что и TUI, и печатает полный трейс — группу, транспорт, авторизацию (только факт, без секретов), итоговый URL,\n" +
			"HTTP-статус, content-type, final URL, фрагмент тела и вердикт классификатора auth\n" +
			"(401/403/login-redirect/HTML), включая факт browser→HTTP fallback.\n\n" +
			"Группа Jira определяется по named-group mapping из repos[].branch.jira; если ключ не замаплен ни одним\n" +
			"regex-правилом, проверяются все настроенные группы.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("укажите ровно один ключ задачи, например: doctor jira PROJ-123")
			}
			if strings.TrimSpace(args[0]) == "" {
				return errors.New("ключ задачи не может быть пустым")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToUpper(strings.TrimSpace(args[0]))
			out := cmd.OutOrStdout()

			cfg, err := config.LoadFromViper(v)
			if err != nil {
				return err
			}

			targets := resolveDoctorTargets(cfg, key)
			if len(targets) == 0 {
				return errors.New("в конфиге не настроено ни одной jira-группы (секция jira)")
			}

			runtime, err := newRuntime(v, cfg, logger)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := runtime.Close(); closeErr != nil {
					logger.Warn("playwright shutdown error", zap.Error(closeErr))
				}
			}()

			var browserStartErr error
			browserSection := ""
			if cfg.PlaywrightEnabled() {
				browserStartErr = runtime.StartPlaywright()
				browserSection = renderBrowserSection(runtime, cfg, targets, browserStartErr)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), diagnoseTimeout)
			defer cancel()

			writef(out, "Jira-диагностика ключа %s\n", key)
			for _, target := range targets {
				result := runtime.Jira.DiagnoseIssue(ctx, target.Group, key)
				renderDiagnoseResult(out, target, result, browserSection)
			}

			if browserStartErr != nil {
				return fmt.Errorf("playwright runtime недоступен: %w", browserStartErr)
			}
			return nil
		},
	}
}

// resolveDoctorTargets определяет jira-группы для ключа через существующий
// named-group mapping (repos[].branch.jira). Если ключ не замаплен ни одним
// regex-правилом — возвращаются все группы конфига с пометкой.
func resolveDoctorTargets(cfg *config.Config, key string) []doctorGroupTarget {
	byGroup := make(map[string]doctorGroupTarget)
	var order []string

	for _, repo := range cfg.Repos {
		_, ok, diag := repo.ExtractJiraMatchDetailed(key)
		if !ok || diag.Group == "" {
			continue
		}
		if !hasJiraGroup(cfg, diag.Group) {
			continue
		}
		if _, seen := byGroup[diag.Group]; !seen {
			byGroup[diag.Group] = doctorGroupTarget{
				Group:   diag.Group,
				Repo:    repo.Name,
				Pattern: diag.Pattern,
				Mapped:  diag.Reason == config.JiraMatchReasonMappedNamedGroup,
			}
			order = append(order, diag.Group)
		}
	}

	if len(order) > 0 {
		targets := make([]doctorGroupTarget, 0, len(order))
		for _, group := range order {
			targets = append(targets, byGroup[group])
		}
		return targets
	}

	targets := make([]doctorGroupTarget, 0, len(cfg.Jira))
	for _, groupCfg := range cfg.Jira {
		targets = append(targets, doctorGroupTarget{Group: groupCfg.Group})
	}
	return targets
}

func hasJiraGroup(cfg *config.Config, group string) bool {
	for _, groupCfg := range cfg.Jira {
		if groupCfg.Group == group {
			return true
		}
	}
	return false
}

func jiraGroupBaseURL(cfg *config.Config, group string) string {
	for _, groupCfg := range cfg.Jira {
		if groupCfg.Group == group {
			return groupCfg.URL
		}
	}
	return ""
}

func renderBrowserSection(runtime *app.Runtime, cfg *config.Config, targets []doctorGroupTarget, startErr error) string {
	var b strings.Builder
	b.WriteString("\n— Browser-транспорт —\n")
	if startErr != nil {
		writef(&b, "Запуск/CDP preflight: недоступен: %s\n", startErr)
		return b.String()
	}

	writef(&b, "Запуск/CDP preflight: ok (режим: %s)\n", runtime.Playwright.Mode())

	probeURL := ""
	if len(targets) > 0 {
		probeURL = jiraGroupBaseURL(cfg, targets[0].Group)
	}
	if probeURL == "" {
		return b.String()
	}

	snapshot, err := runtime.Playwright.SnapshotContexts(probeURL)
	if err != nil {
		writef(&b, "Контексты: не удалось получить (%s)\n", err)
		return b.String()
	}

	selection := fmt.Sprintf("выбран контекст #%d", snapshot.SelectedIndex)
	if snapshot.CreatedContext {
		selection = "будет создан новый чистый контекст (без сессии!)"
	}
	writef(&b, "Контексты: всего %d, с куками для домена: %d, %s\n", snapshot.Total, snapshot.WithCookies, selection)
	return b.String()
}

func renderDiagnoseResult(out io.Writer, target doctorGroupTarget, result jira.DiagnoseResult, browserSection string) {
	writef(out, "\n=== Группа: %s", result.Group)
	switch {
	case target.Mapped:
		writef(out, " (named-group mapping: репозиторий %q, pattern %s)", target.Repo, target.Pattern)
	case target.Repo != "":
		writef(out, " (ключ извлечен правилом %q без прямого mapping)", target.Pattern)
	default:
		writef(out, " (ключ не замаплен ни одним branch.jira regex — проверяю все группы)")
	}
	writef(out, "\n")

	writef(out, "Base URL: %s\n", result.BaseURL)
	writef(out, "Транспорт: %s | Авторизация: %s\n", result.Transport, result.AuthKind)
	writef(out, "Запрос: %s\n", result.SearchURL)

	if browserSection != "" && result.Transport == "browser" {
		writef(out, "%s", browserSection)
	}

	writef(out, "Вердикт: %s\n", result.VerdictText())

	if result.BrowserError != "" {
		writef(out, "Fallback: %s\n", result.BrowserError)
	}

	if result.RequestErr != "" {
		writef(out, "Ошибка запроса: %s\n", result.RequestErr)
		return
	}

	writef(out, "Ответ: HTTP %d", result.StatusCode)
	if result.ContentType != "" {
		writef(out, " | Content-Type: %s", result.ContentType)
	}
	if result.Location != "" {
		writef(out, " | Location: %s", result.Location)
	}
	if result.FinalURL != "" && result.FinalURL != result.SearchURL {
		writef(out, "\nFinal URL: %s", result.FinalURL)
	}
	writef(out, "\nРазмер тела: %d байт\n", result.BodySize)

	if result.BodySnippet != "" {
		writef(out, "Фрагмент тела (санитизировано, до %d байт):\n%s\n", diagnoseBodySnippetBytes, result.BodySnippet)
	}
}
