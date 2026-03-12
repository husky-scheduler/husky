package scheduler_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/scheduler"
)

// TestStartSLATimer_ZeroDuration verifies that a zero duration returns nil
// (no timer created) so callers do not need to nil-check the SLA field.
func TestStartSLATimer_ZeroDuration(t *testing.T) {
	timer := scheduler.StartSLATimer(0, func() bool { return true }, func() {})
	assert.Nil(t, timer, "zero duration must return nil")
}

// TestStartSLATimer_NegativeDuration verifies that a negative duration
// is treated the same as zero — no timer created.
func TestStartSLATimer_NegativeDuration(t *testing.T) {
	timer := scheduler.StartSLATimer(-5*time.Second, func() bool { return true }, func() {})
	assert.Nil(t, timer, "negative duration must return nil")
}

// TestStartSLATimer_FiresWhenRunning verifies that onBreach is called exactly
// once when the job is still running when the timer fires.
func TestStartSLATimer_FiresWhenRunning(t *testing.T) {
	var called atomic.Int32
	timer := scheduler.StartSLATimer(
		20*time.Millisecond,
		func() bool { return true }, // job is running
		func() { called.Add(1) },
	)
	require.NotNil(t, timer)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int32(1), called.Load(), "onBreach should fire exactly once")
}

// TestStartSLATimer_SuppressedWhenNotRunning verifies that onBreach is NOT
// called when isRunning returns false (job finished before SLA deadline).
func TestStartSLATimer_SuppressedWhenNotRunning(t *testing.T) {
	var called atomic.Int32
	timer := scheduler.StartSLATimer(
		20*time.Millisecond,
		func() bool { return false }, // job already finished
		func() { called.Add(1) },
	)
	require.NotNil(t, timer)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load(), "onBreach must not fire when job is not running")
}

// TestStartSLATimer_StopPreventsCallback verifies that calling Stop() before
// the timer fires prevents the callback from being invoked.
func TestStartSLATimer_StopPreventsCallback(t *testing.T) {
	var called atomic.Int32
	timer := scheduler.StartSLATimer(
		100*time.Millisecond,
		func() bool { return true },
		func() { called.Add(1) },
	)
	require.NotNil(t, timer)
	stopped := timer.Stop()
	assert.True(t, stopped, "Stop should return true when timer was pending")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load(), "onBreach must not fire after Stop()")
}

// TestStartSLATimer_TransitionToNotRunning verifies that a race between the
// job finishing and the timer firing is handled correctly: if isRunning changes
// to false before the goroutine runs, the breach is suppressed.
func TestStartSLATimer_TransitionToNotRunning(t *testing.T) {
	var running atomic.Bool
	running.Store(true)
	var called atomic.Int32

	timer := scheduler.StartSLATimer(
		30*time.Millisecond,
		func() bool { return running.Load() },
		func() { called.Add(1) },
	)
	require.NotNil(t, timer)

	// Mark job as completed before the timer fires.
	time.Sleep(10 * time.Millisecond)
	running.Store(false)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load(), "breach must be suppressed when job finishes before timer fires")
}

// TestStartSLATimer_FiringOrder verifies that the breach fires within a
// reasonable tolerance of the configured duration.
func TestStartSLATimer_FiringOrder(t *testing.T) {
	start := time.Now()
	done := make(chan struct{})
	scheduler.StartSLATimer(
		30*time.Millisecond,
		func() bool { return true },
		func() { close(done) },
	)
	select {
	case <-done:
		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, 30*time.Millisecond, "must not fire early")
		assert.Less(t, elapsed, 200*time.Millisecond, "must fire within 200ms")
	case <-time.After(1 * time.Second):
		t.Fatal("timer did not fire within 1 second")
	}
}
