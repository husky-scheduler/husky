package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// StoreConfig contains runtime-tunable SQLite connection parameters driven by
// the huskyd.yaml storage section.
type StoreConfig struct {
	// WALAutocheckpoint sets the SQLite wal_autocheckpoint pragma (pages).
	// Default 0 means "leave at database default (1000 pages)".
	WALAutocheckpoint int

	// BusyTimeoutMS is the SQLite busy-handler timeout in milliseconds.
	// Default 0 means "use the compiled-in default (5000 ms)".
	BusyTimeoutMS int
}

// OpenWithConfig opens the SQLite database at path and applies the additional
// pragmas specified in cfg.  All other behaviour is identical to Open.
func OpenWithConfig(path string, cfg StoreConfig) (*Store, error) {
	busyMS := cfg.BusyTimeoutMS
	if busyMS <= 0 {
		busyMS = 5000
	}
	dsn := fmt.Sprintf(
		"%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=%d",
		path, busyMS,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
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

	// Apply optional pragmas after migration so the schema is stable.
	if cfg.WALAutocheckpoint > 0 {
		if _, err := db.ExecContext(context.Background(),
			fmt.Sprintf("PRAGMA wal_autocheckpoint = %d", cfg.WALAutocheckpoint),
		); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: wal_autocheckpoint pragma: %w", err)
		}
	}

	go s.runWriter()
	return s, nil
}

// ── Retention vacuum ──────────────────────────────────────────────────────────

// VacuumResult holds the per-table row counts pruned during a Vacuum call.
type VacuumResult struct {
	RunsDeleted    int64
	LogsDeleted    int64
	OutputsDeleted int64
}

// Total returns the sum of all deleted rows.
func (vr VacuumResult) Total() int64 {
	return vr.RunsDeleted + vr.LogsDeleted + vr.OutputsDeleted
}

// Vacuum prunes old run records from the database according to the configured
// retention policy:
//
//   - If maxAge > 0:  delete completed job_runs (and their run_logs /
//     run_outputs) with finished_at older than now−maxAge.  PENDING and
//     RUNNING rows are never pruned.
//   - If maxRunsPerJob > 0:  after age pruning, enforce a per-job cap by
//     deleting the oldest completed runs beyond the cap.  PENDING and RUNNING
//     rows are never pruned.
//
// Returns the total number of rows deleted across all three tables.
func (s *Store) Vacuum(ctx context.Context, maxAge time.Duration, maxRunsPerJob int) (VacuumResult, error) {
	var result VacuumResult

	// Collect run IDs to delete.
	runIDs := make([]int64, 0)
	seen := make(map[int64]struct{})

	// ── Age pruning ───────────────────────────────────────────────────────────
	if maxAge > 0 {
		cutoff := time.Now().Add(-maxAge).UTC().Format(timeFormat)
		rows, err := s.db.QueryContext(ctx, `
			SELECT id FROM job_runs
			WHERE status NOT IN ('PENDING','RUNNING')
			  AND finished_at IS NOT NULL
			  AND finished_at < ?`, cutoff)
		if err != nil {
			return result, fmt.Errorf("vacuum: age query: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return result, err
			}
			if _, ok := seen[id]; !ok {
				runIDs = append(runIDs, id)
				seen[id] = struct{}{}
			}
		}
		_ = rows.Close()
		if rows.Err() != nil {
			return result, fmt.Errorf("vacuum: age scan: %w", rows.Err())
		}
	}

	// ── Max-runs-per-job pruning ──────────────────────────────────────────────
	if maxRunsPerJob > 0 {
		// Find distinct job names.
		jobs, err := s.distinctJobNames(ctx)
		if err != nil {
			return result, fmt.Errorf("vacuum: distinct jobs: %w", err)
		}
		for _, job := range jobs {
			rows, err := s.db.QueryContext(ctx, `
				SELECT id FROM job_runs
				WHERE job_name = ?
				  AND status NOT IN ('PENDING','RUNNING')
				ORDER BY id DESC
				LIMIT -1 OFFSET ?`, job, maxRunsPerJob)
			if err != nil {
				return result, fmt.Errorf("vacuum: cap query for %q: %w", job, err)
			}
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return result, err
				}
				if _, ok := seen[id]; !ok {
					runIDs = append(runIDs, id)
					seen[id] = struct{}{}
				}
			}
			_ = rows.Close()
			if rows.Err() != nil {
				return result, fmt.Errorf("vacuum: cap scan for %q: %w", job, rows.Err())
			}
		}
	}

	if len(runIDs) == 0 {
		return result, nil
	}

	// ── Batch delete ──────────────────────────────────────────────────────────
	// SQLite supports IN clauses with up to 999 parameters; chunk if needed.
	const chunkSize = 900
	for start := 0; start < len(runIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[start:end]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}

		err := s.write(ctx, func(tx *sql.Tx) error {
			// Delete cascaded tables first (no ON DELETE CASCADE in schema).
			for _, tbl := range []string{"run_logs", "run_outputs"} {
				r, err := tx.ExecContext(ctx,
					"DELETE FROM "+tbl+" WHERE run_id IN ("+placeholders+")",
					args...)
				if err != nil {
					return fmt.Errorf("delete %s: %w", tbl, err)
				}
				n, _ := r.RowsAffected()
				if tbl == "run_logs" {
					result.LogsDeleted += n
				} else {
					result.OutputsDeleted += n
				}
			}
			// Delete the run rows themselves.
			r, err := tx.ExecContext(ctx,
				"DELETE FROM job_runs WHERE id IN ("+placeholders+")",
				args...)
			if err != nil {
				return fmt.Errorf("delete job_runs: %w", err)
			}
			n, _ := r.RowsAffected()
			result.RunsDeleted += n
			return nil
		})
		if err != nil {
			return result, fmt.Errorf("vacuum: delete chunk: %w", err)
		}
	}

	return result, nil
}

func (s *Store) distinctJobNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT job_name FROM job_runs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
