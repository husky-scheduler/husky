package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/ipc"
	"github.com/husky-scheduler/husky/internal/store"
)

// ──────────────────────────────────────────────────────────────────────────────
// Shared test helpers
// ──────────────────────────────────────────────────────────────────────────────

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-test.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newTestServer(t *testing.T, deps Dependencies) *httptest.Server {
	t.Helper()
	s := New("127.0.0.1:0", deps)
	return httptest.NewServer(s.http.Handler)
}

// minDeps returns a Dependencies struct pre-wired with a real store and a
// simple config containing one "ingest" job (tags: etl) and one "report" job
// (tags: bi).  Extra fields can be overridden by the caller.
func minDeps(t *testing.T) (Dependencies, *store.Store) {
	t.Helper()
	st := openTestStore(t)
	cfg := &config.Config{Jobs: map[string]*config.Job{
		"ingest": {Name: "ingest", Description: "ingest job", Frequency: "manual", Tags: []string{"etl"}},
		"report": {Name: "report", Description: "report job", Frequency: "daily", Tags: []string{"bi"}},
	}}
	deps := Dependencies{
		Store:          st,
		ConfigSnapshot: func() *config.Config { return cfg },
		Status: func() ([]ipc.JobStatus, error) {
			return []ipc.JobStatus{{Name: "ingest"}, {Name: "report"}}, nil
		},
		Trigger:   func(_, _ string) error { return nil },
		Cancel:    func(_ string) error { return nil },
		DAGJSON:   func() ([]byte, error) { return []byte(`[]`), nil },
		PauseTag:  func(_ string) (int, error) { return 1, nil },
		ResumeTag: func(_ string) (int, error) { return 1, nil },
	}
	return deps, st
}

// seedRun inserts a complete (SUCCESS) run with stdout + healthcheck logs and
// one output variable.  Returns the run ID.
func seedRun(t *testing.T, st *store.Store, job string) int64 {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName:   job,
		Status:    store.StatusRunning,
		Attempt:   1,
		Trigger:   store.TriggerManual,
		StartedAt: &now,
	})
	require.NoError(t, err)
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 0, Stream: "stdout", Line: "hello", TS: now}))
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 1, Stream: "healthcheck", Line: "hc", TS: now}))
	require.NoError(t, st.RecordOutput(ctx, store.RunOutput{RunID: runID, JobName: job, VarName: "file", Value: "/tmp/a.csv", CycleID: "cycle-1"}))
	exitCode := 0
	require.NoError(t, st.RecordRunEnd(ctx, runID, store.StatusSuccess, "", &exitCode, now.Add(time.Second), false, nil))
	return runID
}

func idStr(v int64) string { return fmt.Sprintf("%d", v) }

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/status
// ──────────────────────────────────────────────────────────────────────────────

func TestStatus_OK(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
	assert.NotEmpty(t, body["uptime"])
	assert.NotEmpty(t, body["started_at"])
}

func TestStatus_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/status", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/jobs
// ──────────────────────────────────────────────────────────────────────────────

func TestJobs_All(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	require.Len(t, rows, 2)
	// Response is sorted by name.
	assert.Equal(t, "ingest", rows[0]["name"])
	assert.Equal(t, "report", rows[1]["name"])
}

func TestJobs_TagFilter(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs?tag=etl")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "ingest", rows[0]["name"])
}

func TestJobs_TagFilter_NoMatch(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs?tag=nonexistent")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	assert.Empty(t, rows)
}

func TestJobs_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/jobs/:name
// ──────────────────────────────────────────────────────────────────────────────

func TestJobDetail_OK(t *testing.T) {
	deps, st := minDeps(t)
	seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs/ingest")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ingest", body["name"])
	runs, ok := body["runs"].([]any)
	require.True(t, ok)
	assert.Len(t, runs, 1)
}

func TestJobDetail_NotFound(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs/ghost")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestJobDetail_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/jobs/:name/run
// ──────────────────────────────────────────────────────────────────────────────

func TestJobRun_WithReason(t *testing.T) {
	deps, _ := minDeps(t)
	called := false
	gotJob, gotReason := "", ""
	deps.Trigger = func(jobName, reason string) error {
		called = true
		gotJob = jobName
		gotReason = reason
		return nil
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	body := bytes.NewBufferString(`{"reason":"deploy fix"}`)
	resp, err := http.Post(ts.URL+"/api/jobs/ingest/run", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.True(t, called)
	assert.Equal(t, "ingest", gotJob)
	assert.Equal(t, "deploy fix", gotReason)
}

func TestJobRun_NoReason(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/run", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func TestJobRun_UnknownJob_Returns404(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ghost/run", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestJobRun_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs/ingest/run")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/jobs/:name/cancel
// ──────────────────────────────────────────────────────────────────────────────

func TestJobCancel_OK(t *testing.T) {
	deps, _ := minDeps(t)
	cancelled := ""
	deps.Cancel = func(jobName string) error {
		cancelled = jobName
		return nil
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ingest", cancelled)
}

func TestJobCancel_Error(t *testing.T) {
	deps, _ := minDeps(t)
	deps.Cancel = func(_ string) error { return fmt.Errorf("not running") }
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/runs/:id
// ──────────────────────────────────────────────────────────────────────────────

func TestRunDetail_OK(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/" + idStr(runID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var run map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&run))
	assert.Equal(t, float64(runID), run["id"])
}

func TestRunDetail_NotFound(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/999999")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRunDetail_InvalidID(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/abc")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/runs/:id/logs
// ──────────────────────────────────────────────────────────────────────────────

func TestRunLogs_ExcludesHealthcheckByDefault(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/" + idStr(runID) + "/logs")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "healthcheck")
	assert.Contains(t, string(body), "stdout")
}

func TestRunLogs_IncludeHealthcheck(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/" + idStr(runID) + "/logs?include_healthcheck=true")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "healthcheck")
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/runs/:id/outputs
// ──────────────────────────────────────────────────────────────────────────────

func TestRunOutputs_OK(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/" + idStr(runID) + "/outputs")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var outputs []store.RunOutput
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&outputs))
	require.Len(t, outputs, 1)
	assert.Equal(t, "/tmp/a.csv", outputs[0].Value)
	assert.Equal(t, "file", outputs[0].VarName)
}

func TestRunOutputs_Empty(t *testing.T) {
	deps, st := minDeps(t)
	// Seed a run with no outputs.
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName: "report", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now,
	})
	require.NoError(t, err)
	exitCode := 0
	require.NoError(t, st.RecordRunEnd(ctx, runID, store.StatusSuccess, "", &exitCode, now.Add(time.Second), false, nil))

	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runs/" + idStr(runID) + "/outputs")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	// Should be JSON null or empty array — both are acceptable.
	trimmed := string(body)
	assert.True(t, trimmed == "null\n" || trimmed == "[]\n", "expected null or [] but got: %s", trimmed)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/audit
// ──────────────────────────────────────────────────────────────────────────────

func TestAudit_Basic(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/audit")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, runID, runs[0].ID)
}

func TestAudit_FilterByJob(t *testing.T) {
	deps, st := minDeps(t)
	seedRun(t, st, "ingest")
	seedRun(t, st, "report")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/audit?job=report")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, "report", runs[0].JobName)
}

func TestAudit_FilterByStatus(t *testing.T) {
	deps, st := minDeps(t)
	seedRun(t, st, "ingest") // SUCCESS
	// Seed a FAILED run for "report".
	ctx := context.Background()
	now := time.Now().UTC()
	runID2, err := st.RecordRunStart(ctx, store.Run{JobName: "report", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now})
	require.NoError(t, err)
	exitCode := 1
	require.NoError(t, st.RecordRunEnd(ctx, runID2, store.StatusFailed, "", &exitCode, now.Add(time.Second), false, nil))

	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/audit?status=failed")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, store.StatusFailed, runs[0].Status)
}

func TestAudit_FilterByTag_PreLimit(t *testing.T) {
	// The tag filter must resolve to job names and push them into the SQL query,
	// not post-filter after a LIMIT-capped result set.
	deps, st := minDeps(t)
	// Seed many runs for "report" (bi tag) to fill the default limit.
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		now := time.Now().UTC()
		runID, err := st.RecordRunStart(ctx, store.Run{
			JobName: "report", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now,
		})
		require.NoError(t, err)
		exitCode := 0
		require.NoError(t, st.RecordRunEnd(ctx, runID, store.StatusSuccess, "", &exitCode, now.Add(time.Second), false, nil))
	}
	// Seed one run for "ingest" (etl tag).
	ingestRunID := seedRun(t, st, "ingest")

	// Request with limit=5 and tag=etl — the ingest run must appear even though
	// "ingest" may not appear in the top-5 rows when ordered newest-first.
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/audit?tag=etl&limit=5")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, ingestRunID, runs[0].ID)
}

func TestAudit_InvalidSince(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/audit?since=not-a-date")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/tags
// ──────────────────────────────────────────────────────────────────────────────

func TestTags_OK(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/tags")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	type item struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	var tags []item
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tags))
	require.Len(t, tags, 2)
	// Sorted alphabetically: bi, etl.
	assert.Equal(t, "bi", tags[0].Tag)
	assert.Equal(t, 1, tags[0].Count)
	assert.Equal(t, "etl", tags[1].Tag)
	assert.Equal(t, 1, tags[1].Count)
}

func TestTags_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/tags", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/dag
// ──────────────────────────────────────────────────────────────────────────────

func TestDAG_OK(t *testing.T) {
	deps, _ := minDeps(t)
	deps.DAGJSON = func() ([]byte, error) { return []byte(`[{"name":"ingest","deps":[]}]`), nil }
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dag")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "ingest")
}

func TestDAG_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/dag", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/jobs/pause  &  POST /api/jobs/resume
// ──────────────────────────────────────────────────────────────────────────────

func TestPauseByTag_OK(t *testing.T) {
	deps, _ := minDeps(t)
	pausedTag := ""
	deps.PauseTag = func(tag string) (int, error) {
		pausedTag = tag
		return 2, nil
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/pause?tag=etl", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "etl", pausedTag)
}

func TestPauseByTag_MissingTag(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/pause", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestResumeByTag_OK(t *testing.T) {
	deps, _ := minDeps(t)
	resumedTag := ""
	deps.ResumeTag = func(tag string) (int, error) {
		resumedTag = tag
		return 2, nil
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/resume?tag=etl", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "etl", resumedTag)
}

// ──────────────────────────────────────────────────────────────────────────────
// Legacy combined test (retained for regression)
// ──────────────────────────────────────────────────────────────────────────────

func TestRunLogs_Outputs_AndAudit(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	// Logs (healthcheck excluded by default).
	respLogs, err := http.Get(ts.URL + "/api/runs/" + idStr(runID) + "/logs")
	require.NoError(t, err)
	defer respLogs.Body.Close()
	require.Equal(t, http.StatusOK, respLogs.StatusCode)
	logBody, err := io.ReadAll(respLogs.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(logBody), "healthcheck")

	// Outputs.
	respOutputs, err := http.Get(ts.URL + "/api/runs/" + idStr(runID) + "/outputs")
	require.NoError(t, err)
	defer respOutputs.Body.Close()
	require.Equal(t, http.StatusOK, respOutputs.StatusCode)
	var outputs []store.RunOutput
	require.NoError(t, json.NewDecoder(respOutputs.Body).Decode(&outputs))
	require.Len(t, outputs, 1)
	assert.Equal(t, "/tmp/a.csv", outputs[0].Value)

	// Audit with multiple filters.
	respAudit, err := http.Get(ts.URL + "/api/audit?job=ingest&status=success&tag=etl")
	require.NoError(t, err)
	defer respAudit.Body.Close()
	require.Equal(t, http.StatusOK, respAudit.StatusCode)
	var runs []store.Run
	require.NoError(t, json.NewDecoder(respAudit.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, runID, runs[0].ID)
}

// ──────────────────────────────────────────────────────────────────────────────
// T2 — Integration: REST trigger → WebSocket stream → terminal state
// ──────────────────────────────────────────────────────────────────────────────

// TestWSLogs_TerminatesOnRunCompletion seeds a RUNNING run, subscribes via
// WebSocket, completes the run in a background goroutine, and verifies the
// client receives a terminal {"type":"end","status":"SUCCESS"} frame.
func TestWSLogs_TerminatesOnRunCompletion(t *testing.T) {
	deps, st := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// 1. Seed a RUNNING run.
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName:   "ingest",
		Status:    store.StatusRunning,
		Attempt:   1,
		Trigger:   store.TriggerManual,
		StartedAt: &now,
	})
	require.NoError(t, err)
	require.NoError(t, st.RecordLog(ctx, store.LogLine{
		RunID: runID, Seq: 0, Stream: "stdout", Line: "starting…", TS: now,
	}))

	// 2. Dial WebSocket.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/logs/" + idStr(runID)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 3. Transition run to SUCCESS after a short delay.
	go func() {
		time.Sleep(60 * time.Millisecond)
		exitCode := 0
		_ = st.RecordRunEnd(ctx, runID, store.StatusSuccess, "", &exitCode, time.Now().UTC(), false, nil)
	}()

	// 4. Read messages until the end frame (or 3 s deadline).
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	var finalStatus string
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			// Connection closed cleanly after end frame — expected.
			break
		}
		if msg["type"] == "end" {
			if s, ok := msg["status"].(string); ok {
				finalStatus = s
			}
			break
		}
	}

	// 5. Assert terminal state was signalled.
	assert.Equal(t, "SUCCESS", finalStatus, "WebSocket must relay the terminal run status")
}

// TestWSLogs_BackfillsExistingLines verifies that existing log lines are sent
// to a late-joining WebSocket subscriber before streaming new lines.
func TestWSLogs_BackfillsExistingLines(t *testing.T) {
	deps, st := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed a completed run with two log lines.
	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName:   "ingest",
		Status:    store.StatusRunning,
		Attempt:   1,
		Trigger:   store.TriggerManual,
		StartedAt: &now,
	})
	require.NoError(t, err)
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 0, Stream: "stdout", Line: "line 1", TS: now}))
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 1, Stream: "stdout", Line: "line 2", TS: now}))
	exitCode := 0
	require.NoError(t, st.RecordRunEnd(ctx, runID, store.StatusSuccess, "", &exitCode, now.Add(time.Second), false, nil))

	// Dial WebSocket after the run is already complete.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/logs/" + idStr(runID)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	textLines := 0
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		if msg["type"] == "log" {
			textLines++
		}
		if msg["type"] == "end" {
			break
		}
	}
	assert.Equal(t, 2, textLines, "backfill must deliver both pre-existing log lines")
}

func TestWSLogs_TerminatesOnRetryingRun(t *testing.T) {
	deps, st := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	runID, err := st.RecordRunStart(ctx, store.Run{
		JobName:   "ingest",
		Status:    store.StatusRunning,
		Attempt:   1,
		Trigger:   store.TriggerManual,
		StartedAt: &now,
	})
	require.NoError(t, err)
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 0, Stream: "stdout", Line: "attempt 1 failed", TS: now}))

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/logs/" + idStr(runID)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = st.MarkRunStatus(ctx, runID, store.StatusRetrying)
	}()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	var finalStatus string
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		if msg["type"] == "end" {
			if s, ok := msg["status"].(string); ok {
				finalStatus = s
			}
			break
		}
	}

	assert.Equal(t, "RETRYING", finalStatus, "WebSocket must terminate once a run enters RETRYING")
}

// ──────────────────────────────────────────────────────────────────────────────
// Shared helper: seedAlert
// ──────────────────────────────────────────────────────────────────────────────

// seedAlert inserts one alert row with the given status and returns its ID.
func seedAlert(t *testing.T, st *store.Store, job, status string) int64 {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.RecordAlert(ctx, store.Alert{
		JobName: job,
		Event:   "on_failure",
		Channel: "slack:#test",
		Status:  status,
		Payload: "{}",
		SentAt:  time.Now().UTC(),
	}))
	alerts, err := st.ListAlertsPaginated(ctx, job, "", 1, 0)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
	return alerts[0].ID
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/daemon/info
// ──────────────────────────────────────────────────────────────────────────────

func TestDaemonInfo_Basic(t *testing.T) {
	deps, _ := minDeps(t)
	deps.Version = "1.2.3"
	deps.PID = 99
	deps.ConfigPath = "/etc/husky.yaml"
	deps.DBPath = "/var/husky.db"
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/daemon/info")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "1.2.3", body["version"])
	assert.Equal(t, float64(99), body["pid"])
	assert.Equal(t, "/etc/husky.yaml", body["config_path"])
	assert.Equal(t, "/var/husky.db", body["db_path"])
	assert.NotEmpty(t, body["uptime"])
	assert.NotEmpty(t, body["started_at"])
	// Status func is set in minDeps → counts should be present.
	assert.Equal(t, float64(2), body["total_job_count"])
}

func TestDaemonInfo_MethodNotAllowed(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/daemon/info", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/db/job_runs
// ──────────────────────────────────────────────────────────────────────────────

func TestDBJobRuns_All(t *testing.T) {
	deps, st := minDeps(t)
	_ = seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/job_runs")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, "ingest", runs[0].JobName)
}

func TestDBJobRuns_FilterByJob(t *testing.T) {
	deps, st := minDeps(t)
	_ = seedRun(t, st, "ingest")
	_ = seedRun(t, st, "report")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/job_runs?job=report")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, "report", runs[0].JobName)
}

func TestDBJobRuns_Pagination(t *testing.T) {
	deps, st := minDeps(t)
	for i := 0; i < 3; i++ {
		_ = seedRun(t, st, "ingest")
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/job_runs?limit=2&offset=0")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	assert.Len(t, runs, 2)
}

func TestDBJobRuns_SLABreachedFilter(t *testing.T) {
	deps, st := minDeps(t)
	id1 := seedRun(t, st, "ingest")
	_ = seedRun(t, st, "ingest")
	require.NoError(t, st.MarkRunSLABreached(context.Background(), id1))
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/job_runs?sla_breached=true")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, id1, runs[0].ID)
	assert.True(t, runs[0].SLABreached)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/db/job_state/:jobName/clear_lock
// ──────────────────────────────────────────────────────────────────────────────

func TestClearLock_WhenIdle(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/db/job_state/ingest/clear_lock", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestClearLock_WhenRunning_Returns409(t *testing.T) {
	deps, st := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	// Seed a RUNNING run without ending it so ClearJobLock blocks.
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := st.RecordRunStart(ctx, store.Run{
		JobName:   "ingest",
		Status:    store.StatusRunning,
		Attempt:   1,
		Trigger:   store.TriggerManual,
		StartedAt: &now,
	})
	require.NoError(t, err)

	resp, err := http.Post(ts.URL+"/api/db/job_state/ingest/clear_lock", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/db/run_logs
// ──────────────────────────────────────────────────────────────────────────────

func TestDBRunLogs_ByRunID(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/run_logs?run_id=" + idStr(runID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var lines []store.LogLine
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&lines))
	// seedRun inserts stdout + healthcheck = 2 lines.
	assert.Len(t, lines, 2)
}

func TestDBRunLogs_StreamFilter(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/run_logs?run_id=" + idStr(runID) + "&stream=healthcheck")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var lines []store.LogLine
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&lines))
	require.Len(t, lines, 1)
	assert.Equal(t, "healthcheck", lines[0].Stream)
}

func TestDBRunLogs_KeywordSearch(t *testing.T) {
	deps, st := minDeps(t)
	runID := seedRun(t, st, "ingest")
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/db/run_logs?run_id=" + idStr(runID) + "&q=hello")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var lines []store.LogLine
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&lines))
	// Only the stdout line contains "hello".
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0].Line, "hello")
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/db/alerts/:id/retry
// ──────────────────────────────────────────────────────────────────────────────

func TestAlertRetry_Success(t *testing.T) {
	deps, st := minDeps(t)
	alertID := seedAlert(t, st, "ingest", "failed")
	ts := newTestServer(t, deps)
	defer ts.Close()

	url := fmt.Sprintf("%s/api/db/alerts/%d/retry", ts.URL, alertID)
	resp, err := http.Post(url, "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestAlertRetry_AlreadyPending(t *testing.T) {
	deps, st := minDeps(t)
	alertID := seedAlert(t, st, "ingest", "pending")
	ts := newTestServer(t, deps)
	defer ts.Close()

	url := fmt.Sprintf("%s/api/db/alerts/%d/retry", ts.URL, alertID)
	resp, err := http.Post(url, "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestAlertRetry_NotFound(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/db/alerts/99999/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/jobs/:name/pause  and  /resume
// ──────────────────────────────────────────────────────────────────────────────

func TestJobPause_OK(t *testing.T) {
	deps, _ := minDeps(t)
	deps.PauseJob = func(_ string) error { return nil }
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/pause", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestJobPause_NotImplemented(t *testing.T) {
	deps, _ := minDeps(t)
	// PauseJob intentionally left nil.
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/pause", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}

func TestJobResume_OK(t *testing.T) {
	deps, _ := minDeps(t)
	deps.ResumeJob = func(_ string) error { return nil }
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/resume", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestJobResume_NotImplemented(t *testing.T) {
	deps, _ := minDeps(t)
	// ResumeJob intentionally left nil.
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/resume", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/jobs/:name/retry
// ──────────────────────────────────────────────────────────────────────────────

func TestJobRetry_OK(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestJobRetry_UnknownJob(t *testing.T) {
	deps, _ := minDeps(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/does-not-exist/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/jobs/:name/skip
// ──────────────────────────────────────────────────────────────────────────────

func TestJobSkip_OK(t *testing.T) {
	deps, _ := minDeps(t)
	deps.SkipJob = func(_ string) error { return nil }
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/skip", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestJobSkip_NotImplemented(t *testing.T) {
	deps, _ := minDeps(t)
	// SkipJob intentionally left nil.
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/jobs/ingest/skip", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/integrations  and  POST /api/integrations/:name/test
// ──────────────────────────────────────────────────────────────────────────────

func TestIntegrations_List(t *testing.T) {
	deps, _ := minDeps(t)
	deps.ConfigSnapshot = func() *config.Config {
		return &config.Config{
			Jobs: map[string]*config.Job{
				"ingest": {Name: "ingest", Frequency: "manual"},
			},
			Integrations: map[string]*config.Integration{
				"slack": {Provider: "slack", WebhookURL: "https://hooks.slack.com/x"},
				"email": {Provider: "smtp", Host: "mail.example.com"},
			},
		}
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/integrations")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var items []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&items))
	require.Len(t, items, 2)
	// Results are sorted by name.
	assert.Equal(t, "email", items[0]["name"])
	assert.Equal(t, "slack", items[1]["name"])
	assert.Equal(t, "configured", items[1]["status"])
}

func TestIntegrations_TestDelivery_OK(t *testing.T) {
	deps, _ := minDeps(t)
	deps.ConfigSnapshot = func() *config.Config {
		return &config.Config{
			Jobs: map[string]*config.Job{},
			Integrations: map[string]*config.Integration{
				"slack": {Provider: "slack", WebhookURL: "https://hooks.slack.com/x"},
			},
		}
	}
	deps.TestIntegration = func(_ context.Context, _ string) error { return nil }
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/integrations/slack/test", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestIntegrations_TestDelivery_Unknown(t *testing.T) {
	deps, _ := minDeps(t)
	deps.TestIntegration = func(_ context.Context, _ string) error { return nil }
	ts := newTestServer(t, deps)
	defer ts.Close()

	// "nothere" is not in the minDeps config, so handler returns 404.
	resp, err := http.Post(ts.URL+"/api/integrations/nothere/test", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
