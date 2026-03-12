package executor_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/executor"
	"github.com/husky-scheduler/husky/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func job(cmd string) *config.Job {
	return &config.Job{Name: "test-job", Command: cmd}
}

func TestExecutor_SuccessfulExit(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ctx := context.Background()
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "test-job", Status: store.StatusRunning, Trigger: store.TriggerManual,
	})
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	var got executor.Result
	ex.Submit(ctx, job("exit 0"), runID, executor.RunOpts{}, func(r executor.Result) { got = r; wg.Done() })
	wg.Wait()
	assert.Equal(t, 0, got.ExitCode)
	assert.NoError(t, got.Err)
	assert.Greater(t, got.Elapsed, time.Duration(0))
}

func TestExecutor_NonZeroExit(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ctx := context.Background()
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "test-job", Status: store.StatusRunning, Trigger: store.TriggerManual,
	})
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	var got executor.Result
	ex.Submit(ctx, job("exit 42"), runID, executor.RunOpts{}, func(r executor.Result) { got = r; wg.Done() })
	wg.Wait()
	assert.Equal(t, 42, got.ExitCode)
	assert.NoError(t, got.Err, "non-zero exit should not set Err")
}

func TestExecutor_StdoutCaptured(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ctx := context.Background()
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "test-job", Status: store.StatusRunning, Trigger: store.TriggerManual,
	})
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	ex.Submit(ctx, job("echo hello"), runID, executor.RunOpts{}, func(_ executor.Result) { wg.Done() })
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	logs, err := st.GetRunLogs(ctx, runID)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	assert.Equal(t, "hello", logs[0].Line)
	assert.Equal(t, "stdout", logs[0].Stream)
}

func TestExecutor_Timeout(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ctx := context.Background()
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "test-job", Status: store.StatusRunning, Trigger: store.TriggerManual,
	})
	require.NoError(t, err)
	j := &config.Job{Name: "test-job", Command: "sleep 60", Timeout: "100ms"}
	var wg sync.WaitGroup
	wg.Add(1)
	var got executor.Result
	ex.Submit(ctx, j, runID, executor.RunOpts{}, func(r executor.Result) { got = r; wg.Done() })
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(executor.GracePeriod + 2*time.Second):
		t.Fatal("timed out waiting for job to be killed")
	}
	assert.ErrorIs(t, got.Err, executor.ErrTimeout)
	assert.Equal(t, -1, got.ExitCode)
}

func TestExecutor_ContextCancellation(t *testing.T) {
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ctx, cancel := context.WithCancel(context.Background())
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "test-job", Status: store.StatusRunning, Trigger: store.TriggerManual,
	})
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	var got executor.Result
	ex.Submit(ctx, job("sleep 60"), runID, executor.RunOpts{}, func(r executor.Result) { got = r; wg.Done() })
	time.Sleep(100 * time.Millisecond)
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(executor.GracePeriod + 2*time.Second):
		t.Fatal("timed out waiting for job to be cancelled")
	}
	assert.Error(t, got.Err)
	assert.Equal(t, -1, got.ExitCode)
}

func TestExecutor_WorkingDir(t *testing.T) {
	dir := t.TempDir()
	st := openTestStore(t)
	ex := executor.New(2, st, nil)
	ctx := context.Background()
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "test-job", Status: store.StatusRunning, Trigger: store.TriggerManual,
	})
	require.NoError(t, err)
	j := &config.Job{Name: "test-job", Command: "pwd", WorkingDir: dir}
	var wg sync.WaitGroup
	wg.Add(1)
	ex.Submit(ctx, j, runID, executor.RunOpts{}, func(_ executor.Result) { wg.Done() })
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	logs, err := st.GetRunLogs(ctx, runID)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	got, _ := filepath.EvalSymlinks(logs[0].Line)
	want, _ := filepath.EvalSymlinks(dir)
	assert.Equal(t, want, got)
}
