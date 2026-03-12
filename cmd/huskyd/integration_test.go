package daemoncmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/dag"
	"github.com/husky-scheduler/husky/internal/executor"
	"github.com/husky-scheduler/husky/internal/notify"
	"github.com/husky-scheduler/husky/internal/scheduler"
	"github.com/husky-scheduler/husky/internal/store"
)

const integrationTestTimeout = 5 * time.Second

func writeDaemonTestConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "husky.yaml")
	normalized := strings.ReplaceAll(strings.TrimSpace(body), "\t", "  ") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(normalized), 0o644))
	return path
}

func newTestDaemonFromPath(t *testing.T, cfgPath string) *daemon {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	graph, err := dag.Build(cfg)
	require.NoError(t, err)
	st := openDaemonTestStore(t)
	ex := executor.New(4, st, testLogger())
	d := &daemon{
		cfgPath:    cfgPath,
		dataDir:    t.TempDir(),
		logger:     testLogger(),
		cfg:        cfg,
		graph:      graph,
		st:         st,
		exec:       ex,
		notif:      notify.New(st, testLogger()),
		pausedJobs: make(map[string]bool),
		stop:       func() {},
	}
	d.sched = scheduler.New(cfg, testLogger(), func(ctx context.Context, jobName string, _ time.Time) {
		d.dispatch(ctx, jobName, store.TriggerSchedule, "", newCycleID(), 1)
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
		defer cancel()
		d.exec.Drain(ctx)
	})
	return d
}

func waitForRuns(t *testing.T, st *store.Store, jobName string, want int, timeout time.Duration) []store.Run {
	t.Helper()
	var runs []store.Run
	require.Eventually(t, func() bool {
		var err error
		runs, err = st.GetRunsForJob(context.Background(), jobName, 20)
		require.NoError(t, err)
		return len(runs) >= want
	}, timeout, 20*time.Millisecond)
	return runs
}

func waitForLastRunStatus(t *testing.T, st *store.Store, jobName string, want store.RunStatus, timeout time.Duration) *store.Run {
	t.Helper()
	var run *store.Run
	require.Eventually(t, func() bool {
		var err error
		run, err = st.GetLastRunForJob(context.Background(), jobName)
		require.NoError(t, err)
		return run != nil && run.Status == want
	}, timeout, 20*time.Millisecond)
	return run
}

func blockingCommand(startedPath string, releasePath string) string {
	return fmt.Sprintf(
		"printf started > %s; while [ ! -f %s ]; do sleep 0.01; done; echo done",
		strconv.Quote(startedPath),
		strconv.Quote(releasePath),
	)
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, timeout, 20*time.Millisecond)
}

func releaseBlockingCommand(t *testing.T, releasePath string) {
	t.Helper()
	require.NoError(t, os.WriteFile(releasePath, []byte("release"), 0o644))
}

func assertNoRunAppears(t *testing.T, st *store.Store, jobName string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		run, err := st.GetLastRunForJob(context.Background(), jobName)
		require.NoError(t, err)
		if run != nil {
			t.Fatalf("expected no run for %q, but found run %d with status %s", jobName, run.ID, run.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIntegration_FullPipeline_ExecutesEndToEnd(t *testing.T) {
	orderPath := filepath.Join(t.TempDir(), "order.log")
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  ingest_raw_data:
    description: "ingest"
    frequency: manual
    command: "printf '/tmp/raw.csv\\n'; echo ingest >> `+orderPath+`"
    output:
      file_path: last_line
  transform_data:
    description: "transform"
    frequency: after:ingest_raw_data
		command: "echo {{ outputs.ingest_raw_data.file_path }}; echo {{ outputs.ingest_raw_data.file_path }} >> `+orderPath+`; echo transform >> `+orderPath+`"
  generate_report:
    description: "report"
    frequency: after:transform_data
    command: "echo generate_report >> `+orderPath+`"
  sync_to_s3:
    description: "sync"
    frequency: manual
    command: "echo sync_to_s3 >> `+orderPath+`"
  cleanup_old_files:
    description: "cleanup"
    frequency: manual
    command: "echo cleanup_old_files >> `+orderPath+`"
`)

	d := newTestDaemonFromPath(t, cfgPath)
	ctx := context.Background()

	d.dispatch(ctx, "ingest_raw_data", store.TriggerManual, "pipeline", "cycle-full", 1)
	waitForLastRunStatus(t, d.st, "generate_report", store.StatusSuccess, 2*time.Second)

	d.dispatch(ctx, "sync_to_s3", store.TriggerManual, "pipeline", "cycle-sync", 1)
	d.dispatch(ctx, "cleanup_old_files", store.TriggerManual, "pipeline", "cycle-clean", 1)
	waitForLastRunStatus(t, d.st, "sync_to_s3", store.StatusSuccess, time.Second)
	waitForLastRunStatus(t, d.st, "cleanup_old_files", store.StatusSuccess, time.Second)

	transformLogs, err := d.st.GetRunLogs(context.Background(), waitForLastRunStatus(t, d.st, "transform_data", store.StatusSuccess, time.Second).ID)
	require.NoError(t, err)
	joined := make([]string, 0, len(transformLogs))
	for _, line := range transformLogs {
		joined = append(joined, line.Line)
	}
	assert.Contains(t, strings.Join(joined, "\n"), "/tmp/raw.csv")

	content, err := os.ReadFile(orderPath)
	require.NoError(t, err)
	text := string(content)
	assert.Less(t, strings.Index(text, "ingest\n"), strings.Index(text, "/tmp/raw.csv\n"))
	assert.Less(t, strings.Index(text, "/tmp/raw.csv\n"), strings.Index(text, "transform\n"))
	assert.Less(t, strings.Index(text, "transform\n"), strings.Index(text, "generate_report\n"))
	assert.Contains(t, text, "sync_to_s3\n")
	assert.Contains(t, text, "cleanup_old_files\n")
}

func TestIntegration_ConcurrencyForbid_SkipsOverlappingRun(t *testing.T) {
	startedPath := filepath.Join(t.TempDir(), "sleeper.started")
	releasePath := filepath.Join(t.TempDir(), "sleeper.release")
	t.Cleanup(func() {
		_ = os.WriteFile(releasePath, []byte("release"), 0o644)
	})
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  sleeper:
    description: "slow"
    frequency: manual
    concurrency: forbid
    timeout: "5s"
    command: `+strconv.Quote(blockingCommand(startedPath, releasePath))+`
`)
	d := newTestDaemonFromPath(t, cfgPath)
	ctx := context.Background()

	d.dispatch(ctx, "sleeper", store.TriggerManual, "first", "cycle-a", 1)
	waitForFile(t, startedPath, integrationTestTimeout)
	d.dispatch(ctx, "sleeper", store.TriggerManual, "second", "cycle-b", 1)
	releaseBlockingCommand(t, releasePath)

	waitForLastRunStatus(t, d.st, "sleeper", store.StatusSuccess, integrationTestTimeout)
	runs, err := d.st.GetRunsForJob(context.Background(), "sleeper", 10)
	require.NoError(t, err)
	assert.Len(t, runs, 1, "overlapping run must be skipped when concurrency=forbid")
}

func TestIntegration_OnFailureSkip_MarksRunSkipped(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  skippable:
    description: "skip on failure"
    frequency: manual
    command: "exit 1"
    retries: 0
    on_failure: skip
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "skippable", store.TriggerManual, "skip-test", "cycle-skip", 1)
	run := waitForLastRunStatus(t, d.st, "skippable", store.StatusSkipped, 2*time.Second)
	assert.Equal(t, store.StatusSkipped, run.Status)
}

func TestIntegration_Cancel_MarksRunCancelled(t *testing.T) {
	startedPath := filepath.Join(t.TempDir(), "cancel.started")
	releasePath := filepath.Join(t.TempDir(), "cancel.release")
	t.Cleanup(func() {
		_ = os.WriteFile(releasePath, []byte("release"), 0o644)
	})
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  sleeper:
    description: "cancel me"
    frequency: manual
    timeout: "5s"
    command: `+strconv.Quote(blockingCommand(startedPath, releasePath))+`
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "sleeper", store.TriggerManual, "cancel-test", "cycle-cancel", 1)
	waitForFile(t, startedPath, integrationTestTimeout)
	require.NoError(t, d.cancelFunc("sleeper"))

	run := waitForLastRunStatus(t, d.st, "sleeper", store.StatusCancelled, integrationTestTimeout)
	assert.Equal(t, store.StatusCancelled, run.Status)
	assert.Equal(t, "cancelled", run.StatusReason)
	assert.NotNil(t, run.ExitCode)
	assert.Equal(t, -1, *run.ExitCode)
}

func TestIntegration_ReconcileCatchup_TrueTriggersMissedRun(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  daily_job:
    description: "catchup"
    frequency: daily
    time: "0000"
    catchup: true
    command: "echo catchup"
`)
	d := newTestDaemonFromPath(t, cfgPath)
	due := time.Now().Add(-2 * time.Minute)
	require.NoError(t, d.st.UpdateJobState(context.Background(), store.JobState{
		JobName: "daily_job",
		NextRun: &due,
	}))

	d.reconcileCatchup(context.Background())
	run := waitForLastRunStatus(t, d.st, "daily_job", store.StatusSuccess, integrationTestTimeout)
	assert.Equal(t, store.TriggerSchedule, run.Trigger)
	assert.Equal(t, "catchup", run.Reason)
}

func TestIntegration_ReconcileCatchup_FalseSkipsMissedRun(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  daily_job:
    description: "no catchup"
    frequency: daily
    time: "0000"
    catchup: false
    command: "echo should-not-run"
`)
	d := newTestDaemonFromPath(t, cfgPath)
	due := time.Now().Add(-2 * time.Minute)
	require.NoError(t, d.st.UpdateJobState(context.Background(), store.JobState{
		JobName: "daily_job",
		NextRun: &due,
	}))

	d.reconcileCatchup(context.Background())
	assertNoRunAppears(t, d.st, "daily_job", 500*time.Millisecond)
}

func TestIntegration_Reload_RejectsCycleAndKeepsOldConfig(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  a:
    description: "a"
    frequency: manual
    command: "echo a"
  b:
    description: "b"
    frequency: after:a
    command: "echo b"
`)
	d := newTestDaemonFromPath(t, cfgPath)
	oldCfg := d.cfg

	require.NoError(t, os.WriteFile(cfgPath, []byte(`
version: "1"
jobs:
  a:
    description: "a"
    frequency: manual
    command: "echo a"
    depends_on: ["b"]
  b:
    description: "b"
    frequency: manual
    command: "echo b"
    depends_on: ["a"]
`), 0o644))

	err := d.reload()
	require.Error(t, err)
	assert.Same(t, oldCfg, d.cfg, "reload failure must keep the old config active")
	assert.Equal(t, 0, len(d.graph.DepsOf("a")))
}

func TestIntegration_Reload_DoesNotInterruptRunningJob(t *testing.T) {
	startedPath := filepath.Join(t.TempDir(), "reload.started")
	releasePath := filepath.Join(t.TempDir(), "reload.release")
	t.Cleanup(func() {
		_ = os.WriteFile(releasePath, []byte("release"), 0o644)
	})
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  sleeper:
    description: "sleeper"
    frequency: manual
    timeout: "5s"
    command: `+strconv.Quote(blockingCommand(startedPath, releasePath))+`
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "sleeper", store.TriggerManual, "before-reload", "cycle-run", 1)
	waitForFile(t, startedPath, integrationTestTimeout)

	require.NoError(t, os.WriteFile(cfgPath, []byte(`
version: "1"
jobs:
  sleeper:
    description: "sleeper"
    frequency: manual
    timeout: "5s"
    command: `+strconv.Quote(blockingCommand(startedPath, releasePath))+`
  extra:
    description: "extra"
    frequency: manual
    command: "echo extra"
`), 0o644))

	require.NoError(t, d.reload())
	require.Eventually(t, func() bool { return d.exec.IsRunning("sleeper") }, integrationTestTimeout, 20*time.Millisecond)
	releaseBlockingCommand(t, releasePath)
	waitForLastRunStatus(t, d.st, "sleeper", store.StatusSuccess, integrationTestTimeout)
	_, ok := d.cfg.Jobs["extra"]
	assert.True(t, ok, "new config should be active after reload")
}

func TestIntegration_SLABreach_SuccessMarksRun(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  slow_job:
    description: "slow"
    frequency: manual
    command: "sleep 0.15 && echo slow"
    timeout: "1s"
    sla: "50ms"
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "slow_job", store.TriggerManual, "sla-check", "cycle-sla", 1)
	run := waitForLastRunStatus(t, d.st, "slow_job", store.StatusSuccess, integrationTestTimeout)
	assert.True(t, run.SLABreached, "completed run should retain sla_breached flag")
}

func TestIntegration_HealthcheckFail_RetriesTriggered(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  checked_job:
    description: "healthcheck fail"
    frequency: manual
    command: "echo ok"
    retries: 1
    retry_delay: "fixed:20ms"
    healthcheck:
      command: "false"
      timeout: "200ms"
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "checked_job", store.TriggerManual, "hc-fail", "cycle-hc-fail", 1)
	waitForLastRunStatus(t, d.st, "checked_job", store.StatusFailed, integrationTestTimeout)
	runs := waitForRuns(t, d.st, "checked_job", 2, integrationTestTimeout)
	assert.Equal(t, 2, len(runs), "healthcheck failure should trigger a retry attempt")
	assert.Equal(t, 2, runs[0].Attempt)
	assert.Equal(t, store.StatusFailed, runs[0].Status)
	require.NotNil(t, runs[0].HCStatus)
	assert.Equal(t, store.HCFail, *runs[0].HCStatus)
}

func TestIntegration_HealthcheckWarnOnly_SucceedsWithWarn(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  checked_job:
    description: "healthcheck warn"
    frequency: manual
    command: "echo ok"
    healthcheck:
      command: "false"
      timeout: "200ms"
      on_fail: "warn_only"
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "checked_job", store.TriggerManual, "hc-warn", "cycle-hc-warn", 1)
	run := waitForLastRunStatus(t, d.st, "checked_job", store.StatusSuccess, integrationTestTimeout)
	require.NotNil(t, run.HCStatus)
	assert.Equal(t, store.HCWarn, *run.HCStatus)
}

func TestIntegration_RetryNotification_UsesUpcomingAttempt(t *testing.T) {
	requests := make(chan map[string]any, 2)
	capSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		requests <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer capSrv.Close()

	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  flaky:
    description: "retry notify"
    frequency: manual
    command: "exit 1"
    retries: 1
    retry_delay: "fixed:20ms"
    notify:
      on_retry:
        channel: "webhook:`+capSrv.URL+`"
        message: "attempt={{ run.attempt }} retries={{ job.retries }} status={{ run.status }}"
`)
	d := newTestDaemonFromPath(t, cfgPath)

	d.dispatch(context.Background(), "flaky", store.TriggerManual, "retry-notify", "cycle-retry", 1)
	waitForRuns(t, d.st, "flaky", 2, integrationTestTimeout)

	var payload map[string]any
	require.Eventually(t, func() bool {
		select {
		case payload = <-requests:
			return true
		default:
			return false
		}
	}, integrationTestTimeout, 20*time.Millisecond, "timed out waiting for retry notification")
	assert.Equal(t, "attempt=2 retries=1 status=RETRYING", payload["message"])
}

func TestIntegration_PauseResumeByTag(t *testing.T) {
	cfgPath := writeDaemonTestConfig(t, `
version: "1"
jobs:
  one:
    description: "one"
    frequency: manual
    command: "echo one"
    tags: ["etl"]
  two:
    description: "two"
    frequency: manual
    command: "echo two"
    tags: ["bi"]
`)
	d := newTestDaemonFromPath(t, cfgPath)

	paused, err := d.pauseTagFunc("etl")
	require.NoError(t, err)
	assert.Equal(t, 1, paused)
	status, err := d.statusFunc()
	require.NoError(t, err)
	for _, row := range status {
		if row.Name == "one" {
			assert.True(t, row.Paused)
		}
	}

	resumed, err := d.resumeTagFunc("etl")
	require.NoError(t, err)
	assert.Equal(t, 1, resumed)
	status, err = d.statusFunc()
	require.NoError(t, err)
	for _, row := range status {
		if row.Name == "one" {
			assert.False(t, row.Paused)
		}
	}
}
