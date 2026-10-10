package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/agelxnash/go-repo-orchestrator/internal/app"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/git"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

type execRepoJob struct {
	idx  int
	repo config.RepoConfig
	path string
}

func newExecCommand(v *viper.Viper, logger *zap.Logger) *cobra.Command {
	if v == nil {
		v = viper.New()
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	var (
		filterRaw  string
		parallel   int
		timeout    time.Duration
		failFast   bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "exec [flags] -- <command> [args...]",
		Short: "Пакетное выполнение команды во всех настроенных репозиториях",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dashIdx := cmd.ArgsLenAtDash()
			if dashIdx == -1 || dashIdx >= len(args) {
				return errors.New("команда не указана: укажите команду после разделителя '--' (например: -- git status -s)")
			}
			if dashIdx > 0 {
				return fmt.Errorf("неожиданные аргументы перед '--': %s", strings.Join(args[:dashIdx], " "))
			}

			commandParts := args[dashIdx:]

			if parallel <= 0 {
				return fmt.Errorf("недопустимое значение --parallel: %d (должно быть > 0)", parallel)
			}

			if timeout <= 0 {
				return fmt.Errorf("недопустимое значение --timeout: %v (должно быть > 0)", timeout)
			}

			var filterRegex *regexp.Regexp
			if strings.TrimSpace(filterRaw) != "" {
				var err error
				filterRegex, err = regexp.Compile(filterRaw)
				if err != nil {
					return fmt.Errorf("невалидный regex для --filter: %w", err)
				}
			}

			cfg, err := config.LoadFromViper(v)
			if err != nil {
				return err
			}

			var targetRepos []config.RepoConfig
			for _, repo := range cfg.Repos {
				if filterRegex != nil && !filterRegex.MatchString(repo.Name) {
					continue
				}
				targetRepos = append(targetRepos, repo)
			}

			stateDir := v.GetString("state_dir")
			if stateDir == "" {
				stateDir = app.DefaultStateDir()
			}
			workspaceDir := app.DefaultWorkspaceDir(stateDir)
			gitClient := git.NewClient(app.DefaultGitTimeout, workspaceDir)

			results := make([]model.ExecResultDTO, len(targetRepos))

			if len(targetRepos) > 0 {
				logger.Debug("starting exec across repos", zap.Int("repos", len(targetRepos)), zap.Strings("command", commandParts))

				jobs := make(chan execRepoJob, len(targetRepos))
				for i, repo := range targetRepos {
					repoPath := repo.Path
					if repoPath == "" {
						repoPath = gitClient.ManagedRepoPath(repo.Name, repo.URL)
					}
					jobs <- execRepoJob{
						idx:  i,
						repo: repo,
						path: repoPath,
					}
				}
				close(jobs)

				workers := min(parallel, len(targetRepos))
				ctx, cancel := context.WithCancelCause(cmd.Context())
				defer cancel(nil)

				var wg sync.WaitGroup
				var failedCount atomic.Int32

				for range workers {
					wg.Go(func() {
						for j := range jobs {
							if failFast && ctx.Err() != nil {
								results[j.idx] = model.ExecResultDTO{
									Repo:       j.repo.Name,
									Path:       j.path,
									ExitCode:   -1,
									Stdout:     "",
									Stderr:     "",
									DurationMS: 0,
									Error:      "отменено из-за ошибки в другом репозитории (--fail-fast)",
								}
								continue
							}

							res := runExecInRepo(ctx, j.repo.Name, j.path, commandParts, timeout)
							results[j.idx] = res

							logger.Debug("executed command in repo",
								zap.String("repo", j.repo.Name),
								zap.Int("exit_code", res.ExitCode),
								zap.Int64("duration_ms", res.DurationMS),
							)

							if res.ExitCode != 0 || res.Error != "" {
								failedCount.Add(1)
								if failFast {
									cancel(fmt.Errorf("ошибка в репозитории %s: %s", j.repo.Name, res.Error))
								}
							}
						}
					})
				}

				wg.Wait()
			}

			report := model.ExecReport{
				Total:   len(targetRepos),
				Success: 0,
				Failed:  0,
				Results: make([]model.ExecResultDTO, len(targetRepos)),
			}
			copy(report.Results, results)

			for _, res := range report.Results {
				if res.ExitCode == 0 && res.Error == "" {
					report.Success++
				} else {
					report.Failed++
				}
			}

			logger.Info("exec completed",
				zap.Int("total", report.Total),
				zap.Int("success", report.Success),
				zap.Int("failed", report.Failed),
			)

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return fmt.Errorf("сериализовать отчет в json: %w", err)
				}
			} else {
				out := cmd.OutOrStdout()
				for _, res := range report.Results {
					_, _ = fmt.Fprintf(out, "[%s]\n", res.Repo)
					if res.Stdout != "" {
						_, _ = fmt.Fprint(out, res.Stdout)
						if !strings.HasSuffix(res.Stdout, "\n") {
							_, _ = fmt.Fprintln(out)
						}
					}
					if res.Stderr != "" {
						_, _ = fmt.Fprint(out, res.Stderr)
						if !strings.HasSuffix(res.Stderr, "\n") {
							_, _ = fmt.Fprintln(out)
						}
					}
					if res.Error != "" {
						_, _ = fmt.Fprintf(out, "Ошибка: %s\n", res.Error)
					}
					_, _ = fmt.Fprintln(out)
				}

				_, _ = fmt.Fprintf(out, "Всего: %d | Успешно: %d | С ошибками: %d\n", report.Total, report.Success, report.Failed)
			}

			if report.Failed > 0 {
				return fmt.Errorf("выполнение завершилось с ошибками в %d репозиториях", report.Failed)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&filterRaw, "filter", "", "фильтрация репозиториев по регулярному выражению имени")
	cmd.Flags().IntVar(&parallel, "parallel", 4, "количество параллельных потоков выполнения")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "таймаут выполнения команды для одного репозитория")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "прерывать выполнение при первой ошибке")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "вывод структурированного отчета в формате JSON")

	return cmd
}

func runExecInRepo(parentCtx context.Context, repoName, repoPath string, cmdParts []string, timeout time.Duration) model.ExecResultDTO {
	dto := model.ExecResultDTO{
		Repo: repoName,
		Path: repoPath,
	}

	repoCtx, repoCancel := context.WithTimeout(parentCtx, timeout)
	defer repoCancel()

	cmdName := cmdParts[0]
	cmdArgs := cmdParts[1:]

	execCmd := exec.CommandContext(repoCtx, cmdName, cmdArgs...)
	execCmd.Dir = repoPath

	devNull, err := os.Open(os.DevNull)
	if err == nil {
		execCmd.Stdin = devNull
		defer func() { _ = devNull.Close() }()
	} else {
		execCmd.Stdin = strings.NewReader("")
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	start := time.Now()
	runErr := execCmd.Run()
	durationMS := time.Since(start).Milliseconds()
	if durationMS == 0 {
		durationMS = 1
	}
	dto.DurationMS = durationMS

	dto.Stdout = stdoutBuf.String()
	dto.Stderr = stderrBuf.String()

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			dto.ExitCode = exitErr.ExitCode()
		} else {
			dto.ExitCode = -1
		}

		if errors.Is(repoCtx.Err(), context.DeadlineExceeded) {
			dto.Error = fmt.Sprintf("превышен таймаут выполнения (%v)", timeout)
		} else if errors.Is(parentCtx.Err(), context.Canceled) {
			dto.Error = "отменено из-за ошибки в другом репозитории (--fail-fast)"
		} else {
			dto.Error = runErr.Error()
		}
	} else {
		dto.ExitCode = 0
	}

	return dto
}
