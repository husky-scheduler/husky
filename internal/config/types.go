package config

import "gopkg.in/yaml.v3"

// Defaults holds configuration applied to every job unless the individual job
// overrides the field. It maps directly to the `defaults:` block in husky.yaml.
type Defaults struct {
	Timeout         string `yaml:"timeout"`
	Retries         *int   `yaml:"retries"`
	RetryDelay      string `yaml:"retry_delay"`
	NotifyOnFailure bool   `yaml:"notify_on_failure"`
	OnFailure       string `yaml:"on_failure"`
	DefaultRunTime  string `yaml:"default_run_time"`

	// Timezone is the IANA timezone identifier used for all jobs that do not
	// specify their own timezone. Falls back to the system timezone when empty.
	Timezone string `yaml:"timezone"`
}

// NotifyEvent is a single notification target for one lifecycle event.
// It accepts both a shorthand string form ("slack:#channel") and a full object
// form with channel, message, attach_logs, and only_after_failure. The custom
// YAML unmarshaler handles both representations transparently.
type NotifyEvent struct {
	// Channel is the destination in "<provider>:<target>" format.
	// e.g. "slack:#data-alerts", "pagerduty:p1", "webhook:https://…"
	Channel string `yaml:"channel"`

	// Message is an optional Go template string rendered at dispatch time.
	// Template variables: {{ job.name }}, {{ run.duration }}, etc.
	Message string `yaml:"message"`

	// AttachLogs controls whether log lines are included in the notification.
	// Accepted values: "" / "none" (default), "last_N_lines" (e.g. "last_30_lines"), "all".
	AttachLogs string `yaml:"attach_logs"`

	// OnlyAfterFailure, when true, suppresses the notification unless the
	// previous completed run of this job had a FAILED status. Only meaningful
	// on on_success events.
	OnlyAfterFailure bool `yaml:"only_after_failure"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that NotifyEvent accepts both a
// bare string (the v1.0 shorthand) and the full object form. A bare string is
// treated as the Channel field.
func (n *NotifyEvent) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		n.Channel = value.Value
		return nil
	}
	// Object form — use a local alias to avoid infinite recursion.
	type plain NotifyEvent
	return value.Decode((*plain)(n))
}

// Notify holds notification targets for each job lifecycle event.
//
// Each field accepts both the v1.0 shorthand string form and the new full
// object form, preserving full backward compatibility.
type Notify struct {
	// OnFailure fires when all retry attempts are exhausted.
	OnFailure *NotifyEvent `yaml:"on_failure"`

	// OnSuccess fires when a job completes successfully.
	OnSuccess *NotifyEvent `yaml:"on_success"`

	// OnSLABreach fires when a running job exceeds its sla duration.
	// Falls back to OnFailure when not set.
	OnSLABreach *NotifyEvent `yaml:"on_sla_breach"`

	// OnRetry fires at the start of each retry attempt.
	OnRetry *NotifyEvent `yaml:"on_retry"`
}

// Healthcheck defines an optional secondary command that runs after the main
// command exits with code 0 to verify the job actually produced correct results.
type Healthcheck struct {
	// Command is the shell command to run. Exit 0 = healthy; non-zero = unhealthy.
	Command string `yaml:"command"`

	// Timeout is the maximum runtime for the healthcheck command. Default: "30s".
	Timeout string `yaml:"timeout"`

	// OnFail controls what happens when the healthcheck exits non-zero.
	// Accepted values: "mark_failed" (default) — fail the run and trigger retries;
	// "warn_only" — mark SUCCESS with hc_status=warn and fire a warning notification.
	OnFail string `yaml:"on_fail"`
}

// Job is the in-memory representation of a single job definition.
// Required fields are Description, Frequency, and Command. The Time field is
// conditionally required depending on the selected frequency syntax.
type Job struct {
	// ── Required ────────────────────────────────────────────────────────────

	// Description is a human-readable explanation of what the job does.
	Description string `yaml:"description"`

	// Frequency is the recurrence pattern. Accepted values: hourly, daily,
	// weekly, monthly, weekdays, weekends, manual, after:<job_name>,
	// every:<interval>, on:[day[,day...]].
	Frequency string `yaml:"frequency"`

	// Command is the shell command to execute, run via /bin/sh -c by default.
	Command string `yaml:"command"`

	// ── Conditional ─────────────────────────────────────────────────────────

	// Time is a 4-character military-time string (e.g. "0200" = 2:00 AM).
	// Required when Frequency is daily or monthly. Optional when Frequency is
	// weekly, weekdays, weekends, or on:[...], where it falls back to
	// defaults.default_run_time (default: "0300"). Ignored when Frequency is
	// hourly, every:<interval>, manual, or after:<job>.
	Time string `yaml:"time"`

	// ── Optional ────────────────────────────────────────────────────────────

	// WorkingDir is the working directory for the command. Defaults to the
	// directory of husky.yaml if not set.
	WorkingDir string `yaml:"working_dir"`

	// DependsOn is a list of job names that must succeed before this job runs.
	DependsOn []string `yaml:"depends_on"`

	// Timeout is the maximum runtime before SIGTERM + SIGKILL.
	// Parsed as a duration string, e.g. "30m", "1h", "90s".
	Timeout string `yaml:"timeout"`

	// Retries is the number of retry attempts on failure. 0 = no retries.
	// A pointer is used to distinguish "not set" (nil) from "set to 0".
	Retries *int `yaml:"retries"`

	// RetryDelay is the delay strategy between retry attempts.
	// Accepted values: "exponential" or "fixed:<duration>" e.g. "fixed:30s".
	RetryDelay string `yaml:"retry_delay"`

	// Concurrency controls behaviour when a previous run is still executing.
	// Accepted values: "allow" (default), "forbid", "replace".
	Concurrency string `yaml:"concurrency"`

	// OnFailure controls what happens when all retries are exhausted.
	// Accepted values: "alert", "skip", "stop", "ignore".
	OnFailure string `yaml:"on_failure"`

	// Env is a map of environment variables passed to the command.
	// Values may use ${env:HOST_VAR} to interpolate host environment variables.
	Env map[string]string `yaml:"env"`

	// Notify holds notification targets for each lifecycle event.
	Notify *Notify `yaml:"notify"`

	// Catchup controls whether missed runs are triggered after daemon restart.
	// Default: false.
	Catchup bool `yaml:"catchup"`

	// SLA is the expected maximum duration for this job. When the job is still
	// in RUNNING state after this duration, an on_sla_breach notification is fired.
	// Must be less than Timeout when both are set. No SLA monitoring when empty.
	SLA string `yaml:"sla"`

	// Tags is a list of arbitrary labels used to group jobs for filtered status
	// views and bulk CLI operations. Tags are lowercase alphanumeric with hyphens,
	// max 10 tags per job, max 32 characters per tag.
	Tags []string `yaml:"tags"`

	// Timezone is the IANA timezone identifier used to resolve the Time field.
	// Inherits from Defaults.Timezone when empty. Falls back to system timezone.
	Timezone string `yaml:"timezone"`

	// Healthcheck defines an optional secondary command that runs after the main
	// command exits 0 to verify the job's output is correct.
	Healthcheck *Healthcheck `yaml:"healthcheck"`

	// Output declares variables captured from the job's stdout, keyed by
	// variable name. The value is the capture mode string:
	// "last_line", "first_line", "json_field:<key>", "regex:<pattern>", "exit_code".
	Output map[string]string `yaml:"output"`

	// ── Computed ─────────────────────────────────────────────────────────────

	// Name is populated from the map key in husky.yaml. It is not part of the
	// YAML structure itself.
	Name string `yaml:"-"`
}

// Integration holds the configuration and credentials for a single
// notification provider. Credentials must always be referenced via
// ${env:VAR} tokens so that the husky.yaml file can be safely committed
// without secrets.
//
// The provider is inferred from the map key in husky.yaml when the key
// matches a known provider name (slack, pagerduty, discord, smtp, webhook).
// For multiple integrations that share the same provider, use an arbitrary
// key and set Provider explicitly:
//
//	integrations:
//	  slack_ops:
//	    provider: slack
//	    webhook_url: "${env:SLACK_OPS_WEBHOOK}"
//	  slack_data:
//	    provider: slack
//	    webhook_url: "${env:SLACK_DATA_WEBHOOK}"
type Integration struct {
	// Provider explicitly names the notification backend when the map key
	// does not match a known provider name. Accepted values: "slack",
	// "pagerduty", "discord", "smtp", "webhook".
	Provider string `yaml:"provider"`

	// ── Slack / Discord ───────────────────────────────────────────────────

	// WebhookURL is the incoming webhook URL.
	// Required for slack and discord providers.
	WebhookURL string `yaml:"webhook_url"`

	// ── PagerDuty ─────────────────────────────────────────────────────────

	// RoutingKey is the PagerDuty Events API v2 integration routing key.
	// Required for pagerduty providers.
	RoutingKey string `yaml:"routing_key"`

	// ── SMTP ──────────────────────────────────────────────────────────────

	// Host is the SMTP server hostname. Required for smtp providers.
	Host string `yaml:"host"`

	// Port is the SMTP server port. Defaults to 587 when zero.
	Port int `yaml:"port"`

	// Username is the SMTP authentication username.
	Username string `yaml:"username"`

	// Password is the SMTP authentication password.
	// Should always reference ${env:VAR}.
	Password string `yaml:"password"`

	// From is the sender address for outbound emails. Required for smtp.
	From string `yaml:"from"`

	// ── Computed ──────────────────────────────────────────────────────────

	// Name is populated from the map key in husky.yaml.
	Name string `yaml:"-"`

	// EffectiveProvider is the resolved provider after inference.
	// Set by validateConfig; equals Provider when explicit, otherwise the key.
	EffectiveProvider string `yaml:"-"`
}

// Config is the top-level in-memory representation of husky.yaml.
type Config struct {
	// Version is the husky file format version. Currently only "1" is valid.
	Version string `yaml:"version"`

	// Defaults contains values applied to every job unless overridden.
	Defaults Defaults `yaml:"defaults"`

	// Integrations maps integration names to provider configurations.
	// Keys are either known provider names ("slack", "pagerduty", "discord",
	// "smtp", "webhook") or arbitrary names when Provider is set explicitly.
	Integrations map[string]*Integration `yaml:"integrations"`

	// Jobs is the set of all job definitions, keyed by job name.
	Jobs map[string]*Job `yaml:"jobs"`
}
