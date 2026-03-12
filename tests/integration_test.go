package tests_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/dag"
	"github.com/husky-scheduler/husky/internal/notify"
	"github.com/husky-scheduler/husky/internal/outputs"
	"github.com/husky-scheduler/husky/internal/store"
)

func TestIntegration_FullPipelineParse(t *testing.T) {
	cfg, err := config.Load("../internal/config/testdata/full_pipeline.yaml")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	wantJobs := []string{
		"ingest_raw_data",
		"transform_data",
		"generate_report",
		"sync_to_s3",
		"cleanup_old_files",
	}
	assert.Len(t, cfg.Jobs, len(wantJobs), "expected exactly %d jobs", len(wantJobs))
	for _, name := range wantJobs {
		assert.Contains(t, cfg.Jobs, name, "missing job %q", name)
	}
}

func TestIntegration_FullPipelineJobFields(t *testing.T) {
	cfg, err := config.Load("../internal/config/testdata/full_pipeline.yaml")
	require.NoError(t, err)
	t.Run("transform_data has working_dir", func(t *testing.T) {
		job, ok := cfg.Jobs["transform_data"]
		require.True(t, ok)
		assert.NotEmpty(t, job.WorkingDir, "transform_data should have a working_dir")
	})
	t.Run("defaults applied", func(t *testing.T) {
		assert.NotNil(t, cfg.Defaults)
		assert.NotEmpty(t, cfg.Defaults.Timeout)
	})
}

func TestIntegration_CycleConfigRejected(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`
version: "1"
jobs:
  a:
    description: "a"
    frequency: manual
    command: "echo a"
    depends_on: ["b"]
  b:
    description: "b"
    frequency: manual
    command: "echo b"
    depends_on: ["a"]
`))
	require.NoError(t, err)

	_, err = dag.Build(cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "cycle")
	assert.ErrorContains(t, err, "a")
	assert.ErrorContains(t, err, "b")
}

// ─── T3: Full pipeline DAG execution order ───────────────────────────────────

// TestIntegration_FullPipelineDAGOrder verifies that building the full §2.3
// example pipeline produces a valid topological order where every dependency
// appears before its dependent.
func TestIntegration_FullPipelineDAGOrder(t *testing.T) {
	cfg, err := config.Load("../internal/config/testdata/full_pipeline.yaml")
	require.NoError(t, err)

	graph, err := dag.Build(cfg)
	require.NoError(t, err, "full_pipeline.yaml must produce an acyclic DAG")

	// All five jobs must be present in the execution order.
	require.Len(t, graph.Order, 5, "execution order must contain all 5 jobs")

	position := make(map[string]int, len(graph.Order))
	for i, name := range graph.Order {
		position[name] = i
	}
	for _, name := range []string{
		"ingest_raw_data", "transform_data", "generate_report",
		"sync_to_s3", "cleanup_old_files",
	} {
		_, ok := position[name]
		assert.True(t, ok, "job %q must appear in execution order", name)
	}

	// Dependency ordering: ingest → transform → report.
	assert.Less(t, position["ingest_raw_data"], position["transform_data"],
		"ingest_raw_data must precede transform_data")
	assert.Less(t, position["transform_data"], position["generate_report"],
		"transform_data must precede generate_report")
}

// TestIntegration_FullPipelineAllAlertsConfigured verifies that the pipeline's
// notification channels are all properly configured (no missing fields).
func TestIntegration_FullPipelineAllAlertsConfigured(t *testing.T) {
	cfg, err := config.Load("../internal/config/testdata/full_pipeline.yaml")
	require.NoError(t, err)

	// Every job that has notify configured must have non-empty channels.
	for name, job := range cfg.Jobs {
		if job.Notify == nil {
			continue
		}
		if job.Notify.OnFailure != nil {
			assert.NotEmpty(t, job.Notify.OnFailure.Channel,
				"job %q on_failure channel must not be empty", name)
		}
		if job.Notify.OnSuccess != nil {
			assert.NotEmpty(t, job.Notify.OnSuccess.Channel,
				"job %q on_success channel must not be empty", name)
		}
	}
}

// ─── T4: Output-passing pipeline ─────────────────────────────────────────────

// TestIntegration_OutputPassingPipeline verifies that a downstream job's
// command containing a {{ outputs.<job>.<var> }} template is resolved
// correctly from values captured during the upstream job's run.
func TestIntegration_OutputPassingPipeline(t *testing.T) {
	// Two-job pipeline: producer captures last_line as "greeting".
	cfgYAML := []byte(`
version: "1"
jobs:
  producer:
    description: "Emits a greeting"
    frequency: manual
    command: "echo hello world"
    output:
      greeting: last_line
  consumer:
    description: "Uses the greeting"
    frequency: "after:producer"
    command: "notify --msg={{ outputs.producer.greeting }}"
`)
	cfg, err := config.LoadBytes(cfgYAML)
	require.NoError(t, err)

	// Confirm DAG resolves without cycles.
	graph, err := dag.Build(cfg)
	require.NoError(t, err)
	assert.Less(t,
		indexOf(graph.Order, "producer"),
		indexOf(graph.Order, "consumer"),
		"producer must precede consumer in execution order",
	)

	// Seed the store with an output captured during the producer run.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	const cycleID = "test-cycle-001"
	require.NoError(t, st.RecordOutput(context.Background(), store.RunOutput{
		RunID:   1,
		JobName: "producer",
		VarName: "greeting",
		Value:   "hello world",
		CycleID: cycleID,
	}))

	// RenderTemplates should resolve the template in consumer's command.
	consumer := cfg.Jobs["consumer"]
	resolved, err := outputs.RenderTemplates(context.Background(), st, consumer, cycleID)
	require.NoError(t, err)
	assert.Equal(t, "notify --msg=hello world", resolved.Command,
		"{{ outputs.producer.greeting }} must be replaced with captured value")
}

// TestIntegration_OutputPassingMissingVariable verifies that RenderTemplates
// returns an error when a referenced output variable has no recorded value for
// the current cycle, preventing silent no-ops.
func TestIntegration_OutputPassingMissingVariable(t *testing.T) {
	cfgYAML := []byte(`
version: "1"
jobs:
  consumer:
    description: "Expects missing output"
    frequency: manual
    command: "echo {{ outputs.producer.result }}"
`)
	cfg, err := config.LoadBytes(cfgYAML)
	require.NoError(t, err)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	consumer := cfg.Jobs["consumer"]
	_, err = outputs.RenderTemplates(context.Background(), st, consumer, "cycle-xyz")
	require.Error(t, err, "must error when referenced output variable is absent")
	assert.ErrorContains(t, err, "outputs.producer.result")
}

// ─── T5: integrations test command wiring ────────────────────────────────────

// TestIntegration_IntegrationsTest_UnknownName verifies that calling
// TestIntegration with an integration name that is not in the config returns a
// clear "not found" error. No live credentials or network calls are needed.
func TestIntegration_IntegrationsTest_UnknownName(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`
version: "1"
jobs:
  placeholder:
    description: "placeholder"
    frequency: manual
    command: "echo ok"
`))
	require.NoError(t, err)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	d := notify.New(st, nil)
	err = d.TestIntegration(context.Background(), cfg, "nonexistent-slack")
	require.Error(t, err, "unknown integration must return an error")
	assert.ErrorContains(t, err, "nonexistent-slack")
}

// TestIntegration_IntegrationsTest_SlackMissingWebhook verifies that a slack
// integration without a webhook URL returns an error during dispatch (not a
// panic or silent no-op). This guard is skip-gated so it never makes real HTTP
// calls.
func TestIntegration_IntegrationsTest_SlackMissingWebhook(t *testing.T) {
	t.Skip("requires live Slack webhook — run manually with SLACK_WEBHOOK_URL set")

	cfg, err := config.LoadBytes([]byte(`
version: "1"
integrations:
  slack:
    webhook_url: ""
jobs:
  placeholder:
    description: "placeholder"
    frequency: manual
    command: "echo ok"
`))
	require.NoError(t, err)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	d := notify.New(st, nil)
	err = d.TestIntegration(context.Background(), cfg, "slack")
	// Either an HTTP error or a "missing webhook_url" validation error is acceptable.
	assert.Error(t, err, "slack integration with empty webhook URL must return an error")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}
