//go:build !windows

package daemoncmd

import "syscall"

// pidAlive returns true if a process with the given PID is running.
// On Unix this sends signal 0 (no-op) to probe without disturbing the process.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
