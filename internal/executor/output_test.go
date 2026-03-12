package executor_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/executor"
	"github.com/husky-scheduler/husky/internal/store"
)

// run submits a job and waits for completion, returning the Result.
func run(t *testing.T, ex *executor.Executor, st *store.Store, j *config.Job, cycleID string) executor.Result {
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
	var got executor.Result
	ex.Submit(ctx, j, runID, executor.RunOpts{CycleID: cycleID}, func(r executor.Result) {
		got = r
		wg.Done()
	})
	wg.Wait()
	return got
}

func TestOutput_LastLine(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "out-job",
		Command: `printf 'alpha\nbeta\ngamma\n'`,
		Output:  map[string]string{"result": "last_line"},
	}
	res := run(t, ex, st, j, "cycle-ll")
	require.NoError(t, res.Err)

	out, err := st.GetRunOutput(context.Background(), "cycle-ll", "out-job", "result")
	require.NoError(t, err)
	assert.Equal(t, "gamma", out.Value)
}

func TestOutput_FirstLine(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "out-job",
		Command: `printf 'alpha\nbeta\ngamma\n'`,
		Output:  map[string]string{"result": "first_line"},
	}
	res := run(t, ex, st, j, "cycle-fl")
	require.NoError(t, res.Err)

	out, err := st.GetRunOutput(context.Background(), "cycle-fl", "out-job", "result")
	require.NoError(t, err)
	assert.Equal(t, "alpha", out.Value)
}

func TestOutput_ExitCode(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "out-job",
		Command: `exit 7`,
		Output:  map[string]string{"code": "exit_code"},
	}
	// exit 7 is non-zero, so Err will be set — that's expected.
	run(t, ex, st, j, "cycle-ec")

	out, err := st.GetRunOutput(context.Background(), "cycle-ec", "out-job", "code")
	require.NoError(t, err)
	assert.Equal(t, "7", out.Value)
}

func TestOutput_JSONField(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "out-job",
		Command: `echo '{"status":"ok","count":42}'`,
		Output:  map[string]string{"count": "json_field:count"},
	}
	res := run(t, ex, st, j, "cycle-jf")
	require.NoError(t, res.Err)

	out, err := st.GetRunOutput(context.Background(), "cycle-jf", "out-job", "count")
	require.NoError(t, err)
	assert.Equal(t, "42", out.Value)
}

func TestOutput_Regex_CaptureGroup(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "out-job",
		Command: `echo 'processed 17 records'`,
		Output:  map[string]string{"n": `regex:processed (\d+) records`},
	}
	res := run(t, ex, st, j, "cycle-rg")
	require.NoError(t, res.Err)

	out, err := st.GetRunOutput(context.Background(), "cycle-rg", "out-job", "n")
	require.NoError(t, err)
	assert.Equal(t, "17", out.Value)
}

func TestOutput_Regex_WholeMatch(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "out-job",
		Command: `echo 'DONE'`,
		Output:  map[string]string{"status": `regex:DONE`},
	}
	res := run(t, ex, st, j, "cycle-rwm")
	require.NoError(t, res.Err)

	out, err := st.GetRunOutput(context.Background(), "cycle-rwm", "out-job", "status")
	require.NoError(t, err)
	assert.Equal(t, "DONE", out.Value)
}

func TestHealthcheck_Pass(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "hc-job",
		Command: `exit 0`,
		Healthcheck: &config.Healthcheck{
			Command: `exit 0`,
			OnFail:  "mark_failed",
		},
	}
	res := run(t, ex, st, j, "cycle-hcp")
	require.NoError(t, res.Err)
	require.NotNil(t, res.HCStatus)
	assert.Equal(t, store.HCPass, *res.HCStatus)
}

func TestHealthcheck_Fail_MarkFailed(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "hc-job",
		Command: `exit 0`,
		Healthcheck: &config.Healthcheck{
			Command: `exit 1`,
			OnFail:  "mark_failed",
		},
	}
	res := run(t, ex, st, j, "cycle-hcf")
	assert.Error(t, res.Err)
	assert.Equal(t, -2, res.ExitCode)
	require.NotNil(t, res.HCStatus)
	assert.Equal(t, store.HCFail, *res.HCStatus)
}

func TestHealthcheck_Fail_WarnOnly(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "hc-job",
		Command: `exit 0`,
		Healthcheck: &config.Healthcheck{
			Command: `exit 2`,
			OnFail:  "warn_only",
		},
	}
	res := run(t, ex, st, j, "cycle-hcw")
	assert.NoError(t, res.Err)
	require.NotNil(t, res.HCStatus)
	assert.Equal(t, store.HCWarn, *res.HCStatus)
}

func TestHealthcheck_SkippedOnMainFailure(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "hc-job",
		Command: `exit 1`,
		Healthcheck: &config.Healthcheck{
			Command: `exit 0`,
			OnFail:  "mark_failed",
		},
	}
	res := run(t, ex, st, j, "cycle-hcskip")
	assert.NotEqual(t, 0, res.ExitCode, "main command should have failed")
	assert.Nil(t, res.HCStatus, "healthcheck should be skipped when main command fails")
}

func TestHealthcheck_Timeout_TreatedAsFailure(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	j := &config.Job{
		Name:    "hc-job",
		Command: `exit 0`,
		Healthcheck: &config.Healthcheck{
			Command: `sleep 2`,
			Timeout: "50ms",
			OnFail:  "mark_failed",
		},
	}
	res := run(t, ex, st, j, "cycle-hctimeout")
	assert.Error(t, res.Err)
	assert.Equal(t, -2, res.ExitCode)
	require.NotNil(t, res.HCStatus)
	assert.Equal(t, store.HCFail, *res.HCStatus)
}
