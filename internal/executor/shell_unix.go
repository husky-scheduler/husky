//go:build !windows

package executor

// shellCommand returns the argv slice used to execute a shell command string.
// On Unix systems this is /bin/sh -c <command>.
func shellCommand(command string) []string {
	return []string{"/bin/sh", "-c", command}
}
