package daemoncmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/store"
)

func openDaemonTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestIntegration_ReconcileOrphans_MarksRunningAsFailed(t *testing.T) {
	st := openDaemonTestStore(t)
	ctx := context.Background()

	_, err := st.RecordRunStart(ctx, store.Run{
		JobName: "orphaned",
		Status:  store.StatusRunning,
		Attempt: 1,
		Trigger: store.TriggerSchedule,
	})
	require.NoError(t, err)

	d := &daemon{
		logger: testLogger(),
		st:     st,
		cfg: &config.Config{
			Jobs: map[string]*config.Job{
				"orphaned": {Name: "orphaned"},
			},
		},
	}

	d.reconcileOrphans(ctx)

	running, err := st.ListRunsByStatus(ctx, store.StatusRunning)
	require.NoError(t, err)
	assert.Len(t, running, 0)

	failed, err := st.ListRunsByStatus(ctx, store.StatusFailed)
	require.NoError(t, err)
	assert.Len(t, failed, 1)
	assert.Equal(t, "orphaned", failed[0].JobName)
}
