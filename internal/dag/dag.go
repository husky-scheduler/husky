// Package dag builds and validates the job dependency graph for Husky.
//
// It performs Kahn's topological sort on the union of explicit depends_on
// declarations and implicit after:<job> frequency edges. If a cycle is
// detected the full cycle path is returned as an error so users can fix it.
package dag

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/husky-scheduler/husky/internal/config"
)

// Edge represents a single dependency relationship.
type Edge struct {
	// Dependency is the job that must complete first.
	Dependency string `json:"dependency"`
	// Dependent is the job that waits for Dependency.
	Dependent string `json:"dependent"`
}

// Graph is the resolved, cycle-free dependency graph for all configured jobs.
type Graph struct {
	// Order lists job names in topological order (dependencies before dependents).
	Order []string
	// Edges lists every dependency relationship in the graph.
	Edges []Edge

	// deps maps job name → its direct dependency names.
	deps map[string][]string
	// successors maps job name → jobs that depend on it.
	successors map[string][]string
}

// Build constructs a Graph from cfg using Kahn's algorithm.
// It returns a descriptive error containing the full cycle path when a cycle
// is detected; callers should refuse to start the daemon in that case.
func Build(cfg *config.Config) (*Graph, error) {
	// Collect all job names (for a stable, deterministic ordering).
	names := make([]string, 0, len(cfg.Jobs))
	for n := range cfg.Jobs {
		names = append(names, n)
	}
	sort.Strings(names)

	// Build the dependency and successor adjacency maps.
	deps := make(map[string][]string, len(cfg.Jobs))
	successors := make(map[string][]string, len(cfg.Jobs))
	var edges []Edge

	for _, name := range names {
		job := cfg.Jobs[name]
		seen := map[string]bool{}
		var jobDeps []string

		// Explicit depends_on declarations.
		for _, dep := range job.DependsOn {
			if dep == "" || seen[dep] {
				continue
			}
			seen[dep] = true
			jobDeps = append(jobDeps, dep)
			edges = append(edges, Edge{Dependency: dep, Dependent: name})
			successors[dep] = append(successors[dep], name)
		}

		// after:<job> is an implicit depends_on edge.
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(job.Frequency)), "after:") {
			dep := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(job.Frequency)), "after:")
			dep = strings.TrimSpace(dep)
			if dep != "" && !seen[dep] {
				seen[dep] = true
				jobDeps = append(jobDeps, dep)
				edges = append(edges, Edge{Dependency: dep, Dependent: name})
				successors[dep] = append(successors[dep], name)
			}
		}

		deps[name] = jobDeps
	}

	// Kahn's algorithm — compute in-degrees.
	inDegree := make(map[string]int, len(names))
	for _, name := range names {
		inDegree[name] += len(deps[name])
	}

	// Seed queue with zero in-degree nodes in stable order.
	queue := make([]string, 0, len(names))
	for _, name := range names {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	order := make([]string, 0, len(names))
	for len(queue) > 0 {
		sort.Strings(queue) // deterministic: always process alphabetically
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		for _, succ := range successors[n] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}

	if len(order) != len(names) {
		cycle := findCycle(deps, names, order)
		return nil, fmt.Errorf("dag: cycle detected: %s", strings.Join(cycle, " → "))
	}

	return &Graph{
		Order:      order,
		Edges:      edges,
		deps:       deps,
		successors: successors,
	}, nil
}

// DepsOf returns the direct dependencies of jobName (jobs that must succeed first).
func (g *Graph) DepsOf(jobName string) []string {
	return g.deps[jobName]
}

// SuccessorsOf returns the jobs that directly depend on jobName.
func (g *Graph) SuccessorsOf(jobName string) []string {
	return g.successors[jobName]
}

// ASCII renders a human-readable dependency listing suitable for terminal output.
// Each line is: "jobName  ←  [dep1, dep2]" (or just "jobName" when no deps).
func (g *Graph) ASCII() string {
	var sb strings.Builder
	for _, name := range g.Order {
		d := g.deps[name]
		if len(d) == 0 {
			fmt.Fprintf(&sb, "%s\n", name)
		} else {
			sorted := make([]string, len(d))
			copy(sorted, d)
			sort.Strings(sorted)
			fmt.Fprintf(&sb, "%s  ←  [%s]\n", name, strings.Join(sorted, ", "))
		}
	}
	return sb.String()
}

// JSONNode is the per-job entry in the machine-readable DAG output.
type JSONNode struct {
	Name         string   `json:"name"`
	Dependencies []string `json:"dependencies"`
}

// JSONOutput returns the DAG as a slice of JSONNodes in topological order.
func (g *Graph) JSONOutput() []JSONNode {
	nodes := make([]JSONNode, len(g.Order))
	for i, name := range g.Order {
		d := g.deps[name]
		if d == nil {
			d = []string{}
		}
		sorted := make([]string, len(d))
		copy(sorted, d)
		sort.Strings(sorted)
		nodes[i] = JSONNode{Name: name, Dependencies: sorted}
	}
	return nodes
}

// MarshalJSON implements json.Marshaler returning the topological node list.
func (g *Graph) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.JSONOutput())
}

// ─── Cycle finder ─────────────────────────────────────────────────────────────

// findCycle returns a human-readable cycle path from the nodes that Kahn's
// algorithm failed to process (i.e. those still holding non-zero in-degrees).
func findCycle(deps map[string][]string, allNodes []string, processed []string) []string {
	processedSet := make(map[string]bool, len(processed))
	for _, n := range processed {
		processedSet[n] = true
	}

	var remaining []string
	remainingSet := make(map[string]bool)
	for _, n := range allNodes {
		if !processedSet[n] {
			remainingSet[n] = true
			remaining = append(remaining, n)
		}
	}
	if len(remaining) == 0 {
		return nil
	}
	sort.Strings(remaining)

	// DFS to find one actual cycle among remaining nodes.
	visited := map[string]bool{}
	inStack := map[string]bool{}
	path := []string{}

	var dfs func(n string) []string
	dfs = func(n string) []string {
		visited[n] = true
		inStack[n] = true
		path = append(path, n)
		for _, dep := range deps[n] {
			if !remainingSet[dep] {
				continue
			}
			if !visited[dep] {
				if cycle := dfs(dep); cycle != nil {
					return cycle
				}
			} else if inStack[dep] {
				// Found the cycle start — extract the cycle segment from path.
				for i, p := range path {
					if p == dep {
						cycle := make([]string, len(path)-i+1)
						copy(cycle, path[i:])
						cycle[len(cycle)-1] = dep
						return cycle
					}
				}
			}
		}
		path = path[:len(path)-1]
		inStack[n] = false
		return nil
	}

	for _, n := range remaining {
		if !visited[n] {
			if cycle := dfs(n); cycle != nil {
				return cycle
			}
		}
	}
	return remaining // fallback: just list cyclic nodes
}
