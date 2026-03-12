package config

import (
	"strings"
	"time"
)

const BuiltinDefaultRunTime = "0300"

var weekdayNames = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

// FrequencyAcceptedValues returns a human-readable description of the accepted
// frequency syntax.
func FrequencyAcceptedValues() string {
	return "hourly, daily, weekly, monthly, weekdays, weekends, manual, after:<job>, every:<interval>, on:[day[,day...]]"
}

func normalizeFrequency(freq string) string {
	return strings.ToLower(strings.TrimSpace(freq))
}

// EffectiveDefaultRunTime returns the configured default run time, or Husky's
// built-in fallback when the defaults block leaves it empty.
func EffectiveDefaultRunTime(v string) string {
	if strings.TrimSpace(v) == "" {
		return BuiltinDefaultRunTime
	}
	return strings.TrimSpace(v)
}

// FrequencyUsesTimeField reports whether the frequency participates in
// wall-clock scheduling via the Time field.
func FrequencyUsesTimeField(freq string) bool {
	freq = normalizeFrequency(freq)
	switch freq {
	case "daily", "monthly", "weekly", "weekdays", "weekends":
		return true
	}
	_, ok := ParseOnDaysFrequency(freq)
	return ok
}

// FrequencyAllowsDefaultRunTime reports whether omitted Time values should fall
// back to defaults.default_run_time / BuiltinDefaultRunTime.
func FrequencyAllowsDefaultRunTime(freq string) bool {
	freq = normalizeFrequency(freq)
	switch freq {
	case "weekly", "weekdays", "weekends":
		return true
	}
	_, ok := ParseOnDaysFrequency(freq)
	return ok
}

// FrequencyIgnoresTime reports whether the time field has no effect.
func FrequencyIgnoresTime(freq string) bool {
	freq = normalizeFrequency(freq)
	if freq == "hourly" || freq == "manual" || strings.HasPrefix(freq, "after:") {
		return true
	}
	_, ok := ParseEveryIntervalFrequency(freq)
	return ok
}

// ParseEveryIntervalFrequency parses the every:<interval> frequency form.
// Intervals must be positive and less than 24 hours.
func ParseEveryIntervalFrequency(freq string) (time.Duration, bool) {
	freq = normalizeFrequency(freq)
	if !strings.HasPrefix(freq, "every:") {
		return 0, false
	}
	tail := strings.TrimSpace(strings.TrimPrefix(freq, "every:"))
	if tail == "" || strings.HasPrefix(tail, "[") {
		return 0, false
	}
	d, err := time.ParseDuration(tail)
	if err != nil || d <= 0 || d >= 24*time.Hour {
		return 0, false
	}
	return d, true
}

// ParseOnDaysFrequency parses the on:[day[,day...]] frequency form.
func ParseOnDaysFrequency(freq string) ([]time.Weekday, bool) {
	freq = normalizeFrequency(freq)
	if !strings.HasPrefix(freq, "on:") {
		return nil, false
	}
	tail := strings.TrimSpace(strings.TrimPrefix(freq, "on:"))
	if !strings.HasPrefix(tail, "[") || !strings.HasSuffix(tail, "]") {
		return nil, false
	}
	inner := strings.TrimSpace(tail[1 : len(tail)-1])
	if inner == "" {
		return nil, false
	}

	parts := strings.Split(inner, ",")
	seen := make(map[time.Weekday]bool, len(parts))
	days := make([]time.Weekday, 0, len(parts))
	for _, part := range parts {
		day, ok := weekdayNames[strings.TrimSpace(part)]
		if !ok || seen[day] {
			return nil, false
		}
		seen[day] = true
		days = append(days, day)
	}
	return days, true
}

// ScheduledWeekdays returns the weekdays matched by aliases and on:[...] rules.
func ScheduledWeekdays(freq string) ([]time.Weekday, bool) {
	freq = normalizeFrequency(freq)
	switch freq {
	case "weekly":
		return []time.Weekday{time.Monday}, true
	case "weekdays":
		return []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, true
	case "weekends":
		return []time.Weekday{time.Saturday, time.Sunday}, true
	}
	return ParseOnDaysFrequency(freq)
}

// EffectiveJobTime returns the concrete HHMM time that should be used for a
// job after applying default_run_time semantics.
func EffectiveJobTime(job *Job, defaults Defaults) string {
	if strings.TrimSpace(job.Time) != "" {
		return strings.TrimSpace(job.Time)
	}
	if FrequencyAllowsDefaultRunTime(job.Frequency) {
		return EffectiveDefaultRunTime(defaults.DefaultRunTime)
	}
	return ""
}

// IsValidFrequency returns true if freq is one of the accepted values.
func IsValidFrequency(freq string) bool {
	freq = normalizeFrequency(freq)
	switch freq {
	case "hourly", "daily", "weekly", "monthly", "weekdays", "weekends", "manual":
		return true
	}
	if strings.HasPrefix(freq, "after:") {
		tail := strings.TrimSpace(strings.TrimPrefix(freq, "after:"))
		return tail != ""
	}
	if _, ok := ParseEveryIntervalFrequency(freq); ok {
		return true
	}
	if _, ok := ParseOnDaysFrequency(freq); ok {
		return true
	}
	return false
}
