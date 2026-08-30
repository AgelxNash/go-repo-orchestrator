package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/agelxnash/go-repo-orchestrator/internal/workdir"
	"github.com/spf13/viper"
)

// Config — корневой объект YAML-конфигурации приложения.
type Config struct {
	Jira    []JiraConfig  `yaml:"jira" mapstructure:"jira"`
	Browser BrowserConfig `yaml:"browser" mapstructure:"browser"`
	Repos   []RepoConfig  `yaml:"repos" mapstructure:"repos"`

	jiraByGroup map[string]JiraConfig
}

// BrowserConfig содержит настройки подключения к браузеру (например, через CDP).
type BrowserConfig struct {
	CDPURL string `yaml:"cdp_url" mapstructure:"cdp_url"`
}

// RepoConfig описывает правила обработки веток для одного репозитория.
type RepoConfig struct {
	Name   string `yaml:"name" mapstructure:"name"`
	URL    string `yaml:"url" mapstructure:"url"`
	Path   string `yaml:"path" mapstructure:"path"`
	Branch Branch `yaml:"branch" mapstructure:"branch"`

	keep      []*compiledPattern
	jira      []*compiledPattern
	autocheck []*compiledPattern

	jiraGroups map[string]JiraConfig
}

// Branch описывает веточные настройки репозитория в новой схеме.
type Branch struct {
	Autoswitch string   `yaml:"autoswitch,omitempty" mapstructure:"autoswitch"`
	Keep       []string `yaml:"keep" mapstructure:"keep"`
	Jira       []string `yaml:"jira" mapstructure:"jira"`
	Autocheck  []string `yaml:"autocheck" mapstructure:"autocheck"`
}

// JiraConfig парсит верхнеуровневый раздел jira интеграций.
// На MVP этапе структура загружается, но не используется runtime-логикой.
type JiraConfig struct {
	Group      string    `yaml:"group" mapstructure:"group"`
	URL        string    `yaml:"url" mapstructure:"url"`
	Playwright bool      `yaml:"playwright" mapstructure:"playwright"`
	Type       string    `yaml:"type" mapstructure:"type"`
	Login      JiraLogin `yaml:"login" mapstructure:"login"`
	Token      string    `yaml:"token" mapstructure:"token"`
	SSL        JiraSSL   `yaml:"ssl" mapstructure:"ssl"`
}

// JiraLogin хранит учетные данные для базовой аутентификации в Jira.
type JiraLogin struct {
	Username string `yaml:"username" mapstructure:"username"`
	Password string `yaml:"password" mapstructure:"password"`
}

// JiraSSL настраивает TLS-подключение к Jira-группе: клиентские сертификаты
// (mTLS), доверенный CA и режим верификации.
type JiraSSL struct {
	ClientCert        string `yaml:"client_cert" mapstructure:"client_cert"`
	ClientKey         string `yaml:"client_key" mapstructure:"client_key"`
	ClientKeyPassword string `yaml:"client_key_password" mapstructure:"client_key_password"`
	CACert            string `yaml:"ca_cert" mapstructure:"ca_cert"`
	// Verify: nil или true — верифицировать серверный сертификат (по умолчанию);
	// false — отключить проверку цепочки (только для тестовых сред).
	Verify *bool `yaml:"verify" mapstructure:"verify"`
}

// IsZero reports whether no TLS option is set.
func (s JiraSSL) IsZero() bool {
	return s.ClientCert == "" &&
		s.ClientKey == "" &&
		s.ClientKeyPassword == "" &&
		s.CACert == "" &&
		s.Verify == nil
}

// validateJiraSSL проверяет сочетания полей ssl-блока и существование файлов.
func validateJiraSSL(index int, ssl JiraSSL) error {
	if ssl.IsZero() {
		return nil
	}

	switch {
	case ssl.ClientKey != "" && ssl.ClientCert == "":
		return fmt.Errorf("jira[%d].ssl.client_key: задаётся только вместе с client_cert", index)
	case ssl.ClientKeyPassword != "" && ssl.ClientCert == "":
		return fmt.Errorf("jira[%d].ssl.client_key_password: задаётся только вместе с client_cert", index)
	}

	if ssl.Verify != nil && !*ssl.Verify && ssl.CACert != "" {
		return fmt.Errorf("jira[%d].ssl: verify: false и ca_cert взаимоисключаются: укажите ca_cert для корпоративного CA либо отключите verify только в тестовых средах", index)
	}

	for field, path := range map[string]string{
		"client_cert": ssl.ClientCert,
		"client_key":  ssl.ClientKey,
		"ca_cert":     ssl.CACert,
	} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("jira[%d].ssl.%s: недоступен файл %q: %w", index, field, path, err)
		}
	}

	return nil
}

// JiraMatch содержит результат успешного сопоставления ветки с тикетом в Jira.
type JiraMatch struct {
	Key       string
	Group     string
	URL       string
	TicketURL string
}

type JiraMatchReason string

const (
	JiraMatchReasonNoRegexMatch      JiraMatchReason = "no_regex_match"
	JiraMatchReasonMappedNamedGroup  JiraMatchReason = "mapped_named_group"
	JiraMatchReasonNamedGroupNoGroup JiraMatchReason = "named_group_no_group_config"
	JiraMatchReasonFallbackJIRA      JiraMatchReason = "fallback_jira"
	JiraMatchReasonFallbackFullMatch JiraMatchReason = "fallback_full_match"
)

// JiraMatchDiagnostics описывает детали процесса сопоставления для отладки.
type JiraMatchDiagnostics struct {
	Reason  JiraMatchReason
	Pattern string
	Group   string
	Key     string
}

// compiledPattern хранит скомпилированное регулярное выражение и его исходную строку.
type compiledPattern struct {
	raw string
	re  *regexp.Regexp
}

// Load загружает YAML-конфиг, валидирует обязательные поля и компилирует regex-правила.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("прочитать конфиг: %w", err)
	}

	return LoadFromViper(v)
}

// LoadFromViper загружает типизированный конфиг из экземпляра viper.
func LoadFromViper(v *viper.Viper) (*Config, error) {
	if v == nil {
		return nil, errors.New("требуется экземпляр viper")
	}
	if err := validateConfigFileKeys(v); err != nil {
		return nil, err
	}

	var raw struct {
		Jira       []JiraConfig  `mapstructure:"jira"`
		Browser    BrowserConfig `mapstructure:"browser"`
		Repos      []RepoConfig  `mapstructure:"repos"`
		ConfigPath string        `mapstructure:"config"`
		StateDir   string        `mapstructure:"state_dir"`
	}
	if err := v.UnmarshalExact(&raw); err != nil {
		return nil, fmt.Errorf("ошибка структуры YAML: проверьте отступы и правильность ключей (%w)", err)
	}

	cfg := Config{
		Jira:    raw.Jira,
		Browser: raw.Browser,
		Repos:   raw.Repos,
	}

	if len(cfg.Repos) == 0 {
		return nil, fmt.Errorf("ошибка конфигурации: файл пуст или не содержит массива repos")
	}

	cfg.jiraByGroup = make(map[string]JiraConfig, len(cfg.Jira))
	for i := range cfg.Jira {
		jira := &cfg.Jira[i]
		jira.Group = strings.TrimSpace(jira.Group)
		jira.URL = normalizeJiraBaseURL(jira.URL)
		if err := validateJiraURL(jira.URL); err != nil {
			return nil, fmt.Errorf("jira[%d].url: %w", i, err)
		}
		if err := validateJiraSSL(i, jira.SSL); err != nil {
			return nil, err
		}
		if jira.Group == "" {
			continue
		}
		cfg.jiraByGroup[jira.Group] = *jira
	}

	cfg.Browser.CDPURL = strings.TrimSpace(cfg.Browser.CDPURL)
	if err := validateBrowserCDPURL(cfg.Browser.CDPURL); err != nil {
		return nil, fmt.Errorf("browser.cdp_url: %w", err)
	}

	for i := range cfg.Repos {
		repo := &cfg.Repos[i]
		repo.Branch.Autoswitch = strings.TrimSpace(repo.Branch.Autoswitch)
		repo.Name = strings.TrimSpace(repo.Name)
		if repo.Name == "" {
			return nil, fmt.Errorf("ошибка конфигурации: у репозитория под индексом [%d] отсутствует обязательное поле name", i)
		}

		repo.URL = strings.TrimSpace(repo.URL)
		repo.Path = strings.TrimSpace(repo.Path)

		hasURL := repo.URL != ""
		hasPath := repo.Path != ""
		switch {
		case !hasURL && !hasPath:
			return nil, fmt.Errorf("repo[%s]: требуется указать url или path", repo.Name)
		case hasURL && hasPath:
			// opensource repo: validate url and resolve path
			if err := validateRepoURL(repo.URL); err != nil {
				return nil, fmt.Errorf("repo[%s]: некорректный url: %w", repo.Name, err)
			}
			absPath, err := filepath.Abs(repo.Path)
			if err != nil {
				return nil, fmt.Errorf("repo[%s]: определить path: %w", repo.Name, err)
			}
			repo.Path = absPath
		case hasURL:
			if err := validateRepoURL(repo.URL); err != nil {
				return nil, fmt.Errorf("repo[%s]: некорректный url: %w", repo.Name, err)
			}
		case hasPath:
			absPath, err := filepath.Abs(repo.Path)
			if err != nil {
				return nil, fmt.Errorf("repo[%s]: определить path: %w", repo.Name, err)
			}
			repo.Path = absPath
		}

		var err error
		repo.keep, err = compilePatterns(repo.Branch.Keep)
		if err != nil {
			return nil, fmt.Errorf("repo[%s]: branch.keep: %w", repo.Name, err)
		}

		repo.jira, err = compilePatterns(repo.Branch.Jira)
		if err != nil {
			return nil, fmt.Errorf("repo[%s]: branch.jira: %w", repo.Name, err)
		}

		repo.autocheck, err = compilePatterns(repo.Branch.Autocheck)
		if err != nil {
			return nil, fmt.Errorf("repo[%s]: branch.autocheck: %w", repo.Name, err)
		}

		repo.jiraGroups = cfg.jiraByGroup
	}

	if err := validateRepoIdentityConflicts(cfg.Repos); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateConfigFileKeys(v *viper.Viper) error {
	allowed := map[string]bool{
		"browser":   true,
		"jira":      true,
		"repos":     true,
		"state_dir": true,
	}

	var unknown []string
	for _, key := range v.AllKeys() {
		root, _, _ := strings.Cut(key, ".")
		if allowed[root] {
			continue
		}
		if !v.InConfig(key) {
			continue
		}
		unknown = append(unknown, key)
	}
	if len(unknown) == 0 {
		return nil
	}

	slices.Sort(unknown)
	return fmt.Errorf("ошибка структуры YAML: неизвестные ключи конфигурации: %s", strings.Join(unknown, ", "))
}

type repoIdentityRef struct {
	index int
	name  string
}

// validateRepoIdentityConflicts проверяет список репозиториев на наличие дубликатов имен или путей.
func validateRepoIdentityConflicts(repos []RepoConfig) error {
	nameOwners := make(map[string]repoIdentityRef, len(repos))
	workdirOwners := make(map[string]repoIdentityRef, len(repos))
	conflicts := make([]string, 0)

	for idx, repo := range repos {
		current := repoIdentityRef{index: idx, name: repo.Name}

		if owner, exists := nameOwners[repo.Name]; exists {
			conflicts = append(conflicts, fmt.Sprintf(
				"- конфликт имени: repos[%d] %q и repos[%d] %q имеют одинаковое поле name=%q; имя должно быть уникальным, иначе TUI смешивает состояние репозиториев",
				owner.index, owner.name, current.index, current.name, repo.Name,
			))
		} else {
			nameOwners[repo.Name] = current
		}

		workdirKey := repoWorkdirKey(repo)
		if owner, exists := workdirOwners[workdirKey]; exists {
			conflicts = append(conflicts, fmt.Sprintf(
				"- конфликт рабочего ключа: repos[%d] %q и repos[%d] %q используют один и тот же ключ %q; исправьте name/url/path, чтобы каждый репозиторий имел уникальный рабочий каталог",
				owner.index, owner.name, current.index, current.name, workdirKey,
			))
		} else {
			workdirOwners[workdirKey] = current
		}
	}

	if len(conflicts) == 0 {
		return nil
	}

	return fmt.Errorf("ошибка конфигурации: обнаружены конфликтующие репозитории. Приложение остановлено до исправления config.yaml:\n%s", strings.Join(conflicts, "\n"))
}

// repoWorkdirKey генерирует уникальный ключ для рабочего каталога репозитория.
func repoWorkdirKey(repo RepoConfig) string {
	if repo.Path != "" {
		return "path:" + repo.Path
	}

	return "managed:" + managedRepoDirKey(repo.Name, repo.URL)
}

// managedRepoDirKey возвращает путь к каталогу для автоматически управляемого репозитория.
func managedRepoDirKey(repoName, repoURL string) string {
	return workdir.ManagedRepoDirKey(repoName, repoURL)
}

// SourceType возвращает тип источника репозитория: "opensource", "url" или "path".
func (r RepoConfig) SourceType() string {
	hasURL := strings.TrimSpace(r.URL) != ""
	hasPath := strings.TrimSpace(r.Path) != ""
	if hasURL && hasPath {
		return "opensource"
	}
	if hasPath {
		return "path"
	}

	return "url"
}

// compilePatterns компилирует список строк в регулярные выражения.
func compilePatterns(patterns []string) ([]*compiledPattern, error) {
	result := make([]*compiledPattern, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("скомпилировать pattern %q: %w", pattern, err)
		}
		result = append(result, &compiledPattern{raw: pattern, re: re})
	}
	return result, nil
}

var scpLikeGitURLRE = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~/-]+(?:\.git)?$`)

// validateRepoURL проверяет корректность Git URL (HTTP/SSH/SCP).
func validateRepoURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("url пустой")
	}
	if strings.ContainsAny(raw, " \t\n\r") {
		return errors.New("url содержит пробельные символы")
	}

	if scpLikeGitURLRE.MatchString(raw) {
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("разобрать url: %w", err)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "ssh" {
		return fmt.Errorf("неподдерживаемая scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("требуется host")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return errors.New("требуется путь репозитория")
	}

	return nil
}

// validateBrowserCDPURL проверяет валидность URL для CDP подключения.
func validateBrowserCDPURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("разобрать cdp url: %w", err)
	}

	switch parsed.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return fmt.Errorf("неподдерживаемая scheme %q", parsed.Scheme)
	}

	if parsed.Host == "" {
		return errors.New("требуется host")
	}

	return nil
}

// validateJiraURL проверяет корректность базового URL Jira (только HTTPS).
func validateJiraURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("разобрать jira url: %w", err)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf("jira url должен использовать https, получено %q", parsed.Scheme)
	}

	if parsed.Host == "" {
		return errors.New("требуется host")
	}

	return nil
}

// RepoByName ищет репозиторий по имени из конфига.
func (c *Config) RepoByName(name string) (RepoConfig, bool) {
	idx := slices.IndexFunc(c.Repos, func(r RepoConfig) bool {
		return r.Name == name
	})
	if idx < 0 {
		return RepoConfig{}, false
	}

	return c.Repos[idx], true
}

// PlaywrightEnabled возвращает true, если хотя бы одна jira-группа требует playwright runtime.
func (c *Config) PlaywrightEnabled() bool {
	if c == nil {
		return false
	}

	return slices.ContainsFunc(c.Jira, func(jira JiraConfig) bool {
		return jira.Playwright
	})
}

// IsProtected проверяет, попадает ли ветка под branch.keep.
func (r RepoConfig) IsProtected(branch string) bool {
	for _, p := range r.keep {
		if p.re.MatchString(branch) {
			return true
		}
	}
	return false
}

// ProtectedReason возвращает причину защиты ветки по branch.keep.
func (r RepoConfig) ProtectedReason(branch string) (string, bool) {
	for _, p := range r.keep {
		if p.re.MatchString(branch) {
			return "совпадение с keep pattern: " + p.raw, true
		}
	}

	return "", false
}

// MatchesAutocheck проверяет, совпадает ли имя ветки с любым из branch.autocheck правил.
// Возвращает false при пустом autocheck (no-op).
func (r RepoConfig) MatchesAutocheck(branch string) bool {
	for _, p := range r.autocheck {
		if p.re.MatchString(branch) {
			return true
		}
	}
	return false
}

// ExtractJiraKey пытается извлечь Jira key из ветки по repo.jira regex.
// Приоритет: named-group JIRA, иначе полный match.
func (r RepoConfig) ExtractJiraKey(branch string) (string, bool) {
	match, ok := r.ExtractJiraMatch(branch)
	if !ok {
		return "", false
	}

	return match.Key, true
}

// ExtractJiraMatch пытается извлечь Jira key и сопоставить его с jira.group.
// Линк-мэппинг срабатывает только когда имя named-group совпадает с jira.group.
func (r RepoConfig) ExtractJiraMatch(branch string) (JiraMatch, bool) {
	match, ok, _ := r.ExtractJiraMatchDetailed(branch)
	return match, ok
}

// ExtractJiraMatchDetailed пытается извлечь Jira key и вернуть причину mapping/fallback.
func (r RepoConfig) ExtractJiraMatchDetailed(branch string) (JiraMatch, bool, JiraMatchDiagnostics) {
	for _, p := range r.jira {
		matches := p.re.FindStringSubmatch(branch)
		if matches == nil {
			continue
		}

		names := p.re.SubexpNames()
		missingGroupName := ""
		missingGroupKey := ""
		for idx := 1; idx < len(names) && idx < len(matches); idx++ {
			groupName := strings.TrimSpace(names[idx])
			if groupName == "" || matches[idx] == "" {
				continue
			}

			groupCfg, ok := r.jiraGroups[groupName]
			if ok {
				key := matches[idx]
				return JiraMatch{
						Key:       key,
						Group:     groupCfg.Group,
						URL:       groupCfg.URL,
						TicketURL: jiraTicketURL(groupCfg.URL, key),
					}, true, JiraMatchDiagnostics{
						Reason:  JiraMatchReasonMappedNamedGroup,
						Pattern: p.raw,
						Group:   groupCfg.Group,
						Key:     key,
					}
			}

			if groupName != "JIRA" && missingGroupName == "" {
				missingGroupName = groupName
				missingGroupKey = matches[idx]
			}
		}

		if missingGroupName != "" {
			if missingGroupKey == "" {
				missingGroupKey = matches[0]
			}

			return JiraMatch{Key: missingGroupKey}, true, JiraMatchDiagnostics{
				Reason:  JiraMatchReasonNamedGroupNoGroup,
				Pattern: p.raw,
				Group:   missingGroupName,
				Key:     missingGroupKey,
			}
		}

		jiraGroupIdx := p.re.SubexpIndex("JIRA")
		if jiraGroupIdx > 0 && jiraGroupIdx < len(matches) && matches[jiraGroupIdx] != "" {
			return JiraMatch{Key: matches[jiraGroupIdx]}, true, JiraMatchDiagnostics{
				Reason:  JiraMatchReasonFallbackJIRA,
				Pattern: p.raw,
				Key:     matches[jiraGroupIdx],
			}
		}

		if len(matches) > 0 && matches[0] != "" {
			return JiraMatch{Key: matches[0]}, true, JiraMatchDiagnostics{
				Reason:  JiraMatchReasonFallbackFullMatch,
				Pattern: p.raw,
				Key:     matches[0],
			}
		}
	}

	return JiraMatch{}, false, JiraMatchDiagnostics{Reason: JiraMatchReasonNoRegexMatch}
}

// normalizeJiraBaseURL очищает и нормализует базовый URL Jira.
func normalizeJiraBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// jiraTicketURL формирует полную ссылку на задачу в Jira.
func jiraTicketURL(baseURL, ticket string) string {
	baseURL = normalizeJiraBaseURL(baseURL)
	ticket = strings.TrimSpace(ticket)
	if baseURL == "" || ticket == "" {
		return ""
	}

	return baseURL + "/browse/" + url.PathEscape(ticket)
}

// ScanDirectory рекурсивно обходит переданную директорию в поисках папок .git
// и формирует конфигурацию 'на лету', эмулируя список репозиториев.
func ScanDirectory(ctx context.Context, dir string) (*Config, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var repos []RepoConfig

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errCtx := ctx.Err(); errCtx != nil {
			return errCtx
		}

		if err != nil {
			return nil // пропускаем ошибки доступа
		}

		if d.IsDir() {
			// Пропускаем "тяжелые" директории зависимостей для ускорения сканирования
			name := d.Name()
			if name == "node_modules" || name == "vendor" || name == ".venv" || name == "dist" {
				return filepath.SkipDir
			}

			// Если нашли .git, регистрируем родительскую папку как репозиторий
			if name == ".git" {
				repoDir := filepath.Dir(path)

				rel, errRel := filepath.Rel(dir, repoDir)
				repoName := rel
				if errRel != nil || rel == "." {
					repoName = filepath.Base(repoDir)
				}

				// Экранируем слеши для TUI, чтобы они отображались как единое имя
				repoName = strings.ReplaceAll(repoName, string(filepath.Separator), "/")

				// Базовая конфигурация для найденного репозитория
				repo := RepoConfig{
					Name: repoName,
					Path: repoDir,
				}
				if originURL, ok := readOriginURL(ctx, repoDir); ok {
					repo.URL = originURL
				}
				repos = append(repos, repo)

				return filepath.SkipDir // Внутрь .git идти не нужно
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("обойти директорию: %w", err)
	}

	if len(repos) == 0 {
		return nil, errors.New("в указанной директории не найдены git-репозитории")
	}

	return &Config{Repos: repos}, nil
}

// readOriginURL пытается получить URL удаленного репозитория 'origin' для заданной директории.
func readOriginURL(ctx context.Context, repoDir string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "remote", "get-url", "origin")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}

	originURL := strings.TrimSpace(stdout.String())
	if originURL == "" {
		return "", false
	}

	return originURL, true
}
