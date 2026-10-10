package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/agelxnash/go-repo-orchestrator/internal/app"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/git"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"
)

const statusWorkerConcurrency = 8

type statusRepoJob struct {
	idx  int
	repo config.RepoConfig
}

func newStatusCommand(v *viper.Viper, logger *zap.Logger) *cobra.Command {
	if v == nil {
		v = viper.New()
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	var (
		jsonOutput   bool
		filterRaw    string
		ignoreErrors bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Сводный отчет о состоянии отслеживаемых репозиториев",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("неожиданные аргументы: %s", strings.Join(args, " "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("неожиданные аргументы: %s", strings.Join(args, " "))
			}

			filter := strings.ToLower(strings.TrimSpace(filterRaw))
			switch filter {
			case "all", "dirty", "clean", "error":
			default:
				return fmt.Errorf("недопустимое значение фильтра: %q (допустимы: all, dirty, clean, error)", filterRaw)
			}

			cfg, err := config.LoadFromViper(v)
			if err != nil {
				return err
			}

			stateDir := v.GetString("state_dir")
			if stateDir == "" {
				stateDir = app.DefaultStateDir()
			}
			workspaceDir := app.DefaultWorkspaceDir(stateDir)
			gitClient := git.NewClient(app.DefaultGitTimeout, workspaceDir)
			cleaner := usecase.NewCleaner(gitClient, usecase.WithLogger(logger))

			rawResults := collectReposStatus(cmd.Context(), cfg.Repos, cleaner, gitClient)

			report := model.StatusReport{
				Total: len(rawResults),
				Repos: make([]model.RepoStatusDTO, 0),
			}

			for _, res := range rawResults {
				if res.Error != "" {
					report.Errors++
				} else if res.Dirty {
					report.Dirty++
				} else {
					report.Clean++
				}
			}

			for _, res := range rawResults {
				include := false
				switch filter {
				case "all":
					include = true
				case "dirty":
					include = res.Dirty && res.Error == ""
				case "clean":
					include = !res.Dirty && res.Error == ""
				case "error":
					include = res.Error != ""
				}
				if include {
					report.Repos = append(report.Repos, res)
				}
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return fmt.Errorf("сериализовать отчет в json: %w", err)
				}
			} else {
				out := cmd.OutOrStdout()
				w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "РЕПОЗИТОРИЙ\tВЕТКА\tСОСТОЯНИЕ\tСИНХРОНИЗАЦИЯ\tОШИБКА")
				for _, repo := range report.Repos {
					state := "чистый"
					if repo.Error != "" {
						state = "ошибка"
					} else if repo.Dirty {
						state = "с изменениями"
					}

					branch := repo.Branch
					if branch == "" {
						branch = "-"
					}

					syncWarning := repo.SyncWarning
					if syncWarning == "" {
						syncWarning = "-"
					}

					repoErr := repo.Error
					if repoErr == "" {
						repoErr = "-"
					}

					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", repo.Name, branch, state, syncWarning, repoErr)
				}
				_ = w.Flush()

				_, _ = fmt.Fprintf(out, "\nВсего: %d | Чистых: %d | С изменениями: %d | Ошибок: %d\n",
					report.Total, report.Clean, report.Dirty, report.Errors)
			}

			if report.Errors > 0 && !ignoreErrors {
				return fmt.Errorf("обнаружены ошибки в %d репозиториях", report.Errors)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "вывод отчета в формате JSON")
	cmd.Flags().StringVar(&filterRaw, "filter", "all", "фильтр репозиториев (all, dirty, clean, error)")
	cmd.Flags().BoolVar(&ignoreErrors, "ignore-errors", false, "не завершать команду с ошибкой при наличии сбойных репозиториев")

	return cmd
}

func collectReposStatus(ctx context.Context, repos []config.RepoConfig, cleaner *usecase.Cleaner, gitClient *git.Client) []model.RepoStatusDTO {
	results := make([]model.RepoStatusDTO, len(repos))
	if len(repos) == 0 {
		return results
	}

	jobs := make(chan statusRepoJob, len(repos))
	for i, repo := range repos {
		jobs <- statusRepoJob{idx: i, repo: repo}
	}
	close(jobs)

	workers := min(statusWorkerConcurrency, len(repos))
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for j := range jobs {
				results[j.idx] = inspectRepoStatus(ctx, j.repo, cleaner, gitClient)
			}
		})
	}

	wg.Wait()
	return results
}

func inspectRepoStatus(ctx context.Context, repo config.RepoConfig, cleaner *usecase.Cleaner, gitClient *git.Client) model.RepoStatusDTO {
	repoPath := repo.Path
	if repoPath == "" {
		repoPath = gitClient.ManagedRepoPath(repo.Name, repo.URL)
	}

	dto := model.RepoStatusDTO{
		Name: repo.Name,
		Path: repoPath,
	}

	stat, err := cleaner.LoadRepoStat(ctx, repo)
	if err != nil {
		dto.Error = err.Error()
		return dto
	}

	if stat.HasError() {
		dto.Error = stat.LoadError
	} else {
		dto.Branch = stat.CurrentBranch
		dto.Dirty = stat.DirtyStats.HasChanges()
	}

	if stat.HasSyncWarning() {
		if stat.SyncWarning != "" {
			dto.SyncWarning = stat.SyncWarning
		} else if stat.Warning.Message != "" {
			dto.SyncWarning = stat.Warning.Message
		}
	}

	return dto
}
