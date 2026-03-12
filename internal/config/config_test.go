package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func parse(t *testing.T, yaml string) (*config.Config, error) {
	t.Helper()
	return config.LoadBytes([]byte(yaml))
}

func mustParse(t *testing.T, yaml string) *config.Config {
	t.Helper()
	cfg, err := parse(t, yaml)
	require.NoError(t, err)
	return cfg
}

// expectErr asserts that parsing yaml produces a ParseError containing at
// least one ValidationError whose Field==wantField and whose Msg contains
// wantMsgSubstr (both optional when empty string is passed).
func expectErr(t *testing.T, yaml, wantField, wantMsgSubstr string) {
	t.Helper()
	_, err := parse(t, yaml)
	require.Error(t, err)
	var pe *config.ParseError
	require.ErrorAs(t, err, &pe, "expected *config.ParseError, got %T: %v", err, err)

	for _, ve := range pe.Errors {
		fieldMatch := wantField == "" || ve.Field == wantField
		msgMatch := wantMsgSubstr == "" || strings.Contains(ve.Msg, wantMsgSubstr)
		if fieldMatch && msgMatch {
			return
		}
	}
	t.Errorf("no ValidationError with field=%q msg~=%q\ngot errors:\n%v",
		wantField, wantMsgSubstr, err)
}

// ── valid configs ─────────────────────────────────────────────────────────────

func TestLoad_MinimalValid(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  ping:
    description: "health check"
    frequency: daily
    time: "0900"
    command: "echo ok"
`)
	assert.Equal(t, "1", cfg.Version)
	require.Contains(t, cfg.Jobs, "ping")
	assert.Equal(t, "ping", cfg.Jobs["ping"].Name)
	assert.Equal(t, "daily", cfg.Jobs["ping"].Frequency)
	assert.Equal(t, "0900", cfg.Jobs["ping"].Time)
}

func TestLoad_FullPipelineFile(t *testing.T) {
	data, err := os.ReadFile("testdata/full_pipeline.yaml")
	require.NoError(t, err)
	cfg, err := config.LoadBytes(data)
	require.NoError(t, err)
	assert.Len(t, cfg.Jobs, 5)
	assert.Contains(t, cfg.Jobs, "ingest_raw_data")
	assert.Contains(t, cfg.Jobs, "transform_data")
	assert.Contains(t, cfg.Jobs, "generate_report")
	assert.Contains(t, cfg.Jobs, "sync_to_s3")
	assert.Contains(t, cfg.Jobs, "cleanup_old_files")
}

func TestLoad_MinimalFile(t *testing.T) {
	data, err := os.ReadFile("testdata/minimal.yaml")
	require.NoError(t, err)
	_, err = config.LoadBytes(data)
	require.NoError(t, err)
}

func TestLoad_JobNamePopulated(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  my_job:
    description: "d"
    frequency: manual
    command: "c"
`)
	job, ok := cfg.Jobs["my_job"]
	require.True(t, ok)
	assert.Equal(t, "my_job", job.Name)
}

// ── version ───────────────────────────────────────────────────────────────────

func TestValidation_MissingVersion(t *testing.T) {
	expectErr(t, `
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
`, "version", "required")
}

func TestValidation_UnsupportedVersion(t *testing.T) {
	expectErr(t, `
version: "2"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
`, "version", "unsupported")
}

// ── required job fields ───────────────────────────────────────────────────────

func TestValidation_MissingDescription(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    frequency: daily
    time: "0900"
    command: "c"
`, "description", "required")
}

func TestValidation_MissingCommand(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
`, "command", "required")
}

func TestValidation_MissingFrequency(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    command: "c"
`, "frequency", "required")
}

// ── frequency values ──────────────────────────────────────────────────────────

func TestValidation_ValidFrequencies(t *testing.T) {
	cases := []struct{ freq, time string }{
		{"hourly", ""},
		{"daily", "0900"},
		{"weekly", ""},
		{"monthly", "0900"},
		{"weekdays", ""},
		{"weekends", ""},
		{"manual", ""},
		{"after:other_job", ""},
		{"every:15s", ""},
		{"every:15m", ""},
		{"on:[monday]", ""},
		{"on:[tuesday, friday]", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.freq, func(t *testing.T) {
			timeField := ""
			if tc.time != "" {
				timeField = `time: "` + tc.time + `"`
			}
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: " +
				tc.freq + "\n    " + timeField + "\n    command: \"c\"\n"
			_, err := parse(t, src)
			require.NoError(t, err, "frequency %q should be valid", tc.freq)
		})
	}
}

func TestValidation_InvalidFrequency(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: fortnightly
    time: "0900"
    command: "c"
`, "frequency", "unrecognised")
}

func TestValidation_InvalidEveryIntervalFrequency(t *testing.T) {
	for _, freq := range []string{"every:0s", "every:-15m", "every:24h", "every:notaduration"} {
		t.Run(freq, func(t *testing.T) {
			expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: `+freq+`
    command: "c"
`, "frequency", "unrecognised")
		})
	}
}

func TestValidation_InvalidOnDaysFrequency(t *testing.T) {
	for _, freq := range []string{"on:[]", "on:[funday]", "on:[monday,monday]"} {
		t.Run(freq, func(t *testing.T) {
			expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: "`+freq+`"
    command: "c"
`, "frequency", "unrecognised")
		})
	}
}

func TestValidation_AfterEmptyJobName(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: "after:"
    command: "c"
`, "frequency", "unrecognised")
}

// ── time field ────────────────────────────────────────────────────────────────

// TestValidation_TimeField covers all rows from Table §2.2.2 of the TDD.
func TestValidation_TimeField(t *testing.T) {
	cases := []struct {
		time    string
		wantErr bool
		errFrag string
	}{
		{"0000", false, ""},
		{"0200", false, ""},
		{"0930", false, ""},
		{"1200", false, ""},
		{"1430", false, ""},
		{"2359", false, ""},
		{"200", true, "exactly 4 characters"},
		{"25:00", true, "exactly 4 characters"},
		{"2500", true, "hour 25 is out of range"},
		{"1260", true, "minute 60 is out of range"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.time, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"" +
				tc.time + "\"\n    command: \"c\"\n"
			_, err := parse(t, src)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errFrag != "" {
					assert.Contains(t, err.Error(), tc.errFrag)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidation_TimeRequired_ForDailyFrequency(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    command: "c"
`, "time", "required")
}

func TestValidation_TimeRequired_ForMonthlyFrequency(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: monthly
    command: "c"
`, "time", "required")
}

func TestValidation_TimeNotRequired_ForHourly(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: hourly
    command: "c"
`)
	require.NoError(t, err)
}

func TestValidation_TimeNotRequired_ForEveryInterval(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: every:15m
    command: "c"
`)
	require.NoError(t, err)
}

func TestValidation_TimeNotRequired_ForOnDays(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: "on:[monday, friday]"
    command: "c"
`)
	require.NoError(t, err)
}

func TestDefaults_DefaultRunTime_Invalid(t *testing.T) {
	expectErr(t, `
version: "1"
defaults:
  default_run_time: "25ab"
jobs:
  x:
    description: "d"
    frequency: weekly
    command: "c"
`, "defaults.default_run_time", "contain only digits")
}

func TestDefaults_DefaultRunTime_AppliedToWeeklyAlias(t *testing.T) {
	cfg, err := parse(t, `
version: "1"
defaults:
  default_run_time: "0415"
jobs:
  x:
    description: "d"
    frequency: weekly
    command: "c"
`)
	require.NoError(t, err)
	assert.Equal(t, "0415", cfg.Jobs["x"].Time)
}

func TestDefaults_DefaultRunTime_AppliedToOnDays(t *testing.T) {
	cfg, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: "on:[tuesday, friday]"
    command: "c"
`)
	require.NoError(t, err)
	assert.Equal(t, config.BuiltinDefaultRunTime, cfg.Jobs["x"].Time)
}

func TestValidation_TimeNotRequired_ForManual(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: manual
    command: "c"
`)
	require.NoError(t, err)
}

func TestValidation_TimeNotRequired_ForAfter(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: after:some_job
    command: "c"
`)
	require.NoError(t, err)
}

// ── on_failure ────────────────────────────────────────────────────────────────

func TestValidation_ValidOnFailureValues(t *testing.T) {
	for _, v := range []string{"alert", "skip", "stop", "ignore"} {
		v := v
		t.Run(v, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    on_failure: " + v + "\n"
			_, err := parse(t, src)
			require.NoError(t, err)
		})
	}
}

func TestValidation_InvalidOnFailure(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    on_failure: restart
`, "on_failure", "unrecognised")
}

// ── concurrency ───────────────────────────────────────────────────────────────

func TestValidation_ValidConcurrencyValues(t *testing.T) {
	for _, v := range []string{"allow", "forbid", "replace"} {
		v := v
		t.Run(v, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    concurrency: " + v + "\n"
			_, err := parse(t, src)
			require.NoError(t, err)
		})
	}
}

func TestValidation_InvalidConcurrency(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    concurrency: queue
`, "concurrency", "unrecognised")
}

// ── retry_delay ───────────────────────────────────────────────────────────────

func TestValidation_ValidRetryDelayValues(t *testing.T) {
	for _, v := range []string{"exponential", "fixed:30s", "fixed:5m", "fixed:1h"} {
		v := v
		t.Run(v, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    retry_delay: " + v + "\n"
			_, err := parse(t, src)
			require.NoError(t, err)
		})
	}
}

func TestValidation_InvalidRetryDelay(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    retry_delay: linear
`, "retry_delay", "must be")
}

func TestValidation_InvalidFixedRetryDelay(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    retry_delay: "fixed:notaduration"
`, "retry_delay", "invalid duration")
}

// ── defaults application ──────────────────────────────────────────────────────

func TestDefaults_Applied(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
defaults:
  timeout: "30m"
  retries: 3
  retry_delay: exponential
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
`)
	job := cfg.Jobs["x"]
	assert.Equal(t, "30m", job.Timeout)
	require.NotNil(t, job.Retries)
	assert.Equal(t, 3, *job.Retries)
	assert.Equal(t, "exponential", job.RetryDelay)
}

func TestDefaults_JobOverridesDefault(t *testing.T) {
	zero := 0
	cfg := mustParse(t, `
version: "1"
defaults:
  timeout: "30m"
  retries: 3
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    timeout: "1h"
    retries: 0
`)
	job := cfg.Jobs["x"]
	assert.Equal(t, "1h", job.Timeout, "job timeout should override default")
	require.NotNil(t, job.Retries)
	assert.Equal(t, zero, *job.Retries, "explicit 0 retries should not be overridden by default of 3")
}

// ── env interpolation ─────────────────────────────────────────────────────────

func TestEnv_Interpolation(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret123")
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    env:
      API_KEY: "${env:TEST_API_KEY}"
      STATIC: "literal"
`)
	job := cfg.Jobs["x"]
	assert.Equal(t, "secret123", job.Env["API_KEY"])
	assert.Equal(t, "literal", job.Env["STATIC"])
}

func TestEnv_MissingHostVar_ExpandsToEmpty(t *testing.T) {
	t.Setenv("HUSKY_TEST_MISSING_VAR", "")
	os.Unsetenv("HUSKY_TEST_MISSING_VAR")
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    env:
      MISSING: "${env:HUSKY_TEST_MISSING_VAR}"
`)
	assert.Equal(t, "", cfg.Jobs["x"].Env["MISSING"])
}

// ── multiple errors at once ───────────────────────────────────────────────────

func TestValidation_MultipleErrors_CollectedTogether(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  bad_job:
    description: ""
    frequency: invalid_freq
    command: ""
`)
	require.Error(t, err)
	var pe *config.ParseError
	require.ErrorAs(t, err, &pe)
	// Expect errors for: description, command, frequency (at minimum)
	assert.GreaterOrEqual(t, len(pe.Errors), 3,
		"expected multiple validation errors, got:\n%v", err)
}

// ── self-referencing depends_on ───────────────────────────────────────────────

func TestValidation_SelfDependency(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    depends_on: [x]
`, "depends_on", "cannot depend on itself")
}

// ── timezone ──────────────────────────────────────────────────────────────────

func TestTimezone_ValidIdentifiers(t *testing.T) {
	cases := []string{"UTC", "America/New_York", "Europe/London", "Asia/Tokyo", "America/Los_Angeles"}
	for _, tz := range cases {
		tz := tz
		t.Run(tz, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    timezone: " + tz + "\n"
			_, err := parse(t, src)
			require.NoError(t, err, "timezone %q should be valid", tz)
		})
	}
}

func TestTimezone_InvalidIdentifier(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    timezone: NotReal/Zone
`, "timezone", "unknown IANA timezone")
}

func TestTimezone_DefaultsInherited(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
defaults:
  timezone: America/New_York
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
  y:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    timezone: Asia/Tokyo
`)
	assert.Equal(t, "America/New_York", cfg.Jobs["x"].Timezone,
		"job with no timezone should inherit defaults.timezone")
	assert.Equal(t, "Asia/Tokyo", cfg.Jobs["y"].Timezone,
		"job with explicit timezone should not be overridden by default")
}

func TestTimezone_DefaultsInvalid(t *testing.T) {
	expectErr(t, `
version: "1"
defaults:
  timezone: BadZone
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
`, "defaults.timezone", "unknown IANA timezone")
}

// ── sla ───────────────────────────────────────────────────────────────────────

func TestSLA_ValidDuration(t *testing.T) {
	cases := []string{"20m", "1h30m", "90s", "2h"}
	for _, d := range cases {
		d := d
		t.Run(d, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    timeout: \"3h\"\n    sla: " + d + "\n"
			_, err := parse(t, src)
			require.NoError(t, err, "sla %q should be valid", d)
		})
	}
}

func TestSLA_InvalidDuration(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    sla: "notaduration"
`, "sla", "invalid duration")
}

func TestSLA_MustBeLessThanTimeout(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    timeout: "30m"
    sla: "30m"
`, "sla", "sla >= timeout")
}

func TestSLA_EqualToTimeoutRejected(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    timeout: "1h"
    sla: "1h"
`, "sla", "sla >= timeout")
}

func TestSLA_GreaterThanTimeoutRejected(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    timeout: "20m"
    sla: "1h"
`, "sla", "sla >= timeout")
}

func TestSLA_WithoutTimeout_IsValid(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    sla: "20m"
`)
	require.NoError(t, err, "sla without timeout should be valid")
}

// ── tags ──────────────────────────────────────────────────────────────────────

func TestTags_ValidTags(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    tags: [data-pipeline, critical, nightly]
`)
	assert.Equal(t, []string{"data-pipeline", "critical", "nightly"}, cfg.Jobs["x"].Tags)
}

func TestTags_TooMany(t *testing.T) {
	// 11 tags — exceeds max of 10
	src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    tags: [a,b,c,d,e,f,g,h,i,j,k]\n"
	expectErr(t, src, "tags", "too many tags")
}

func TestTags_TooLong(t *testing.T) {
	longTag := strings.Repeat("a", 33)
	src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    tags: [" + longTag + "]\n"
	expectErr(t, src, "tags", "exceeds maximum length")
}

func TestTags_InvalidCharacters(t *testing.T) {
	cases := []string{"CamelCase", "has space", "has_underscore", "has.dot"}
	for _, tag := range cases {
		tag := tag
		t.Run(tag, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    tags: [\"" + tag + "\"]\n"
			expectErr(t, src, "tags", "invalid")
		})
	}
}

func TestTags_DuplicateTag(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    tags: [foo, foo]
`, "tags", "duplicate")
}

func TestTags_EmptyListIsValid(t *testing.T) {
	_, err := parse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
`)
	require.NoError(t, err, "job with no tags should be valid")
}

// ── healthcheck ──────────────────────────────────────────────────────────────

func TestHealthcheck_Valid(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    healthcheck:
      command: "./check.sh"
      timeout: "30s"
      on_fail: mark_failed
`)
	require.NotNil(t, cfg.Jobs["x"].Healthcheck)
	hc := cfg.Jobs["x"].Healthcheck
	assert.Equal(t, "./check.sh", hc.Command)
	assert.Equal(t, "30s", hc.Timeout)
	assert.Equal(t, "mark_failed", hc.OnFail)
}

func TestHealthcheck_WarnOnly(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    healthcheck:
      command: "./check.sh"
      on_fail: warn_only
`)
	require.NotNil(t, cfg.Jobs["x"].Healthcheck)
	assert.Equal(t, "warn_only", cfg.Jobs["x"].Healthcheck.OnFail)
}

func TestHealthcheck_MissingCommand(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    healthcheck:
      timeout: "30s"
`, "healthcheck.command", "required")
}

func TestHealthcheck_InvalidTimeout(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    healthcheck:
      command: "./check.sh"
      timeout: "notaduration"
`, "healthcheck.timeout", "invalid duration")
}

func TestHealthcheck_InvalidOnFail(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    healthcheck:
      command: "./check.sh"
      on_fail: kill
`, "healthcheck.on_fail", "unrecognised")
}

// ── output capture ────────────────────────────────────────────────────────────

func TestOutput_ValidCaptureModes(t *testing.T) {
	cases := []struct {
		name string
		mode string
	}{
		{"last_line", "last_line"},
		{"first_line", "first_line"},
		{"exit_code", "exit_code"},
		{"json_field", "json_field:total"},
		{"json_field_nested", "json_field:data.count"},
		{"regex", `regex:v(\d+)`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := "version: \"1\"\njobs:\n  x:\n    description: \"d\"\n    frequency: daily\n    time: \"0900\"\n    command: \"c\"\n    output:\n      result: " + tc.mode + "\n"
			_, err := parse(t, src)
			require.NoError(t, err, "capture mode %q should be valid", tc.mode)
		})
	}
}

func TestOutput_InvalidCaptureMode(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    output:
      result: second_line
`, "output.result", "unrecognised")
}

func TestOutput_JsonFieldEmptyKey(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    output:
      result: "json_field:"
`, "output.result", "non-empty key")
}

func TestOutput_RegexEmptyPattern(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    output:
      result: "regex:"
`, "output.result", "non-empty pattern")
}

func TestOutput_RegexInvalidPattern(t *testing.T) {
	expectErr(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    output:
      result: "regex:[invalid"
`, "output.result", "invalid pattern")
}

func TestOutput_MultipleVars(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    output:
      file_path: last_line
      record_count: "json_field:total"
      status_code: exit_code
`)
	require.NotNil(t, cfg.Jobs["x"].Output)
	assert.Equal(t, "last_line", cfg.Jobs["x"].Output["file_path"])
	assert.Equal(t, "json_field:total", cfg.Jobs["x"].Output["record_count"])
	assert.Equal(t, "exit_code", cfg.Jobs["x"].Output["status_code"])
}

// ── rich notify ───────────────────────────────────────────────────────────────

func TestNotify_ShorthandStringForm(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    notify:
      on_failure: pagerduty:p1
      on_success: slack:#reports
`)
	require.NotNil(t, cfg.Jobs["x"].Notify)
	n := cfg.Jobs["x"].Notify
	require.NotNil(t, n.OnFailure)
	assert.Equal(t, "pagerduty:p1", n.OnFailure.Channel)
	require.NotNil(t, n.OnSuccess)
	assert.Equal(t, "slack:#reports", n.OnSuccess.Channel)
}

func TestNotify_FullObjectForm(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    notify:
      on_failure:
        channel: slack:#data-alerts
        message: "{{ job.name }} failed"
        attach_logs: last_30_lines
      on_success:
        channel: slack:#data-reports
        message: "{{ job.name }} recovered"
        only_after_failure: true
`)
	require.NotNil(t, cfg.Jobs["x"].Notify)
	n := cfg.Jobs["x"].Notify
	require.NotNil(t, n.OnFailure)
	assert.Equal(t, "slack:#data-alerts", n.OnFailure.Channel)
	assert.Equal(t, "{{ job.name }} failed", n.OnFailure.Message)
	assert.Equal(t, "last_30_lines", n.OnFailure.AttachLogs)
	require.NotNil(t, n.OnSuccess)
	assert.Equal(t, "slack:#data-reports", n.OnSuccess.Channel)
	assert.True(t, n.OnSuccess.OnlyAfterFailure)
}

func TestNotify_SLABreachAndRetryEvents(t *testing.T) {
	cfg := mustParse(t, `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
    notify:
      on_sla_breach:
        channel: slack:#warnings
        message: "{{ job.name }} exceeded SLA"
      on_retry: slack:#alerts
`)
	require.NotNil(t, cfg.Jobs["x"].Notify)
	n := cfg.Jobs["x"].Notify
	require.NotNil(t, n.OnSLABreach)
	assert.Equal(t, "slack:#warnings", n.OnSLABreach.Channel)
	assert.Equal(t, "{{ job.name }} exceeded SLA", n.OnSLABreach.Message)
	require.NotNil(t, n.OnRetry)
	assert.Equal(t, "slack:#alerts", n.OnRetry.Channel)
}

// ── integrations ──────────────────────────────────────────────────────────────

// minimalJobYAML is the minimal valid jobs block reused across integration tests.
const minimalJobYAML = `
version: "1"
jobs:
  x:
    description: "d"
    frequency: daily
    time: "0900"
    command: "c"
`

func TestIntegration_SlackInferred(t *testing.T) {
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  slack:
    webhook_url: "https://hooks.slack.com/services/abc123"
`)
	require.Len(t, cfg.Integrations, 1)
	intg := cfg.Integrations["slack"]
	require.NotNil(t, intg)
	assert.Equal(t, "https://hooks.slack.com/services/abc123", intg.WebhookURL)
	assert.Equal(t, "slack", intg.EffectiveProvider)
}

func TestIntegration_PagerDutyInferred(t *testing.T) {
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  pagerduty:
    routing_key: "r3routingkey"
`)
	intg := cfg.Integrations["pagerduty"]
	require.NotNil(t, intg)
	assert.Equal(t, "r3routingkey", intg.RoutingKey)
	assert.Equal(t, "pagerduty", intg.EffectiveProvider)
}

func TestIntegration_DiscordInferred(t *testing.T) {
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  discord:
    webhook_url: "https://discord.com/api/webhooks/123/abc"
`)
	intg := cfg.Integrations["discord"]
	require.NotNil(t, intg)
	assert.Equal(t, "discord", intg.EffectiveProvider)
}

func TestIntegration_SMTPInferred(t *testing.T) {
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  smtp:
    host: "smtp.gmail.com"
    port: 587
    username: "user@example.com"
    password: "secret"
    from: "husky@example.com"
`)
	intg := cfg.Integrations["smtp"]
	require.NotNil(t, intg)
	assert.Equal(t, "smtp.gmail.com", intg.Host)
	assert.Equal(t, 587, intg.Port)
	assert.Equal(t, "smtp", intg.EffectiveProvider)
}

func TestIntegration_NamedMultiSlack(t *testing.T) {
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  slack_ops:
    provider: slack
    webhook_url: "https://hooks.slack.com/ops"
  slack_data:
    provider: slack
    webhook_url: "https://hooks.slack.com/data"
`)
	require.Len(t, cfg.Integrations, 2)
	assert.Equal(t, "slack", cfg.Integrations["slack_ops"].EffectiveProvider)
	assert.Equal(t, "slack", cfg.Integrations["slack_data"].EffectiveProvider)
	assert.Equal(t, "https://hooks.slack.com/ops", cfg.Integrations["slack_ops"].WebhookURL)
}

func TestIntegration_AbsentIsValid(t *testing.T) {
	_, err := parse(t, minimalJobYAML)
	require.NoError(t, err, "omitting integrations block should be valid")
}

func TestIntegration_MissingWebhookURL(t *testing.T) {
	expectErr(t, minimalJobYAML+`
integrations:
  slack:
    webhook_url: ""
`, "integrations.slack.webhook_url", "required for provider")
}

func TestIntegration_MissingRoutingKey(t *testing.T) {
	expectErr(t, minimalJobYAML+`
integrations:
  pagerduty:
    routing_key: ""
`, "integrations.pagerduty.routing_key", "required for provider")
}

func TestIntegration_SMTPMissingHost(t *testing.T) {
	expectErr(t, minimalJobYAML+`
integrations:
  smtp:
    from: "husky@example.com"
`, "integrations.smtp.host", "required for provider")
}

func TestIntegration_SMTPMissingFrom(t *testing.T) {
	expectErr(t, minimalJobYAML+`
integrations:
  smtp:
    host: "smtp.gmail.com"
`, "integrations.smtp.from", "required for provider")
}

func TestIntegration_UnknownKeyNeedsProvider(t *testing.T) {
	expectErr(t, minimalJobYAML+`
integrations:
  my_custom_thing:
    webhook_url: "https://example.com"
`, "integrations.my_custom_thing.provider", "set the \"provider\" field explicitly")
}

func TestIntegration_UnsupportedProvider(t *testing.T) {
	expectErr(t, minimalJobYAML+`
integrations:
  my_notifications:
    provider: teams
    webhook_url: "https://outlook.office.com/webhook/abc"
`, "integrations.my_notifications.provider", "unsupported provider")
}

func TestIntegration_AggregatesMultipleErrors(t *testing.T) {
	_, err := parse(t, minimalJobYAML+`
integrations:
  slack:
    provider: slack

  smtp:
    port: 587
    username: user@example.com
    password: "${env:SMTP_PASS}"

  pagerduty:
    provider: pagerduty

  smtp_bad_port:
    provider: smtp
    host: mail.example.com
    from: husky@example.com
    port: 99999
`)
	require.Error(t, err)

	var pe *config.ParseError
	require.ErrorAs(t, err, &pe)

	got := map[string]string{}
	for _, ve := range pe.Errors {
		got[ve.Field] = ve.Msg
	}

	assert.Contains(t, got, "integrations.slack.webhook_url")
	assert.Contains(t, got["integrations.slack.webhook_url"], "required for provider")
	assert.Contains(t, got, "integrations.smtp.host")
	assert.Contains(t, got["integrations.smtp.host"], "required for provider")
	assert.Contains(t, got, "integrations.smtp.from")
	assert.Contains(t, got["integrations.smtp.from"], "required for provider")
	assert.Contains(t, got, "integrations.pagerduty.routing_key")
	assert.Contains(t, got["integrations.pagerduty.routing_key"], "required for provider")
	assert.Contains(t, got, "integrations.smtp_bad_port.port")
	assert.Contains(t, got["integrations.smtp_bad_port.port"], "out of range")
}

func TestIntegration_EnvTokenExpansion(t *testing.T) {
	t.Setenv("TEST_WEBHOOK", "https://hooks.slack.com/env-injected")
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  slack:
    webhook_url: "${env:TEST_WEBHOOK}"
`)
	assert.Equal(t, "https://hooks.slack.com/env-injected",
		cfg.Integrations["slack"].WebhookURL,
		"${env:VAR} in webhook_url should be expanded")
}

func TestIntegration_EnvTokenExpansionRoutingKey(t *testing.T) {
	t.Setenv("TEST_ROUTING_KEY", "abc-routing-key-xyz")
	cfg := mustParse(t, minimalJobYAML+`
integrations:
  pagerduty:
    routing_key: "${env:TEST_ROUTING_KEY}"
`)
	assert.Equal(t, "abc-routing-key-xyz",
		cfg.Integrations["pagerduty"].RoutingKey,
		"${env:VAR} in routing_key should be expanded")
}

// TestLoadDotEnv tests the .env file loader directly.
func TestLoadDotEnv_BasicParsing(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ".env"), []byte(`
# This is a comment
HUSKY_TEST_A=hello
HUSKY_TEST_B = world
HUSKY_TEST_C="quoted"
HUSKY_TEST_D='single'

HUSKY_TEST_E=no_quotes
`), 0o600)
	require.NoError(t, err)

	// Unset before test to avoid leakage from test runner.
	for _, k := range []string{"HUSKY_TEST_A", "HUSKY_TEST_B", "HUSKY_TEST_C", "HUSKY_TEST_D", "HUSKY_TEST_E"} {
		os.Unsetenv(k)
	}

	require.NoError(t, config.LoadDotEnv(dir))
	assert.Equal(t, "hello", os.Getenv("HUSKY_TEST_A"))
	assert.Equal(t, "world", os.Getenv("HUSKY_TEST_B"))
	assert.Equal(t, "quoted", os.Getenv("HUSKY_TEST_C"))
	assert.Equal(t, "single", os.Getenv("HUSKY_TEST_D"))
	assert.Equal(t, "no_quotes", os.Getenv("HUSKY_TEST_E"))
}

func TestLoadDotEnv_NonDestructive(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ".env"), []byte("HUSKY_TEST_NONDESTRUCTIVE=from_dotenv\n"), 0o600)
	require.NoError(t, err)

	t.Setenv("HUSKY_TEST_NONDESTRUCTIVE", "from_process_env")

	require.NoError(t, config.LoadDotEnv(dir))
	// Process env must win.
	assert.Equal(t, "from_process_env", os.Getenv("HUSKY_TEST_NONDESTRUCTIVE"))
}

func TestLoadDotEnv_MissingFileIsOK(t *testing.T) {
	dir := t.TempDir() // no .env written
	err := config.LoadDotEnv(dir)
	require.NoError(t, err, "missing .env should not return an error")
}
