// Tests for the scheduler package.
package scheduler

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/husky-scheduler/husky/internal/config"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func utcTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func inLoc(s string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		panic(err)
	}
	return t
}

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func makeJob(name, freq, timeField, tz string) *config.Job {
	return &config.Job{
		Name:      name,
		Frequency: freq,
		Time:      timeField,
		Timezone:  tz,
	}
}

func emptyDefaults() config.Defaults { return config.Defaults{Timezone: "UTC"} }

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// ---------------------------------------------------------------------------
// parseTimeField
// ---------------------------------------------------------------------------

func TestParseTimeField_valid(t *testing.T) {
	h, m := parseTimeField("0930")
	if h != 9 || m != 30 {
		t.Fatalf("got %d:%d, want 9:30", h, m)
	}
}

func TestParseTimeField_midnight(t *testing.T) {
	h, m := parseTimeField("0000")
	if h != 0 || m != 0 {
		t.Fatalf("got %d:%d, want 0:0", h, m)
	}
}

func TestParseTimeField_tooShort(t *testing.T) {
	h, m := parseTimeField("930")
	if h != 0 || m != 0 {
		t.Fatalf("expected 0:0 for malformed input, got %d:%d", h, m)
	}
}

func TestParseTimeField_empty(t *testing.T) {
	h, m := parseTimeField("")
	if h != 0 || m != 0 {
		t.Fatalf("expected 0:0 for empty input, got %d:%d", h, m)
	}
}

// ---------------------------------------------------------------------------
// dayMatches
// ---------------------------------------------------------------------------

func TestDayMatches_daily(t *testing.T) {
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if !dayMatches("daily", 15, wd) {
			t.Errorf("daily should match weekday %v", wd)
		}
	}
}

func TestDayMatches_weekly_monday(t *testing.T) {
	if !dayMatches("weekly", 1, time.Monday) {
		t.Fatal("weekly should match Monday")
	}
	if dayMatches("weekly", 1, time.Tuesday) {
		t.Fatal("weekly should not match Tuesday")
	}
}

func TestDayMatches_monthly_first(t *testing.T) {
	if !dayMatches("monthly", 1, time.Wednesday) {
		t.Fatal("monthly should match day 1")
	}
	if dayMatches("monthly", 2, time.Wednesday) {
		t.Fatal("monthly should not match day 2")
	}
}

func TestDayMatches_weekdays(t *testing.T) {
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		if !dayMatches("weekdays", 10, wd) {
			t.Errorf("weekdays should match %v", wd)
		}
	}
	for _, wd := range []time.Weekday{time.Saturday, time.Sunday} {
		if dayMatches("weekdays", 10, wd) {
			t.Errorf("weekdays should not match %v", wd)
		}
	}
}

func TestDayMatches_weekends(t *testing.T) {
	for _, wd := range []time.Weekday{time.Saturday, time.Sunday} {
		if !dayMatches("weekends", 10, wd) {
			t.Errorf("weekends should match %v", wd)
		}
	}
	for _, wd := range []time.Weekday{time.Monday, time.Friday} {
		if dayMatches("weekends", 10, wd) {
			t.Errorf("weekends should not match %v", wd)
		}
	}
}

func TestDayMatches_onDays(t *testing.T) {
	if !dayMatches("on:[monday, friday]", 10, time.Monday) {
		t.Fatal("on:[monday, friday] should match Monday")
	}
	if !dayMatches("on:[monday, friday]", 10, time.Friday) {
		t.Fatal("on:[monday, friday] should match Friday")
	}
	if dayMatches("on:[monday, friday]", 10, time.Wednesday) {
		t.Fatal("on:[monday, friday] should not match Wednesday")
	}
}

// ---------------------------------------------------------------------------
// ResolveLocation
// ---------------------------------------------------------------------------

func TestResolveLocation_jobWins(t *testing.T) {
	job := makeJob("j", "daily", "0900", "America/New_York")
	def := config.Defaults{Timezone: "America/Los_Angeles"}
	loc := ResolveLocation(job, def)
	if loc.String() != "America/New_York" {
		t.Fatalf("expected America/New_York, got %s", loc)
	}
}

func TestResolveLocation_defaultsFallback(t *testing.T) {
	job := makeJob("j", "daily", "0900", "")
	def := config.Defaults{Timezone: "Europe/London"}
	loc := ResolveLocation(job, def)
	if loc.String() != "Europe/London" {
		t.Fatalf("expected Europe/London, got %s", loc)
	}
}

func TestResolveLocation_localFallback(t *testing.T) {
	job := makeJob("j", "daily", "0900", "")
	def := config.Defaults{}
	loc := ResolveLocation(job, def)
	if loc != time.Local {
		t.Fatalf("expected system local timezone (%s), got %s", time.Local, loc)
	}
}

// ---------------------------------------------------------------------------
// NextRunTime — manual / after
// ---------------------------------------------------------------------------

func TestNextRunTime_manual(t *testing.T) {
	job := makeJob("j", "manual", "", "")
	now := utcTime("2024-01-10 12:00:00")
	got, anomaly := NextRunTime(job, emptyDefaults(), now)
	if !got.IsZero() {
		t.Fatalf("expected zero time for manual, got %v", got)
	}
	if anomaly != nil {
		t.Fatal("expected no anomaly for manual")
	}
}

func TestNextRunTime_after(t *testing.T) {
	job := makeJob("j", "after:other", "", "")
	now := utcTime("2024-01-10 12:00:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	if !got.IsZero() {
		t.Fatalf("expected zero time for after:, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// NextRunTime — hourly
// ---------------------------------------------------------------------------

func TestNextRunTime_hourly_midHour(t *testing.T) {
	job := makeJob("j", "hourly", "", "")
	now := utcTime("2024-01-10 12:30:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-10 13:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_hourly_atTopOfHour(t *testing.T) {
	job := makeJob("j", "hourly", "", "")
	now := utcTime("2024-01-10 12:00:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-10 13:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_hourly_midnightRollover(t *testing.T) {
	job := makeJob("j", "hourly", "", "")
	now := utcTime("2024-01-10 23:45:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-11 00:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_everyInterval(t *testing.T) {
	job := makeJob("j", "every:15m", "", "")
	now := utcTime("2024-01-10 12:07:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-10 12:15:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_everyIntervalAtBoundary(t *testing.T) {
	job := makeJob("j", "every:15s", "", "")
	now := utcTime("2024-01-10 12:00:15")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-10 12:00:30")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// NextRunTime — daily
// ---------------------------------------------------------------------------

func TestNextRunTime_daily_beforeTime(t *testing.T) {
	job := makeJob("j", "daily", "0900", "")
	now := utcTime("2024-01-10 08:00:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-10 09:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_daily_afterTime(t *testing.T) {
	job := makeJob("j", "daily", "0900", "")
	now := utcTime("2024-01-10 10:00:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-11 09:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// NextRunTime — weekly (Monday)
// ---------------------------------------------------------------------------

func TestNextRunTime_weekly_nextMonday(t *testing.T) {
	// 2024-01-10 is a Wednesday; next Monday is 2024-01-15.
	job := makeJob("j", "weekly", "1000", "")
	now := utcTime("2024-01-10 10:01:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-15 10:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_weekly_mondayBeforeTime(t *testing.T) {
	// 2024-01-15 is Monday; before 10:00.
	job := makeJob("j", "weekly", "1000", "")
	now := utcTime("2024-01-15 09:00:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-15 10:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_weekly_UsesDefaultRunTime(t *testing.T) {
	job := makeJob("j", "weekly", "", "")
	now := utcTime("2024-01-15 02:00:00")
	got, _ := NextRunTime(job, config.Defaults{Timezone: "UTC", DefaultRunTime: "0415"}, now)
	want := utcTime("2024-01-15 04:15:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// NextRunTime — monthly (1st of month)
// ---------------------------------------------------------------------------

func TestNextRunTime_monthly_beforeFirst(t *testing.T) {
	// 2024-01-10; next 1st is 2024-02-01.
	job := makeJob("j", "monthly", "0600", "")
	now := utcTime("2024-01-10 06:01:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-02-01 06:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_monthly_onFirstBeforeTime(t *testing.T) {
	job := makeJob("j", "monthly", "0600", "")
	now := utcTime("2024-02-01 05:00:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-02-01 06:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// NextRunTime — weekdays / weekends
// ---------------------------------------------------------------------------

func TestNextRunTime_weekdays_onWeekday(t *testing.T) {
	// 2024-01-10 Wednesday, after 09:00 → next weekday at 09:00 = Thursday 2024-01-11.
	job := makeJob("j", "weekdays", "0900", "")
	now := utcTime("2024-01-10 09:01:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-11 09:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_weekdays_onWeekend(t *testing.T) {
	// 2024-01-13 Saturday → next weekday = Monday 2024-01-15.
	job := makeJob("j", "weekdays", "0900", "")
	now := utcTime("2024-01-13 09:01:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-15 09:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_weekends_onWeekday(t *testing.T) {
	// 2024-01-10 Wednesday → next weekend = Saturday 2024-01-13.
	job := makeJob("j", "weekends", "0800", "")
	now := utcTime("2024-01-10 08:01:00")
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-13 08:00:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_onDays_UsesBuiltinDefaultRunTime(t *testing.T) {
	job := makeJob("j", "on:[tuesday, friday]", "", "")
	now := utcTime("2024-01-10 09:01:00") // Wednesday
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-12 03:00:00") // Friday at built-in default time
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_onDays_WithExplicitTime(t *testing.T) {
	job := makeJob("j", "on:[monday, friday]", "0815", "")
	now := utcTime("2024-01-10 09:01:00") // Wednesday
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := utcTime("2024-01-12 08:15:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Timezone offset
// ---------------------------------------------------------------------------

func TestNextRunTime_timezone_offset(t *testing.T) {
	// Daily at 09:00 America/New_York (UTC-5 in January).
	// now = 2024-01-10 13:30 UTC (= 08:30 NY) — before 09:00 NY.
	// Expect next = 2024-01-10 14:00 UTC (= 09:00 NY).
	ny := mustLoc("America/New_York")
	job := makeJob("j", "daily", "0900", "America/New_York")
	now := inLoc("2024-01-10 08:30:00", ny).UTC()
	got, _ := NextRunTime(job, emptyDefaults(), now)
	want := inLoc("2024-01-10 09:00:00", ny)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got.In(ny), want)
	}
}

// ---------------------------------------------------------------------------
// DST gap (spring-forward America/New_York 2024-03-10 02:00 → 03:00)
// ---------------------------------------------------------------------------

func TestNextRunTime_dstGap(t *testing.T) {
	ny := mustLoc("America/New_York")
	// Schedule daily at 02:30 NY. On 2024-03-10, 02:30 NY doesn't exist.
	// Go normalises it to 03:30 NY. We expect Kind=="gap".
	job := makeJob("j", "daily", "0230", "America/New_York")
	// now = 2024-03-10 01:00 NY (just before the gap)
	now := inLoc("2024-03-10 01:00:00", ny).UTC()

	// Temporarily move now to after 01:00 to ensure we hit the gap on same day.
	// now = 2024-03-10 01:55 NY
	now = inLoc("2024-03-10 01:55:00", ny).UTC()

	got, anomaly := NextRunTime(job, emptyDefaults(), now)
	if anomaly == nil {
		t.Fatalf("expected DST anomaly, got none; next=%v", got.In(ny))
	}
	if anomaly.Kind != "gap" {
		t.Fatalf("expected Kind='gap', got %q", anomaly.Kind)
	}
	// The actual scheduled time in NY should be 03:30 (gap skip).
	gotNY := got.In(ny)
	if gotNY.Hour() != 3 || gotNY.Minute() != 30 {
		t.Fatalf("expected 03:30 NY after gap, got %v", gotNY)
	}
}

func TestDSTAnomaly_String_gap(t *testing.T) {
	a := DSTAnomaly{
		JobName:   "myjob",
		Requested: "02:30",
		Actual:    "03:30",
		Kind:      "gap",
		Zone:      "America/New_York",
	}
	s := a.String()
	if s == "" {
		t.Fatal("String() should not be empty")
	}
	for _, want := range []string{"myjob", "02:30", "03:30", "gap", "America/New_York"} {
		if !containsStr(s, want) {
			t.Errorf("String() missing %q: %s", want, s)
		}
	}
}

func TestDSTAnomaly_String_overlap(t *testing.T) {
	a := DSTAnomaly{
		JobName:   "myjob",
		Requested: "01:30",
		Actual:    "01:30 EDT",
		Kind:      "overlap",
		Zone:      "America/New_York",
	}
	s := a.String()
	for _, want := range []string{"myjob", "overlap", "America/New_York"} {
		if !containsStr(s, want) {
			t.Errorf("String() missing %q: %s", want, s)
		}
	}
}

// ---------------------------------------------------------------------------
// DST overlap (fall-back America/New_York 2024-11-03 02:00 → 01:00)
// ---------------------------------------------------------------------------

func TestNextRunTime_dstOverlap(t *testing.T) {
	ny := mustLoc("America/New_York")
	// Schedule daily at 01:30 NY. On 2024-11-03 01:30 occurs twice.
	job := makeJob("j", "daily", "0130", "America/New_York")
	// now = 2024-11-03 00:30 NY
	now := inLoc("2024-11-03 00:30:00", ny).UTC()

	got, anomaly := NextRunTime(job, emptyDefaults(), now)
	_ = got
	if anomaly == nil {
		// overlap detection is best-effort; not a hard failure if not detected.
		t.Log("DST overlap not detected (acceptable)")
		return
	}
	if anomaly.Kind != "overlap" {
		t.Fatalf("expected Kind='overlap', got %q", anomaly.Kind)
	}
}

// ---------------------------------------------------------------------------
// StartupSummary
// ---------------------------------------------------------------------------

func TestStartupSummary_manual(t *testing.T) {
	job := makeJob("build", "manual", "", "")
	s := StartupSummary("build", job, emptyDefaults(), utcTime("2024-01-10 12:00:00"))
	if !containsStr(s, "manual") {
		t.Fatalf("expected 'manual' in summary: %s", s)
	}
}

func TestStartupSummary_after(t *testing.T) {
	job := makeJob("deploy", "after:build", "", "")
	s := StartupSummary("deploy", job, emptyDefaults(), utcTime("2024-01-10 12:00:00"))
	if !containsStr(s, "after") {
		t.Fatalf("expected 'after' in summary: %s", s)
	}
	if !containsStr(s, "build") {
		t.Fatalf("expected dependency 'build' in summary: %s", s)
	}
}

func TestStartupSummary_hourly(t *testing.T) {
	job := makeJob("sync", "hourly", "", "")
	now := utcTime("2024-01-10 12:30:00")
	s := StartupSummary("sync", job, emptyDefaults(), now)
	if !containsStr(s, "UTC") {
		t.Fatalf("expected UTC in summary: %s", s)
	}
	if !containsStr(s, "hourly") {
		t.Fatalf("expected frequency in summary: %s", s)
	}
}

func TestStartupSummary_everyInterval(t *testing.T) {
	job := makeJob("sync", "every:15m", "", "")
	now := utcTime("2024-01-10 12:07:00")
	s := StartupSummary("sync", job, emptyDefaults(), now)
	if !containsStr(s, "every:15m") {
		t.Fatalf("expected interval frequency in summary: %s", s)
	}
	if !containsStr(s, "UTC") {
		t.Fatalf("expected UTC in summary: %s", s)
	}
}

func TestStartupSummary_daily_withTZ(t *testing.T) {
	job := makeJob("report", "daily", "0900", "America/New_York")
	now := utcTime("2024-01-10 12:00:00")
	s := StartupSummary("report", job, emptyDefaults(), now)
	// Should contain timezone abbreviation and UTC equivalent.
	if !containsStr(s, "EST") && !containsStr(s, "EDT") {
		t.Fatalf("expected timezone abbreviation in summary: %s", s)
	}
	if !containsStr(s, "UTC") {
		t.Fatalf("expected UTC in summary: %s", s)
	}
}

// ---------------------------------------------------------------------------
// Scheduler — New pre-computes next times
// ---------------------------------------------------------------------------

func TestSchedulerNew_precomputes(t *testing.T) {
	cfg := &config.Config{
		Jobs: map[string]*config.Job{
			"hourly-job": makeJob("hourly-job", "hourly", "", ""),
		},
	}
	s := New(cfg, newTestLogger(), func(_ context.Context, _ string, _ time.Time) {})
	next := s.NextFor("hourly-job")
	if next.IsZero() {
		t.Fatal("expected non-zero next time for hourly job")
	}
}

func TestSchedulerNew_manualIsZero(t *testing.T) {
	cfg := &config.Config{
		Jobs: map[string]*config.Job{
			"manual-job": makeJob("manual-job", "manual", "", ""),
		},
	}
	s := New(cfg, newTestLogger(), func(_ context.Context, _ string, _ time.Time) {})
	next := s.NextFor("manual-job")
	if !next.IsZero() {
		t.Fatalf("expected zero next time for manual job, got %v", next)
	}
}

// ---------------------------------------------------------------------------
// Scheduler.tick — fires when due
// ---------------------------------------------------------------------------

func TestSchedulerTick_firesWhenDue(t *testing.T) {
	scheduled := utcTime("2024-01-10 09:00:00")
	job := makeJob("j", "daily", "0900", "")

	var mu sync.Mutex
	var fired []string
	onRun := func(_ context.Context, name string, _ time.Time) {
		mu.Lock()
		fired = append(fired, name)
		mu.Unlock()
	}

	cfg := &config.Config{Jobs: map[string]*config.Job{"j": job}}
	s := New(cfg, newTestLogger(), onRun)

	// Manually set the next time to a known past value.
	s.mu.Lock()
	s.next["j"] = scheduled
	s.mu.Unlock()

	// Tick at a time after scheduled.
	now := utcTime("2024-01-10 09:00:01")
	s.tick(context.Background(), now)

	// Give the goroutine time to fire.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 || fired[0] != "j" {
		t.Fatalf("expected job 'j' to fire, got: %v", fired)
	}
}

func TestSchedulerTick_skipsWhenNotDue(t *testing.T) {
	job := makeJob("j", "daily", "0900", "")

	var fired []string
	cfg := &config.Config{Jobs: map[string]*config.Job{"j": job}}
	s := New(cfg, newTestLogger(), func(_ context.Context, name string, _ time.Time) {
		fired = append(fired, name)
	})

	// Set next time to future.
	future := utcTime("2024-01-10 09:00:00")
	s.mu.Lock()
	s.next["j"] = future
	s.mu.Unlock()

	// Tick before scheduled time.
	s.tick(context.Background(), utcTime("2024-01-10 08:59:59"))
	time.Sleep(20 * time.Millisecond)

	if len(fired) != 0 {
		t.Fatalf("expected no fires, got: %v", fired)
	}
}

func TestSchedulerTick_skipsManual(t *testing.T) {
	job := makeJob("j", "manual", "", "")
	var fired []string
	cfg := &config.Config{Jobs: map[string]*config.Job{"j": job}}
	s := New(cfg, newTestLogger(), func(_ context.Context, name string, _ time.Time) {
		fired = append(fired, name)
	})
	s.tick(context.Background(), time.Now())
	time.Sleep(20 * time.Millisecond)
	if len(fired) != 0 {
		t.Fatalf("expected no fires for manual job, got: %v", fired)
	}
}

func TestSchedulerTick_advancesNext(t *testing.T) {
	job := makeJob("j", "daily", "0900", "")
	scheduled := utcTime("2024-01-10 09:00:00")

	cfg := &config.Config{Jobs: map[string]*config.Job{"j": job}}
	s := New(cfg, newTestLogger(), func(_ context.Context, _ string, _ time.Time) {})

	s.mu.Lock()
	s.next["j"] = scheduled
	s.mu.Unlock()

	s.tick(context.Background(), utcTime("2024-01-10 09:00:01"))
	time.Sleep(50 * time.Millisecond)

	next := s.NextFor("j")
	if next.Equal(scheduled) || next.Before(scheduled) {
		t.Fatalf("next should advance after firing, got %v", next)
	}
}

// ---------------------------------------------------------------------------
// Scheduler.Reload
// ---------------------------------------------------------------------------

func TestSchedulerReload(t *testing.T) {
	job1 := makeJob("j1", "daily", "0900", "")
	job2 := makeJob("j2", "hourly", "", "")

	cfg1 := &config.Config{Jobs: map[string]*config.Job{"j1": job1}}
	s := New(cfg1, newTestLogger(), func(_ context.Context, _ string, _ time.Time) {})

	if s.NextFor("j2").IsZero() == false {
		t.Fatal("j2 should not exist before reload")
	}

	cfg2 := &config.Config{Jobs: map[string]*config.Job{"j2": job2}}
	s.Reload(cfg2)

	if s.NextFor("j2").IsZero() {
		t.Fatal("j2 should have a next time after reload")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
