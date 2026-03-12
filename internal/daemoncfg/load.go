package daemoncfg

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

//go:embed schema/huskyd.schema.json
var schemaJSON string

const schemaURL = "https://github.com/husky-scheduler/husky/schema/huskyd.schema.json"

var compiledSchema *jsonschema.Schema

func init() {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	if err := compiler.AddResource(schemaURL, strings.NewReader(schemaJSON)); err != nil {
		panic(fmt.Sprintf("daemoncfg: failed to add embedded JSON Schema: %v", err))
	}
	var err error
	compiledSchema, err = compiler.Compile(schemaURL)
	if err != nil {
		panic(fmt.Sprintf("daemoncfg: failed to compile embedded JSON Schema: %v", err))
	}
}

// ErrNoDaemonConfig is returned by Load when the given path does not exist and
// a caller explicitly provided a path (rather than using the default discovery
// path). Callers that use default discovery should treat absence as "use
// defaults".
var ErrNoDaemonConfig = errors.New("huskyd.yaml not found")

// Load reads and validates a huskyd.yaml file from path.  If path is empty the
// function looks for "huskyd.yaml" in the same directory as husky.yaml (the
// value of huskyConfigDir). When the file is absent at the default location
// Load returns (Defaults(), nil) — no error. When an explicit path is given
// and the file does not exist Load returns ErrNoDaemonConfig.
func Load(path, huskyConfigDir string) (DaemonConfig, error) {
	cfg := Defaults()

	// Determine the actual file path to read.
	if path == "" {
		// Default discovery: look for huskyd.yaml next to husky.yaml.
		if huskyConfigDir != "" {
			path = filepath.Join(huskyConfigDir, "huskyd.yaml")
		}
		if path == "" {
			return cfg, nil
		}
		// Silently return defaults when the file is absent at the default location.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return cfg, nil
		}
	} else {
		// Explicit path — the caller expects the file to exist.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return cfg, ErrNoDaemonConfig
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("daemoncfg: read %s: %w", path, err)
	}

	return LoadBytes(data)
}

// LoadBytes parses and validates huskyd.yaml content from a raw byte slice.
// It starts with Defaults() and merges the parsed values on top, so any field
// absent in the YAML retains its default value.
func LoadBytes(data []byte) (DaemonConfig, error) {
	cfg := Defaults()

	// Step 1: JSON Schema structural validation (converts YAML → generic
	// map[string]any for the schema library).
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("daemoncfg: YAML parse error: %w", err)
	}
	// The JSON Schema library needs a JSON-compatible value. Convert via
	// yaml → map normalisation.
	normalised, err := normalise(raw)
	if err != nil {
		return cfg, fmt.Errorf("daemoncfg: normalise: %w", err)
	}
	if normalised != nil {
		if err := compiledSchema.Validate(normalised); err != nil {
			var ve *jsonschema.ValidationError
			if errors.As(err, &ve) {
				return cfg, fmt.Errorf("daemoncfg: validation error: %s", ve.Error())
			}
			return cfg, fmt.Errorf("daemoncfg: schema validation: %w", err)
		}
	}

	// Step 2: Unmarshal into the concrete struct. We unmarshal into a
	// zero-value struct and then merge into defaults so only explicitly set
	// fields override the defaults.
	var parsed DaemonConfig
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return cfg, fmt.Errorf("daemoncfg: struct unmarshal: %w", err)
	}
	mergeDaemonConfig(&cfg, parsed)

	// Step 3: Semantic validation of a few critical fields.
	if err := validate(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// mergeDaemonConfig copies non-zero fields from src into dst, preserving
// default values for any fields that were not set in the source.
//
// The merge is intentionally shallow for scalar fields and per-struct for
// sub-structs — the pattern follows the same convention as the config package.
func mergeDaemonConfig(dst *DaemonConfig, src DaemonConfig) {
	mergeAPI(&dst.API, src.API)
	mergeAuth(&dst.Auth, src.Auth)
	mergeLog(&dst.Log, src.Log)
	mergeStorage(&dst.Storage, src.Storage)
	mergeScheduler(&dst.Scheduler, src.Scheduler)
	mergeExecutor(&dst.Executor, src.Executor)
	mergeMetrics(&dst.Metrics, src.Metrics)
	mergeTracing(&dst.Tracing, src.Tracing)
	mergeSecrets(&dst.Secrets, src.Secrets)
	mergeAlerts(&dst.Alerts, src.Alerts)
	mergeDashboard(&dst.Dashboard, src.Dashboard)
	mergeHTTPClient(&dst.HTTPClient, src.HTTPClient)
	mergeProcess(&dst.Process, src.Process)
}

func mergeAPI(dst *APIConfig, src APIConfig) {
	if src.Addr != "" {
		dst.Addr = src.Addr
	}
	if src.BasePath != "" {
		dst.BasePath = src.BasePath
	}
	mergeTLS(&dst.TLS, src.TLS)
	mergeCORS(&dst.CORS, src.CORS)
	mergeTimeouts(&dst.Timeouts, src.Timeouts)
}

func mergeTLS(dst *TLSConfig, src TLSConfig) {
	if src.Enabled {
		dst.Enabled = src.Enabled
	}
	if src.Cert != "" {
		dst.Cert = src.Cert
	}
	if src.Key != "" {
		dst.Key = src.Key
	}
	if src.MinVersion != "" {
		dst.MinVersion = src.MinVersion
	}
	if src.ClientCA != "" {
		dst.ClientCA = src.ClientCA
	}
}

func mergeCORS(dst *CORSConfig, src CORSConfig) {
	if len(src.AllowedOrigins) > 0 {
		dst.AllowedOrigins = src.AllowedOrigins
	}
	if src.AllowCredentials {
		dst.AllowCredentials = src.AllowCredentials
	}
}

func mergeTimeouts(dst *TimeoutsConfig, src TimeoutsConfig) {
	if src.ReadHeader != "" {
		dst.ReadHeader = src.ReadHeader
	}
	if src.Read != "" {
		dst.Read = src.Read
	}
	if src.Write != "" {
		dst.Write = src.Write
	}
	if src.Idle != "" {
		dst.Idle = src.Idle
	}
}

func mergeAuth(dst *AuthConfig, src AuthConfig) {
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Bearer.TokenFile != "" {
		dst.Bearer.TokenFile = src.Bearer.TokenFile
	}
	if src.Bearer.Token != "" {
		dst.Bearer.Token = src.Bearer.Token
	}
	if len(src.Basic.Users) > 0 {
		dst.Basic.Users = src.Basic.Users
	}
	if src.OIDC.Issuer != "" {
		dst.OIDC = src.OIDC
	}
	if len(src.RBAC) > 0 {
		dst.RBAC = src.RBAC
	}
}

func mergeLog(dst *LogConfig, src LogConfig) {
	if src.Level != "" {
		dst.Level = src.Level
	}
	if src.Format != "" {
		dst.Format = src.Format
	}
	if src.Output != "" {
		dst.Output = src.Output
	}
	if src.File.Path != "" {
		dst.File = src.File
	}
	if src.AuditLog.Enabled || src.AuditLog.Path != "" {
		dst.AuditLog = src.AuditLog
	}
}

func mergeStorage(dst *StorageConfig, src StorageConfig) {
	if src.SQLite.Path != "" {
		dst.SQLite.Path = src.SQLite.Path
	}
	if src.SQLite.WALAutocheckpoint != 0 {
		dst.SQLite.WALAutocheckpoint = src.SQLite.WALAutocheckpoint
	}
	if src.SQLite.BusyTimeout != "" {
		dst.SQLite.BusyTimeout = src.SQLite.BusyTimeout
	}
	if src.Retention.MaxAge != "" {
		dst.Retention.MaxAge = src.Retention.MaxAge
	}
	if src.Retention.MaxRunsPerJob != 0 {
		dst.Retention.MaxRunsPerJob = src.Retention.MaxRunsPerJob
	}
	if src.Engine != "" {
		dst.Engine = src.Engine
	}
}

func mergeScheduler(dst *SchedulerConfig, src SchedulerConfig) {
	if src.MaxConcurrentJobs != 0 {
		dst.MaxConcurrentJobs = src.MaxConcurrentJobs
	}
	if src.CatchupWindow != "" {
		dst.CatchupWindow = src.CatchupWindow
	}
	if src.ShutdownTimeout != "" {
		dst.ShutdownTimeout = src.ShutdownTimeout
	}
	if src.ScheduleJitter != "" {
		dst.ScheduleJitter = src.ScheduleJitter
	}
}

func mergeExecutor(dst *ExecutorConfig, src ExecutorConfig) {
	if src.PoolSize != 0 {
		dst.PoolSize = src.PoolSize
	}
	if src.Shell != "" {
		dst.Shell = src.Shell
	}
	if src.WorkingDir != "" {
		dst.WorkingDir = src.WorkingDir
	}
	if src.ResourceLimits.MaxMemoryMB != 0 {
		dst.ResourceLimits.MaxMemoryMB = src.ResourceLimits.MaxMemoryMB
	}
	if src.ResourceLimits.MaxOpenFiles != 0 {
		dst.ResourceLimits.MaxOpenFiles = src.ResourceLimits.MaxOpenFiles
	}
	if src.ResourceLimits.MaxPIDs != 0 {
		dst.ResourceLimits.MaxPIDs = src.ResourceLimits.MaxPIDs
	}
	if len(src.GlobalEnv) > 0 {
		dst.GlobalEnv = src.GlobalEnv
	}
}

func mergeMetrics(dst *MetricsConfig, src MetricsConfig) {
	if src.Enabled {
		dst.Enabled = src.Enabled
	}
	if src.Addr != "" {
		dst.Addr = src.Addr
	}
	if src.Path != "" {
		dst.Path = src.Path
	}
	if src.Auth {
		dst.Auth = src.Auth
	}
}

func mergeTracing(dst *TracingConfig, src TracingConfig) {
	if src.Enabled {
		dst.Enabled = src.Enabled
	}
	if src.Exporter != "" {
		dst.Exporter = src.Exporter
	}
	if src.Endpoint != "" {
		dst.Endpoint = src.Endpoint
	}
	if src.ServiceName != "" {
		dst.ServiceName = src.ServiceName
	}
	if src.SampleRate != 0 {
		dst.SampleRate = src.SampleRate
	}
}

func mergeSecrets(dst *SecretsConfig, src SecretsConfig) {
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.Vault.Addr != "" {
		dst.Vault = src.Vault
	}
	if src.AWSSSM.Region != "" {
		dst.AWSSSM = src.AWSSSM
	}
	if src.AWSSecretsManager.Region != "" {
		dst.AWSSecretsManager = src.AWSSecretsManager
	}
	if src.GCPSecretManager.ProjectID != "" {
		dst.GCPSecretManager = src.GCPSecretManager
	}
}

func mergeAlerts(dst *AlertsConfig, src AlertsConfig) {
	if len(src.OnDaemonStart) > 0 {
		dst.OnDaemonStart = src.OnDaemonStart
	}
	if len(src.OnSLABreach) > 0 {
		dst.OnSLABreach = src.OnSLABreach
	}
	if len(src.OnForcedKill) > 0 {
		dst.OnForcedKill = src.OnForcedKill
	}
}

func mergeDashboard(dst *DashboardConfig, src DashboardConfig) {
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
	}
	if src.Title != "" {
		dst.Title = src.Title
	}
	if src.AccentColor != "" {
		dst.AccentColor = src.AccentColor
	}
	if src.LogBackfillLines != 0 {
		dst.LogBackfillLines = src.LogBackfillLines
	}
	if src.PollInterval != "" {
		dst.PollInterval = src.PollInterval
	}
}

func mergeHTTPClient(dst *HTTPClientConfig, src HTTPClientConfig) {
	if src.Timeout != "" {
		dst.Timeout = src.Timeout
	}
	if src.MaxRetries != 0 {
		dst.MaxRetries = src.MaxRetries
	}
	if src.RetryBackoff != "" {
		dst.RetryBackoff = src.RetryBackoff
	}
	if src.Proxy != "" {
		dst.Proxy = src.Proxy
	}
	if src.CABundle != "" {
		dst.CABundle = src.CABundle
	}
}

func mergeProcess(dst *ProcessConfig, src ProcessConfig) {
	if src.User != "" {
		dst.User = src.User
	}
	if src.Group != "" {
		dst.Group = src.Group
	}
	if src.PIDFile != "" {
		dst.PIDFile = src.PIDFile
	}
	if src.UlimitNofile != 0 {
		dst.UlimitNofile = src.UlimitNofile
	}
	if src.WatchdogInterval != "" {
		dst.WatchdogInterval = src.WatchdogInterval
	}
}

// validate performs semantic checks on the loaded config.
func validate(cfg DaemonConfig) error {
	// TLS: if enabled, cert and key must be present and readable.
	if cfg.API.TLS.Enabled {
		if cfg.API.TLS.Cert == "" || cfg.API.TLS.Key == "" {
			return errors.New("daemoncfg: api.tls.enabled requires api.tls.cert and api.tls.key to be set")
		}
		if _, err := os.Stat(cfg.API.TLS.Cert); err != nil {
			return fmt.Errorf("daemoncfg: api.tls.cert: %w", err)
		}
		if _, err := os.Stat(cfg.API.TLS.Key); err != nil {
			return fmt.Errorf("daemoncfg: api.tls.key: %w", err)
		}
	}

	// TLS MinVersion must be "1.2" or "1.3".
	if v := cfg.API.TLS.MinVersion; v != "" && v != "1.2" && v != "1.3" {
		return fmt.Errorf("daemoncfg: api.tls.min_version %q is invalid; must be \"1.2\" or \"1.3\"", v)
	}

	// Log level must be one of the accepted values.
	switch strings.ToLower(cfg.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("daemoncfg: log.level %q is invalid; must be debug, info, warn, or error", cfg.Log.Level)
	}

	// Log format.
	switch strings.ToLower(cfg.Log.Format) {
	case "text", "json":
	default:
		return fmt.Errorf("daemoncfg: log.format %q is invalid; must be text or json", cfg.Log.Format)
	}

	// Log output.
	switch strings.ToLower(cfg.Log.Output) {
	case "stdout", "stderr", "file":
	default:
		return fmt.Errorf("daemoncfg: log.output %q is invalid; must be stdout, stderr, or file", cfg.Log.Output)
	}
	if strings.ToLower(cfg.Log.Output) == "file" && cfg.Log.File.Path == "" {
		return errors.New("daemoncfg: log.output is file but log.file.path is not set")
	}

	// Storage engine.
	switch strings.ToLower(cfg.Storage.Engine) {
	case "sqlite":
		// supported
	case "postgres":
		return errors.New("daemoncfg: storage.engine postgres is not yet supported")
	default:
		return fmt.Errorf("daemoncfg: storage.engine %q is not recognised", cfg.Storage.Engine)
	}

	// Auth type.
	switch strings.ToLower(cfg.Auth.Type) {
	case "none", "bearer", "basic", "oidc":
	default:
		return fmt.Errorf("daemoncfg: auth.type %q is invalid; must be none, bearer, basic, or oidc", cfg.Auth.Type)
	}

	// Secrets provider.
	switch strings.ToLower(cfg.Secrets.Provider) {
	case "env", "vault", "aws_ssm", "aws_secrets_manager", "gcp_secret_manager":
	default:
		return fmt.Errorf("daemoncfg: secrets.provider %q is not recognised", cfg.Secrets.Provider)
	}

	return nil
}

// normalise converts a yaml.v3-decoded value (which may contain
// map[string]interface{} or map[interface{}]interface{} nodes) into a value
// suitable for JSON Schema validation.
func normalise(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			n, err := normalise(val)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string map key: %v", k)
			}
			n, err := normalise(val)
			if err != nil {
				return nil, err
			}
			out[ks] = n
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			n, err := normalise(val)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	default:
		return v, nil
	}
}
