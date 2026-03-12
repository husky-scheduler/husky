package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/husky-scheduler/husky/internal/config"
)

// ReadyFunc is called by the Scheduler when a job is due to run.
// jobName is the job identifier; scheduledAt is the time it was scheduled for.
type ReadyFunc func(ctx context.Context, jobName string, scheduledAt time.Time)

// Scheduler evaluates all configured jobs every second and fires ReadyFunc
// when a job's next-run time is reached.
type Scheduler struct {
	cfg    *config.Config
	logger *slog.Logger
	onRun  ReadyFunc

	mu      sync.Mutex
	next    map[string]time.Time // keyed by job name
	manualC chan manualTrigger   // signals from TriggerRun

	// Jitter, when positive, adds a uniform random delay in [0, Jitter) to
	// each job's next scheduled run time.  This spreads simultaneous cron
	// firings to avoid thundering-herd bursts.
	Jitter time.Duration
}

type manualTrigger struct {
	jobName string
	reason  string
}

// New creates a Scheduler and pre-computes the initial next-run times for all jobs.
func New(cfg *config.Config, logger *slog.Logger, onRun ReadyFunc) *Scheduler {
	s := &Scheduler{
		cfg:     cfg,
		logger:  logger,
		onRun:   onRun,
		next:    make(map[string]time.Time),
		manualC: make(chan manualTrigger, 64),
	}
	s.recompute(time.Now())
	return s
}

// Start runs the 1-second ticker loop until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.tick(ctx, t)
		case m := <-s.manualC:
			s.logger.Info("manual trigger", "job", m.jobName, "reason", m.reason)
			go s.onRun(ctx, m.jobName, time.Now())
		}
	}
}

// TriggerRun schedules an immediate manual run of jobName.
// It returns an error if jobName is not in the current configuration.
// The call is non-blocking; the run is dispatched on the next scheduler loop.
func (s *Scheduler) TriggerRun(jobName, reason string) error {
	s.mu.Lock()
	_, ok := s.cfg.Jobs[jobName]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("scheduler: unknown job %q", jobName)
	}
	select {
	case s.manualC <- manualTrigger{jobName: jobName, reason: reason}:
	default:
		return fmt.Errorf("scheduler: trigger queue full, try again")
	}
	return nil
}

// LogSchedule logs a startup summary for every job at INFO level.
func (s *Scheduler) LogSchedule() {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	now := time.Now()
	for name, job := range cfg.Jobs {
		msg := StartupSummary(name, job, cfg.Defaults, now)
		s.logger.Info(msg)
	}
}

// NextFor returns the pre-computed next-run time for jobName.
// Returns zero time for jobs that are not auto-scheduled.
func (s *Scheduler) NextFor(jobName string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next[jobName]
}

// Reload atomically swaps the configuration and recomputes next-run times.
func (s *Scheduler) Reload(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.next = make(map[string]time.Time)
	s.recomputeLocked(time.Now())
}

// recompute rebuilds s.next for all jobs. Caller must NOT hold s.mu.
func (s *Scheduler) recompute(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recomputeLocked(now)
}

// recomputeLocked rebuilds s.next. Caller MUST hold s.mu.
func (s *Scheduler) recomputeLocked(now time.Time) {
	for name, job := range s.cfg.Jobs {
		t, anomaly := NextRunTime(job, s.cfg.Defaults, now)
		if s.Jitter > 0 && !t.IsZero() {
			t = t.Add(time.Duration(rand.Int63n(int64(s.Jitter))))
		}
		s.next[name] = t
		if anomaly != nil {
			s.logger.Warn(anomaly.String())
		}
	}
}

// tick checks every job and fires onRun for any job whose scheduled time has
// been reached. After firing, it advances the job's next-run time.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, job := range s.cfg.Jobs {
		freq := strings.ToLower(strings.TrimSpace(job.Frequency))
		if freq == "manual" || strings.HasPrefix(freq, "after:") {
			continue
		}

		scheduled := s.next[name]
		if scheduled.IsZero() {
			continue
		}
		if !now.Before(scheduled) { // now >= scheduled
			go s.onRun(ctx, name, scheduled)
			// Advance to the next occurrence.
			next, anomaly := NextRunTime(job, s.cfg.Defaults, now)
			if s.Jitter > 0 && !next.IsZero() {
				next = next.Add(time.Duration(rand.Int63n(int64(s.Jitter))))
			}
			s.next[name] = next
			if anomaly != nil {
				s.logger.Warn(anomaly.String())
			}
		}
	}
}