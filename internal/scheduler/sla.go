package scheduler

import "time"

// StartSLATimer starts a time.AfterFunc timer that calls onBreach after d.
// The callback is suppressed if isRunning returns false when the timer fires,
// meaning the job finished before the SLA deadline was reached.
// Returns nil when d is zero or negative.
func StartSLATimer(d time.Duration, isRunning func() bool, onBreach func()) *time.Timer {
	if d <= 0 {
		return nil
	}
	return time.AfterFunc(d, func() {
		if isRunning() {
			onBreach()
		}
	})
}
