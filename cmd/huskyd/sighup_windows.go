//go:build windows

package daemoncmd

// watchSIGHUP is a no-op on Windows; SIGHUP is not a POSIX concept there.
// Config hot-reload must be triggered via the IPC "reload" command instead.
func watchSIGHUP(_ <-chan struct{}, _ func()) {}
