package api

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/ipc"
	"github.com/husky-scheduler/husky/internal/store"
)

// ServerConfig holds optional HTTP server settings loaded from huskyd.yaml.
// The zero value is safe and falls back to the existing hardcoded defaults.
type ServerConfig struct {
	// BasePath is a URL prefix prepended to all routes (e.g. "/husky").
	// The prefix is stripped before the request reaches any handler so internal
	// code always sees clean /api/… and /ws/… paths.
	BasePath string

	// CORS settings — when AllowedOrigins is non-empty the server injects
	// Access-Control-* headers on every response.
	CORSOrigins     []string
	CORSCredentials bool

	// AuthMiddleware, when non-nil, is applied to every route served by the
	// API server.  The caller composes auth + RBAC together before passing
	// them in.  Preflight OPTIONS requests are bypassed by the auth package.
	AuthMiddleware func(http.Handler) http.Handler

	// HTTP server timeouts. Zero means "use default".
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Dependencies contains the daemon callbacks and state used by the API server.
type Dependencies struct {
	Store  *store.Store
	Logger *slog.Logger

	ConfigSnapshot  func() *config.Config
	Status          func() ([]ipc.JobStatus, error)
	Trigger         func(jobName, reason string) error
	Cancel          func(jobName string) error
	PauseTag        func(tag string) (int, error)
	ResumeTag       func(tag string) (int, error)
	PauseJob        func(jobName string) error
	ResumeJob       func(jobName string) error
	SkipJob         func(jobName string) error
	TestIntegration func(ctx context.Context, name string) error
	DAGJSON         func() ([]byte, error)
	StopDaemon      func()
	ReloadDaemon    func() error

	// Metadata exposed on /api/daemon/info
	ConfigPath       string
	DaemonConfigPath string // path to huskyd.yaml; empty if not present
	DBPath           string
	Version          string
	PID              int

	StartedAt time.Time
}

// Server is the observability API server.
type Server struct {
	addr  string
	deps  Dependencies
	scfg  ServerConfig
	http  *http.Server
	start time.Time
}

// New creates a new API server. An optional ServerConfig can be passed as the
// third argument to enable base_path routing, CORS headers, and custom
// http.Server timeouts. The zero value of ServerConfig is safe.
func New(addr string, deps Dependencies, cfgs ...ServerConfig) *Server {
	var scfg ServerConfig
	if len(cfgs) > 0 {
		scfg = cfgs[0]
	}

	mux := http.NewServeMux()
	s := &Server{addr: addr, deps: deps, scfg: scfg, start: deps.StartedAt}
	if s.start.IsZero() {
		s.start = time.Now()
	}
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/pause", s.handlePauseByTag)
	mux.HandleFunc("/api/jobs/resume", s.handleResumeByTag)
	mux.HandleFunc("/api/jobs/", s.handleJobRoutes)
	mux.HandleFunc("/api/integrations/", s.handleIntegrationRoutes)
	mux.HandleFunc("/api/integrations", s.handleIntegrations)
	mux.HandleFunc("/api/runs/", s.handleRunRoutes)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/tags", s.handleTags)
	mux.HandleFunc("/api/dag", s.handleDAG)
	mux.HandleFunc("/api/daemon/stop", s.handleDaemonStop)
	mux.HandleFunc("/api/daemon/reload", s.handleDaemonReload)
	mux.HandleFunc("/api/daemon/info", s.handleDaemonInfo)
	mux.HandleFunc("/api/db/run_outputs", s.handleDBRunOutputs)
	mux.HandleFunc("/api/db/alerts", s.handleDBAlerts)
	mux.HandleFunc("/api/db/alerts/", s.handleDBAlertRoutes)
	mux.HandleFunc("/api/db/job_runs", s.handleDBJobRuns)
	mux.HandleFunc("/api/db/run_logs", s.handleDBRunLogs)
	mux.HandleFunc("/api/db/state", s.handleDBState)
	mux.HandleFunc("/api/db/job_state/", s.handleDBJobStateRoutes)
	mux.HandleFunc("/api/config/validate", s.handleConfigValidate)
	mux.HandleFunc("/api/config/save", s.handleConfigSave)
	mux.HandleFunc("/api/config/daemon", s.handleConfigDaemon)
	mux.HandleFunc("/api/config", s.handleConfigGet)
	mux.HandleFunc("/ws/logs/", s.handleWSLogs)
	mux.Handle("/", s.dashboardHandler())

	// Build the root handler, optionally wrapping with authentication,
	// base_path stripping, and CORS middleware.
	// Chain order (outer → inner): StripPrefix → CORS → Auth → mux
	var root http.Handler = mux
	if scfg.AuthMiddleware != nil {
		root = scfg.AuthMiddleware(root)
	}
	if len(scfg.CORSOrigins) > 0 {
		root = corsMiddleware(scfg.CORSOrigins, scfg.CORSCredentials, root)
	}
	if prefix := strings.TrimRight(scfg.BasePath, "/"); prefix != "" {
		root = http.StripPrefix(prefix, root)
	}

	// Resolve timeouts — use provided values or fall back to the same defaults
	// the server has always used.
	readHeaderTimeout := coerceDuration(scfg.ReadHeaderTimeout, 5*time.Second)
	readTimeout := coerceDuration(scfg.ReadTimeout, 30*time.Second)
	writeTimeout := coerceDuration(scfg.WriteTimeout, 60*time.Second)
	idleTimeout := coerceDuration(scfg.IdleTimeout, 120*time.Second)

	s.http = &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	return s
}

// coerceDuration returns v if v > 0, otherwise returns def.
func coerceDuration(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// corsMiddleware wraps h with an HTTP handler that injects Access-Control-*
// headers for the allowed origins. Preflight OPTIONS requests are handled
// immediately without forwarding to h.
func corsMiddleware(origins []string, credentials bool, h http.Handler) http.Handler {
	originsMap := make(map[string]bool, len(origins))
	hasWildcard := false
	for _, o := range origins {
		if o == "*" {
			hasWildcard = true
		}
		originsMap[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := hasWildcard || originsMap[origin]
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if credentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) dashboardHandler() http.Handler {
	sub, err := fs.Sub(dashboardAssets, "dashboard")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "dashboard assets not found")
		})
	}

	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) handlePauseByTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.PauseTag == nil {
		writeError(w, http.StatusNotImplemented, "pause not available")
		return
	}
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	if tag == "" {
		writeError(w, http.StatusBadRequest, "tag query parameter is required")
		return
	}
	count, err := s.deps.PauseTag(tag)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": count, "tag": tag})
}

func (s *Server) handleResumeByTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ResumeTag == nil {
		writeError(w, http.StatusNotImplemented, "resume not available")
		return
	}
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	if tag == "" {
		writeError(w, http.StatusBadRequest, "tag query parameter is required")
		return
	}
	count, err := s.deps.ResumeTag(tag)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resumed": count, "tag": tag})
}

// Serve starts the HTTP server on the provided listener and shuts it down when
// ctx is canceled. The caller is responsible for creating the listener so it
// can retrieve the actual bound address (e.g. when addr was "host:0").
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()

	if s.deps.Logger != nil {
		s.deps.Logger.Info("api server listening", "addr", ln.Addr().String())
	}
	err := s.http.Serve(ln)
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}

// ListenAndServe is a convenience wrapper that creates its own listener and
// calls Serve. It is used by tests and any caller that does not need to know
// the actual bound address.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	uptime := time.Since(s.start).Round(time.Second)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"started_at": s.start.UTC().Format(time.RFC3339),
		"uptime":     uptime.String(),
		"version":    s.deps.Version,
		"pid":        s.deps.PID,
	})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ConfigSnapshot == nil || s.deps.Status == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	cfg := s.deps.ConfigSnapshot()
	statusRows, err := s.deps.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	statusMap := make(map[string]ipc.JobStatus, len(statusRows))
	for _, row := range statusRows {
		statusMap[row.Name] = row
	}

	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	type jobSummary struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Frequency   string   `json:"frequency"`
		Timezone    string   `json:"timezone,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		NextRun     *string  `json:"next_run,omitempty"`
		LastSuccess *string  `json:"last_success,omitempty"`
		LastFailure *string  `json:"last_failure,omitempty"`
		Running     bool     `json:"running"`
		Paused      bool     `json:"paused,omitempty"`
	}

	out := make([]jobSummary, 0, len(cfg.Jobs))
	for name, job := range cfg.Jobs {
		if tagFilter != "" && !hasTag(job.Tags, tagFilter) {
			continue
		}
		row := statusMap[name]
		tz := job.Timezone
		if tz == "" {
			tz = cfg.Defaults.Timezone
		}
		out = append(out, jobSummary{
			Name:        name,
			Description: job.Description,
			Frequency:   job.Frequency,
			Timezone:    tz,
			Tags:        append([]string(nil), job.Tags...),
			NextRun:     row.NextRun,
			LastSuccess: row.LastSuccess,
			LastFailure: row.LastFailure,
			Running:     row.Running,
			Paused:      row.Paused,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleJobRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	jobName := parts[0]
	if len(parts) == 1 {
		s.handleJobDetail(w, r, jobName)
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "run":
			s.handleJobRun(w, r, jobName)
			return
		case "cancel":
			s.handleJobCancel(w, r, jobName)
			return
		case "pause":
			s.handleJobPause(w, r, jobName)
			return
		case "resume":
			s.handleJobResume(w, r, jobName)
			return
		case "retry":
			s.handleJobRetry(w, r, jobName)
			return
		case "skip":
			s.handleJobSkip(w, r, jobName)
			return
		}
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ConfigSnapshot == nil || s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	cfg := s.deps.ConfigSnapshot()
	job, ok := cfg.Jobs[jobName]
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	runsN := parseIntDefault(r.URL.Query().Get("runs"), 20)
	runs, err := s.deps.Store.GetRunsForJob(r.Context(), jobName, runsN)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Redact environment variable values before sending to client (§7.3).
	redacted := *job
	if len(redacted.Env) > 0 {
		masked := make(map[string]string, len(redacted.Env))
		for k := range redacted.Env {
			masked[k] = "***"
		}
		redacted.Env = masked
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": jobName,
		"job":  &redacted,
		"runs": runs,
	})
}

func (s *Server) handleJobRun(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Trigger == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}
	type req struct {
		Reason string `json:"reason"`
	}
	var body req
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	// Return 404 when the job doesn't exist in the current config.
	if s.deps.ConfigSnapshot != nil {
		if _, ok := s.deps.ConfigSnapshot().Jobs[jobName]; !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
	}
	if err := s.deps.Trigger(jobName, body.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Cancel == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}
	if err := s.deps.Cancel(jobName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRunRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	runID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run id")
		return
	}

	if len(parts) == 1 {
		s.handleRunDetail(w, r, runID)
		return
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "logs":
			s.handleRunLogs(w, r, runID)
			return
		case "outputs":
			s.handleRunOutputs(w, r, runID)
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request, runID int64) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	run, err := s.deps.Store.GetRun(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleRunLogs(w http.ResponseWriter, r *http.Request, runID int64) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	limit := parseIntDefault(r.URL.Query().Get("limit"), 500)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	includeHC := r.URL.Query().Get("include_healthcheck") == "true"

	lines, err := s.deps.Store.GetRunLogsPaginated(r.Context(), runID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !includeHC {
		filtered := make([]store.LogLine, 0, len(lines))
		for _, line := range lines {
			if line.Stream == "healthcheck" {
				continue
			}
			filtered = append(filtered, line)
		}
		lines = filtered
	}
	writeJSON(w, http.StatusOK, lines)
}

func (s *Server) handleRunOutputs(w http.ResponseWriter, r *http.Request, runID int64) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}
	out, err := s.deps.Store.GetRunOutputsByRunID(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil || s.deps.ConfigSnapshot == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	var since *time.Time
	if rawSince := strings.TrimSpace(r.URL.Query().Get("since")); rawSince != "" {
		parsed, err := time.Parse(time.RFC3339, rawSince)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since; expected RFC3339")
			return
		}
		since = &parsed
	}

	query := store.RunSearchParams{
		Job:     strings.TrimSpace(r.URL.Query().Get("job")),
		Trigger: store.Trigger(strings.TrimSpace(r.URL.Query().Get("trigger"))),
		Status:  store.RunStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))),
		Since:   since,
		Reason:  strings.TrimSpace(r.URL.Query().Get("reason")),
		Limit:   parseIntDefault(r.URL.Query().Get("limit"), 100),
		Offset:  parseIntDefault(r.URL.Query().Get("offset"), 0),
	}

	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	if tag != "" {
		cfg := s.deps.ConfigSnapshot()
		var names []string
		for name, job := range cfg.Jobs {
			if hasTag(job.Tags, tag) {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			writeJSON(w, http.StatusOK, []store.Run{})
			return
		}
		query.JobNames = names
	}

	runs, err := s.deps.Store.SearchRuns(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ConfigSnapshot == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	cfg := s.deps.ConfigSnapshot()
	counts := map[string]int{}
	for _, job := range cfg.Jobs {
		for _, tag := range job.Tags {
			counts[tag]++
		}
	}
	type item struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := make([]item, 0, len(counts))
	for tag, count := range counts {
		out = append(out, item{Tag: tag, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDAG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.DAGJSON == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	raw, err := s.deps.DAGJSON()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleWSLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}

	rawID := strings.TrimPrefix(r.URL.Path, "/ws/logs/")
	rawID = strings.Trim(rawID, "/")
	runID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run id")
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := r.Context()
	includeHC := r.URL.Query().Get("include_healthcheck") == "true"

	lastSeq := -1
	backfill, err := s.deps.Store.GetRunLogsPaginated(ctx, runID, 5000, 0)
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	for _, line := range backfill {
		if !includeHC && line.Stream == "healthcheck" {
			continue
		}
		if err := conn.WriteJSON(mapLogLine(line)); err != nil {
			return
		}
		if line.Seq > lastSeq {
			lastSeq = line.Seq
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lines, err := s.deps.Store.GetRunLogsAfterSeq(ctx, runID, lastSeq, 1000)
			if err != nil {
				_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
				return
			}
			for _, line := range lines {
				if !includeHC && line.Stream == "healthcheck" {
					continue
				}
				if err := conn.WriteJSON(mapLogLine(line)); err != nil {
					return
				}
				if line.Seq > lastSeq {
					lastSeq = line.Seq
				}
			}

			run, err := s.deps.Store.GetRun(ctx, runID)
			if err != nil {
				_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
				return
			}
			if run == nil {
				_ = conn.WriteJSON(map[string]any{"type": "end", "reason": "run not found"})
				return
			}
			if isTerminal(run.Status) {
				_ = conn.WriteJSON(map[string]any{"type": "end", "status": run.Status})
				return
			}
		}
	}
}

func hasTag(tags []string, needle string) bool {
	for _, tag := range tags {
		if tag == needle {
			return true
		}
	}
	return false
}

func parseIntDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func isTerminal(status store.RunStatus) bool {
	switch status {
	case store.StatusPending, store.StatusRunning:
		return false
	case store.StatusSuccess, store.StatusFailed, store.StatusCancelled, store.StatusRetrying, store.StatusSkipped:
		return true
	default:
		return false
	}
}

func mapLogLine(line store.LogLine) map[string]any {
	return map[string]any{
		"type":   "log",
		"run_id": line.RunID,
		"seq":    line.Seq,
		"stream": line.Stream,
		"line":   line.Line,
		"ts":     line.TS.UTC().Format(time.RFC3339),
	}
}

// ─── Per-job pause / resume ───────────────────────────────────────────────────

func (s *Server) handleJobPause(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.PauseJob == nil {
		writeError(w, http.StatusNotImplemented, "pause not available")
		return
	}
	if err := s.deps.PauseJob(jobName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleJobResume(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ResumeJob == nil {
		writeError(w, http.StatusNotImplemented, "resume not available")
		return
	}
	if err := s.deps.ResumeJob(jobName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleJobRetry(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Trigger == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}
	if s.deps.ConfigSnapshot != nil {
		if _, ok := s.deps.ConfigSnapshot().Jobs[jobName]; !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
	}
	if err := s.deps.Trigger(jobName, "manual retry"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) handleJobSkip(w http.ResponseWriter, r *http.Request, jobName string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.SkipJob == nil {
		writeError(w, http.StatusNotImplemented, "skip not available")
		return
	}
	if err := s.deps.SkipJob(jobName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Daemon control ───────────────────────────────────────────────────────────

func (s *Server) handleDaemonStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.StopDaemon == nil {
		writeError(w, http.StatusNotImplemented, "stop not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "daemon stopping"})
	go s.deps.StopDaemon()
}

func (s *Server) handleDaemonReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ReloadDaemon == nil {
		writeError(w, http.StatusNotImplemented, "reload not available")
		return
	}
	if err := s.deps.ReloadDaemon(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDaemonInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	uptime := time.Since(s.start).Round(time.Second)
	resp := map[string]any{
		"ok":                 true,
		"version":            s.deps.Version,
		"pid":                s.deps.PID,
		"config_path":        s.deps.ConfigPath,
		"daemon_config_path": s.deps.DaemonConfigPath,
		"db_path":            s.deps.DBPath,
		"started_at":         s.start.UTC().Format(time.RFC3339),
		"uptime":             uptime.String(),
	}
	if s.deps.Status != nil {
		if statuses, err := s.deps.Status(); err == nil {
			active, paused := 0, 0
			for _, js := range statuses {
				if js.Running {
					active++
				}
				if js.Paused {
					paused++
				}
			}
			resp["active_job_count"] = active
			resp["paused_job_count"] = paused
			resp["total_job_count"] = len(statuses)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── Database explorers ───────────────────────────────────────────────────────

func (s *Server) handleDBRunOutputs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "store not configured")
		return
	}
	jobName := strings.TrimSpace(r.URL.Query().Get("job"))
	cycleID := strings.TrimSpace(r.URL.Query().Get("cycle_id"))
	var runID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("run_id")); raw != "" {
		runID, _ = strconv.ParseInt(raw, 10, 64)
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	out, err := s.deps.Store.SearchRunOutputs(r.Context(), jobName, cycleID, runID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []store.RunOutput{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDBAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "store not configured")
		return
	}
	jobName := strings.TrimSpace(r.URL.Query().Get("job"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	out, err := s.deps.Store.ListAlertsPaginated(r.Context(), jobName, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []store.Alert{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDBAlertRoutes(w http.ResponseWriter, r *http.Request) {
	// /api/db/alerts/:id/retry
	path := strings.TrimPrefix(r.URL.Path, "/api/db/alerts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[1] == "retry" && r.Method == http.MethodPost {
		if s.deps.Store == nil {
			writeError(w, http.StatusInternalServerError, "store not configured")
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid alert id")
			return
		}
		err = s.deps.Store.MarkAlertForRetry(r.Context(), id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "alert not found")
			return
		}
		if err == store.ErrAlreadyPending {
			writeError(w, http.StatusConflict, "alert is already pending retry")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleDBJobRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "store not configured")
		return
	}

	var since *time.Time
	if rawSince := strings.TrimSpace(r.URL.Query().Get("since")); rawSince != "" {
		parsed, err := time.Parse(time.RFC3339, rawSince)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since; expected RFC3339")
			return
		}
		since = &parsed
	}

	var until *time.Time
	if rawUntil := strings.TrimSpace(r.URL.Query().Get("until")); rawUntil != "" {
		parsed, err := time.Parse(time.RFC3339, rawUntil)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid until; expected RFC3339")
			return
		}
		until = &parsed
	}

	slaBreachedRaw := strings.TrimSpace(r.URL.Query().Get("sla_breached"))
	params := store.RunSearchParams{
		Job:     strings.TrimSpace(r.URL.Query().Get("job")),
		Trigger: store.Trigger(strings.TrimSpace(r.URL.Query().Get("trigger"))),
		Status:  store.RunStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))),
		Since:   since,
		Until:   until,
		Limit:   parseIntDefault(r.URL.Query().Get("limit"), 50),
		Offset:  parseIntDefault(r.URL.Query().Get("offset"), 0),
	}
	if slaBreachedRaw == "true" || slaBreachedRaw == "1" {
		v := true
		params.SLABreached = &v
	}

	runs, err := s.deps.Store.SearchRuns(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleDBRunLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "store not configured")
		return
	}
	var runID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("run_id")); raw != "" {
		runID, _ = strconv.ParseInt(raw, 10, 64)
	}
	params := store.LogSearchParams{
		RunID:   runID,
		JobName: strings.TrimSpace(r.URL.Query().Get("job")),
		Stream:  strings.TrimSpace(r.URL.Query().Get("stream")),
		Query:   strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:   parseIntDefault(r.URL.Query().Get("limit"), 200),
		Offset:  parseIntDefault(r.URL.Query().Get("offset"), 0),
	}
	lines, err := s.deps.Store.SearchRunLogs(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if lines == nil {
		lines = []store.LogLine{}
	}
	writeJSON(w, http.StatusOK, lines)
}

func (s *Server) handleDBJobStateRoutes(w http.ResponseWriter, r *http.Request) {
	// /api/db/job_state/:jobName/clear_lock
	path := strings.TrimPrefix(r.URL.Path, "/api/db/job_state/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[1] == "clear_lock" && r.Method == http.MethodPost {
		if s.deps.Store == nil {
			writeError(w, http.StatusInternalServerError, "store not configured")
			return
		}
		jobName := parts[0]
		if jobName == "" {
			writeError(w, http.StatusBadRequest, "job name required")
			return
		}
		err := s.deps.Store.ClearJobLock(r.Context(), jobName)
		if err != nil {
			if strings.Contains(err.Error(), "RUNNING") {
				writeError(w, http.StatusConflict, err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleDBState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "store not configured")
		return
	}
	states, err := s.deps.Store.ListJobStates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type stateOut struct {
		JobName     string  `json:"job_name"`
		LastSuccess *string `json:"last_success,omitempty"`
		LastFailure *string `json:"last_failure,omitempty"`
		NextRun     *string `json:"next_run,omitempty"`
		LockPID     *int    `json:"lock_pid,omitempty"`
	}
	out := make([]stateOut, 0, len(states))
	for _, st := range states {
		o := stateOut{JobName: st.JobName, LockPID: st.LockPID}
		if st.LastSuccess != nil {
			ts := st.LastSuccess.UTC().Format(time.RFC3339)
			o.LastSuccess = &ts
		}
		if st.LastFailure != nil {
			ts := st.LastFailure.UTC().Format(time.RFC3339)
			o.LastFailure = &ts
		}
		if st.NextRun != nil {
			ts := st.NextRun.UTC().Format(time.RFC3339)
			o.NextRun = &ts
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── Config endpoints ─────────────────────────────────────────────────────────

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ConfigPath == "" {
		writeError(w, http.StatusNotImplemented, "config path not available")
		return
	}
	data, err := os.ReadFile(s.deps.ConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": s.deps.ConfigPath,
		"yaml": string(data),
	})
}

// handleConfigDaemon serves the raw content of huskyd.yaml (read-only).
// Returns 404 when no daemon config file is present.
func (s *Server) handleConfigDaemon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.DaemonConfigPath == "" {
		writeError(w, http.StatusNotFound, "no huskyd.yaml found")
		return
	}
	data, err := os.ReadFile(s.deps.DaemonConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read huskyd.yaml: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": s.deps.DaemonConfigPath,
		"yaml": string(data),
	})
}

func (s *Server) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	var req struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := config.LoadBytes([]byte(req.YAML)); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ConfigPath == "" || s.deps.ReloadDaemon == nil {
		writeError(w, http.StatusNotImplemented, "config save not available")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	var req struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := config.LoadBytes([]byte(req.YAML)); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := os.WriteFile(s.deps.ConfigPath, []byte(req.YAML), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write config: "+err.Error())
		return
	}
	if err := s.deps.ReloadDaemon(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "saved but reload failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Integrations ─────────────────────────────────────────────────────────────

func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if s.deps.ConfigSnapshot == nil {
		writeError(w, http.StatusInternalServerError, "api dependencies not configured")
		return
	}
	cfg := s.deps.ConfigSnapshot()
	type integrationInfo struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Status   string `json:"status"`
	}
	result := make([]integrationInfo, 0, len(cfg.Integrations))
	for name, intg := range cfg.Integrations {
		if intg == nil {
			continue
		}
		provider := intg.EffectiveProvider
		if provider == "" {
			provider = intg.Provider
		}
		if provider == "" {
			provider = name
		}
		status := integrationCredStatus(provider, intg)
		result = append(result, integrationInfo{Name: name, Provider: provider, Status: status})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleIntegrationRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/integrations/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	name := parts[0]
	if len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost {
		if s.deps.ConfigSnapshot != nil {
			if _, ok := s.deps.ConfigSnapshot().Integrations[name]; !ok {
				writeError(w, http.StatusNotFound, "integration not found")
				return
			}
		}
		if s.deps.TestIntegration == nil {
			writeError(w, http.StatusNotImplemented, "integration testing not available")
			return
		}
		if err := s.deps.TestIntegration(r.Context(), name); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func integrationCredStatus(provider string, intg *config.Integration) string {
	switch provider {
	case "slack", "discord":
		if intg.WebhookURL != "" {
			return "configured"
		}
		return "missing webhook_url"
	case "pagerduty":
		if intg.RoutingKey != "" {
			return "configured"
		}
		return "missing routing_key"
	case "smtp":
		var missing []string
		if intg.Host == "" {
			missing = append(missing, "host")
		}
		if intg.From == "" {
			missing = append(missing, "from")
		}
		if len(missing) > 0 {
			return "missing " + strings.Join(missing, ", ")
		}
		return "configured"
	case "webhook":
		if intg.WebhookURL != "" {
			return "configured"
		}
		return "missing webhook_url"
	default:
		return "configured"
	}
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": msg,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Headers are already sent; errors here can only be logged, not written to client.
	_ = json.NewEncoder(w).Encode(v)
}
