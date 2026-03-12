package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RunSearchParams controls filtering for audit-style run history queries.
type RunSearchParams struct {
	Job         string
	JobNames    []string // if non-empty, restrict to this set of job names (used for tag filtering)
	Trigger     Trigger
	Status      RunStatus
	Since       *time.Time
	Until       *time.Time
	SLABreached *bool
	Reason      string
	Limit       int
	Offset      int
}

// GetLastRunForJob returns the most recently created run record for jobName,
// or nil when no runs have been recorded yet.
func (s *Store) GetLastRunForJob(ctx context.Context, jobName string) (*Run, error) {
	const q = `
		SELECT id, job_name, status, status_reason, attempt, trigger, triggered_by, reason,
		       started_at, finished_at, exit_code, sla_breached, hc_status, created_at
		FROM job_runs WHERE job_name = ?
		ORDER BY id DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, jobName)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// GetRunsForJob returns the most recent `limit` runs for jobName, newest first.
func (s *Store) GetRunsForJob(ctx context.Context, jobName string, limit int) ([]Run, error) {
	const q = `
		SELECT id, job_name, status, status_reason, attempt, trigger, triggered_by, reason,
		       started_at, finished_at, exit_code, sla_breached, hc_status, created_at
		FROM job_runs WHERE job_name = ?
		ORDER BY id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, jobName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

// ListRunsByStatus returns all runs whose status equals the given value.
func (s *Store) ListRunsByStatus(ctx context.Context, status RunStatus) ([]Run, error) {
	const q = `
		SELECT id, job_name, status, status_reason, attempt, trigger, triggered_by, reason,
		       started_at, finished_at, exit_code, sla_breached, hc_status, created_at
		FROM job_runs WHERE status = ?
		ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

// MarkRunStatus forcibly sets the status (and finished_at) of a run.
// Used during crash recovery to close orphaned RUNNING rows.
func (s *Store) MarkRunStatus(ctx context.Context, runID int64, status RunStatus) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `UPDATE job_runs SET status=?, finished_at=? WHERE id=?`
		_, err := tx.Exec(q,
			string(status),
			time.Now().UTC().Format(timeFormat),
			runID,
		)
		return err
	})
}

// IncrementRunAttempt bumps the attempt counter on a run row (used by the
// retry FSM when reusing an existing run record is not desired; normally a
// new run row is created per attempt instead).
func (s *Store) IncrementRunAttempt(ctx context.Context, runID int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		const q = `UPDATE job_runs SET attempt = attempt + 1 WHERE id = ?`
		_, err := tx.Exec(q, runID)
		return err
	})
}

// GetRunOutput returns a single captured output variable for a cycle.
// Returns nil when not found.
func (s *Store) GetRunOutput(ctx context.Context, cycleID, jobName, varName string) (*RunOutput, error) {
	const q = `
		SELECT run_id, job_name, var_name, value, cycle_id
		FROM run_outputs
		WHERE cycle_id = ? AND job_name = ? AND var_name = ?
		ORDER BY id DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, cycleID, jobName, varName)
	var out RunOutput
	err := row.Scan(&out.RunID, &out.JobName, &out.VarName, &out.Value, &out.CycleID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRunOutputs returns all captured output variables for a given cycle.
func (s *Store) ListRunOutputs(ctx context.Context, cycleID string) ([]RunOutput, error) {
	const q = `
		SELECT run_id, job_name, var_name, value, cycle_id
		FROM run_outputs WHERE cycle_id = ?
		ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, cycleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunOutput
	for rows.Next() {
		var o RunOutput
		if err := rows.Scan(&o.RunID, &o.JobName, &o.VarName, &o.Value, &o.CycleID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SearchRuns returns run history rows ordered newest-first, filtered by params.
func (s *Store) SearchRuns(ctx context.Context, params RunSearchParams) ([]Run, error) {
	where := make([]string, 0, 5)
	args := make([]any, 0, 8)

	if params.Job != "" {
		where = append(where, "job_name = ?")
		args = append(args, params.Job)
	}
	if len(params.JobNames) > 0 {
		placeholders := make([]string, len(params.JobNames))
		for i, n := range params.JobNames {
			placeholders[i] = "?"
			args = append(args, n)
		}
		where = append(where, "job_name IN ("+strings.Join(placeholders, ",")+")")
	}
	if params.Trigger != "" {
		where = append(where, "trigger = ?")
		args = append(args, string(params.Trigger))
	}
	if params.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(params.Status))
	}
	if params.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, params.Since.UTC().Format(timeFormat))
	}
	if params.Until != nil {
		where = append(where, "created_at <= ?")
		args = append(args, params.Until.UTC().Format(timeFormat))
	}
	if params.SLABreached != nil {
		v := 0
		if *params.SLABreached {
			v = 1
		}
		where = append(where, "sla_breached = ?")
		args = append(args, v)
	}
	if params.Reason != "" {
		where = append(where, "LOWER(COALESCE(reason, '')) LIKE ?")
		args = append(args, "%"+strings.ToLower(params.Reason)+"%")
	}

	query := `
		SELECT id, job_name, status, status_reason, attempt, trigger, triggered_by, reason,
		       started_at, finished_at, exit_code, sla_breached, hc_status, created_at
		FROM job_runs`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC"

	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	defer rows.Close()
	return scanRuns(rows)
}

// GetRunLogsPaginated returns log lines for runID ordered by seq, with limit/offset.
func (s *Store) GetRunLogsPaginated(ctx context.Context, runID int64, limit, offset int) ([]LogLine, error) {
	if limit <= 0 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	const q = `
		SELECT run_id, seq, stream, line, ts
		FROM run_logs
		WHERE run_id = ?
		ORDER BY seq
		LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, q, runID, limit, offset)
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

// GetRunLogsAfterSeq returns log lines whose seq is greater than afterSeq.
func (s *Store) GetRunLogsAfterSeq(ctx context.Context, runID int64, afterSeq int, limit int) ([]LogLine, error) {
	if limit <= 0 {
		limit = 500
	}
	const q = `
		SELECT run_id, seq, stream, line, ts
		FROM run_logs
		WHERE run_id = ? AND seq > ?
		ORDER BY seq
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, runID, afterSeq, limit)
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

// GetRunOutputsByRunID returns captured output variables for a specific run.
func (s *Store) GetRunOutputsByRunID(ctx context.Context, runID int64) ([]RunOutput, error) {
	const q = `
		SELECT run_id, job_name, var_name, value, cycle_id
		FROM run_outputs
		WHERE run_id = ?
		ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunOutput
	for rows.Next() {
		var ro RunOutput
		if err := rows.Scan(&ro.RunID, &ro.JobName, &ro.VarName, &ro.Value, &ro.CycleID); err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, rows.Err()
}

// GetPreviousCompletedRun returns the most recent SUCCESS/FAILED/SKIPPED run
// for jobName whose id is lower than beforeRunID.
func (s *Store) GetPreviousCompletedRun(ctx context.Context, jobName string, beforeRunID int64) (*Run, error) {
	const q = `
		SELECT id, job_name, status, status_reason, attempt, trigger, triggered_by, reason,
		       started_at, finished_at, exit_code, sla_breached, hc_status, created_at
		FROM job_runs
		WHERE job_name = ?
		  AND id < ?
		  AND status IN ('SUCCESS', 'FAILED', 'SKIPPED')
		ORDER BY id DESC
		LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, jobName, beforeRunID)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// ListAlerts returns alert delivery rows ordered newest-first.
func (s *Store) ListAlerts(ctx context.Context, limit int) ([]Alert, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT id, job_name, run_id, event, channel, status, attempts, last_attempt_at, error, sent_at, payload
		FROM alerts
		ORDER BY id DESC
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		var a Alert
		var sentAt, lastAttempt *string
		var event, deliveryError sql.NullString
		if err := rows.Scan(&a.ID, &a.JobName, &a.RunID, &event, &a.Channel,
			&a.Status, &a.Attempts, &lastAttempt, &deliveryError, &sentAt, &a.Payload); err != nil {
			return nil, err
		}
		a.Event = event.String
		a.DeliveryError = deliveryError.String
		if sentAt != nil {
			t, _ := time.Parse(timeFormat, *sentAt)
			a.SentAt = t
		}
		a.LastAttemptAt = parseTime(lastAttempt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SearchRunOutputs returns run_outputs rows with optional filters.
// Pass jobName="" or cycleID="" to skip those filters; pass runID<=0 to skip run_id filter.
func (s *Store) SearchRunOutputs(ctx context.Context, jobName, cycleID string, runID int64, limit, offset int) ([]RunOutput, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where := make([]string, 0, 3)
	args := make([]any, 0, 5)
	if jobName != "" {
		where = append(where, "job_name = ?")
		args = append(args, jobName)
	}
	if cycleID != "" {
		where = append(where, "cycle_id = ?")
		args = append(args, cycleID)
	}
	if runID > 0 {
		where = append(where, "run_id = ?")
		args = append(args, runID)
	}
	q := "SELECT run_id, job_name, var_name, value, cycle_id FROM run_outputs"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search run outputs: %w", err)
	}
	defer rows.Close()
	var out []RunOutput
	for rows.Next() {
		var o RunOutput
		if err := rows.Scan(&o.RunID, &o.JobName, &o.VarName, &o.Value, &o.CycleID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListAlertsPaginated returns alert rows newest-first with optional job_name
// and status filters.
func (s *Store) ListAlertsPaginated(ctx context.Context, jobName, status string, limit, offset int) ([]Alert, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if jobName != "" {
		where = append(where, "job_name = ?")
		args = append(args, jobName)
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	q := `SELECT id, job_name, run_id, event, channel, status, attempts, last_attempt_at, error, sent_at, payload FROM alerts`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var sentAt, lastAttempt *string
		var event, deliveryError sql.NullString
		if err := rows.Scan(&a.ID, &a.JobName, &a.RunID, &event, &a.Channel,
			&a.Status, &a.Attempts, &lastAttempt, &deliveryError, &sentAt, &a.Payload); err != nil {
			return nil, err
		}
		a.Event = event.String
		a.DeliveryError = deliveryError.String
		if sentAt != nil {
			t, _ := time.Parse(timeFormat, *sentAt)
			a.SentAt = t
		}
		a.LastAttemptAt = parseTime(lastAttempt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAlertByID returns a single alert row, or nil if not found.
func (s *Store) GetAlertByID(ctx context.Context, id int64) (*Alert, error) {
	const q = `SELECT id, job_name, run_id, event, channel, status, attempts, last_attempt_at, error, sent_at, payload
	           FROM alerts WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	var a Alert
	var sentAt, lastAttempt *string
	var event, deliveryError sql.NullString
	err := row.Scan(&a.ID, &a.JobName, &a.RunID, &event, &a.Channel,
		&a.Status, &a.Attempts, &lastAttempt, &deliveryError, &sentAt, &a.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Event = event.String
	a.DeliveryError = deliveryError.String
	if sentAt != nil {
		t, _ := time.Parse(timeFormat, *sentAt)
		a.SentAt = t
	}
	a.LastAttemptAt = parseTime(lastAttempt)
	return &a, nil
}

// LogSearchParams controls filtering for the global run_logs explorer.
type LogSearchParams struct {
	RunID   int64
	JobName string // joined via job_runs
	Stream  string // stdout | stderr | healthcheck
	Query   string // full-text search on line content
	Limit   int
	Offset  int
}

// SearchRunLogs returns log lines across all runs with optional filters.
func (s *Store) SearchRunLogs(ctx context.Context, params LogSearchParams) ([]LogLine, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 200
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	where := make([]string, 0, 4)
	args := make([]any, 0, 6)

	// Use a join only when filtering by job_name.
	from := "run_logs rl"
	if params.JobName != "" {
		from = "run_logs rl JOIN job_runs jr ON jr.id = rl.run_id"
		where = append(where, "jr.job_name = ?")
		args = append(args, params.JobName)
	}
	if params.RunID > 0 {
		where = append(where, "rl.run_id = ?")
		args = append(args, params.RunID)
	}
	if params.Stream != "" {
		where = append(where, "rl.stream = ?")
		args = append(args, params.Stream)
	}
	if params.Query != "" {
		where = append(where, "LOWER(rl.line) LIKE ?")
		args = append(args, "%"+strings.ToLower(params.Query)+"%")
	}

	q := "SELECT rl.run_id, rl.seq, rl.stream, rl.line, rl.ts FROM " + from
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY rl.run_id DESC, rl.seq ASC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search run logs: %w", err)
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

// ─── shared scan helper ───────────────────────────────────────────────────────

func scanRuns(rows *sql.Rows) ([]Run, error) {
	var out []Run
	for rows.Next() {
		var r Run
		var statusStr, triggerStr string
		var startedAt, finishedAt, createdAt *string
		var hcStatusStr *string
		var slaBreached int
		var triggeredBy, reason, statusReason sql.NullString

		if err := rows.Scan(
			&r.ID, &r.JobName, &statusStr, &statusReason, &r.Attempt, &triggerStr,
			&triggeredBy, &reason,
			&startedAt, &finishedAt, &r.ExitCode,
			&slaBreached, &hcStatusStr, &createdAt,
		); err != nil {
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
		out = append(out, r)
	}
	return out, rows.Err()
}
