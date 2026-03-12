package tests_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/api"
	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/ipc"
	"github.com/husky-scheduler/husky/internal/notify"
	"github.com/husky-scheduler/husky/internal/outputs"
	"github.com/husky-scheduler/husky/internal/scheduler"
	"github.com/husky-scheduler/husky/internal/store"
)

func openPhase4Store(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "phase4.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func startPhase4APIServer(t *testing.T, deps api.Dependencies) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := api.New(ln.Addr().String(), deps)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx, ln) }()
	return "http://" + ln.Addr().String(), func() {
		cancel()
		_ = ln.Close()
	}
}

func TestIntegration_OutputPassing_CycleIsolation(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`
version: "1"
jobs:
  consumer:
    description: "Consumes producer output"
    frequency: manual
    command: "echo {{ outputs.producer.result }}"
`))
	require.NoError(t, err)

	st := openPhase4Store(t)
	require.NoError(t, st.RecordOutput(context.Background(), store.RunOutput{
		RunID:   1,
		JobName: "producer",
		VarName: "result",
		Value:   "cycle-a-value",
		CycleID: "cycle-a",
	}))
	require.NoError(t, st.RecordOutput(context.Background(), store.RunOutput{
		RunID:   2,
		JobName: "producer",
		VarName: "result",
		Value:   "cycle-b-value",
		CycleID: "cycle-b",
	}))

	resolvedA, err := outputs.RenderTemplates(context.Background(), st, cfg.Jobs["consumer"], "cycle-a")
	require.NoError(t, err)
	assert.Equal(t, "echo cycle-a-value", resolvedA.Command)

	resolvedB, err := outputs.RenderTemplates(context.Background(), st, cfg.Jobs["consumer"], "cycle-b")
	require.NoError(t, err)
	assert.Equal(t, "echo cycle-b-value", resolvedB.Command)

	_, err = outputs.RenderTemplates(context.Background(), st, cfg.Jobs["consumer"], "cycle-c")
	require.Error(t, err)
	assert.ErrorContains(t, err, "outputs.producer.result")
}

func TestIntegration_CLIValidate_CatchesInvalidFieldCombinations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "husky.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: "1"
jobs:
  broken:
    description: "Invalid SLA"
    frequency: manual
    command: "echo nope"
    timeout: "1m"
    sla: "2m"
`), 0o644))

	root, err := filepath.Abs("..")
	require.NoError(t, err)
	cmd := exec.Command("go", "run", "./cmd/husky", "validate", "--config", path)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), "sla")
	assert.Contains(t, string(out), "timeout")
}

func TestIntegration_RunReason_AuditFilter_AndNotificationTemplate(t *testing.T) {
	var webhookBody string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		webhookBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	cfg, err := config.LoadBytes([]byte(`
version: "1"
jobs:
  deploy:
    description: "Deploy app"
    frequency: manual
    command: "echo deploy"
    tags: ["release"]
    notify:
      on_success:
        channel: "webhook:` + webhook.URL + `"
        message: "reason={{ run.reason }} job={{ job.name }}"
`))
	require.NoError(t, err)

	st := openPhase4Store(t)
	started := time.Now().UTC().Add(-time.Second)
	runID, err := st.RecordRunStart(context.Background(), store.Run{
		JobName:     "deploy",
		Status:      store.StatusRunning,
		Attempt:     1,
		Trigger:     store.TriggerManual,
		TriggeredBy: "cli: deploy hotfix",
		Reason:      "deploy hotfix",
		StartedAt:   &started,
	})
	require.NoError(t, err)
	code := 0
	finished := time.Now().UTC()
	require.NoError(t, st.RecordRunEnd(context.Background(), runID, store.StatusSuccess, "", &code, finished, false, nil))
	run, err := st.GetRun(context.Background(), runID)
	require.NoError(t, err)
	require.NotNil(t, run)

	d := notify.New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, d.Dispatch(context.Background(), cfg, "deploy", cfg.Jobs["deploy"], run, notify.EventSuccess))
	require.Contains(t, webhookBody, "deploy hotfix")
	require.Contains(t, webhookBody, "deploy")

	deps := api.Dependencies{
		Store:          st,
		ConfigSnapshot: func() *config.Config { return cfg },
		Status:         func() ([]ipc.JobStatus, error) { return []ipc.JobStatus{{Name: "deploy"}}, nil },
		Trigger:        func(_, _ string) error { return nil },
		Cancel:         func(_ string) error { return nil },
		DAGJSON:        func() ([]byte, error) { return []byte(`[]`), nil },
		StartedAt:      time.Now(),
	}
	baseURL, cleanup := startPhase4APIServer(t, deps)
	defer cleanup()

	resp, err := http.Get(baseURL + "/api/audit?reason=deploy%20hotfix")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []store.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 1)
	assert.Equal(t, "deploy hotfix", runs[0].Reason)
}

func TestIntegration_Timezone_DSTTransitions(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	job := &config.Job{
		Name:        "ny-daily",
		Description: "DST job",
		Frequency:   "daily",
		Time:        "0230",
		Timezone:    "America/New_York",
	}

	beforeSpringForward := time.Date(2026, 3, 8, 1, 55, 0, 0, loc)
	nextGap, anomalyGap := scheduler.NextRunTime(job, config.Defaults{}, beforeSpringForward)
	require.NotNil(t, anomalyGap)
	assert.Equal(t, "gap", anomalyGap.Kind)
	assert.Equal(t, 3, nextGap.In(loc).Hour())
	assert.Equal(t, 30, nextGap.In(loc).Minute())

	job.Time = "0130"
	beforeFallBack := time.Date(2026, 11, 1, 0, 45, 0, 0, loc)
	nextOverlap, anomalyOverlap := scheduler.NextRunTime(job, config.Defaults{}, beforeFallBack)
	require.NotNil(t, anomalyOverlap)
	assert.Equal(t, "overlap", anomalyOverlap.Kind)
	assert.Equal(t, 1, nextOverlap.In(loc).Hour())
	assert.Equal(t, 30, nextOverlap.In(loc).Minute())
	assert.True(t, strings.Contains(nextOverlap.In(loc).Format(time.RFC3339), "-04:00") || strings.Contains(nextOverlap.In(loc).Format(time.RFC3339), "-05:00"))
}
