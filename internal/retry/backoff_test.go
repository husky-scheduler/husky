package retry_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	retry "github.com/husky-scheduler/husky/internal/retry"
)

func within(t *testing.T, want, got time.Duration, frac float64) {
	t.Helper()
	delta := time.Duration(float64(want) * frac)
	lo, hi := want-delta, want+delta
	assert.GreaterOrEqualf(t, got, lo, "expected >= %s, got %s", lo, got)
	assert.LessOrEqualf(t, got, hi, "expected <= %s, got %s", hi, got)
}

func TestDelay_FirstAttemptIsZero(t *testing.T) {
	assert.Equal(t, time.Duration(0), retry.Delay("", 1))
	assert.Equal(t, time.Duration(0), retry.Delay("exponential", 1))
	assert.Equal(t, time.Duration(0), retry.Delay("fixed:10s", 1))
}

func TestDelay_ZeroOrNegativeAttemptIsZero(t *testing.T) {
	assert.Equal(t, time.Duration(0), retry.Delay("", 0))
	assert.Equal(t, time.Duration(0), retry.Delay("", -5))
}

func TestDelay_Exponential_Attempt2(t *testing.T) {
	within(t, 30*time.Second, retry.Delay("", 2), 0.26)
}

func TestDelay_Exponential_Attempt3(t *testing.T) {
	within(t, 60*time.Second, retry.Delay("exponential", 3), 0.26)
}

func TestDelay_Exponential_Attempt4(t *testing.T) {
	within(t, 120*time.Second, retry.Delay("exponential", 4), 0.26)
}

func TestDelay_Exponential_Grows(t *testing.T) {
	const samples = 20
	var prev float64
	for attempt := 2; attempt <= 6; attempt++ {
		var sum float64
		for i := 0; i < samples; i++ {
			sum += float64(retry.Delay("exponential", attempt))
		}
		avg := sum / float64(samples)
		if attempt > 2 {
			assert.Greater(t, avg, prev*1.5,
				"attempt %d avg should be > 1.5x attempt %d avg", attempt, attempt-1)
		}
		prev = avg
	}
}

func TestDelay_Fixed_45s(t *testing.T) {
	within(t, 45*time.Second, retry.Delay("fixed:45s", 2), 0.26)
}

func TestDelay_Fixed_SameAcrossAttempts(t *testing.T) {
	for attempt := 2; attempt <= 5; attempt++ {
		within(t, 30*time.Second, retry.Delay("fixed:30s", attempt), 0.26)
	}
}

func TestDelay_Fixed_InvalidFallsBack(t *testing.T) {
	within(t, 30*time.Second, retry.Delay("fixed:notaduration", 2), 0.26)
}

func TestDelay_Jitter_Varies(t *testing.T) {
	first := retry.Delay("exponential", 2)
	varied := false
	for i := 0; i < 50; i++ {
		if retry.Delay("exponential", 2) != first {
			varied = true
			break
		}
	}
	assert.True(t, varied, "jitter should produce different values")
}

func TestDelay_NeverNegative(t *testing.T) {
	for i := 0; i < 1000; i++ {
		assert.GreaterOrEqual(t, int64(retry.Delay("exponential", 2)), int64(0))
	}
}
