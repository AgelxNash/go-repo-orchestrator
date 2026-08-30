package cli

import (
	"io"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestRootCommandFailsFastWithoutConfigFlag(t *testing.T) {
	t.Setenv("GBC_CONFIG", "./config.example.yaml")

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "root run",
			args: nil,
		},
		{
			name: "generate command",
			args: []string{"generate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			cmd := NewRootCommand("dev", "none", "unknown", logger)
			cmd.SetOut(io.Discard)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected missing --config error")
			}

			if !strings.Contains(err.Error(), "параметр --config обязателен") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
