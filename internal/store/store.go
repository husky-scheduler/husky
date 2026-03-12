// Package store manages persistence to the local SQLite database (WAL mode).
//
// It owns the job_runs, job_state, run_logs, alerts, and run_outputs tables.
// All writes are serialised through a single goroutine channel to avoid lock
// contention; reads use the same connection taking advantage of WAL
// concurrent-read semantics.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register the sqlite driver.
)

// timeFormat is the ISO 8601 UTC format used for all timestamp columns.
const timeFormat = "2006-01-02T15:04:05Z"

// Sentinel errors returned by write helpers.
var (
	ErrNotFound       = errors.New("store: record not found")
	ErrAlreadyPending = errors.New("store: alert is already pending retry")
)

// ─── Value types ──────────────────────────────────────────────────────────────

// RunStatus is the FSM state of a job run.
type RunStatus string

const (
	StatusPending   RunStatus = "PENDING"
	StatusRunning   RunStatus = "RUNNING"
	StatusSuccess   RunStatus = "SUCCESS"
	StatusFailed    RunStatus = "FAILED"
	StatusCancelled RunStatus = "CANCELLED"
	StatusRetrying  RunStatus = "RETRYING"
	StatusSkipped   RunStatus = "SKIPPED"
)

// Trigger describes what initiated a run.
type Trigger string

const (
	TriggerSchedule   Trigger = "schedule"
	TriggerManual     Trigger = "manual"
	TriggerDependency Trigger = "dependency"
)

// HCStatus represents the healthcheck outcome of a completed run.
type HCStatus string

const (
	HCPass HCStatus = "pass"
	HCFail HCStatus = "fail"
	HCWarn HCStatus = "warn"
)

// ─── Domain structs ───────────────────────────────────────────────────────────

// Run is one record in the job_runs table.
type Run struct {
	ID           int64      `json:"id"`
	JobName      string     `json:"job_name"`
	Status       RunStatus  `json:"status"`
	StatusReason string     `json:"status_reason,omitempty"`
	Attempt      int        `json:"attempt"`
	Trigger      Trigger    `json:"trigger"`
	TriggeredBy  string     `json:"triggered_by"`
	Reason       string     `json:"reason"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	SLABreached  bool       `json:"sla_breached"`
	HCStatus     *HCStatus  `json:"hc_status,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// JobState is one record in the job_state table.
type JobState struct {
	JobName     string
	LastSuccess *time.Time
	LastFailure *time.Time
	NextRun     *time.Time
	LockPID     *int
}

// LogLine is one record in the run_logs table.
type LogLine struct {
	RunID  int64     `json:"run_id"`
	Seq    int       `json:"seq"`
	Stream string    `json:"stream"` // stdout | stderr | healthcheck
	Line   string    `json:"line"`
	TS     time.Time `json:"ts"`
}

// RunOutput is one record in the run_outputs table.
type RunOutput struct {
	RunID   int64  `json:"run_id"`
	JobName string `json:"job_name"`
	VarName string `json:"var_name"`
	Value   string `json:"value"`
	CycleID string `json:"cycle_id"`
}

// Alert is one record in the alerts table.
type Alert struct {
	ID            int64      `json:"id"`
	JobName       string     `json:"job_name"`
	RunID         *int64     `json:"run_id,omitempty"`
	Event         string     `json:"event,omitempty"`
	Channel       string     `json:"channel"`
	Status        string     `json:"status"` // delivered | failed | pending
	Attempts      int        `json:"attempts"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	DeliveryError string     `json:"error,omitempty"`
	SentAt        time.Time  `json:"sent_at"`
	Payload       string     `json:"payload,omitempty"`
}

// ─── Store ────────────────────────────────────────────────────────────────────

type writeOp struct {
	ctx context.Context //nolint:containedctx // intentional: carry ctx into writer goroutine.
	fn  func(*sql.Tx) error
	res chan<- error
}

// Store manages the SQLite database for Husky.
type Store struct {
	db      *sql.DB
	writeCh chan writeOp
	closed  chan struct{}
}

// Open opens (or creates) the SQLite database at path, applies schema
// migrations, and starts the serialised writer goroutine.
func Open(path string) (*Store, error) {
	dsn := path + "?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
	// A single open connection serialises all SQLite I/O at the driver level,
	// which pairs with the write-channel pattern to prevent SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	s := &Store{
		db:      db,
		writeCh: make(chan writeOp, 256),
		closed:  make(chan struct{}),
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	go s.runWriter()
	return s, nil
}

// Close drains the write channel and closes the underlying database.
func (s *Store) Close() error {
	close(s.writeCh)
	<-s.closed
	return s.db.Close()
}

// runWriter is the single goroutine that executes all write transactions.
func (s *Store) runWriter() {
	defer close(s.closed)
	for op := range s.writeCh {
		select {
		case <-op.ctx.Done():
			op.res <- op.ctx.Err()
		default:
			op.res <- s.execTx(op.fn)
		}
	}
}

func (s *Store) execTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// write submits a write operation to the serialised writer goroutine and waits
// for its result, honouring ctx cancellation at both the submission and
// collection points.
func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error {
	res := make(chan error, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeCh <- writeOp{ctx: ctx, fn: fn, res: res}:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-res:
		return err
	}
}

// ─── Schema ───────────────────────────────────────────────────────────────────

const schema = `
CREATE TABLE IF NOT EXISTS job_runs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    job_name     TEXT    NOT NULL,
    status       TEXT    NOT NULL,
	status_reason TEXT,
    attempt      INTEGER NOT NULL DEFAULT 1,
    trigger      TEXT    NOT NULL,
    triggered_by TEXT    NOT NULL DEFAULT 'scheduler',
    reason       TEXT,
    started_at   TEXT,
    finished_at  TEXT,
    exit_code    INTEGER,
    sla_breached INTEGER NOT NULL DEFAULT 0,
    hc_status    TEXT,
    created_at   TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS job_state (
    job_name     TEXT PRIMARY KEY,
    last_success TEXT,
    last_failure TEXT,
    next_run     TEXT,
    lock_pid     INTEGER
);

CREATE TABLE IF NOT EXISTS run_logs (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES job_runs(id),
    seq    INTEGER NOT NULL,
    stream TEXT    NOT NULL,
    line   TEXT    NOT NULL,
    ts     TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS run_outputs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER NOT NULL REFERENCES job_runs(id),
    job_name   TEXT    NOT NULL,
    var_name   TEXT    NOT NULL,
    value      TEXT    NOT NULL,
    cycle_id   TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_run_outputs_cycle   ON run_outputs(cycle_id);
CREATE INDEX IF NOT EXISTS idx_job_runs_job_name   ON job_runs(job_name);
CREATE INDEX IF NOT EXISTS idx_run_logs_run_seq     ON run_logs(run_id, seq);

CREATE TABLE IF NOT EXISTS alerts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    job_name        TEXT    NOT NULL,
    run_id          INTEGER REFERENCES job_runs(id),
    event           TEXT    NOT NULL DEFAULT '',
    channel         TEXT    NOT NULL,
    status          TEXT    NOT NULL DEFAULT 'delivered',
    attempts        INTEGER NOT NULL DEFAULT 1,
    last_attempt_at TEXT,
    error           TEXT,
    sent_at         TEXT    NOT NULL,
    payload         TEXT
);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Apply incremental column additions one at a time, ignoring duplicate-column.
	alterStmts := []string{
		"ALTER TABLE job_runs ADD COLUMN status_reason   TEXT",
		"ALTER TABLE alerts ADD COLUMN event           TEXT    NOT NULL DEFAULT ''",
		"ALTER TABLE alerts ADD COLUMN status          TEXT    NOT NULL DEFAULT 'delivered'",
		"ALTER TABLE alerts ADD COLUMN attempts        INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE alerts ADD COLUMN last_attempt_at TEXT",
		"ALTER TABLE alerts ADD COLUMN error           TEXT",
	}
	for _, stmt := range alterStmts {
		if _, err := s.db.Exec(stmt); err != nil {
			// SQLite returns "duplicate column name" when the column already exists.
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("store: migrate: %w", err)
			}
		}
	}
	return nil
}

// ─── Write operations ─────────────────────────────────────────────────────────

// RecordRunStart inserts a new row into job_runs and returns its ID.
// The caller should set run.Status to StatusPending or StatusRunning.
// TriggeredBy defaults to "scheduler" when empty.
func (s *Store) RecordRunStart(ctx context.Context, run Run) (int64, error) {
	if run.TriggeredBy == "" {
		run.TriggeredBy = "scheduler"
	}
	if run.Status == "" {
		run.Status = StatusPending
	}
	now := time.Now().UTC().Format(timeFormat)

	var id int64
	err := s.write(ctx, func(tx *sql.Tx) error {
		const q = `
			INSERT INTO job_runs
				(job_name, status, status_reason, attempt, trigger, triggered_by, reason, started_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
		res, err := tx.Exec(q,
			run.JobName,
			string(run.Status),
			nullString(run.StatusReason),
			run.Attempt,
			string(run.Trigger),
			run.TriggeredBy,
			nullString(run.Reason),
			nullTimeVal(run.StartedAt),
			now,
		)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// RecordRunEnd updates the terminal fields of a run.
func (s *Store) RecordRunEnd(
	ctx context.Context,
	id int64,
	status RunStatus,
	statusReason string,
	exitCode *int,
	finishedAt time.Time,
	slaBreached bool,
	hcStatus *HCStatus,
) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `
			UPDATE job_runs
			SET status=?, status_reason=?, exit_code=?, finished_at=?, sla_breached=?, hc_status=?
			WHERE id=?`
		slab := 0
		if slaBreached {
			slab = 1
		}
		_, err := tx.Exec(q,
			string(status),
			nullString(statusReason),
			exitCode,
			finishedAt.UTC().Format(timeFormat),
			slab,
			nullHCStatus(hcStatus),
			id,
		)
		return err
	})
}

// SetRunStatusReason updates only the status_reason field of a run.
func (s *Store) SetRunStatusReason(ctx context.Context, id int64, statusReason string) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `UPDATE job_runs SET status_reason=? WHERE id=?`
		_, err := tx.Exec(q, nullString(statusReason), id)
		return err
	})
}

// RecordLog appends one line to run_logs.
func (s *Store) RecordLog(ctx context.Context, line LogLine) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `INSERT INTO run_logs (run_id, seq, stream, line, ts) VALUES (?, ?, ?, ?, ?)`
		_, err := tx.Exec(q,
			line.RunID, line.Seq, line.Stream, line.Line,
			line.TS.UTC().Format(timeFormat),
		)
		return err
	})
}

// RecordOutput writes a captured output variable to run_outputs.
func (s *Store) RecordOutput(ctx context.Context, out RunOutput) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `
			INSERT INTO run_outputs (run_id, job_name, var_name, value, cycle_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`
		_, err := tx.Exec(q,
			out.RunID, out.JobName, out.VarName, out.Value, out.CycleID,
			time.Now().UTC().Format(timeFormat),
		)
		return err
	})
}

// RecordAlert appends an alert delivery audit row to alerts.
func (s *Store) RecordAlert(ctx context.Context, alert Alert) error {
	if alert.SentAt.IsZero() {
		alert.SentAt = time.Now().UTC()
	}
	if alert.Status == "" {
		alert.Status = "delivered"
	}
	if alert.Attempts == 0 {
		alert.Attempts = 1
	}
	now := alert.SentAt.UTC().Format(timeFormat)
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `
			INSERT INTO alerts
				(job_name, run_id, event, channel, status, attempts, last_attempt_at, error, sent_at, payload)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		_, err := tx.Exec(q,
			alert.JobName,
			alert.RunID,
			nullString(alert.Event),
			alert.Channel,
			alert.Status,
			alert.Attempts,
			now,
			nullString(alert.DeliveryError),
			now,
			nullString(alert.Payload),
		)
		return err
	})
}

// ClearJobLock sets lock_pid = NULL for the given job.
// Returns an error if the job currently has a RUNNING run.
func (s *Store) ClearJobLock(ctx context.Context, jobName string) error {
	// Check for a running row first.
	const checkQ = `SELECT COUNT(*) FROM job_runs WHERE job_name = ? AND status = 'RUNNING'`
	var running int
	if err := s.db.QueryRowContext(ctx, checkQ, jobName).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return fmt.Errorf("job %q has a RUNNING execution; stop it before clearing the lock", jobName)
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `UPDATE job_state SET lock_pid = NULL WHERE job_name = ?`
		_, err := tx.Exec(q, jobName)
		return err
	})
}

// MarkAlertForRetry sets an alert's status back to 'pending' so the
// notification dispatcher will attempt re-delivery.
// Returns store.ErrNotFound when no alert with the given id exists.
func (s *Store) MarkAlertForRetry(ctx context.Context, alertID int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const checkQ = `SELECT status FROM alerts WHERE id = ?`
		var status string
		if err := tx.QueryRow(checkQ, alertID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status == "pending" {
			return ErrAlreadyPending
		}
		const q = `UPDATE alerts SET status = 'pending', attempts = attempts + 1 WHERE id = ?`
		_, err := tx.Exec(q, alertID)
		return err
	})
}

// MarkRunSLABreached sets sla_breached=1 for a run row.
func (s *Store) MarkRunSLABreached(ctx context.Context, runID int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `UPDATE job_runs SET sla_breached = 1 WHERE id = ?`
		_, err := tx.Exec(q, runID)
		return err
	})
}

// UpdateJobState upserts the job_state row for a job.
func (s *Store) UpdateJobState(ctx context.Context, state JobState) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `
			INSERT INTO job_state (job_name, last_success, last_failure, next_run, lock_pid)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(job_name) DO UPDATE SET
				last_success = excluded.last_success,
				last_failure = excluded.last_failure,
				next_run     = excluded.next_run,
				lock_pid     = excluded.lock_pid`
		_, err := tx.Exec(q,
			state.JobName,
			nullTimeVal(state.LastSuccess),
			nullTimeVal(state.LastFailure),
			nullTimeVal(state.NextRun),
			state.LockPID,
		)
		return err
	})
}

// ─── Read operations ──────────────────────────────────────────────────────────

// GetJobState returns the scheduler state for a job, or nil if no row exists.
func (s *Store) GetJobState(ctx context.Context, jobName string) (*JobState, error) {
	const q = `
		SELECT job_name, last_success, last_failure, next_run, lock_pid
		FROM job_state WHERE job_name = ?`
	row := s.db.QueryRowContext(ctx, q, jobName)
	return scanJobState(row)
}

// GetRun returns the run record for id, or nil if not found.
func (s *Store) GetRun(ctx context.Context, id int64) (*Run, error) {
	const q = `
		SELECT id, job_name, status, status_reason, attempt, trigger, triggered_by, reason,
		       started_at, finished_at, exit_code, sla_breached, hc_status, created_at
		FROM job_runs WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	return scanRun(row)
}

// ListJobStates returns all rows from job_state, ordered by job_name.
func (s *Store) ListJobStates(ctx context.Context) ([]JobState, error) {
	const q = `
		SELECT job_name, last_success, last_failure, next_run, lock_pid
		FROM job_state ORDER BY job_name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []JobState
	for rows.Next() {
		var js JobState
		var lastSuccess, lastFailure, nextRun *string
		if err := rows.Scan(&js.JobName, &lastSuccess, &lastFailure, &nextRun, &js.LockPID); err != nil {
			return nil, err
		}
		js.LastSuccess = parseTime(lastSuccess)
		js.LastFailure = parseTime(lastFailure)
		js.NextRun = parseTime(nextRun)
		out = append(out, js)
	}
	return out, rows.Err()
}

// GetRunLogs returns all log lines for runID ordered by seq.
func (s *Store) GetRunLogs(ctx context.Context, runID int64) ([]LogLine, error) {
	const q = `
		SELECT run_id, seq, stream, line, ts
		FROM run_logs WHERE run_id = ? ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var ll LogLine
		var ts string
		if err := rows.Scan(&ll.RunID, &ll.Seq, &ll.Stream, &ll.Line, &ts); err != nil {
			return nil, err
		}
		t, _ := time.Parse(timeFormat, ts)
		ll.TS = t
		out = append(out, ll)
	}
	return out, rows.Err()
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

func scanJobState(row *sql.Row) (*JobState, error) {
	var js JobState
	var lastSuccess, lastFailure, nextRun *string
	err := row.Scan(&js.JobName, &lastSuccess, &lastFailure, &nextRun, &js.LockPID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	js.LastSuccess = parseTime(lastSuccess)
	js.LastFailure = parseTime(lastFailure)
	js.NextRun = parseTime(nextRun)
	return &js, nil
}

func scanRun(row *sql.Row) (*Run, error) {
	var r Run
	var statusStr, triggerStr string
	var startedAt, finishedAt, createdAt *string
	var hcStatusStr *string
	var slaBreached int
	var triggeredBy, reason, statusReason sql.NullString

	err := row.Scan(
		&r.ID, &r.JobName, &statusStr, &statusReason, &r.Attempt, &triggerStr,
		&triggeredBy, &reason,
		&startedAt, &finishedAt, &r.ExitCode,
		&slaBreached, &hcStatusStr, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.Status = RunStatus(statusStr)
	r.StatusReason = statusReason.String
	r.Trigger = Trigger(triggerStr)
	r.TriggeredBy = triggeredBy.String
	r.Reason = reason.String
	r.SLABreached = slaBreached != 0
	r.StartedAt = parseTime(startedAt)
	r.FinishedAt = parseTime(finishedAt)
	if t := parseTime(createdAt); t != nil {
		r.CreatedAt = *t
	}
	if hcStatusStr != nil {
		h := HCStatus(*hcStatusStr)
		r.HCStatus = &h
	}
	return &r, nil
}

// ─── Null / time helpers ──────────────────────────────────────────────────────

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullTimeVal(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeFormat)
}

func nullHCStatus(h *HCStatus) interface{} {
	if h == nil {
		return nil
	}
	return string(*h)
}

func parseTime(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse(timeFormat, *s)
	if err != nil {
		return nil
	}
	return &t
}
