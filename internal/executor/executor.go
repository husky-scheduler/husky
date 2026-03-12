// Package executor runs job subprocesses in a bounded goroutine pool.
//
// Each job is executed as `/bin/sh -c <command>` (or `cmd.exe /C` on Windows)
// inside an isolated process group (Setpgid=true on Unix). Stdout and stderr
// are captured line-by-line and persisted to the store. When a timeout or
// context cancellation occurs the executor sends SIGTERM to the entire process
// group, waits for a grace period, and then sends SIGKILL.
//
// Phase 2 additions:
//   - Per-job cancel map: Cancel(jobName) + IsRunning(jobName)
//   - Healthcheck: runs healthcheck.command after a successful main exit
//   - Output capture: captures stdout into variables per the output block
package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/store"
)

const (
	// DefaultPoolSize is the number of concurrent job slots.
	DefaultPoolSize = 8

	// DefaultTimeout is applied when a job has no explicit timeout.
	DefaultTimeout = 30 * time.Minute

	// DefaultHealthcheckTimeout is the maximum runtime for a healthcheck command.
	DefaultHealthcheckTimeout = 30 * time.Second

	// GracePeriod is how long the executor waits after SIGTERM before SIGKILL.
	GracePeriod = 10 * time.Second
)

// ErrTimeout is returned when a job exceeds its allowed run time.
var ErrTimeout = errors.New("job timed out")

// Result is the outcome of a single job execution (main + optional healthcheck).
type Result struct {
	ExitCode int
	Err      error
	Elapsed  time.Duration
	// HCStatus is non-nil when a healthcheck was evaluated.
	HCStatus *store.HCStatus
}

// RunOpts carries per-run optional parameters.
type RunOpts struct {
	// CycleID is propagated to run_outputs records; empty for non-DAG runs.
	CycleID string
}

// Executor runs jobs in a bounded goroutine pool.
type Executor struct {
	sem    chan struct{} // counting semaphore
	st     *store.Store
	logger *slog.Logger

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // job name → cancel for its current run

	// Shell overrides the platform-default shell used to run commands.
	// When empty, shellCommand() (build-tag default) is used.
	Shell string

	// GlobalEnv injects key-value pairs into every job's environment.
	// Priority: host env < GlobalEnv < per-job env.
	GlobalEnv map[string]string
}

// New creates an Executor with a pool of poolSize concurrent job slots.
// If poolSize is <= 0 it defaults to DefaultPoolSize.
func New(poolSize int, st *store.Store, logger *slog.Logger) *Executor {
	if poolSize <= 0 {
		poolSize = DefaultPoolSize
	}
	return &Executor{
		sem:     make(chan struct{}, poolSize),
		st:      st,
		logger:  logger,
		cancels: make(map[string]context.CancelFunc),
	}
}

// IsRunning reports whether a job is currently executing in this pool.
func (e *Executor) IsRunning(jobName string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.cancels[jobName]
	return ok
}

// RunningCount returns the number of jobs currently executing in this pool.
func (e *Executor) RunningCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.cancels)
}

// Drain blocks until all currently-running jobs have finished or ctx is
// cancelled.  It polls every 100 ms.  Use Drain during graceful shutdown to
// wait for in-flight jobs before the process exits.
func (e *Executor) Drain(ctx context.Context) {
	for {
		if e.RunningCount() == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Cancel sends a cancellation signal to a currently running job.
// Returns true if the job was running and was cancelled, false if not found.
func (e *Executor) Cancel(jobName string) bool {
	e.mu.Lock()
	cancel, ok := e.cancels[jobName]
	e.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// Submit enqueues a job run. done is called with the Result once the job
// finishes (including any healthcheck). Submit returns immediately; the work
// happens in a goroutine.
func (e *Executor) Submit(ctx context.Context, job *config.Job, runID int64, opts RunOpts, done func(Result)) {
	go func() {
		// Acquire a pool slot.
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			done(Result{ExitCode: -1, Err: ctx.Err()})
			return
		}
		defer func() { <-e.sem }()

		// Create a per-job cancellable context so Cancel(jobName) works.
		jobCtx, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		e.cancels[job.Name] = cancel
		e.mu.Unlock()
		defer func() {
			cancel()
			e.mu.Lock()
			delete(e.cancels, job.Name)
			e.mu.Unlock()
		}()

		done(e.run(jobCtx, job, runID, opts))
	}()
}

// ─── main run ────────────────────────────────────────────────────────────────

// run executes the job subprocess and returns a Result.
func (e *Executor) run(ctx context.Context, job *config.Job, runID int64, opts RunOpts) Result {
	start := time.Now()

	timeout := DefaultTimeout
	if job.Timeout != "" {
		if d, err := time.ParseDuration(job.Timeout); err == nil && d > 0 {
			timeout = d
		}
	}

	// Collect stdout lines for output capture; stderr just goes to run_logs.
	var stdoutLines []string
	var stdoutMu sync.Mutex

	argv := e.shellArgs(job.Command)
	cmd := exec.Command(argv[0], argv[1:]...)
	if job.WorkingDir != "" {
		cmd.Dir = job.WorkingDir
	}
	cmd.Env = buildEnv(e.GlobalEnv, job.Env)
	setProcessGroup(cmd)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{ExitCode: -1, Err: err, Elapsed: time.Since(start)}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{ExitCode: -1, Err: err, Elapsed: time.Since(start)}
	}

	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Err: err, Elapsed: time.Since(start)}
	}

	var seq atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e.captureStreamCollect(runID, "stdout", stdoutPipe, &seq, &stdoutLines, &stdoutMu)
	}()
	go func() {
		defer wg.Done()
		e.captureStream(runID, "stderr", stderrPipe, &seq)
	}()

	// Wait for both stream readers to finish, then wait for the process.
	waitDone := make(chan error, 1)
	go func() {
		wg.Wait()
		waitDone <- cmd.Wait()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var runErr error
	select {
	case runErr = <-waitDone:
		// Normal completion.

	case <-timer.C:
		if e.logger != nil {
			e.logger.Warn("job timed out: sending SIGTERM",
				"run_id", runID, "timeout", timeout)
		}
		sigterm(cmd)
		select {
		case <-waitDone:
		case <-time.After(GracePeriod):
			sigkill(cmd)
			<-waitDone
		}
		runErr = ErrTimeout

	case <-ctx.Done():
		if e.logger != nil {
			e.logger.Info("job cancelled: sending SIGTERM", "run_id", runID)
		}
		sigterm(cmd)
		select {
		case <-waitDone:
		case <-time.After(GracePeriod):
			sigkill(cmd)
			<-waitDone
		}
		runErr = ctx.Err()
	}

	elapsed := time.Since(start)
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			runErr = nil // non-zero exit is reported via ExitCode, not Err
		} else {
			exitCode = -1 // timeout, cancellation, or spawn failure
		}
	}

	res := Result{ExitCode: exitCode, Err: runErr, Elapsed: elapsed}

	// Always collect stdout lines for output capture.
	stdoutMu.Lock()
	lines := make([]string, len(stdoutLines))
	copy(lines, stdoutLines)
	stdoutMu.Unlock()

	// Capture output variables for all exit codes (exit_code mode works on failure too).
	if len(job.Output) > 0 && e.st != nil {
		e.captureOutputs(ctx, job, runID, opts.CycleID, lines, exitCode)
	}

	// Only proceed to healthcheck on clean exit.
	if exitCode != 0 || runErr != nil {
		return res
	}

	// Run healthcheck (only when main command succeeded).
	if job.Healthcheck != nil && job.Healthcheck.Command != "" {
		hc := e.runHealthcheck(ctx, job, runID, &seq)
		// Apply on_fail policy before storing the status.
		if hc == store.HCFail {
			onFail := strings.ToLower(strings.TrimSpace(job.Healthcheck.OnFail))
			if onFail == "warn_only" {
				hc = store.HCWarn
			} else {
				res.ExitCode = -2
				res.Err = fmt.Errorf("healthcheck failed")
			}
		}
		res.HCStatus = &hc
	}

	return res
}

// ─── healthcheck ─────────────────────────────────────────────────────────────

func (e *Executor) runHealthcheck(ctx context.Context, job *config.Job, runID int64, seq *atomic.Int64) store.HCStatus {
	hcTimeout := DefaultHealthcheckTimeout
	if job.Healthcheck.Timeout != "" {
		if d, err := time.ParseDuration(job.Healthcheck.Timeout); err == nil && d > 0 {
			hcTimeout = d
		}
	}

	argv := e.shellArgs(job.Healthcheck.Command)
	cmd := exec.Command(argv[0], argv[1:]...)
	if job.WorkingDir != "" {
		cmd.Dir = job.WorkingDir
	}
	cmd.Env = buildEnv(e.GlobalEnv, job.Env)
	setProcessGroup(cmd)

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		if e.logger != nil {
			e.logger.Warn("healthcheck failed to start", "run_id", runID, "error", err)
		}
		return store.HCFail
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if stdoutPipe != nil {
			e.captureStream(runID, "healthcheck", stdoutPipe, seq)
		}
	}()
	go func() {
		defer wg.Done()
		if stderrPipe != nil {
			e.captureStream(runID, "healthcheck", stderrPipe, seq)
		}
	}()

	waitDone := make(chan error, 1)
	go func() {
		wg.Wait()
		waitDone <- cmd.Wait()
	}()

	timer := time.NewTimer(hcTimeout)
	defer timer.Stop()

	var hcErr error
	select {
	case hcErr = <-waitDone:
	case <-timer.C:
		if e.logger != nil {
			e.logger.Warn("healthcheck timed out", "run_id", runID)
		}
		sigterm(cmd)
		select {
		case <-waitDone:
		case <-time.After(GracePeriod):
			sigkill(cmd)
			<-waitDone
		}
		return store.HCFail
	case <-ctx.Done():
		sigterm(cmd)
		select {
		case <-waitDone:
		case <-time.After(GracePeriod):
			sigkill(cmd)
			<-waitDone
		}
		return store.HCFail
	}

	if hcErr != nil {
		return store.HCFail
	}
	return store.HCPass
}

// ─── output capture ──────────────────────────────────────────────────────────

// captureOutputs evaluates each entry in job.Output and writes the result to
// run_outputs. Errors are logged but never fail the run.
func (e *Executor) captureOutputs(
	ctx context.Context,
	job *config.Job,
	runID int64,
	cycleID string,
	stdoutLines []string,
	exitCode int,
) {
	for varName, mode := range job.Output {
		value, err := captureValue(mode, stdoutLines, exitCode)
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("output capture failed",
					"job", job.Name, "var", varName, "mode", mode, "error", err)
			}
			continue
		}
		_ = e.st.RecordOutput(ctx, store.RunOutput{
			RunID:   runID,
			JobName: job.Name,
			VarName: varName,
			Value:   value,
			CycleID: cycleID,
		})
	}
}

// captureValue applies one capture mode to the collected stdout lines.
func captureValue(mode string, lines []string, exitCode int) (string, error) {
	switch {
	case mode == "last_line":
		if len(lines) == 0 {
			return "", nil
		}
		return lines[len(lines)-1], nil

	case mode == "first_line":
		if len(lines) == 0 {
			return "", nil
		}
		return lines[0], nil

	case mode == "exit_code":
		return fmt.Sprintf("%d", exitCode), nil

	case strings.HasPrefix(mode, "json_field:"):
		key := strings.TrimPrefix(mode, "json_field:")
		for i := len(lines) - 1; i >= 0; i-- {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(lines[i]), &obj); err == nil {
				if v, ok := obj[key]; ok {
					return fmt.Sprintf("%v", v), nil
				}
			}
		}
		return "", fmt.Errorf("json_field %q not found in stdout", key)

	case strings.HasPrefix(mode, "regex:"):
		pattern := strings.TrimPrefix(mode, "regex:")
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if m := re.FindStringSubmatch(lines[i]); len(m) > 0 {
				if len(m) > 1 {
					return m[1], nil
				}
				return m[0], nil
			}
		}
		return "", fmt.Errorf("regex %q matched nothing in stdout", pattern)

	default:
		return "", fmt.Errorf("unknown capture mode %q", mode)
	}
}

// ─── stream helpers ───────────────────────────────────────────────────────────

// captureStream reads lines from r, stores them in run_logs, and logs at DEBUG.
func (e *Executor) captureStream(runID int64, stream string, r io.Reader, seq *atomic.Int64) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		s := int(seq.Add(1))
		if e.st != nil {
			_ = e.st.RecordLog(context.Background(), store.LogLine{
				RunID:  runID,
				Seq:    s,
				Stream: stream,
				Line:   line,
				TS:     time.Now().UTC(),
			})
		}
		if e.logger != nil {
			e.logger.Debug("job output", "run_id", runID, "stream", stream, "line", line)
		}
	}
}

// captureStreamCollect is like captureStream but also appends lines to *buf
// (protected by mu). Used for stdout so output capture modes can read it.
func (e *Executor) captureStreamCollect(
	runID int64,
	stream string,
	r io.Reader,
	seq *atomic.Int64,
	buf *[]string,
	mu *sync.Mutex,
) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		s := int(seq.Add(1))
		if e.st != nil {
			_ = e.st.RecordLog(context.Background(), store.LogLine{
				RunID:  runID,
				Seq:    s,
				Stream: stream,
				Line:   line,
				TS:     time.Now().UTC(),
			})
		}
		if e.logger != nil {
			e.logger.Debug("job output", "run_id", runID, "stream", stream, "line", line)
		}
		mu.Lock()
		*buf = append(*buf, line)
		mu.Unlock()
	}
}

// ─── env builder ─────────────────────────────────────────────────────────────

// shellArgs returns the argv slice for a shell command, using the configured
// shell when set, otherwise the platform-default shell.
func (e *Executor) shellArgs(command string) []string {
	sh := e.Shell
	if sh == "" {
		return shellCommand(command)
	}
	return []string{sh, "-c", command}
}

// buildEnv merges os.Environ with globalEnv overrides and then per-job
// overrides.  Priority (highest last): host env < globalEnv < per-job env.
func buildEnv(globalEnv, overrides map[string]string) []string {
	base := make(map[string]string, 64)
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		base[k] = v
	}
	for k, v := range globalEnv {
		if v != "" {
			base[k] = v
		}
	}
	for k, v := range overrides {
		// Don't clobber the inherited environment with an empty string.
		// This typically happens when a "${env:VAR}" token could not be
		// resolved because the variable was not set in the daemon's
		// environment at config-load time.  Skipping preserves any value
		// the variable might have in the parent environment and avoids
		// defeating shell-level defaults such as ${VAR:-fallback}.
		if v == "" {
			continue
		}
		base[k] = v
	}
	env := make([]string, 0, len(base))
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	return env
}
