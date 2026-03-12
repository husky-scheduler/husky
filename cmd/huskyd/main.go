// Package daemoncmd runs the Husky scheduler daemon inside the single `husky`
// binary.
//
// It reads husky.yaml, resolves the job dependency graph, and executes jobs
// according to their schedules. All state is persisted to a local SQLite
// database. The daemon exposes a REST + WebSocket API on a local TCP port
// (auto-assigned by the OS; recorded in <data>/api.addr) and communicates
// with the husky CLI via a Unix socket.
//
// Usage:
//
//	husky daemon run [flags]
//
// Flags:
//
//	--config   path to husky.yaml (default: ./husky.yaml)
//	--data     directory for SQLite database and daemon.pid (default: .husky)
//	--daemon-config path to huskyd.yaml (default: <config dir>/huskyd.yaml)
package daemoncmd

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/husky-scheduler/husky/internal/api"
	authpkg "github.com/husky-scheduler/husky/internal/auth"
	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/daemoncfg"
	"github.com/husky-scheduler/husky/internal/dag"
	"github.com/husky-scheduler/husky/internal/executor"
	"github.com/husky-scheduler/husky/internal/ipc"
	"github.com/husky-scheduler/husky/internal/logging"
	"github.com/husky-scheduler/husky/internal/notify"
	"github.com/husky-scheduler/husky/internal/outputs"
	retrypkg "github.com/husky-scheduler/husky/internal/retry"
	"github.com/husky-scheduler/husky/internal/scheduler"
	"github.com/husky-scheduler/husky/internal/store"
	"github.com/husky-scheduler/husky/internal/version"
)

// Options defines the filesystem and config inputs needed to launch the
// embedded daemon runtime.
type Options struct {
	ConfigPath       string
	DataDir          string
	DaemonConfigPath string
}

// ─── daemon ───────────────────────────────────────────────────────────────────

type daemon struct {
	cfgPath string
	dataDir string
	logger  *slog.Logger

	mu    sync.RWMutex
	cfg   *config.Config
	graph *dag.Graph

	st    *store.Store
	exec  *executor.Executor
	sched *scheduler.Scheduler
	notif *notify.Dispatcher

	pausedJobs map[string]bool

	// concSem is a non-nil channel when scheduler.max_concurrent_jobs is
	// configured; its capacity equals the global job concurrency limit.
	concSem chan struct{}

	// auditLog is non-nil when log.audit_log.enabled is true.
	auditLog *logging.AuditLogger

	// levelVar allows hot-reloading the log level on SIGHUP.
	levelVar *slog.LevelVar

	// catchupWindow is the maximum look-back when reconciling missed schedules.
	// A zero value means no limit (trigger everything that's overdue).
	catchupWindow time.Duration

	stop context.CancelFunc
}

// ─── run ──────────────────────────────────────────────────────────────────────

// Run starts the Husky background daemon using the provided options.
func Run(opts Options) error {
	cfgPath := opts.ConfigPath
	if strings.TrimSpace(cfgPath) == "" {
		cfgPath = "husky.yaml"
	}
	dataDir := opts.DataDir
	if strings.TrimSpace(dataDir) == "" {
		dataDir = ".husky"
	}
	daemonCfgPath := opts.DaemonConfigPath

	// ── Load husky.yaml (job definitions) ────────────────────────────────────
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", cfgPath, err)
	}

	// ── Load huskyd.yaml (daemon runtime config) ─────────────────────────────
	huskyDir := filepath.Dir(cfgPath)
	dcfg, err := daemoncfg.Load(daemonCfgPath, huskyDir)
	if err != nil {
		return fmt.Errorf("load daemon config: %w", err)
	}
	// Determine the resolved path for the daemon config file (may be empty).
	resolvedDaemonCfgPath := daemonCfgPath
	if resolvedDaemonCfgPath == "" {
		candidate := filepath.Join(huskyDir, "huskyd.yaml")
		if _, statErr := os.Stat(candidate); statErr == nil {
			resolvedDaemonCfgPath = candidate
		}
	}

	// ── Set up structured logging ─────────────────────────────────────────────
	logger, levelVar, auditLog, err := logging.Setup(dcfg.Log)
	if err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}
	defer func() { _ = auditLog.Close() }()

	if resolvedDaemonCfgPath != "" {
		logger.Info("daemon config loaded", "path", resolvedDaemonCfgPath)
	} else {
		logger.Info("no huskyd.yaml found, using defaults")
	}

	// ── Storage engine check (postgres stub) ─────────────────────────────────
	if strings.EqualFold(dcfg.Storage.Engine, "postgres") {
		return errors.New("storage.engine=postgres is not yet supported in this release; only 'sqlite' is available")
	}

	// ── Build and validate DAG ───────────────────────────────────────────────
	graph, err := dag.Build(cfg)
	if err != nil {
		return fmt.Errorf("dag validation failed: %w", err)
	}

	// ── Data directory ───────────────────────────────────────────────────────
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory %s: %w", dataDir, err)
	}

	// ── PID lock ─────────────────────────────────────────────────────────────
	pidPath := filepath.Join(dataDir, "husky.pid")
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if pidAlive(pid) {
				return fmt.Errorf("daemon already running (pid %d)", pid)
			}
			logger.Warn("stale PID file found, overwriting", "stale_pid", pid)
		}
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		logger.Warn("failed to write PID file", "path", pidPath, "error", err)
	}
	defer func() { _ = os.Remove(pidPath) }()

	// ── Open store ───────────────────────────────────────────────────────────
	dbPath := dcfg.Storage.SQLite.Path
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "husky.db")
	}
	busyTimeoutMS := 5000
	if bt, err := time.ParseDuration(dcfg.Storage.SQLite.BusyTimeout); err == nil && bt > 0 {
		busyTimeoutMS = int(bt.Milliseconds())
	}
	st, err := store.OpenWithConfig(dbPath, store.StoreConfig{
		WALAutocheckpoint: dcfg.Storage.SQLite.WALAutocheckpoint,
		BusyTimeoutMS:     busyTimeoutMS,
	})
	if err != nil {
		return fmt.Errorf("open store %s: %w", dbPath, err)
	}
	defer func() { _ = st.Close() }()

	// ── Auth middleware ───────────────────────────────────────────────────────
	authn, err := authpkg.New(dcfg.Auth, logger)
	if err != nil {
		return fmt.Errorf("auth configuration error: %w", err)
	}

	// ── Context (shutdown signal) ────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Build daemon ─────────────────────────────────────────────────────────
	poolSize := dcfg.Executor.PoolSize
	if poolSize <= 0 {
		poolSize = executor.DefaultPoolSize
	}
	exec := executor.New(poolSize, st, logger)
	if sh := strings.TrimSpace(dcfg.Executor.Shell); sh != "" {
		exec.Shell = sh
	}
	if len(dcfg.Executor.GlobalEnv) > 0 {
		exec.GlobalEnv = dcfg.Executor.GlobalEnv
	}

	// Global concurrent-job semaphore (scheduler.max_concurrent_jobs).
	var concSem chan struct{}
	if n := dcfg.Scheduler.MaxConcurrentJobs; n > 0 {
		concSem = make(chan struct{}, n)
	}

	var catchupWindow time.Duration
	if s := strings.TrimSpace(dcfg.Scheduler.CatchupWindow); s != "" {
		catchupWindow, _ = time.ParseDuration(s)
	}

	d := &daemon{
		cfgPath:       cfgPath,
		dataDir:       dataDir,
		logger:        logger,
		cfg:           cfg,
		graph:         graph,
		st:            st,
		exec:          exec,
		notif:         notify.New(st, logger),
		pausedJobs:    make(map[string]bool),
		concSem:       concSem,
		auditLog:      auditLog,
		levelVar:      levelVar,
		catchupWindow: catchupWindow,
		stop:          stop,
	}

	// ── Scheduler (onRun calls d.dispatch) ───────────────────────────────────
	onRun := func(runCtx context.Context, jobName string, _ time.Time) {
		d.dispatch(runCtx, jobName, store.TriggerSchedule, "", newCycleID(), 1)
	}
	sched := scheduler.New(cfg, logger, onRun)
	if jitter, err := time.ParseDuration(dcfg.Scheduler.ScheduleJitter); err == nil && jitter > 0 {
		sched.Jitter = jitter
	}
	d.sched = sched

	// ── Crash recovery: reconcile orphaned RUNNING runs ──────────────────────
	d.reconcileOrphans(ctx)

	// ── Catchup: trigger missed schedules ────────────────────────────────────
	d.reconcileCatchup(ctx)

	sched.LogSchedule()

	// ── Storage retention vacuum goroutine ───────────────────────────────────
	go func() {
		// Initial delay to avoid contending with catchup on startup.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
		}
		vacuumTick(ctx, st, dcfg, logger)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				vacuumTick(ctx, st, dcfg, logger)
			}
		}
	}()

	// ── SIGHUP hot-reload ────────────────────────────────────────────────────
	watchSIGHUP(ctx.Done(), func() {
		if err := d.reload(); err != nil {
			logger.Error("hot-reload failed, keeping old config", "error", err)
		} else {
			logger.Info("config hot-reloaded")
		}
		// Hot-reload bearer tokens from the token file.
		if err := authn.ReloadTokens(); err != nil {
			logger.Error("failed to reload bearer tokens", "error", err)
		}
		// Re-load huskyd.yaml for hot-reloadable settings (log level).
		if newDcfg, err := daemoncfg.Load(resolvedDaemonCfgPath, huskyDir); err == nil {
			logging.SetLevel(d.levelVar, newDcfg.Log.Level)
			logger.Info("log level updated", "level", newDcfg.Log.Level)
		}
	})

	// ── IPC server ───────────────────────────────────────────────────────────
	sockPath := filepath.Join(dataDir, "husky.sock")
	srv := ipc.NewServer(sockPath, ipc.Callbacks{
		OnStatus:  d.statusFunc,
		OnTrigger: d.triggerFunc,
		OnStop:    ipc.StopFunc(stop),
		OnDag:     d.dagFunc,
		OnCancel:  d.cancelFunc,
		OnSkip:    d.skipFunc,
		OnReload: func() error {
			return d.reload()
		},
	}, logger)
	go func() {
		if err := srv.ListenAndServe(ctx); err != nil {
			logger.Error("ipc server error", "error", err)
		}
	}()

	// ── REST + WebSocket API server ──────────────────────────────────────────
	// Determine bind address: use api.addr from huskyd.yaml when set, otherwise
	// let the OS pick a free port (allows multiple daemons to coexist).
	apiBindAddr := dcfg.API.Addr
	if apiBindAddr == "" {
		apiBindAddr = "127.0.0.1:0"
	}

	// Warn when binding to 0.0.0.0 without both TLS and auth.
	if strings.HasPrefix(apiBindAddr, "0.0.0.0") {
		authEnabled := strings.ToLower(dcfg.Auth.Type) != "none" && dcfg.Auth.Type != ""
		if !dcfg.API.TLS.Enabled || !authEnabled {
			logger.Warn("api server binding to 0.0.0.0 without both TLS and auth enabled — ensure firewall rules are in place")
		}
	}

	// Create the TCP listener.
	tcpLn, err := net.Listen("tcp", apiBindAddr)
	if err != nil {
		return fmt.Errorf("bind api listener %s: %w", apiBindAddr, err)
	}

	// Optionally wrap in TLS.
	var apiLn net.Listener = tcpLn
	if dcfg.API.TLS.Enabled {
		tlsCert, err := tls.LoadX509KeyPair(dcfg.API.TLS.Cert, dcfg.API.TLS.Key)
		if err != nil {
			return fmt.Errorf("load TLS certificate %s: %w", dcfg.API.TLS.Cert, err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tlsMinVersion(dcfg.API.TLS.MinVersion),
		}
		if dcfg.API.TLS.ClientCA != "" {
			caPool, err := loadCACertPool(dcfg.API.TLS.ClientCA)
			if err != nil {
				return fmt.Errorf("load client CA %s: %w", dcfg.API.TLS.ClientCA, err)
			}
			tlsCfg.ClientCAs = caPool
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
		apiLn = tls.NewListener(tcpLn, tlsCfg)
		logger.Info("TLS enabled for API server", "cert", dcfg.API.TLS.Cert)
	}

	boundAddr := apiLn.Addr().String()

	// Write the bound address to <data>/api.addr so the CLI can discover it.
	addrFile := filepath.Join(dataDir, "api.addr")
	if err := os.WriteFile(addrFile, []byte(boundAddr), 0o644); err != nil {
		logger.Warn("failed to write api.addr", "path", addrFile, "error", err)
	}
	defer func() { _ = os.Remove(addrFile) }()

	// Build API ServerConfig from daemoncfg.
	rh, rd, rw, ri := dcfg.API.Timeouts.ParsedTimeouts()
	apiServerCfg := api.ServerConfig{
		BasePath:          dcfg.API.BasePath,
		CORSOrigins:       dcfg.API.CORS.AllowedOrigins,
		CORSCredentials:   dcfg.API.CORS.AllowCredentials,
		ReadHeaderTimeout: rh,
		ReadTimeout:       rd,
		WriteTimeout:      rw,
		IdleTimeout:       ri,
		// Compose auth + RBAC middleware into a single chain: auth sets the role
		// in the context, RBAC then enforces method+path rules.
		AuthMiddleware: func(next http.Handler) http.Handler {
			return authn.Middleware()(authn.RBACMiddleware()(next))
		},
	}

	if len(dcfg.API.CORS.AllowedOrigins) > 0 {
		for _, o := range dcfg.API.CORS.AllowedOrigins {
			if o == "*" {
				logger.Warn("CORS wildcard origin '*' configured — all origins permitted")
				break
			}
		}
	}

	apiServer := api.New(boundAddr, api.Dependencies{
		Store:  st,
		Logger: logger,
		ConfigSnapshot: func() *config.Config {
			d.mu.RLock()
			defer d.mu.RUnlock()
			return d.cfg
		},
		Status:    d.statusFunc,
		Trigger:   d.triggerFunc,
		Cancel:    d.cancelFunc,
		PauseTag:  d.pauseTagFunc,
		ResumeTag: d.resumeTagFunc,
		PauseJob:  d.pauseJobFunc,
		ResumeJob: d.resumeJobFunc,
		SkipJob:   d.skipFunc,
		TestIntegration: func(ctx context.Context, name string) error {
			d.mu.RLock()
			cfg := d.cfg
			d.mu.RUnlock()
			return d.notif.TestIntegration(ctx, cfg, name)
		},
		DAGJSON: func() ([]byte, error) {
			return d.dagFunc(true)
		},
		StopDaemon:       stop,
		ReloadDaemon:     d.reload,
		ConfigPath:       cfgPath,
		DaemonConfigPath: resolvedDaemonCfgPath,
		DBPath:           dbPath,
		Version:          version.Version,
		PID:              os.Getpid(),
		StartedAt:        time.Now(),
	}, apiServerCfg)
	go func() {
		if err := apiServer.Serve(ctx, apiLn); err != nil {
			logger.Error("api server error", "error", err)
		}
	}()

	logger.Info("daemon started",
		"version", version.Version,
		"config", cfgPath,
		"data", dataDir,
		"socket", sockPath,
		"api", boundAddr,
	)
	sched.Start(ctx)

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	// After the scheduler stops (SIGTERM received), wait for in-flight jobs to
	// complete up to scheduler.shutdown_timeout before the process exits.
	shutdownDur := 60 * time.Second
	if sd, err := time.ParseDuration(dcfg.Scheduler.ShutdownTimeout); err == nil && sd > 0 {
		shutdownDur = sd
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), shutdownDur)
	defer drainCancel()
	d.exec.Drain(drainCtx)
	if n := d.exec.RunningCount(); n > 0 {
		logger.Warn("shutdown: some jobs did not complete within shutdown_timeout",
			"still_running", n, "timeout", shutdownDur)
	}
	logger.Info("daemon stopped")
	return nil
}

// ─── dispatch ─────────────────────────────────────────────────────────────────

// dispatch is the central job dispatch function. It checks concurrency and
// DAG dependencies, renders output templates, records the run, submits to the
// executor, and handles retries and on_failure actions when the run completes.
func (d *daemon) dispatch(
	ctx context.Context,
	jobName string,
	trigger store.Trigger,
	reason string,
	cycleID string,
	attempt int,
) {
	d.mu.RLock()
	cfg := d.cfg
	graph := d.graph
	d.mu.RUnlock()

	job, ok := cfg.Jobs[jobName]
	if !ok {
		d.logger.Warn("dispatch: unknown job", "job", jobName)
		return
	}

	d.mu.RLock()
	paused := d.pausedJobs[jobName]
	d.mu.RUnlock()
	if paused {
		d.logger.Info("dispatch skipped: job is paused", "job", jobName)
		return
	}

	// ── 1. Concurrency check ─────────────────────────────────────────────────
	concurrency := strings.ToLower(strings.TrimSpace(job.Concurrency))
	if concurrency == "" {
		concurrency = "allow"
	}
	if d.exec.IsRunning(jobName) {
		switch concurrency {
		case "forbid":
			d.logger.Info("concurrency=forbid: skipping overlapping run", "job", jobName)
			return
		case "replace":
			d.logger.Info("concurrency=replace: cancelling current run", "job", jobName)
			d.exec.Cancel(jobName)
		}
	}

	// ── 2. DAG dependency check ──────────────────────────────────────────────
	for _, dep := range graph.DepsOf(jobName) {
		state, err := d.st.GetJobState(ctx, dep)
		if err != nil || state == nil || state.LastSuccess == nil {
			d.logger.Info("dag: dependency not yet successful, skipping",
				"job", jobName, "dep", dep)
			return
		}
	}

	// ── 3. Render output templates ───────────────────────────────────────────
	resolvedJob, err := outputs.RenderTemplates(ctx, d.st, job, cycleID)
	if err != nil {
		d.logger.Error("output template error, skipping run",
			"job", jobName, "error", err)
		return
	}

	// ── 4. Record run start ──────────────────────────────────────────────────
	now := time.Now()
	runID, err := d.st.RecordRunStart(ctx, store.Run{
		JobName:     jobName,
		Status:      store.StatusRunning,
		Attempt:     attempt,
		Trigger:     trigger,
		TriggeredBy: triggerSource(trigger, reason),
		Reason:      reason,
		StartedAt:   &now,
	})
	if err != nil {
		d.logger.Error("failed to record run start", "job", jobName, "error", err)
		return
	}

	var slaTimer *time.Timer
	var slaBreached atomic.Bool
	if strings.TrimSpace(job.SLA) != "" {
		if slaDur, err := time.ParseDuration(job.SLA); err == nil && slaDur > 0 {
			slaTimer = time.AfterFunc(slaDur, func() {
				if !d.exec.IsRunning(jobName) {
					return
				}
				slaBreached.Store(true)
				_ = d.st.MarkRunSLABreached(context.Background(), runID)
				run, _ := d.st.GetRun(context.Background(), runID)
				if run == nil {
					run = &store.Run{
						ID:          runID,
						JobName:     jobName,
						Status:      store.StatusRunning,
						Attempt:     attempt,
						Trigger:     trigger,
						TriggeredBy: triggerSource(trigger, reason),
						Reason:      reason,
					}
				}
				run.SLABreached = true
				_ = d.notif.Dispatch(context.Background(), cfg, jobName, job, run, notify.EventSLABreach)
				d.logger.Warn("sla breached", "job", jobName, "run_id", runID, "sla", slaDur)
			})
		}
	}

	// ── 5. Global concurrency gate (scheduler.max_concurrent_jobs) ──────────
	if d.concSem != nil {
		select {
		case d.concSem <- struct{}{}:
			// Slot acquired; release when done.
		case <-ctx.Done():
			_ = d.st.MarkRunStatus(context.Background(), runID, store.StatusSkipped)
			return
		}
	}

	// ── 6. Submit to executor ────────────────────────────────────────────
	d.exec.Submit(ctx, resolvedJob, runID, executor.RunOpts{CycleID: cycleID}, func(res executor.Result) {
		if d.concSem != nil {
			<-d.concSem // release global slot
		}
		bgCtx := context.Background()
		if slaTimer != nil {
			slaTimer.Stop()
		}

		cancelled := errors.Is(res.Err, context.Canceled)
		failed := res.ExitCode != 0 || res.Err != nil
		status := store.StatusSuccess
		statusReason := ""
		if cancelled {
			status = store.StatusCancelled
			statusReason = "cancelled"
		} else if failed {
			status = store.StatusFailed
		}

		finished := time.Now()
		_ = d.st.RecordRunEnd(bgCtx, runID, status, statusReason, &res.ExitCode, finished, slaBreached.Load(), res.HCStatus)
		// Write to audit log (start time captured from the run-start phase above
		// via closure; the res.Elapsed field carries the measured duration).
		d.auditLog.Log(jobName, runID, string(status), string(trigger), res.Elapsed.Milliseconds(), reason)
		runRow, _ := d.st.GetRun(bgCtx, runID)
		if runRow == nil {
			runRow = &store.Run{ID: runID, JobName: jobName, Status: status, StatusReason: statusReason, Trigger: trigger, Reason: reason}
		}
		runRow.Status = status
		runRow.StatusReason = statusReason

		switch {
		case !failed:
			_ = d.st.UpdateJobState(bgCtx, store.JobState{
				JobName:     jobName,
				LastSuccess: &finished,
			})
			d.logger.Info("job succeeded",
				"job", jobName, "attempt", attempt,
				"elapsed", res.Elapsed.Round(time.Millisecond))
			if err := d.notif.Dispatch(bgCtx, cfg, jobName, job, runRow, notify.EventSuccess); err != nil {
				d.logger.Warn("success notification failed", "job", jobName, "run_id", runID, "error", err)
			}
			// Trigger any after:<jobName> dependents.
			d.triggerAfterSuccessors(ctx, jobName, cycleID)
		case cancelled:
			d.logger.Info("job cancelled",
				"job", jobName, "attempt", attempt,
				"elapsed", res.Elapsed.Round(time.Millisecond))
		default:
			_ = d.st.UpdateJobState(bgCtx, store.JobState{
				JobName:     jobName,
				LastFailure: &finished,
			})
			d.logger.Warn("job failed",
				"job", jobName, "attempt", attempt,
				"exit_code", res.ExitCode,
				"elapsed", res.Elapsed.Round(time.Millisecond))

			// Retry logic.
			maxRetries := 0
			if job.Retries != nil {
				maxRetries = *job.Retries
			}
			if attempt <= maxRetries && !cancelled {
				delay := retrypkg.Delay(job.RetryDelay, attempt+1)
				d.logger.Info("retry scheduled",
					"job", jobName, "attempt", attempt+1, "delay", delay)
				_ = d.st.MarkRunStatus(bgCtx, runID, store.StatusRetrying)
				retryRun := *runRow
				retryRun.Status = store.StatusRetrying
				retryRun.Attempt = attempt + 1
				if err := d.notif.Dispatch(bgCtx, cfg, jobName, job, &retryRun, notify.EventRetry); err != nil {
					d.logger.Warn("retry notification failed", "job", jobName, "run_id", runID, "error", err)
				}
				time.AfterFunc(delay, func() {
					d.dispatch(ctx, jobName, trigger, reason, cycleID, attempt+1)
				})
			} else {
				finalStatus := d.handleOnFailure(ctx, jobName, job, runRow)
				if finalStatus != "" && runRow != nil && runRow.Status != finalStatus {
					if err := d.st.MarkRunStatus(bgCtx, runID, finalStatus); err != nil {
						d.logger.Warn("failed to update terminal run status", "job", jobName, "run_id", runID, "status", finalStatus, "error", err)
					} else {
						runRow.Status = finalStatus
					}
				}
			}
		}
	})
}

// ─── after-successor triggering ───────────────────────────────────────────────

func (d *daemon) triggerAfterSuccessors(ctx context.Context, finishedJob, cycleID string) {
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()

	for name, job := range cfg.Jobs {
		freq := strings.ToLower(strings.TrimSpace(job.Frequency))
		if freq == "after:"+finishedJob {
			d.logger.Info("triggering after-dependent", "job", name, "parent", finishedJob)
			go d.dispatch(ctx, name, store.TriggerDependency, "after:"+finishedJob, cycleID, 1)
		}
	}
}

// ─── on_failure handler ───────────────────────────────────────────────────────

func (d *daemon) handleOnFailure(ctx context.Context, jobName string, job *config.Job, run *store.Run) store.RunStatus {
	action := strings.ToLower(strings.TrimSpace(job.OnFailure))
	switch action {
	case "stop":
		d.logger.Error("on_failure=stop: halting pipeline continuation", "job", jobName)
		return store.StatusFailed
	case "skip":
		d.logger.Info("on_failure=skip: job failure skipped", "job", jobName)
		return store.StatusSkipped
	case "ignore":
		// log already happened at the failure site
		return store.StatusFailed
	default: // "alert" or empty
		if err := d.notif.Dispatch(ctx, d.cfg, jobName, job, run, notify.EventFailure); err != nil {
			d.logger.Warn("failure notification failed", "job", jobName, "run_id", run.ID, "error", err)
		}
		return store.StatusFailed
	}
}

// ─── crash recovery ───────────────────────────────────────────────────────────

// reconcileOrphans marks any RUNNING runs from a previous daemon invocation as
// FAILED and schedules retries according to each job's retry policy.
func (d *daemon) reconcileOrphans(ctx context.Context) {
	runs, err := d.st.ListRunsByStatus(ctx, store.StatusRunning)
	if err != nil {
		d.logger.Warn("orphan reconciliation: could not query running runs", "error", err)
		return
	}
	if len(runs) == 0 {
		return
	}
	d.logger.Info("orphan reconciliation", "orphaned_runs", len(runs))
	for _, run := range runs {
		if err := d.st.MarkRunStatus(ctx, run.ID, store.StatusFailed); err != nil {
			d.logger.Warn("failed to mark orphaned run as failed", "run_id", run.ID, "error", err)
			continue
		}
		// Schedule a retry if configured.
		job, ok := d.cfg.Jobs[run.JobName]
		if !ok {
			continue
		}
		maxRetries := 0
		if job.Retries != nil {
			maxRetries = *job.Retries
		}
		if run.Attempt <= maxRetries {
			delay := retrypkg.Delay(job.RetryDelay, run.Attempt+1)
			capturedRun := run
			time.AfterFunc(delay, func() {
				d.dispatch(ctx, capturedRun.JobName,
					capturedRun.Trigger, capturedRun.Reason,
					newCycleID(), capturedRun.Attempt+1)
			})
		}
		d.logger.Warn("orphaned run marked failed",
			"job", run.JobName, "run_id", run.ID, "attempt", run.Attempt)
	}
}

// reconcileCatchup triggers missed schedule runs for jobs that have
// catchup: true and whose next_run timestamp has passed.
func (d *daemon) reconcileCatchup(ctx context.Context) {
	now := time.Now()
	for name, job := range d.cfg.Jobs {
		if !job.Catchup {
			continue
		}
		state, err := d.st.GetJobState(ctx, name)
		if err != nil || state == nil || state.NextRun == nil {
			continue
		}
		if now.After(*state.NextRun) {
			// Honour the configured catchup window: skip runs that are older
			// than the window so a long outage does not trigger a flood of
			// stale catch-up executions.
			if d.catchupWindow > 0 && now.Sub(*state.NextRun) > d.catchupWindow {
				d.logger.Warn("catchup: skipping run outside catchup window",
					"job", name, "was_due", *state.NextRun,
					"catchup_window", d.catchupWindow)
				continue
			}
			d.logger.Info("catchup: triggering missed run", "job", name, "was_due", *state.NextRun)
			go d.dispatch(ctx, name, store.TriggerSchedule, "catchup", newCycleID(), 1)
		}
	}
}

// ─── hot-reload ───────────────────────────────────────────────────────────────

func (d *daemon) reload() error {
	newCfg, err := config.Load(d.cfgPath)
	if err != nil {
		return fmt.Errorf("reload: config load failed: %w", err)
	}
	newGraph, err := dag.Build(newCfg)
	if err != nil {
		return fmt.Errorf("reload: dag validation failed: %w", err)
	}

	d.mu.Lock()
	d.cfg = newCfg
	d.graph = newGraph
	d.mu.Unlock()

	d.sched.Reload(newCfg)
	d.logger.Info("config reloaded", "config", d.cfgPath)
	return nil
}

// ─── IPC callbacks ────────────────────────────────────────────────────────────

func (d *daemon) triggerFunc(jobName, reason string) error {
	d.mu.RLock()
	_, ok := d.cfg.Jobs[jobName]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown job %q", jobName)
	}
	go d.dispatch(context.Background(), jobName, store.TriggerManual, reason, newCycleID(), 1)
	return nil
}

func (d *daemon) pauseTagFunc(tag string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for name, job := range d.cfg.Jobs {
		for _, t := range job.Tags {
			if t == tag {
				d.pausedJobs[name] = true
				count++
				break
			}
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no jobs found for tag %q", tag)
	}
	return count, nil
}

func (d *daemon) resumeTagFunc(tag string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for name, job := range d.cfg.Jobs {
		for _, t := range job.Tags {
			if t == tag {
				if d.pausedJobs[name] {
					delete(d.pausedJobs, name)
					count++
				}
				break
			}
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no paused jobs found for tag %q", tag)
	}
	return count, nil
}

func (d *daemon) pauseJobFunc(jobName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.cfg.Jobs[jobName]; !ok {
		return fmt.Errorf("unknown job %q", jobName)
	}
	d.pausedJobs[jobName] = true
	return nil
}

func (d *daemon) resumeJobFunc(jobName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.cfg.Jobs[jobName]; !ok {
		return fmt.Errorf("unknown job %q", jobName)
	}
	delete(d.pausedJobs, jobName)
	return nil
}

func (d *daemon) statusFunc() ([]ipc.JobStatus, error) {
	ctx := context.Background()
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()

	states, err := d.st.ListJobStates(ctx)
	if err != nil {
		return nil, err
	}
	stateMap := make(map[string]store.JobState, len(states))
	for _, s := range states {
		stateMap[s.JobName] = s
	}

	out := make([]ipc.JobStatus, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		js := ipc.JobStatus{
			Name:    name,
			Running: d.exec.IsRunning(name),
		}
		d.mu.RLock()
		js.Paused = d.pausedJobs[name]
		d.mu.RUnlock()
		if s, ok := stateMap[name]; ok {
			if s.LastSuccess != nil {
				ts := s.LastSuccess.UTC().Format(time.RFC3339)
				js.LastSuccess = &ts
			}
			if s.LastFailure != nil {
				ts := s.LastFailure.UTC().Format(time.RFC3339)
				js.LastFailure = &ts
			}
		}
		next := d.sched.NextFor(name)
		if !next.IsZero() {
			ts := next.UTC().Format(time.RFC3339)
			js.NextRun = &ts
		}
		out = append(out, js)
	}
	return out, nil
}

func (d *daemon) dagFunc(asJSON bool) ([]byte, error) {
	d.mu.RLock()
	graph := d.graph
	d.mu.RUnlock()

	if asJSON {
		return json.Marshal(graph.JSONOutput())
	}
	// ASCII is plain text; JSON-encode it so it embeds correctly in the
	// json.RawMessage Data field of the IPC Response.
	return json.Marshal(graph.ASCII())
}

func (d *daemon) cancelFunc(jobName string) error {
	if !d.exec.Cancel(jobName) {
		return fmt.Errorf("job %q is not currently running", jobName)
	}
	return nil
}

func (d *daemon) skipFunc(jobName string) error {
	d.mu.RLock()
	_, ok := d.cfg.Jobs[jobName]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown job %q", jobName)
	}
	ctx := context.Background()
	// Mark any PENDING run for this job as SKIPPED.
	runs, err := d.st.ListRunsByStatus(ctx, store.StatusPending)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.JobName == jobName {
			return d.st.MarkRunStatus(ctx, run.ID, store.StatusSkipped)
		}
	}
	// No pending run — record a SKIPPED entry so the audit trail shows it.
	now := time.Now()
	_, err = d.st.RecordRunStart(ctx, store.Run{
		JobName:     jobName,
		Status:      store.StatusSkipped,
		Attempt:     1,
		Trigger:     store.TriggerManual,
		TriggeredBy: "cli",
		StartedAt:   &now,
	})
	return err
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func triggerSource(t store.Trigger, reason string) string {
	switch t {
	case store.TriggerManual:
		if reason != "" {
			return "cli: " + reason
		}
		return "cli"
	case store.TriggerSchedule:
		return "scheduler"
	case store.TriggerDependency:
		return "dependency"
	default:
		return "scheduler"
	}
}

// newCycleID generates a random UUID-v4-like identifier for a trigger chain.
func newCycleID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// tlsMinVersion converts a version string ("1.2" or "1.3") to the
// corresponding crypto/tls constant. Defaults to TLS 1.2.
func tlsMinVersion(v string) uint16 {
	if v == "1.3" {
		return tls.VersionTLS13
	}
	return tls.VersionTLS12
}

// loadCACertPool reads a PEM CA bundle and returns an x509.CertPool.
func loadCACertPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no valid certificates found in %s", path)
	}
	return pool, nil
}

// vacuumTick executes one storage-retention vacuum pass and logs the result.
// It is a no-op when neither max_age nor max_runs_per_job is configured.
func vacuumTick(ctx context.Context, st *store.Store, dcfg daemoncfg.DaemonConfig, logger *slog.Logger) {
	var maxAge time.Duration
	if s := strings.TrimSpace(dcfg.Storage.Retention.MaxAge); s != "" {
		maxAge, _ = time.ParseDuration(s)
	}
	maxRunsPerJob := dcfg.Storage.Retention.MaxRunsPerJob
	if maxAge == 0 && maxRunsPerJob == 0 {
		return
	}
	result, err := st.Vacuum(ctx, maxAge, maxRunsPerJob)
	if err != nil {
		logger.Error("storage vacuum failed", "error", err)
		return
	}
	if result.Total() > 0 {
		logger.Info("storage vacuum complete",
			"runs_deleted", result.RunsDeleted,
			"logs_deleted", result.LogsDeleted,
			"outputs_deleted", result.OutputsDeleted)
	}
}
