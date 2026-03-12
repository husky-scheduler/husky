//go:build windows

package executor

// shellCommand returns the argv slice used to execute a shell command string.
// On Windows this is cmd.exe /C <command>.
func shellCommand(command string) []string {
	return []string{"cmd.exe", "/C", command}
}
