package config

// applyDefaults fills in any unset optional fields on each job using the
// values from cfg.Defaults. A field is considered "unset" when it holds its
// Go zero value: "" for strings, nil for *int, and false for bool.
func applyDefaults(cfg *Config) {
	d := &cfg.Defaults
	d.DefaultRunTime = EffectiveDefaultRunTime(d.DefaultRunTime)

	for _, job := range cfg.Jobs {
		if job.Timeout == "" && d.Timeout != "" {
			job.Timeout = d.Timeout
		}
		if job.Retries == nil && d.Retries != nil {
			v := *d.Retries
			job.Retries = &v
		}
		if job.RetryDelay == "" && d.RetryDelay != "" {
			job.RetryDelay = d.RetryDelay
		}
		if job.Timezone == "" && d.Timezone != "" {
			job.Timezone = d.Timezone
		}
		if job.OnFailure == "" && d.OnFailure != "" {
			job.OnFailure = d.OnFailure
		}
		if job.Time == "" && FrequencyAllowsDefaultRunTime(job.Frequency) {
			job.Time = d.DefaultRunTime
		}
	}
}
