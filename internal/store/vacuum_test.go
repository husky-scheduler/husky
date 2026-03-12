package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/store"
)

// insertFinished creates a completed job_run with a specific finished_at time.
func insertFinished(t *testing.T, s *store.Store, jobName string, finishedAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: jobName,
		Status:  store.StatusRunning,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)
	code := 0
	err = s.RecordRunEnd(ctx, id, store.StatusSuccess, "", &code, finishedAt, false, nil)
	require.NoError(t, err)
	return id
}

// insertPending creates a PENDING run (never finishes).
func insertPending(t *testing.T, s *store.Store, jobName string) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := s.RecordRunStart(ctx, store.Run{
		JobName: jobName,
		Status:  store.StatusPending,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)
	return id
}

// runExists reports whether the given run ID still exists in job_runs.
func runExists(t *testing.T, s *store.Store, id int64) bool {
	t.Helper()
	ctx := context.Background()
	run, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	return run != nil
}

func TestVacuum_MaxAge_DeletesOldRows(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	maxAge := 24 * time.Hour

	oldID := insertFinished(t, s, "job-a", now.Add(-48*time.Hour))
	recentID := insertFinished(t, s, "job-a", now.Add(-1*time.Hour))

	result, err := s.Vacuum(ctx, maxAge, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.RunsDeleted)
	assert.False(t, runExists(t, s, oldID), "old run should be deleted")
	assert.True(t, runExists(t, s, recentID), "recent run should be kept")
}

func TestVacuum_MaxAge_KeepsPendingAndRunning(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	maxAge := 1 * time.Second

	pendingID := insertPending(t, s, "job-b")

	result, err := s.Vacuum(ctx, maxAge, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.RunsDeleted)
	assert.True(t, runExists(t, s, pendingID), "PENDING run must never be pruned")
}

func TestVacuum_MaxAge_NoRowsOlderThanWindow_NoDeletion(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	insertFinished(t, s, "job-c", time.Now().Add(-10*time.Second))
	insertFinished(t, s, "job-c", time.Now().Add(-20*time.Second))

	result, err := s.Vacuum(ctx, 24*time.Hour, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.RunsDeleted)
}

func TestVacuum_MaxRunsPerJob_KeepsNMostRecent(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	ids := make([]int64, 5)
	for i := range ids {
		ids[i] = insertFinished(t, s, "heavy-job", now.Add(-time.Duration(5-i)*time.Hour))
	}

	result, err := s.Vacuum(ctx, 0, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.RunsDeleted, "should delete the 2 oldest runs")

	assert.False(t, runExists(t, s, ids[0]), "oldest run should be deleted")
	assert.False(t, runExists(t, s, ids[1]), "second-oldest run should be deleted")
	assert.True(t, runExists(t, s, ids[2]), "third run should be kept")
	assert.True(t, runExists(t, s, ids[3]), "fourth run should be kept")
	assert.True(t, runExists(t, s, ids[4]), "newest run should be kept")
}

func TestVacuum_MaxRunsPerJob_BelowCap_NoDeletion(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	insertFinished(t, s, "light-job", now.Add(-2*time.Hour))
	insertFinished(t, s, "light-job", now.Add(-1*time.Hour))

	result, err := s.Vacuum(ctx, 0, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.RunsDeleted)
}

func TestVacuum_MaxRunsPerJob_IgnoresPendingInCap(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	ids := make([]int64, 3)
	for i := range ids {
		ids[i] = insertFinished(t, s, "mixed-job", now.Add(-time.Duration(3-i)*time.Hour))
	}
	pendingID := insertPending(t, s, "mixed-job")

	result, err := s.Vacuum(ctx, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.RunsDeleted)
	assert.False(t, runExists(t, s, ids[0]), "oldest completed run should be deleted")
	assert.True(t, runExists(t, s, ids[1]), "second completed run should be kept")
	assert.True(t, runExists(t, s, ids[2]), "newest completed run should be kept")
	assert.True(t, runExists(t, s, pendingID), "PENDING run must never be pruned")
}

func TestVacuum_Combined_MaxAgeAndMaxRunsApplied(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	oldA := insertFinished(t, s, "combo-job", now.Add(-48*time.Hour))
	oldB := insertFinished(t, s, "combo-job", now.Add(-36*time.Hour))

	recent := make([]int64, 4)
	for i := range recent {
		recent[i] = insertFinished(t, s, "combo-job", now.Add(-time.Duration(4-i)*time.Hour))
	}

	result, err := s.Vacuum(ctx, 24*time.Hour, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(4), result.RunsDeleted)

	assert.False(t, runExists(t, s, oldA))
	assert.False(t, runExists(t, s, oldB))
	assert.False(t, runExists(t, s, recent[0]))
	assert.False(t, runExists(t, s, recent[1]))
	assert.True(t, runExists(t, s, recent[2]))
	assert.True(t, runExists(t, s, recent[3]))
}
