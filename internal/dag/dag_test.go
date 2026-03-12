package dag_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/dag"
)

func cfg(jobs map[string][]string) *config.Config {
	c := &config.Config{Jobs: make(map[string]*config.Job, len(jobs))}
	for name, deps := range jobs {
		c.Jobs[name] = &config.Job{
			Name: name, Command: "echo " + name, Frequency: "manual", DependsOn: deps,
		}
	}
	return c
}

func TestBuild_NoDeps(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": nil, "c": nil}))
	require.NoError(t, err)
	assert.Len(t, g.Order, 3)
	assert.Empty(t, g.Edges)
}

func TestBuild_LinearChain(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}}))
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, g.Order)
	assert.Len(t, g.Edges, 2)
}

func TestBuild_FanOut(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": {"a"}, "c": {"a"}}))
	require.NoError(t, err)
	assert.Equal(t, "a", g.Order[0])
	assert.Len(t, g.Edges, 2)
}

func TestBuild_FanIn(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": nil, "c": {"a", "b"}}))
	require.NoError(t, err)
	assert.Equal(t, "c", g.Order[len(g.Order)-1])
	assert.Len(t, g.Edges, 2)
}

func TestBuild_CycleDetected(t *testing.T) {
	_, err := dag.Build(cfg(map[string][]string{"a": {"c"}, "b": {"a"}, "c": {"b"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle detected")
}

func TestBuild_SelfLoop(t *testing.T) {
	_, err := dag.Build(cfg(map[string][]string{"a": {"a"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle detected")
}

func TestBuild_AfterEdge(t *testing.T) {
	c := &config.Config{
		Jobs: map[string]*config.Job{
			"a": {Name: "a", Command: "echo a", Frequency: "manual"},
			"b": {Name: "b", Command: "echo b", Frequency: "after:a"},
		},
	}
	g, err := dag.Build(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, g.Order)
	require.Len(t, g.Edges, 1)
	assert.Equal(t, "a", g.Edges[0].Dependency)
	assert.Equal(t, "b", g.Edges[0].Dependent)
}

func TestDepsOf(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": {"a"}}))
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, g.DepsOf("b"))
	assert.Empty(t, g.DepsOf("a"))
}

func TestSuccessorsOf(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": {"a"}, "c": {"a"}}))
	require.NoError(t, err)
	succs := g.SuccessorsOf("a")
	assert.Len(t, succs, 2)
	assert.Contains(t, succs, "b")
	assert.Contains(t, succs, "c")
	assert.Empty(t, g.SuccessorsOf("b"))
}

func TestASCII_NoEdges(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil}))
	require.NoError(t, err)
	assert.Equal(t, "a\n", g.ASCII())
}

func TestASCII_WithDeps(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": {"a"}}))
	require.NoError(t, err)
	assert.Contains(t, g.ASCII(), "[a]")
}

func TestJSONOutput_Order(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil, "b": {"a"}}))
	require.NoError(t, err)
	nodes := g.JSONOutput()
	require.Len(t, nodes, 2)
	assert.Equal(t, "a", nodes[0].Name)
	assert.Empty(t, nodes[0].Dependencies)
	assert.Equal(t, "b", nodes[1].Name)
	assert.Equal(t, []string{"a"}, nodes[1].Dependencies)
}

func TestJSONOutput_EmptyDepsNotNil(t *testing.T) {
	g, err := dag.Build(cfg(map[string][]string{"a": nil}))
	require.NoError(t, err)
	nodes := g.JSONOutput()
	require.Len(t, nodes, 1)
	assert.NotNil(t, nodes[0].Dependencies)
}

func TestBuild_DeterministicOrder(t *testing.T) {
	specs := map[string][]string{"z": nil, "y": {"z"}, "x": {"y"}, "w": nil}
	first, err := dag.Build(cfg(specs))
	require.NoError(t, err)
	for i := 0; i < 9; i++ {
		g, err := dag.Build(cfg(specs))
		require.NoError(t, err)
		assert.Equal(t, first.Order, g.Order, "iteration %d", i)
	}
}

func TestCycleErrorContainsInfo(t *testing.T) {
	_, err := dag.Build(cfg(map[string][]string{"a": {"b"}, "b": {"a"}}))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "cycle"),
		"cycle error should mention cycle: %s", err.Error())
}
