//go:build windows

package cli

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {
	// В Windows поведение CommandContext по умолчанию использует Process.Kill()
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
