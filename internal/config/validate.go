package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // embed IANA timezone database into the binary
	"unicode"
)

// validateTime checks that t is a valid 4-character military-time string.
// Returns an error message, or "" if valid.
func validateTime(t string) string {
	if len(t) != 4 {
		return fmt.Sprintf("must be exactly 4 characters (e.g. \"0200\"), got %q", t)
	}
	for _, ch := range t {
		if !unicode.IsDigit(ch) {
			return fmt.Sprintf("must contain only digits, got %q", t)
		}
	}
	hour := (int(t[0]-'0') * 10) + int(t[1]-'0')
	min := (int(t[2]-'0') * 10) + int(t[3]-'0')
	if hour > 23 {
		return fmt.Sprintf("hour %d is out of range (00-23) in %q", hour, t)
	}
	if min > 59 {
		return fmt.Sprintf("minute %d is out of range (00-59) in %q", min, t)
	}
	return ""
}

// validateRetryDelay returns an error message if delay is not a recognised
// retry_delay value, or "" if valid.
func validateRetryDelay(delay string) string {
	if delay == "" || delay == "exponential" {
		return ""
	}
	if strings.HasPrefix(delay, "fixed:") {
		raw := strings.TrimPrefix(delay, "fixed:")
		if _, err := time.ParseDuration(raw); err != nil {
			return fmt.Sprintf("fixed:<duration> has invalid duration %q: %v", raw, err)
		}
		return ""
	}
	return fmt.Sprintf("must be \"exponential\" or \"fixed:<duration>\", got %q", delay)
}

// validateTimezone returns an error message if tz is not a valid IANA timezone
// identifier, or "" if valid. An empty string is always valid (means "use
// default/system timezone"). Uses the embedded time/tzdata database.
func validateTimezone(tz string) string {
	if tz == "" {
		return ""
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Sprintf("unknown IANA timezone identifier %q", tz)
	}
	return ""
}

// validateSLAvsTimeout checks that sla < timeout when both are non-empty.
// Returns an error message if the constraint is violated, or "" if valid.
func validateSLAvsTimeout(sla, timeout string) string {
	if sla == "" || timeout == "" {
		return ""
	}
	slaDur, err := time.ParseDuration(sla)
	if err != nil {
		return fmt.Sprintf("sla has invalid duration %q: %v", sla, err)
	}
	timeoutDur, err := time.ParseDuration(timeout)
	if err != nil {
		// timeout format is validated separately; skip the comparison.
		return ""
	}
	if slaDur >= timeoutDur {
		return fmt.Sprintf(
			"sla %q must be less than timeout %q (sla >= timeout is not allowed)",
			sla, timeout)
	}
	return ""
}

// tagPattern matches a valid tag: lowercase alphanumeric and hyphens only,
// must start with an alphanumeric character.
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validateTags checks the tags slice against all tag constraints. Returns a
// slice of error messages (one per violation), or nil if all tags are valid.
func validateTags(tags []string) []string {
	const maxTags = 10
	const maxTagLen = 32

	var msgs []string
	if len(tags) > maxTags {
		msgs = append(msgs, fmt.Sprintf(
			"too many tags (%d); maximum is %d", len(tags), maxTags))
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if seen[tag] {
			msgs = append(msgs, fmt.Sprintf("duplicate tag %q", tag))
			continue
		}
		seen[tag] = true
		if len(tag) > maxTagLen {
			msgs = append(msgs, fmt.Sprintf(
				"tag %q exceeds maximum length of %d characters", tag, maxTagLen))
			continue
		}
		if !tagPattern.MatchString(tag) {
			msgs = append(msgs, fmt.Sprintf(
				"tag %q is invalid: tags must be lowercase alphanumeric with hyphens (e.g. \"data-pipeline\")", tag))
		}
	}
	return msgs
}

// outputCaptureModes lists the fixed valid capture mode tokens.
var outputCaptureModes = map[string]bool{
	"last_line":  true,
	"first_line": true,
	"exit_code":  true,
}

// validateOutputCaptureMode returns an error message if mode is not a
// recognised output capture mode, or "" if valid.
// Valid forms: last_line | first_line | exit_code | json_field:<key> | regex:<pattern>
func validateOutputCaptureMode(mode string) string {
	if outputCaptureModes[mode] {
		return ""
	}
	if strings.HasPrefix(mode, "json_field:") {
		key := strings.TrimPrefix(mode, "json_field:")
		if strings.TrimSpace(key) == "" {
			return "json_field: capture mode requires a non-empty key (e.g. \"json_field:total\")"
		}
		return ""
	}
	if strings.HasPrefix(mode, "regex:") {
		pattern := strings.TrimPrefix(mode, "regex:")
		if strings.TrimSpace(pattern) == "" {
			return "regex: capture mode requires a non-empty pattern (e.g. \"regex:v(\\d+)\")"
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Sprintf("regex: capture mode has invalid pattern %q: %v", pattern, err)
		}
		return ""
	}
	return fmt.Sprintf(
		"unrecognised output capture mode %q; accepted: last_line, first_line, exit_code, json_field:<key>, regex:<pattern>",
		mode)
}

// validateConfig runs semantic validation over a fully-parsed Config. It
// populates errs with every problem found so that all failures surface at once.
func validateConfig(cfg *Config, errs *ParseError) {
	if cfg.Version == "" {
		errs.addTop("version", "field is required")
	} else if cfg.Version != "1" {
		errs.addTop("version", fmt.Sprintf("unsupported version %q; only \"1\" is valid", cfg.Version))
	}

	if len(cfg.Jobs) == 0 {
		errs.addTop("jobs", "at least one job must be defined")
	}

	if msg := validateRetryDelay(cfg.Defaults.RetryDelay); msg != "" {
		errs.addTop("defaults.retry_delay", msg)
	}

	if msg := validateTime(EffectiveDefaultRunTime(cfg.Defaults.DefaultRunTime)); cfg.Defaults.DefaultRunTime != "" && msg != "" {
		errs.addTop("defaults.default_run_time", msg)
	}

	if msg := validateTimezone(cfg.Defaults.Timezone); msg != "" {
		errs.addTop("defaults.timezone", msg)
	}

	for name, job := range cfg.Jobs {
		validateJob(name, job, cfg.Defaults, errs)
	}

	for name, intg := range cfg.Integrations {
		validateIntegration(name, intg, errs)
	}
}

// knownProviders lists the provider names that are inferred from the map key
// when no explicit "provider" field is set.
var knownProviders = map[string]bool{
	"slack":      true,
	"pagerduty":  true,
	"discord":    true,
	"smtp":       true,
	"webhook":    true,
}

// validateIntegration validates a single integration entry.
// It sets EffectiveProvider as a side-effect when validation passes.
func validateIntegration(name string, intg *Integration, errs *ParseError) {
	add := func(field, msg string) {
		errs.addTop(fmt.Sprintf("integrations.%s.%s", name, field), msg)
	}

	// Resolve provider — infer from key OR use explicit provider field.
	effective := intg.Provider
	if effective == "" {
		if !knownProviders[name] {
			add("provider",
				fmt.Sprintf("integration %q uses an unknown key; set the \"provider\" field explicitly (accepted: slack, pagerduty, discord, smtp, webhook)", name))
			return
		}
		effective = name
	} else if !knownProviders[effective] {
		add("provider",
			fmt.Sprintf("unsupported provider %q; accepted: slack, pagerduty, discord, smtp, webhook", effective))
		return
	}
	intg.EffectiveProvider = effective

	switch effective {
	case "slack", "discord", "webhook":
		if strings.TrimSpace(intg.WebhookURL) == "" {
			add("webhook_url",
				fmt.Sprintf("field is required for provider %q", effective))
		}
	case "pagerduty":
		if strings.TrimSpace(intg.RoutingKey) == "" {
			add("routing_key", "field is required for provider \"pagerduty\"")
		}
	case "smtp":
		if strings.TrimSpace(intg.Host) == "" {
			add("host", "field is required for provider \"smtp\"")
		}
		if strings.TrimSpace(intg.From) == "" {
			add("from", "field is required for provider \"smtp\"")
		}
		if intg.Port != 0 && (intg.Port < 1 || intg.Port > 65535) {
			add("port",
				fmt.Sprintf("port %d is out of range (1-65535)", intg.Port))
		}
	}
}

// validateJob validates a single job definition.
func validateJob(name string, job *Job, defaults Defaults, errs *ParseError) {
	add := func(field, msg string) { errs.add(name, field, msg) }

	// ── Required fields ───────────────────────────────────────────────────

	if strings.TrimSpace(job.Description) == "" {
		add("description", "field is required and must not be blank")
	}

	if strings.TrimSpace(job.Command) == "" {
		add("command", "field is required and must not be blank")
	}

	// ── Frequency ─────────────────────────────────────────────────────────

	frequencyOK := job.Frequency != "" && IsValidFrequency(job.Frequency)
	if job.Frequency == "" {
		add("frequency", "field is required")
	} else if !frequencyOK {
		add("frequency", fmt.Sprintf(
			"unrecognised value %q; accepted: %s",
			job.Frequency, FrequencyAcceptedValues()))
	}

	// ── Time (conditional) — only validated when frequency is known-valid ─
	// Skipped entirely when frequency is invalid to avoid spurious cascade
	// errors such as "time: field is required when frequency is 'hourl'".

	if frequencyOK {
		if FrequencyUsesTimeField(job.Frequency) {
			effectiveTime := EffectiveJobTime(job, defaults)
			requiresExplicitTime := !FrequencyAllowsDefaultRunTime(job.Frequency)

			if effectiveTime == "" && requiresExplicitTime {
				add("time", fmt.Sprintf(
					"field is required when frequency is %q", job.Frequency))
			} else if job.Time != "" {
				if msg := validateTime(job.Time); msg != "" {
					add("time", msg)
				}
			} else if effectiveTime != "" {
				if msg := validateTime(effectiveTime); msg != "" {
					add("time", msg)
				}
			}
		} else if job.Time != "" {
			// Time is set but will be ignored — warn via validation so the user
			// knows the value has no effect.
			if FrequencyIgnoresTime(job.Frequency) {
				add("time", fmt.Sprintf(
					"field is ignored when frequency is %q and should be removed",
					job.Frequency))
			}
		}
	}

	// ── Optional enum fields ──────────────────────────────────────────────

	if job.OnFailure != "" {
		switch job.OnFailure {
		case "alert", "skip", "stop", "ignore":
			// valid
		default:
			add("on_failure", fmt.Sprintf(
				"unrecognised value %q; accepted: alert, skip, stop, ignore",
				job.OnFailure))
		}
	}

	if job.Concurrency != "" {
		switch job.Concurrency {
		case "allow", "forbid", "replace":
			// valid
		default:
			add("concurrency", fmt.Sprintf(
				"unrecognised value %q; accepted: allow, forbid, replace",
				job.Concurrency))
		}
	}

	if msg := validateRetryDelay(job.RetryDelay); msg != "" {
		add("retry_delay", msg)
	}

	// ── depends_on self-reference ─────────────────────────────────────────

	for _, dep := range job.DependsOn {
		if dep == name {
			add("depends_on", fmt.Sprintf("job %q cannot depend on itself", name))
		}
	}

	// ── timezone ──────────────────────────────────────────────────────────

	if msg := validateTimezone(job.Timezone); msg != "" {
		add("timezone", msg)
	}

	// ── sla ───────────────────────────────────────────────────────────────

	if job.SLA != "" {
		if _, err := time.ParseDuration(job.SLA); err != nil {
			add("sla", fmt.Sprintf("invalid duration %q: %v", job.SLA, err))
		} else if msg := validateSLAvsTimeout(job.SLA, job.Timeout); msg != "" {
			add("sla", msg)
		}
	}

	// ── tags ──────────────────────────────────────────────────────────────

	for _, msg := range validateTags(job.Tags) {
		add("tags", msg)
	}

	// ── healthcheck ───────────────────────────────────────────────────────

	if job.Healthcheck != nil {
		hc := job.Healthcheck
		if strings.TrimSpace(hc.Command) == "" {
			add("healthcheck.command", "field is required when healthcheck block is present")
		}
		if hc.Timeout != "" {
			if _, err := time.ParseDuration(hc.Timeout); err != nil {
				add("healthcheck.timeout", fmt.Sprintf("invalid duration %q: %v", hc.Timeout, err))
			}
		}
		if hc.OnFail != "" {
			switch hc.OnFail {
			case "mark_failed", "warn_only":
				// valid
			default:
				add("healthcheck.on_fail", fmt.Sprintf(
					"unrecognised value %q; accepted: mark_failed, warn_only", hc.OnFail))
			}
		}
	}

	// ── output ────────────────────────────────────────────────────────────

	for varName, mode := range job.Output {
		if strings.TrimSpace(varName) == "" {
			add("output", "variable name must not be blank")
			continue
		}
		if msg := validateOutputCaptureMode(mode); msg != "" {
			add(fmt.Sprintf("output.%s", varName), msg)
		}
	}
}
