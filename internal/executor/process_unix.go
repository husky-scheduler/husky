//go:build !windows

package executor

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures cmd to run in its own process group so that
// signals can be delivered to the entire group (parent + children).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// sigterm sends SIGTERM to the process group of cmd.
func sigterm(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}

// sigkill sends SIGKILL to the process group of cmd.
func sigkill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
