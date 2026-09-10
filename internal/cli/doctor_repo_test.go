package cli

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func writeDoctorRepoConfig(t *testing.T, configDir string, body string) string {
	t.Helper()
	path := filepath.Join(configDir, "git.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runDoctorRepo(t *testing.T, configPath, repoName string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "repo", repoName, "--config", configPath})
	err := cmd.Execute()
	return out.String(), err
}

func TestDoctorRepoHealthyRepository(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "cfg")
	repoDir := filepath.Join(root, "b2b", "portal")
	originBare := filepath.Join(root, "origin.git")
	for _, dir := range []string{configDir, repoDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runShell(t, root, "git init -q --bare origin.git")
	runShell(t, repoDir, "git init -q -b main .")
	runShell(t, repoDir, "git config user.email test@example.com")
	runShell(t, repoDir, "git config user.name tester")
	runShell(t, repoDir, "git commit -q --allow-empty -m init")
	runShell(t, repoDir, "git remote add origin "+originBare)
	runShell(t, repoDir, "git push -q -u origin main")

	configPath := writeDoctorRepoConfig(t, configDir,
		"repos:\n  - name: group/portal\n    url: git@example.com:group/portal.git\n    path: ../b2b/portal\n")

	rendered, err := runDoctorRepo(t, configPath, "group/portal")
	if err != nil {
		t.Fatalf("doctor repo: %v", err)
	}

	for _, fragment := range []string{
		"Репозиторий-диагностика: group/portal",
		"Источник: opensource",
		"Путь (резолв): " + repoDir,
		"Относительно каталога конфига: ../b2b/portal",
		"Режим opensource",
		"HEAD: main (валид)",
		"fetch --prune origin: ok",
		"Вердикт: репозиторий загрузится",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, rendered)
		}
	}
}

func TestDoctorRepoBrokenCloneVerdict(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "cfg")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	runShell(t, root, "git init -q --bare -b main remote.git")
	seed := filepath.Join(root, "seed")
	runShell(t, root, "git clone -q remote.git seed")
	runShell(t, seed, "git config user.email test@example.com")
	runShell(t, seed, "git config user.name tester")
	runShell(t, seed, "git commit -q --allow-empty -m init")
	runShell(t, seed, "git push -q -u origin main")

	broken := filepath.Join(root, "svc")
	runShell(t, root, "git clone -q remote.git svc")
	runShell(t, broken, "git update-ref -d refs/heads/main")

	configPath := writeDoctorRepoConfig(t, configDir,
		"repos:\n  - name: svc\n    url: git@example.com:group/svc.git\n    path: ../svc\n")

	rendered, err := runDoctorRepo(t, configPath, "svc")
	if err != nil {
		t.Fatalf("doctor repo: %v", err)
	}
	for _, fragment := range []string{
		"HEAD: невалиден (ветка без коммитов)",
		"Вердикт: клон-обрывок",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, rendered)
		}
	}
}

func TestDoctorRepoMissingNameListsAlternatives(t *testing.T) {
	configPath := writeDoctorRepoConfig(t, t.TempDir(),
		"repos:\n  - name: svc\n    path: /definitely/missing/path\n")

	_, err := runDoctorRepo(t, configPath, "unknown/svc")
	if err == nil {
		t.Fatal("expected error for unknown repo name")
	}
	if !strings.Contains(err.Error(), "svc") {
		t.Fatalf("expected available names in error, got: %v", err)
	}
}

func TestDoctorRepoMasksURLCredentials(t *testing.T) {
	secret := "super-secret-token"
	origin := "https://user:" + url.QueryEscape(secret) + "@example.com/group/repo.git"

	root := t.TempDir()
	repoDir := filepath.Join(root, "r")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := writeDoctorRepoConfig(t, root, "repos:\n  - name: r\n    url: "+origin+"\n")

	if got := sanitizeRepoURL(origin); strings.Contains(got, secret) {
		t.Fatalf("sanitizeRepoURL must mask credentials, got %q", got)
	}

	rendered, err := runDoctorRepo(t, configPath, "r")
	if err != nil {
		t.Fatalf("doctor repo: %v", err)
	}
	if strings.Contains(rendered, secret) {
		t.Fatalf("doctor repo output must not contain url credentials:\n%s", rendered)
	}
}

func runShell(t *testing.T, dir, command string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command %q in %s failed: %v\n%s", command, dir, err, out)
	}
}
