// Package scheduler provides job scheduling logic for Husky.
package scheduler

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/husky-scheduler/husky/internal/config"
)

// DSTAnomaly describes a Daylight Saving Time gap or overlap encountered when
// computing the next run time for a job.
type DSTAnomaly struct {
	JobName   string
	Requested string // wall-clock time requested, e.g. "02:30"
	Actual    string // wall-clock time actually used, e.g. "03:30"
	Kind      string // "gap" or "overlap"
	Zone      string // timezone name, e.g. "America/New_York"
}

// String returns a human-readable description of the anomaly.
func (a DSTAnomaly) String() string {
	if a.Kind == "gap" {
		return fmt.Sprintf(
			"job %q: DST gap in %s — wall-clock %s does not exist, running at %s instead",
			a.JobName, a.Zone, a.Requested, a.Actual,
		)
	}
	return fmt.Sprintf(
		"job %q: DST overlap in %s — wall-clock %s occurs twice, running on first occurrence (%s)",
		a.JobName, a.Zone, a.Requested, a.Actual,
	)
}

// ResolveLocation returns the *time.Location for a job using the precedence:
// job.Timezone → defaults.Timezone → system local timezone.
func ResolveLocation(job *config.Job, defaults config.Defaults) *time.Location {
	for _, tz := range []string{job.Timezone, defaults.Timezone} {
		if tz == "" {
			continue
		}
		loc, err := time.LoadLocation(tz)
		if err == nil {
			return loc
		}
	}
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}

// NextRunTime computes the next trigger time for job relative to now.
// For manual and after:<job> frequencies, it returns the zero time (never).
// It also returns a *DSTAnomaly when the scheduled wall-clock time falls in a
// DST gap or overlap.
func NextRunTime(job *config.Job, defaults config.Defaults, now time.Time) (time.Time, *DSTAnomaly) {
	freq := strings.ToLower(strings.TrimSpace(job.Frequency))

	// Non-scheduled frequencies:
	if freq == "manual" || strings.HasPrefix(freq, "after:") {
		return time.Time{}, nil
	}

	if freq == "hourly" {
		return nextHourly(now), nil
	}

	if interval, ok := config.ParseEveryIntervalFrequency(freq); ok {
		return nextInterval(now, interval), nil
	}

	loc := ResolveLocation(job, defaults)
	h, m := parseTimeField(config.EffectiveJobTime(job, defaults))
	return nextScheduled(job.Name, freq, h, m, loc, now)
}

// StartupSummary returns a one-line human-readable string describing when a job
// will next run, suitable for logging at daemon startup.
func StartupSummary(name string, job *config.Job, defaults config.Defaults, now time.Time) string {
	freq := strings.ToLower(strings.TrimSpace(job.Frequency))

	if freq == "manual" {
		return fmt.Sprintf("job %q: frequency=manual — will not run automatically", name)
	}
	if strings.HasPrefix(freq, "after:") {
		dep := strings.TrimPrefix(freq, "after:")
		return fmt.Sprintf("job %q: frequency=%s — runs when %q completes", name, freq, dep)
	}
	if interval, ok := config.ParseEveryIntervalFrequency(freq); ok {
		t, _ := NextRunTime(job, defaults, now)
		return fmt.Sprintf(
			"job %q: frequency=%s — next run in %s at %s UTC",
			name, freq, interval,
			t.UTC().Format("2006-01-02 15:04:05"),
		)
	}

	t, _ := NextRunTime(job, defaults, now)
	if t.IsZero() {
		return fmt.Sprintf("job %q: next run unknown", name)
	}

	if freq == "hourly" {
		return fmt.Sprintf("job %q: frequency=hourly — next run at %s UTC", name, t.UTC().Format("2006-01-02 15:04:05"))
	}

	loc := ResolveLocation(job, defaults)
	local := t.In(loc)
	_, offset := local.Zone()
	abbr, _ := local.Zone()
	_ = offset
	return fmt.Sprintf(
		"job %q: frequency=%s — next run at %s %s (%s UTC)",
		name, freq,
		local.Format("2006-01-02 15:04:05"), abbr,
		t.UTC().Format("2006-01-02 15:04:05"),
	)
}

// nextHourly returns the next top-of-the-hour in UTC after now.
func nextHourly(now time.Time) time.Time {
	return now.UTC().Truncate(time.Hour).Add(time.Hour)
}

// nextInterval returns the next aligned interval boundary after now.
func nextInterval(now time.Time, interval time.Duration) time.Time {
	return now.UTC().Truncate(interval).Add(interval)
}

// nextScheduled finds the next time satisfying freq at HH:MM in loc, scanning
// up to 40 days ahead.
func nextScheduled(jobName, freq string, h, m int, loc *time.Location, now time.Time) (time.Time, *DSTAnomaly) {
	base := now.In(loc)
	for offset := 0; offset <= 40; offset++ {
		day := base.AddDate(0, 0, offset)
		y, mo, d := day.Date()

		if !dayMatches(freq, d, day.Weekday()) {
			continue
		}

		t := time.Date(y, mo, d, h, m, 0, 0, loc)

		// On some DST spring-forward transitions, Go may normalize a non-existent
		// local wall-clock (e.g. 02:30) backward (e.g. 01:30). In that case,
		// move forward by the difference so the run lands after the gap on the
		// same calendar day rather than skipping to the next day.
		local := t.In(loc)
		if local.Year() == y && local.Month() == mo && local.Day() == d {
			requestedMinutes := h*60 + m
			actualMinutes := local.Hour()*60 + local.Minute()
			if actualMinutes < requestedMinutes {
				t = t.Add(time.Duration(requestedMinutes-actualMinutes) * time.Minute)
			}
		}

		if !t.After(now) {
			continue
		}

		anomaly := detectDSTAnomaly(jobName, t, h, m, loc)
		return t, anomaly
	}
	// Fallback: return zero (should not happen for valid frequencies).
	return time.Time{}, nil
}

// dayMatches reports whether a day with the given monthDay and weekday satisfies freq.
func dayMatches(freq string, monthDay int, weekday time.Weekday) bool {
	switch freq {
	case "daily":
		return true
	case "monthly":
		return monthDay == 1
	}
	if days, ok := config.ScheduledWeekdays(freq); ok {
		for _, d := range days {
			if weekday == d {
				return true
			}
		}
	}
	return false
}

// detectDSTAnomaly checks whether t falls in a DST gap or overlap in loc.
func detectDSTAnomaly(jobName string, t time.Time, h, m int, loc *time.Location) *DSTAnomaly {
	local := t.In(loc)
	actualH, actualM, _ := local.Clock()
	requested := fmt.Sprintf("%02d:%02d", h, m)

	// Gap: Go normalises gap times forward; the actual wall clock differs.
	if actualH != h || actualM != m {
		actual := fmt.Sprintf("%02d:%02d", actualH, actualM)
		return &DSTAnomaly{
			JobName:   jobName,
			Requested: requested,
			Actual:    actual,
			Kind:      "gap",
			Zone:      loc.String(),
		}
	}

	// Overlap: the same wall-clock time also exists one hour later.
	// Go's time.Date always returns the first occurrence; we just warn.
	candidate := t.Add(time.Hour)
	candLocal := candidate.In(loc)
	ch, cm, _ := candLocal.Clock()
	if ch == h && cm == m {
		actual := local.Format("15:04 MST")
		return &DSTAnomaly{
			JobName:   jobName,
			Requested: requested,
			Actual:    actual,
			Kind:      "overlap",
			Zone:      loc.String(),
		}
	}

	return nil
}

// parseTimeField parses a 4-char military time string (e.g. "0930") into
// integer hour and minute components. Returns 0, 0 for any malformed input.
func parseTimeField(t string) (h, m int) {
	if len(t) != 4 {
		return 0, 0
	}
	h = int(t[0]-'0')*10 + int(t[1]-'0')
	m = int(t[2]-'0')*10 + int(t[3]-'0')
	return h, m
}
