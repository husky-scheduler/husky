//go:build windows

package executor

import "os/exec"

// setProcessGroup is a no-op on Windows.
func setProcessGroup(_ *exec.Cmd) {}

// sigterm kills the process on Windows (no signal support).
func sigterm(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// sigkill kills the process on Windows.
func sigkill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
