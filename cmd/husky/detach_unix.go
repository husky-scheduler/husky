//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess configures cmd to start in a new session so it survives
// after the parent process exits.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
