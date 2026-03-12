// Package retry implements the backoff delay strategy for job retry attempts.
//
// Two strategies are supported:
//   - "exponential" (default): 30 s × 2^(retryNum-1), ±25% random jitter.
//     Sequence: ~30 s, ~60 s, ~120 s, ~240 s …
//   - "fixed:<duration>": constant d, ±25% random jitter.
//
// Jitter prevents multiple simultaneously-failing jobs from thundering-herd
// on shared resources when they all retry at the same moment.
package retry

import (
	"math/rand/v2"
	"strings"
	"time"
)

const (
	// BaseDelay is the seed delay for exponential backoff.
	BaseDelay = 30 * time.Second

	// JitterFraction is the maximum ±fraction applied to any delay.
	JitterFraction = 0.25
)

// Delay returns the wait duration to observe before attempt number n.
// n is 1-indexed: n=1 is the initial execution (no delay), n=2 is the first
// retry attempt. The retryDelay string uses the same format as husky.yaml.
func Delay(retryDelay string, attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	retryNum := attempt - 1 // how many retries have already occurred

	var base time.Duration
	if strings.HasPrefix(retryDelay, "fixed:") {
		d, err := time.ParseDuration(strings.TrimPrefix(retryDelay, "fixed:"))
		if err != nil || d <= 0 {
			d = BaseDelay
		}
		base = d
	} else {
		// Exponential: BaseDelay * 2^(retryNum-1).
		base = BaseDelay
		for i := 1; i < retryNum; i++ {
			base *= 2
		}
	}

	// Apply ±JitterFraction random jitter.
	jitter := time.Duration(float64(base) * JitterFraction * (rand.Float64()*2 - 1))
	result := base + jitter
	if result < 0 {
		result = 0
	}
	return result
}
