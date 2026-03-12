package outputs_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/outputs"
	"github.com/husky-scheduler/husky/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustRecordOutput(t *testing.T, st *store.Store, cycleID, jobName, varName, value string) {
	t.Helper()
	err := st.RecordOutput(context.Background(), store.RunOutput{
		RunID:   1,
		JobName: jobName,
		VarName: varName,
		Value:   value,
		CycleID: cycleID,
	})
	require.NoError(t, err)
}

func TestRenderTemplates_NoTemplates_ReturnsSamePointer(t *testing.T) {
	st := openTestStore(t)
	job := &config.Job{Name: "report", Command: `echo hi`}

	got, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-1")
	require.NoError(t, err)
	assert.Same(t, job, got)
}

func TestRenderTemplates_NoTemplates_EnvPassthrough(t *testing.T) {
	st := openTestStore(t)
	job := &config.Job{
		Name:    "report",
		Command: `echo ok`,
		Env: map[string]string{
			"TOKEN": "abc",
		},
	}

	got, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-2")
	require.NoError(t, err)
	assert.Same(t, job, got)
}

func TestRenderTemplates_CommandResolved(t *testing.T) {
	st := openTestStore(t)
	mustRecordOutput(t, st, "cycle-cmd", "ingest", "file_path", "/tmp/data.csv")

	job := &config.Job{
		Name:    "transform",
		Command: `cat {{ outputs.ingest.file_path }}`,
	}

	got, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-cmd")
	require.NoError(t, err)
	assert.Equal(t, `cat /tmp/data.csv`, got.Command)
}

func TestRenderTemplates_MultipleExpressionsInCommand(t *testing.T) {
	st := openTestStore(t)
	mustRecordOutput(t, st, "cycle-multi", "a", "one", "hello")
	mustRecordOutput(t, st, "cycle-multi", "b", "two", "world")

	job := &config.Job{
		Name:    "join",
		Command: `echo {{ outputs.a.one }}-{{ outputs.b.two }}`,
	}

	got, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-multi")
	require.NoError(t, err)
	assert.Equal(t, `echo hello-world`, got.Command)
}

func TestRenderTemplates_EnvResolved(t *testing.T) {
	st := openTestStore(t)
	mustRecordOutput(t, st, "cycle-env", "build", "artifact", "release.tar.gz")

	job := &config.Job{
		Name:    "publish",
		Command: `echo publish`,
		Env: map[string]string{
			"ART": `{{ outputs.build.artifact }}`,
		},
	}

	got, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-env")
	require.NoError(t, err)
	assert.Equal(t, "release.tar.gz", got.Env["ART"])
}

func TestRenderTemplates_MissingVar_ReturnsError(t *testing.T) {
	st := openTestStore(t)
	job := &config.Job{
		Name:    "publish",
		Command: `echo {{ outputs.ingest.file_path }}`,
	}

	_, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-missing")
	require.Error(t, err)
	assert.ErrorContains(t, err, "outputs.ingest.file_path")
	assert.ErrorContains(t, err, "cycle-missing")
}

func TestRenderTemplates_EmptyCycleID_MissingVarError(t *testing.T) {
	st := openTestStore(t)
	job := &config.Job{
		Name:    "publish",
		Command: `echo {{ outputs.ingest.file_path }}`,
	}

	_, err := outputs.RenderTemplates(context.Background(), st, job, "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "outputs.ingest.file_path")
}

func TestRenderTemplates_DoesNotMutateOriginal(t *testing.T) {
	st := openTestStore(t)
	mustRecordOutput(t, st, "cycle-mut", "ingest", "file_path", "/tmp/a.csv")

	job := &config.Job{
		Name:    "transform",
		Command: `cat {{ outputs.ingest.file_path }}`,
		Env: map[string]string{
			"INPUT": `{{ outputs.ingest.file_path }}`,
		},
	}

	got, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-mut")
	require.NoError(t, err)

	assert.Equal(t, `cat {{ outputs.ingest.file_path }}`, job.Command)
	assert.Equal(t, `{{ outputs.ingest.file_path }}`, job.Env["INPUT"])
	assert.Equal(t, `cat /tmp/a.csv`, got.Command)
	assert.Equal(t, `/tmp/a.csv`, got.Env["INPUT"])
}

func TestRenderTemplates_CycleScoping_CycleANotVisibleInCycleB(t *testing.T) {
	st := openTestStore(t)
	mustRecordOutput(t, st, "cycle-a", "ingest", "file_path", "/tmp/a.csv")

	job := &config.Job{
		Name:    "transform",
		Command: `cat {{ outputs.ingest.file_path }}`,
	}

	_, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-b")
	require.Error(t, err)
	assert.ErrorContains(t, err, "outputs.ingest.file_path")
	assert.ErrorContains(t, err, "cycle-b")
}

func TestRenderTemplates_CycleScoping_IndependentValues(t *testing.T) {
	st := openTestStore(t)
	mustRecordOutput(t, st, "cycle-a", "ingest", "file_path", "/tmp/a.csv")
	mustRecordOutput(t, st, "cycle-b", "ingest", "file_path", "/tmp/b.csv")

	job := &config.Job{
		Name:    "transform",
		Command: `cat {{ outputs.ingest.file_path }}`,
	}

	gotA, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-a")
	require.NoError(t, err)
	assert.Equal(t, `cat /tmp/a.csv`, gotA.Command)

	gotB, err := outputs.RenderTemplates(context.Background(), st, job, "cycle-b")
	require.NoError(t, err)
	assert.Equal(t, `cat /tmp/b.csv`, gotB.Command)
}
