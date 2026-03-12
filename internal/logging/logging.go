// Package logging provides structured-logging setup for huskyd, driven by the
// LogConfig section of huskyd.yaml.
//
// It creates the appropriate slog.Handler (text or JSON), routes output to the
// configured destination (stdout, stderr, or a rotating file), and optionally
// starts an audit log that records job state-transition events as newline-
// delimited JSON.
//
// Hot-reload of log.level is supported via the exported LevelVar: callers can
// swap the level at runtime (e.g. on SIGHUP) without recreating the logger.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"gopkg.in/lumberjack.v2"

	"github.com/husky-scheduler/husky/internal/daemoncfg"
)

// Setup initialises structured logging from cfg and returns:
//
//   - logger   — the primary slog.Logger for daemon use.
//   - levelVar — a *slog.LevelVar that can be changed at runtime (SIGHUP).
//   - audit    — an AuditLogger (may be nil when audit_log.enabled is false).
//   - err      — non-nil only when file output cannot be opened.
func Setup(cfg daemoncfg.LogConfig) (*slog.Logger, *slog.LevelVar, *AuditLogger, error) {
	lv := new(slog.LevelVar)
	setLevel(lv, cfg.Level)

	w, err := buildWriter(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("logging: open output: %w", err)
	}

	opts := &slog.HandlerOptions{Level: lv}
	var handler slog.Handler
	if strings.ToLower(strings.TrimSpace(cfg.Format)) == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	logger := slog.New(handler)

	var audit *AuditLogger
	if cfg.AuditLog.Enabled {
		audit, err = newAuditLogger(cfg.AuditLog)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("logging: open audit log: %w", err)
		}
	}

	return logger, lv, audit, nil
}

// SetLevel changes the active log level on an existing LevelVar.
// Accepted values: "debug", "info", "warn", "error" (case-insensitive).
// Unknown values default to "info".
func SetLevel(lv *slog.LevelVar, lvl string) {
	setLevel(lv, lvl)
}

// setLevel is the unexported implementation shared by Setup and SetLevel.
func setLevel(lv *slog.LevelVar, lvl string) {
	switch strings.ToLower(strings.TrimSpace(lvl)) {
	case "debug":
		lv.Set(slog.LevelDebug)
	case "warn", "warning":
		lv.Set(slog.LevelWarn)
	case "error":
		lv.Set(slog.LevelError)
	default: // "info" or empty
		lv.Set(slog.LevelInfo)
	}
}

// buildWriter returns the io.Writer to use for the main logger based on cfg.Output.
func buildWriter(cfg daemoncfg.LogConfig) (io.Writer, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Output)) {
	case "stderr":
		return os.Stderr, nil
	case "file":
		if cfg.File.Path == "" {
			return nil, fmt.Errorf("log.output=file requires log.file.path to be set")
		}
		maxSize := cfg.File.MaxSizeMB
		if maxSize <= 0 {
			maxSize = 100
		}
		maxBackups := cfg.File.MaxBackups
		if maxBackups <= 0 {
			maxBackups = 5
		}
		maxAge := cfg.File.MaxAgeDays
		if maxAge <= 0 {
			maxAge = 30
		}
		return &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     maxAge,
			Compress:   cfg.File.Compress,
		}, nil
	default: // "stdout" or empty
		return os.Stdout, nil
	}
}

// ── AuditLogger ───────────────────────────────────────────────────────────────

// AuditLogger writes one newline-delimited JSON record per job state transition
// to a dedicated rotating file.
type AuditLogger struct {
	lj *lumberjack.Logger
}

func newAuditLogger(cfg daemoncfg.AuditLogConfig) (*AuditLogger, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("log.audit_log.path must be set when audit_log.enabled is true")
	}
	maxSize := cfg.MaxSizeMB
	if maxSize <= 0 {
		maxSize = 50
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 5
	}
	lj := &lumberjack.Logger{
		Filename:   cfg.Path,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
	}
	return &AuditLogger{lj: lj}, nil
}

// Log writes a single audit event as a JSON line to the rotating audit file.
// Safe to call on a nil *AuditLogger (no-op).
func (a *AuditLogger) Log(job string, runID int64, status, trigger string, durationMS int64, reason string) {
	if a == nil {
		return
	}
	evt := map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339),
		"job":         job,
		"run_id":      runID,
		"status":      status,
		"trigger":     trigger,
		"duration_ms": durationMS,
	}
	if reason != "" {
		evt["reason"] = reason
	}
	b, err := json.Marshal(evt)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = a.lj.Write(b)
}

// Close releases the audit log file handle.  Safe to call on a nil *AuditLogger.
func (a *AuditLogger) Close() error {
	if a == nil {
		return nil
	}
	return a.lj.Close()
}
