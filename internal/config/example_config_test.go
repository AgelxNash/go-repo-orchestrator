package config

import (
	"path/filepath"
	"testing"
)

// TestLoadExampleConfigMaxScaleKeepRule загружает реальный config.example.yaml
// и проверяет семантику keep-правила MaxScale: стабильные ветки 22.xx–24.xx
// защищены, артефакт опечатки (ветка с "}") — нет. Регрессия issue #51.
func TestLoadExampleConfigMaxScaleKeepRule(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("load example config failed: %v", err)
	}

	var maxScale *RepoConfig
	for i := range cfg.Repos {
		if cfg.Repos[i].Name == "MaxScale" {
			maxScale = &cfg.Repos[i]
			break
		}
	}
	if maxScale == nil {
		t.Fatal("example config must contain MaxScale repo")
	}

	protected := []string{"22.08", "23.08", "24.02"}
	for _, branch := range protected {
		reason, ok := maxScale.ProtectedReason(branch)
		if !ok || reason == "" {
			t.Errorf("branch %q must be protected by MaxScale keep rule", branch)
		}
	}

	notProtected := []string{"24}.02", "21.02", "25.02"}
	for _, branch := range notProtected {
		if _, ok := maxScale.ProtectedReason(branch); ok {
			t.Errorf("branch %q must not be protected by MaxScale keep rule", branch)
		}
	}
}
