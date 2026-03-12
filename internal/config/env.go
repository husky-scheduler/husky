package config

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// envVarPattern matches ${env:VARIABLE_NAME} tokens in env values.
var envVarPattern = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadDotEnv reads a .env file from dir (if one exists) and sets any key that
// is not already present in the process environment. Variables already set in
// the environment always take precedence over the .env file.
//
// The file format follows the Docker Compose convention:
//   - Lines starting with # are comments and are ignored.
//   - Blank lines are ignored.
//   - KEY=VALUE pairs set the variable. Surrounding whitespace is trimmed.
//   - Values may be bare or wrapped in single/double quotes (quotes are stripped).
//
// Returns nil if the .env file does not exist.
func LoadDotEnv(dir string) error {
	path := filepath.Join(dir, ".env")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // .env is optional
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue // not a KEY=VALUE line
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip surrounding quotes.
		if len(value) >= 2 &&
			((value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		// Do not overwrite variables already set in the environment.
		if _, alreadySet := os.LookupEnv(key); !alreadySet {
			os.Setenv(key, value) //nolint:errcheck // Setenv rarely fails
		}
	}
	return scanner.Err()
}

// interpolateEnv replaces all ${env:VAR} tokens in every job's env map and
// all integration credential fields with the corresponding host env value.
// Missing host variables are replaced with an empty string (values are never
// written to storage).
func interpolateEnv(cfg *Config) {
	for _, job := range cfg.Jobs {
		for k, v := range job.Env {
			job.Env[k] = expandEnvTokens(v)
		}
	}
	for _, intg := range cfg.Integrations {
		intg.WebhookURL = expandEnvTokens(intg.WebhookURL)
		intg.RoutingKey = expandEnvTokens(intg.RoutingKey)
		intg.Username = expandEnvTokens(intg.Username)
		intg.Password = expandEnvTokens(intg.Password)
	}
}

// expandEnvTokens replaces every ${env:VAR} occurrence in s with the value of
// os.Getenv("VAR"). Unknown variables are expanded to "".
func expandEnvTokens(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := envVarPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return os.Getenv(sub[1])
	})
}
