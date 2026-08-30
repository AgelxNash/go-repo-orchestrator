package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/agelxnash/go-repo-orchestrator/internal/app"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/tui"
)

type runOptions struct {
	ConfigPath string
	StateDir   string
}

// NewRootCommand создает root-команду CLI приложения.
func NewRootCommand(version, commit, date string, logger *zap.Logger) *cobra.Command {
	if logger == nil {
		logger = zap.NewNop()
	}

	v := viper.New()
	opts := runOptions{
		ConfigPath: "",
		StateDir:   app.DefaultStateDir(),
	}

	cmd := &cobra.Command{
		Use:           "go-repo-orchestrator",
		Short:         "Безопасная генерация локальных скриптов удаления git-веток",
		Version:       fmt.Sprintf("%s (commit: %s, date: %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("config") {
				return errors.New("параметр --config обязателен: укажите путь к YAML-конфигу")
			}

			if err := validateConfigPath(v.GetString("config")); err != nil {
				return err
			}

			if cmd.Name() == "generate" {
				return nil
			}

			return initConfig(v)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("неожиданные аргументы: %s", strings.Join(args, " "))
			}

			cfg, err := config.LoadFromViper(v)
			if err != nil {
				return err
			}

			runtime, err := newRuntime(v, cfg, logger)
			if err != nil {
				return err
			}
			defer func() {
				if err := runtime.Close(); err != nil {
					logger.Warn("playwright shutdown error", zap.Error(err))
				}
			}()

			model := tui.NewModel(cfg, runtime.Cleaner, false)
			if cfg.PlaywrightEnabled() {
				model.SetPlaywrightStartupStartFn(func() error {
					err := runtime.StartPlaywright()
					if err != nil {
						logger.Warn("playwright runtime unavailable", zap.Error(err))
						return err
					}
					logger.Info("playwright browser started", zap.String("mode", runtime.Playwright.Mode()))
					return nil
				})
			}

			p := tea.NewProgram(model, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return err
			}

			logger.Info("tui session finished")
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "путь к YAML-конфигу (обязательно)")
	cmd.PersistentFlags().StringVar(&opts.StateDir, "state-dir", opts.StateDir, "каталог состояния приложения и управляемого рабочего каталога")

	mustBind(cmd, v, "config", "config")
	mustBind(cmd, v, "state_dir", "state-dir")

	v.SetDefault("state_dir", opts.StateDir)
	v.SetEnvPrefix(app.DefaultEnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	cmd.AddCommand(newGenerateCommand(v, logger))

	return cmd
}

func initConfig(v *viper.Viper) error {
	if v == nil {
		return errors.New("требуется экземпляр viper")
	}

	configPath := strings.TrimSpace(v.GetString("config"))
	if err := validateConfigPath(configPath); err != nil {
		return err
	}

	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		var notFoundErr viper.ConfigFileNotFoundError
		if errors.As(err, &notFoundErr) || strings.Contains(err.Error(), "Not Found") {
			return fmt.Errorf("файл конфигурации не найден: %s", configPath)
		}
		return fmt.Errorf("прочитать конфиг: %w", err)
	}

	return nil
}

func validateConfigPath(configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return errors.New("параметр --config обязателен: укажите путь к YAML-конфигу")
	}

	ext := strings.ToLower(filepath.Ext(configPath))
	if ext != ".yml" && ext != ".yaml" {
		return errors.New("параметр --config должен указывать на YAML-файл (.yaml или .yml)")
	}

	return nil
}

func mustBind(cmd *cobra.Command, v *viper.Viper, key, flag string) {
	if err := v.BindPFlag(key, cmd.PersistentFlags().Lookup(flag)); err != nil {
		panic(err)
	}
}

func newRuntime(v *viper.Viper, cfg *config.Config, logger *zap.Logger) (*app.Runtime, error) {
	opts := app.DefaultRuntimeOptions(v.GetString("state_dir"))
	if cfg != nil {
		opts.BrowserCDPURL = cfg.Browser.CDPURL
		opts.JiraGroups = cfg.Jira
	}
	opts.Logger = logger

	return app.NewRuntimeFromOptions(opts)
}
