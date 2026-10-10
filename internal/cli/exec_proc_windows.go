//go:build windows

package cli

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {
	// В Windows поведение CommandContext по умолчанию использует Process.Kill()
}
