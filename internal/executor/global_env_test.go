package executor_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/executor"
	"github.com/husky-scheduler/husky/internal/store"
)

// ── helpers shared with executor_test.go ─────────────────────────────────────
// openTestStore and job() are already declared in executor_test.go (same package).

// runJobLogs submits j against ex (backed by st), waits for completion, and
// returns the captured stdout log lines.  The caller owns both st and ex.
func runJobLogs(t *testing.T, st *store.Store, ex *executor.Executor, j *config.Job) []store.LogLine {
	t.Helper()
	ctx := context.Background()
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: j.Name,
		Status:  store.StatusRunning,
		Trigger: store.TriggerManual,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	ex.Submit(ctx, j, runID, executor.RunOpts{}, func(_ executor.Result) { wg.Done() })
	wg.Wait()
	time.Sleep(50 * time.Millisecond) // give store writes a moment to flush

	logs, err := st.GetRunLogs(ctx, runID)
	require.NoError(t, err)
	return logs
}

// firstStdout returns the first stdout log line, or "" if none.
func firstStdout(logs []store.LogLine) string {
	for _, l := range logs {
		if l.Stream == "stdout" {
			return l.Line
		}
	}
	return ""
}

// ── GlobalEnv tests ───────────────────────────────────────────────────────────

func TestExecutor_GlobalEnv_PresentInSubprocess(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ex.GlobalEnv = map[string]string{"HUSKY_GLOBAL_SMOKE": "hello_global"}

	j := &config.Job{Name: "test-job", Command: "echo $HUSKY_GLOBAL_SMOKE"}
	logs := runJobLogs(t, st, ex, j)

	assert.Equal(t, "hello_global", firstStdout(logs))
}

func TestExecutor_GlobalEnv_PerJobOverridesGlobal(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ex.GlobalEnv = map[string]string{"MY_VAR": "from_global"}

	j := &config.Job{
		Name:    "test-job",
		Command: "echo $MY_VAR",
		Env:     map[string]string{"MY_VAR": "from_job"},
	}
	logs := runJobLogs(t, st, ex, j)

	assert.Equal(t, "from_job", firstStdout(logs),
		"per-job env must take precedence over global env")
}

func TestExecutor_GlobalEnv_EmptyGlobalEnv_NoError(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	// GlobalEnv is nil — no global env configured; host env still available.

	j := &config.Job{Name: "test-job", Command: "echo baseline"}
	logs := runJobLogs(t, st, ex, j)

	assert.Equal(t, "baseline", firstStdout(logs))
}

func TestExecutor_GlobalEnv_HostEnvAvailable(t *testing.T) {
	// The host environment is always the baseline layer; global and per-job
	// env sit on top.  This test confirms a host env var survives when not
	// overridden by either global or per-job env.
	t.Setenv("HUSKY_HOST_TEST_VAR", "host_value")

	st := openTestStore(t)
	ex := executor.New(2, st, nil)

	j := &config.Job{Name: "test-job", Command: "echo $HUSKY_HOST_TEST_VAR"}
	logs := runJobLogs(t, st, ex, j)

	assert.Equal(t, "host_value", firstStdout(logs))
}
