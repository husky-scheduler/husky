package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/store"
)

// openTestDB opens an in-memory SQLite database via a temp-dir file.
// Using a file (not ":memory:") exercises WAL-mode migration.
func openTestDB(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func ptr[T any](v T) *T { return &v }

// ─── Open / Close ─────────────────────────────────────────────────────────────

func TestOpen(t *testing.T) {
	s := openTestDB(t)
	assert.NotNil(t, s)
}

func TestClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestOpen_InvalidPath(t *testing.T) {
	_, err := store.Open("/nonexistent/deeply/nested/path/test.db")
	require.Error(t, err)
}

// ─── RecordRunStart ───────────────────────────────────────────────────────────

func TestRecordRunStart_ReturnsID(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
}

func TestRecordRunStart_DefaultStatus(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, store.StatusPending, run.Status)
}

func TestRecordRunStart_DefaultTriggeredBy(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Status:  store.StatusPending,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, "scheduler", run.TriggeredBy)
}

func TestRecordRunStart_WithReason(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	start := time.Now().UTC().Truncate(time.Second)
	id, err := s.RecordRunStart(ctx, store.Run{
		JobName:     "ingest",
		Status:      store.StatusRunning,
		Attempt:     1,
		Trigger:     store.TriggerManual,
		TriggeredBy: "alice",
		Reason:      "hotfix deployment",
		StartedAt:   &start,
	})
	require.NoError(t, err)

	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, "ingest", run.JobName)
	assert.Equal(t, store.StatusRunning, run.Status)
	assert.Equal(t, 1, run.Attempt)
	assert.Equal(t, store.TriggerManual, run.Trigger)
	assert.Equal(t, "alice", run.TriggeredBy)
	assert.Equal(t, "hotfix deployment", run.Reason)
	require.NotNil(t, run.StartedAt)
	assert.Equal(t, start, *run.StartedAt)
}

func TestRecordRunStart_MultipleRunsAutoincrement(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id1, err := s.RecordRunStart(ctx, store.Run{JobName: "j1", Status: store.StatusPending, Trigger: store.TriggerSchedule})
	require.NoError(t, err)
	id2, err := s.RecordRunStart(ctx, store.Run{JobName: "j2", Status: store.StatusPending, Trigger: store.TriggerSchedule})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id1)
	assert.Equal(t, int64(2), id2)
}

// ─── RecordRunEnd ─────────────────────────────────────────────────────────────

func TestRecordRunEnd_Success(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "transform",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	finished := time.Now().UTC().Truncate(time.Second)
	err = s.RecordRunEnd(ctx, id, store.StatusSuccess, "", ptr(0), finished, false, nil)
	require.NoError(t, err)

	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, store.StatusSuccess, run.Status)
	require.NotNil(t, run.ExitCode)
	assert.Equal(t, 0, *run.ExitCode)
	require.NotNil(t, run.FinishedAt)
	assert.Equal(t, finished, *run.FinishedAt)
	assert.False(t, run.SLABreached)
	assert.Nil(t, run.HCStatus)
}

func TestRecordRunEnd_FailedWithSLABreach(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "report",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	finished := time.Now().UTC().Truncate(time.Second)
	hc := store.HCFail
	err = s.RecordRunEnd(ctx, id, store.StatusFailed, "", ptr(1), finished, true, &hc)
	require.NoError(t, err)

	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, store.StatusFailed, run.Status)
	assert.True(t, run.SLABreached)
	require.NotNil(t, run.HCStatus)
	assert.Equal(t, store.HCFail, *run.HCStatus)
}

func TestRecordRunEnd_HCWarn(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "check",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	hc := store.HCWarn
	err = s.RecordRunEnd(ctx, id, store.StatusSuccess, "", ptr(0), time.Now().UTC(), false, &hc)
	require.NoError(t, err)

	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, run.HCStatus)
	assert.Equal(t, store.HCWarn, *run.HCStatus)
}

// ─── GetRun ───────────────────────────────────────────────────────────────────

func TestGetRun_NotFound(t *testing.T) {
	s := openTestDB(t)
	run, err := s.GetRun(context.Background(), 9999)
	require.NoError(t, err)
	assert.Nil(t, run)
}

// ─── RecordLog ────────────────────────────────────────────────────────────────

func TestRecordLog(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	lines := []store.LogLine{
		{RunID: id, Seq: 0, Stream: "stdout", Line: "starting", TS: time.Now().UTC()},
		{RunID: id, Seq: 1, Stream: "stdout", Line: "row 1 processed", TS: time.Now().UTC()},
		{RunID: id, Seq: 2, Stream: "stderr", Line: "warning: low memory", TS: time.Now().UTC()},
	}
	for _, l := range lines {
		require.NoError(t, s.RecordLog(ctx, l))
	}
}

func TestRecordLog_HealthcheckStream(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "check",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	err = s.RecordLog(ctx, store.LogLine{
		RunID:  id,
		Seq:    0,
		Stream: "healthcheck",
		Line:   "HTTP 200 OK",
		TS:     time.Now().UTC(),
	})
	require.NoError(t, err)
}

// ─── RecordOutput ─────────────────────────────────────────────────────────────

func TestRecordOutput(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	out := store.RunOutput{
		RunID:   id,
		JobName: "ingest",
		VarName: "file_path",
		Value:   "/tmp/data_20260304.csv",
		CycleID: "cycle-abc-123",
	}
	require.NoError(t, s.RecordOutput(ctx, out))
}

func TestRecordOutput_MultipleSameCycle(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "transform",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	cycleID := "cycle-xyz"
	outputs := []store.RunOutput{
		{RunID: id, JobName: "transform", VarName: "row_count", Value: "42", CycleID: cycleID},
		{RunID: id, JobName: "transform", VarName: "output_file", Value: "/out.parquet", CycleID: cycleID},
	}
	for _, o := range outputs {
		require.NoError(t, s.RecordOutput(ctx, o))
	}
}

// ─── UpdateJobState / GetJobState ─────────────────────────────────────────────

func TestUpdateJobState_Insert(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	next := now.Add(time.Hour)

	err := s.UpdateJobState(ctx, store.JobState{
		JobName:     "ingest",
		LastSuccess: &now,
		NextRun:     &next,
	})
	require.NoError(t, err)

	state, err := s.GetJobState(ctx, "ingest")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "ingest", state.JobName)
	require.NotNil(t, state.LastSuccess)
	assert.Equal(t, now, *state.LastSuccess)
	require.NotNil(t, state.NextRun)
	assert.Equal(t, next, *state.NextRun)
	assert.Nil(t, state.LastFailure)
	assert.Nil(t, state.LockPID)
}

func TestUpdateJobState_Upsert(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	t1 := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.UpdateJobState(ctx, store.JobState{
		JobName:     "ingest",
		LastSuccess: &t1,
	}))

	t2 := t1.Add(time.Hour)
	require.NoError(t, s.UpdateJobState(ctx, store.JobState{
		JobName:     "ingest",
		LastFailure: &t2,
	}))

	state, err := s.GetJobState(ctx, "ingest")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Nil(t, state.LastSuccess)
	require.NotNil(t, state.LastFailure)
	assert.Equal(t, t2, *state.LastFailure)
}

func TestUpdateJobState_WithLockPID(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	pid := 12345
	require.NoError(t, s.UpdateJobState(ctx, store.JobState{
		JobName: "report",
		LockPID: &pid,
	}))

	state, err := s.GetJobState(ctx, "report")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.LockPID)
	assert.Equal(t, pid, *state.LockPID)
}

func TestUpdateJobState_ClearLockPID(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	pid := 99
	require.NoError(t, s.UpdateJobState(ctx, store.JobState{JobName: "report", LockPID: &pid}))
	require.NoError(t, s.UpdateJobState(ctx, store.JobState{JobName: "report", LockPID: nil}))

	state, err := s.GetJobState(ctx, "report")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Nil(t, state.LockPID)
}

// ─── GetJobState ──────────────────────────────────────────────────────────────

func TestGetJobState_NotFound(t *testing.T) {
	s := openTestDB(t)
	state, err := s.GetJobState(context.Background(), "unknown-job")
	require.NoError(t, err)
	assert.Nil(t, state)
}

// ─── Context cancellation ─────────────────────────────────────────────────────

func TestWrite_CancelledContext(t *testing.T) {
	s := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Status:  store.StatusPending,
		Trigger: store.TriggerSchedule,
	})
	require.ErrorIs(t, err, context.Canceled)
}

// ─── Concurrent reads ─────────────────────────────────────────────────────────

func TestConcurrentReads(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: "ingest",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)
	require.NoError(t, s.UpdateJobState(ctx, store.JobState{JobName: "ingest"}))

	errc := make(chan error, 10)
	for i := 0; i < 5; i++ {
		go func() {
			_, e := s.GetRun(ctx, id)
			errc <- e
		}()
		go func() {
			_, e := s.GetJobState(ctx, "ingest")
			errc <- e
		}()
	}
	for i := 0; i < 10; i++ {
		assert.NoError(t, <-errc)
	}
}
