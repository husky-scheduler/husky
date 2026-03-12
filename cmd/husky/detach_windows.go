//go:build windows

package main

import "os/exec"

// detachProcess is a no-op on Windows; the child process is already
// independent once cmd.Start() returns with cmd.Wait() not called.
func detachProcess(_ *exec.Cmd) {}
