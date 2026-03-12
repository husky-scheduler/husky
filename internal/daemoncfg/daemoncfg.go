// Package daemoncfg defines and loads the huskyd.yaml daemon runtime
// configuration file. huskyd.yaml governs HOW the daemon runs — API binding,
// auth, TLS, logging, storage retention, resource limits, metrics, tracing,
// and secrets backends — independently of the job definitions in husky.yaml.
//
// All fields are optional. Absence of any section falls back to documented
// defaults; no breaking change for v1.0 users who have no huskyd.yaml.
package daemoncfg

import "time"

// DaemonConfig is the top-level structure parsed from huskyd.yaml.
type DaemonConfig struct {
	API        APIConfig        `yaml:"api"`
	Auth       AuthConfig       `yaml:"auth"`
	Log        LogConfig        `yaml:"log"`
	Storage    StorageConfig    `yaml:"storage"`
	Scheduler  SchedulerConfig  `yaml:"scheduler"`
	Executor   ExecutorConfig   `yaml:"executor"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Tracing    TracingConfig    `yaml:"tracing"`
	Secrets    SecretsConfig    `yaml:"secrets"`
	Alerts     AlertsConfig     `yaml:"alerts"`
	Dashboard  DashboardConfig  `yaml:"dashboard"`
	HTTPClient HTTPClientConfig `yaml:"http_client"`
	Process    ProcessConfig    `yaml:"process"`
}

// ── API Server ────────────────────────────────────────────────────────────────

// APIConfig controls the HTTP/WebSocket server binding, TLS, CORS, and
// per-request timeouts.
type APIConfig struct {
	// Addr overrides the default "127.0.0.1:0" bind address. Setting this to a
	// fixed value (e.g. "127.0.0.1:8420") enables stable-port deployments; set
	// to "0.0.0.0:<port>" to allow remote access (emits a startup warning unless
	// TLS and auth are both enabled).
	Addr string `yaml:"addr"`

	// BasePath mounts the dashboard and all /api/* routes under the given URL
	// prefix (e.g. "/husky"). The prefix is stripped transparently so internal
	// handlers always see clean paths.
	BasePath string `yaml:"base_path"`

	TLS      TLSConfig      `yaml:"tls"`
	CORS     CORSConfig     `yaml:"cors"`
	Timeouts TimeoutsConfig `yaml:"timeouts"`
}

// TLSConfig holds PEM certificate and key paths for HTTPS termination.
type TLSConfig struct {
	// Enabled switches from net.Listen to tls.Listen. The daemon refuses to
	// start if cert/key files are missing or unreadable when this is true.
	Enabled bool `yaml:"enabled"`

	// Cert is the absolute path to the PEM-encoded TLS certificate file.
	Cert string `yaml:"cert"`

	// Key is the absolute path to the PEM-encoded private key file.
	Key string `yaml:"key"`

	// MinVersion enforces a minimum TLS version. Accepted values: "1.2"
	// (default) or "1.3".
	MinVersion string `yaml:"min_version"`

	// ClientCA, when set, enables mutual TLS. Connections whose client
	// certificate is not signed by this CA are rejected.
	ClientCA string `yaml:"client_ca"`
}

// CORSConfig injects CORS response headers on API responses.
type CORSConfig struct {
	// AllowedOrigins is the list of origins that are permitted to make
	// cross-origin requests. "*" is accepted but emits a startup warning.
	AllowedOrigins []string `yaml:"allowed_origins"`

	// AllowCredentials sets the Access-Control-Allow-Credentials header.
	// Required for browser-based OAuth flows.
	AllowCredentials bool `yaml:"allow_credentials"`
}

// TimeoutsConfig maps directly to the corresponding http.Server fields.
// All values are Go duration strings (e.g. "30s", "2m"). Empty string
// retains the current default.
type TimeoutsConfig struct {
	ReadHeader string `yaml:"read_header"` // default "5s"
	Read       string `yaml:"read"`        // default "30s"
	Write      string `yaml:"write"`       // default "60s"
	Idle       string `yaml:"idle"`        // default "120s"
}

// ── Authentication ────────────────────────────────────────────────────────────

// AuthConfig selects the authentication strategy for all REST + WebSocket
// endpoints.
type AuthConfig struct {
	// Type is one of: "none" (default), "bearer", "basic", "oidc".
	Type string `yaml:"type"`

	Bearer BearerAuthConfig `yaml:"bearer"`
	Basic  BasicAuthConfig  `yaml:"basic"`
	OIDC   OIDCAuthConfig   `yaml:"oidc"`

	// RBAC defines per-role allowed HTTP method + path patterns. Built-in roles
	// are "viewer", "operator", and "admin".
	RBAC []RBACRule `yaml:"rbac"`
}

// BearerAuthConfig configures static bearer-token authentication.
type BearerAuthConfig struct {
	// TokenFile contains one bearer token per line.  Hot-reloaded on SIGHUP.
	TokenFile string `yaml:"token_file"`

	// Token is a single inline token (file-based is preferred; using this
	// field emits a log warning).
	Token string `yaml:"token"`
}

// BasicAuthUserEntry is a single username / bcrypt-hashed-password pair.
type BasicAuthUserEntry struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
}

// BasicAuthConfig configures HTTP Basic authentication.
type BasicAuthConfig struct {
	// Users is the list of accepted credentials. Passwords must be bcrypt
	// hashes; plaintext passwords are rejected at load time.
	Users []BasicAuthUserEntry `yaml:"users"`
}

// OIDCAuthConfig configures OIDC / JWT authentication.
type OIDCAuthConfig struct {
	Issuer      string `yaml:"issuer"`
	ClientID    string `yaml:"client_id"`
	Audience    string `yaml:"audience"`
	JWKSURI     string `yaml:"jwks_uri"`
	RoleClaim   string `yaml:"role_claim"`
	DefaultRole string `yaml:"default_role"`
}

// RBACRule grants a role access to a set of HTTP routes.
type RBACRule struct {
	Role    string   `yaml:"role"`
	Methods []string `yaml:"methods"`
	Paths   []string `yaml:"paths"`
}

// ── Logging ───────────────────────────────────────────────────────────────────

// LogConfig controls structured logging for the daemon process.
type LogConfig struct {
	// Level is one of "debug", "info", "warn", "error". Hot-reloadable via SIGHUP.
	// Default: "info".
	Level string `yaml:"level"`

	// Format switches the slog handler. "text" (default) produces human-readable
	// lines; "json" produces one JSON object per line.
	Format string `yaml:"format"`

	// Output is one of "stdout" (default), "stderr", or "file".
	Output string `yaml:"output"`

	File     LogFileConfig  `yaml:"file"`
	AuditLog AuditLogConfig `yaml:"audit_log"`
}

// LogFileConfig controls file-based log output with rotation.
type LogFileConfig struct {
	Path       string `yaml:"path"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAgeDays int    `yaml:"max_age_days"`
	Compress   bool   `yaml:"compress"`
}

// AuditLogConfig writes a separate newline-delimited JSON audit log file.
type AuditLogConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Path       string `yaml:"path"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

// ── Storage ───────────────────────────────────────────────────────────────────

// StorageConfig controls SQLite settings and data-retention policy.
type StorageConfig struct {
	SQLite    SQLiteConfig    `yaml:"sqlite"`
	Retention RetentionConfig `yaml:"retention"`

	// Engine is the storage backend selector. Only "sqlite" is currently
	// supported; "postgres" parses but emits a clear "not yet supported" error
	// at startup.
	Engine string `yaml:"engine"`
}

// SQLiteConfig tunes the SQLite connection.
type SQLiteConfig struct {
	// Path overrides the default "<data>/husky.db" location.
	Path string `yaml:"path"`

	// WALAutocheckpoint sets the SQLite wal_autocheckpoint pragma.
	// Default: 1000 pages.
	WALAutocheckpoint int `yaml:"wal_autocheckpoint"`

	// BusyTimeout sets the SQLite busy-handler timeout (Go duration string).
	// Default: "5s".
	BusyTimeout string `yaml:"busy_timeout"`
}

// RetentionConfig defines automatic pruning of old run records.
type RetentionConfig struct {
	// MaxAge is the maximum age of completed job_runs before they are pruned
	// (Go duration string, e.g. "30d"). Default: unlimited.
	MaxAge string `yaml:"max_age"`

	// MaxRunsPerJob keeps only the N most recent completed runs per job after
	// age-based pruning. PENDING and RUNNING runs are never pruned.
	MaxRunsPerJob int `yaml:"max_runs_per_job"`
}

// ── Scheduler ────────────────────────────────────────────────────────────────

// SchedulerConfig tunes the global scheduling engine.
type SchedulerConfig struct {
	// MaxConcurrentJobs is the global ceiling on concurrently executing jobs.
	// Default: 32.
	MaxConcurrentJobs int `yaml:"max_concurrent_jobs"`

	// CatchupWindow is the maximum look-back window when reconciling missed
	// schedules on restart (Go duration string). Default: "24h".
	CatchupWindow string `yaml:"catchup_window"`

	// ShutdownTimeout is the grace period given to in-flight jobs on graceful
	// stop before SIGKILL (Go duration string). Default: "60s".
	ShutdownTimeout string `yaml:"shutdown_timeout"`

	// ScheduleJitter adds random jitter (up to this duration) to each scheduled
	// trigger to avoid thundering-herd bursts (Go duration string). Default: "0s".
	ScheduleJitter string `yaml:"schedule_jitter"`
}

// ── Executor ─────────────────────────────────────────────────────────────────

// ExecutorConfig controls the worker pool and subprocess environment.
type ExecutorConfig struct {
	// PoolSize is the bounded goroutine worker pool size. Default: 8.
	PoolSize int `yaml:"pool_size"`

	// Shell is the shell used to run job commands. Default: "/bin/sh".
	Shell string `yaml:"shell"`

	// WorkingDir is the working directory for all child processes. "config_dir"
	// (default) uses the directory containing husky.yaml.
	WorkingDir string `yaml:"working_dir"`

	ResourceLimits ResourceLimitsConfig `yaml:"resource_limits"`

	// GlobalEnv injects key-value pairs into every job's environment. Lower
	// priority than per-job env in husky.yaml; higher priority than host env.
	GlobalEnv map[string]string `yaml:"global_env"`
}

// ResourceLimitsConfig sets OS-level resource limits on child processes.
type ResourceLimitsConfig struct {
	// MaxMemoryMB sets RLIMIT_AS on Linux; best-effort setrlimit on macOS.
	// 0 = unlimited.
	MaxMemoryMB int `yaml:"max_memory_mb"`

	// MaxOpenFiles sets RLIMIT_NOFILE on the child process. Default: 1024.
	MaxOpenFiles int `yaml:"max_open_files"`

	// MaxPIDs sets RLIMIT_NPROC on Linux; limits forking within a job.
	MaxPIDs int `yaml:"max_pids"`
}

// ── Metrics ───────────────────────────────────────────────────────────────────

// MetricsConfig controls the Prometheus-compatible scrape endpoint.
type MetricsConfig struct {
	// Enabled exposes a /metrics HTTP endpoint when true. Default: false.
	Enabled bool `yaml:"enabled"`

	// Addr is the bind address for the metrics HTTP server.
	// Default: "127.0.0.1:9091".
	Addr string `yaml:"addr"`

	// Path is the URL path for the scrape endpoint. Default: "/metrics".
	Path string `yaml:"path"`

	// Auth protects /metrics with the same bearer token as the main API when true.
	Auth bool `yaml:"auth"`
}

// ── Tracing ───────────────────────────────────────────────────────────────────

// TracingConfig controls OpenTelemetry span emission.
type TracingConfig struct {
	// Enabled emits OTel spans when true. Default: false.
	Enabled bool `yaml:"enabled"`

	// Exporter selects the span exporter: "otlp" (default), "jaeger",
	// "zipkin", or "stdout".
	Exporter string `yaml:"exporter"`

	// Endpoint is the OTLP collector endpoint (gRPC or HTTP).
	Endpoint string `yaml:"endpoint"`

	// ServiceName is the value of the service.name resource attribute.
	// Default: "huskyd".
	ServiceName string `yaml:"service_name"`

	// SampleRate is the fraction of traces to sample (0.0–1.0). Default: 0.1.
	SampleRate float64 `yaml:"sample_rate"`
}

// ── Secrets ───────────────────────────────────────────────────────────────────

// SecretsConfig selects the secrets resolution backend.
type SecretsConfig struct {
	// Provider is one of: "env" (default), "vault", "aws_ssm",
	// "aws_secrets_manager", "gcp_secret_manager".
	Provider string `yaml:"provider"`

	Vault             VaultConfig             `yaml:"vault"`
	AWSSSM            AWSSSMConfig            `yaml:"aws_ssm"`
	AWSSecretsManager AWSSecretsManagerConfig `yaml:"aws_secrets_manager"`
	GCPSecretManager  GCPSecretManagerConfig  `yaml:"gcp_secret_manager"`
}

// VaultConfig holds HashiCorp Vault KV v2 connection settings.
type VaultConfig struct {
	Addr       string `yaml:"addr"`
	TokenFile  string `yaml:"token_file"`
	Mount      string `yaml:"mount"`
	PathPrefix string `yaml:"path_prefix"`
}

// AWSSSMConfig holds AWS Systems Manager Parameter Store settings.
type AWSSSMConfig struct {
	Region      string `yaml:"region"`
	PathPrefix  string `yaml:"path_prefix"`
	AccessKeyID string `yaml:"access_key_id"`
}

// AWSSecretsManagerConfig holds AWS Secrets Manager settings.
type AWSSecretsManagerConfig struct {
	Region      string `yaml:"region"`
	AccessKeyID string `yaml:"access_key_id"`
}

// GCPSecretManagerConfig holds GCP Secret Manager settings.
type GCPSecretManagerConfig struct {
	ProjectID string `yaml:"project_id"`
}

// ── Daemon-Level Alerts ───────────────────────────────────────────────────────

// AlertsConfig configures daemon-level (non-job) notification events.
type AlertsConfig struct {
	// OnDaemonStart fires notification(s) when huskyd starts successfully.
	OnDaemonStart []string `yaml:"on_daemon_start"`

	// OnSLABreach is the daemon-level fallback for SLA breach notifications
	// when the job does not define notify.on_sla_breach.
	OnSLABreach []string `yaml:"on_sla_breach"`

	// OnForcedKill fires when a job is killed by the shutdown_timeout SIGKILL.
	OnForcedKill []string `yaml:"on_forced_kill"`
}

// ── Dashboard Customisation ───────────────────────────────────────────────────

// DashboardConfig controls runtime customisation of the embedded web dashboard.
type DashboardConfig struct {
	// Enabled disables the dashboard entirely (keeping REST + WebSocket API
	// available) when false. Default: true.
	Enabled *bool `yaml:"enabled"`

	// Title is injected into the <title> tag and dashboard header.
	Title string `yaml:"title"`

	// AccentColor is a hex colour applied as the primary CSS accent variable.
	AccentColor string `yaml:"accent_color"`

	// LogBackfillLines is the number of historical log lines sent on WebSocket
	// connect before switching to live streaming. Default: 200.
	LogBackfillLines int `yaml:"log_backfill_lines"`

	// PollInterval is the interval at which the dashboard polls /api/jobs.
	// Go duration string. Default: "5s".
	PollInterval string `yaml:"poll_interval"`
}

// ── HTTP Client ───────────────────────────────────────────────────────────────

// HTTPClientConfig controls all outbound HTTP calls (notifications, secret
// fetches).
type HTTPClientConfig struct {
	// Timeout is the global timeout for outbound HTTP calls. Default: "15s".
	Timeout string `yaml:"timeout"`

	// MaxRetries is the retry count for failed notification deliveries.
	// Default: 3.
	MaxRetries int `yaml:"max_retries"`

	// RetryBackoff is the fixed backoff between notification retries.
	// Default: "2s".
	RetryBackoff string `yaml:"retry_backoff"`

	// Proxy is an optional HTTP proxy URL for all outbound calls.
	Proxy string `yaml:"proxy"`

	// CABundle is the path to a PEM CA bundle appended to the system cert pool.
	CABundle string `yaml:"ca_bundle"`
}

// ── Process / System ─────────────────────────────────────────────────────────

// ProcessConfig controls OS-level process settings.
type ProcessConfig struct {
	// User / Group: after binding ports, drop privileges to this user/group.
	// No-op on macOS and Windows (logs a warning).
	User  string `yaml:"user"`
	Group string `yaml:"group"`

	// PIDFile overrides the PID file location. Default: "<data>/husky.pid".
	PIDFile string `yaml:"pid_file"`

	// UlimitNofile calls setrlimit(RLIMIT_NOFILE) on the daemon process at
	// startup to enable large numbers of concurrent subprocesses.
	UlimitNofile int `yaml:"ulimit_nofile"`

	// WatchdogInterval, when non-zero, sends sd_notify WATCHDOG=1 pings to
	// the systemd watchdog socket at this interval (Go duration string).
	WatchdogInterval string `yaml:"watchdog_interval"`
}

// ── Defaults ─────────────────────────────────────────────────────────────────

// Defaults returns a DaemonConfig with all documented default values applied.
// Any field the user sets in huskyd.yaml overrides the corresponding default.
func Defaults() DaemonConfig {
	enabled := true
	return DaemonConfig{
		API: APIConfig{
			Addr: "", // empty → use 127.0.0.1:0 (OS-assigned)
			TLS: TLSConfig{
				MinVersion: "1.2",
			},
			Timeouts: TimeoutsConfig{
				ReadHeader: "5s",
				Read:       "30s",
				Write:      "60s",
				Idle:       "120s",
			},
		},
		Auth: AuthConfig{
			Type: "none",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
		Storage: StorageConfig{
			SQLite: SQLiteConfig{
				WALAutocheckpoint: 1000,
				BusyTimeout:       "5s",
			},
			Engine: "sqlite",
		},
		Scheduler: SchedulerConfig{
			MaxConcurrentJobs: 32,
			CatchupWindow:     "24h",
			ShutdownTimeout:   "60s",
			ScheduleJitter:    "0s",
		},
		Executor: ExecutorConfig{
			PoolSize:   8,
			Shell:      "/bin/sh",
			WorkingDir: "config_dir",
			ResourceLimits: ResourceLimitsConfig{
				MaxOpenFiles: 1024,
			},
		},
		Metrics: MetricsConfig{
			Addr: "127.0.0.1:9091",
			Path: "/metrics",
		},
		Tracing: TracingConfig{
			Exporter:    "otlp",
			ServiceName: "huskyd",
			SampleRate:  0.1,
		},
		Secrets: SecretsConfig{
			Provider: "env",
		},
		Dashboard: DashboardConfig{
			Enabled:          &enabled,
			LogBackfillLines: 200,
			PollInterval:     "5s",
		},
		HTTPClient: HTTPClientConfig{
			Timeout:      "15s",
			MaxRetries:   3,
			RetryBackoff: "2s",
		},
	}
}

// ParsedTimeouts returns the TimeoutsConfig values as time.Duration, falling
// back to the built-in defaults for any empty or unparseable field.
func (tc TimeoutsConfig) ParsedTimeouts() (readHeader, read, write, idle time.Duration) {
	parse := func(s string, def time.Duration) time.Duration {
		if s == "" {
			return def
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return def
		}
		return d
	}
	readHeader = parse(tc.ReadHeader, 5*time.Second)
	read = parse(tc.Read, 30*time.Second)
	write = parse(tc.Write, 60*time.Second)
	idle = parse(tc.Idle, 120*time.Second)
	return
}
