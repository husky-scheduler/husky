// Package outputs provides helpers for resolving {{ outputs.<job>.<var> }}
// template expressions inside a job's Command and Env values at dispatch time.
package outputs

import (
	"context"
	"fmt"
	"regexp"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/store"
)

// TemplateRe matches {{ outputs.<job>.<var> }} patterns.
var TemplateRe = regexp.MustCompile(`\{\{\s*outputs\.(\w[\w-]*)\.(\w[\w-]*)\s*\}\}`)

// RenderTemplates returns a shallow copy of job with every
// {{ outputs.<job_name>.<var_name> }} expression in Command and Env values
// resolved from the run_outputs table for the given cycleID.
// Returns an error if any referenced variable has no recorded value.
// Returns the original pointer unchanged when no template expressions exist.
func RenderTemplates(ctx context.Context, st *store.Store, job *config.Job, cycleID string) (*config.Job, error) {
	hasTemplates := TemplateRe.MatchString(job.Command)
	if !hasTemplates {
		for _, v := range job.Env {
			if TemplateRe.MatchString(v) {
				hasTemplates = true
				break
			}
		}
	}
	if !hasTemplates {
		return job, nil
	}

	outs, err := st.ListRunOutputs(ctx, cycleID)
	if err != nil {
		return nil, fmt.Errorf("render templates: list outputs: %w", err)
	}
	lookup := make(map[string]string, len(outs))
	for _, o := range outs {
		lookup[o.JobName+"."+o.VarName] = o.Value
	}

	replace := func(s string) (string, error) {
		var firstErr error
		result := TemplateRe.ReplaceAllStringFunc(s, func(match string) string {
			m := TemplateRe.FindStringSubmatch(match)
			if len(m) != 3 {
				return match
			}
			key := m[1] + "." + m[2]
			val, ok := lookup[key]
			if !ok {
				firstErr = fmt.Errorf("output variable outputs.%s.%s not found for cycle %q", m[1], m[2], cycleID)
				return match
			}
			return val
		})
		return result, firstErr
	}

	resolved := *job
	cmd, err := replace(job.Command)
	if err != nil {
		return nil, err
	}
	resolved.Command = cmd

	if len(job.Env) > 0 {
		resolved.Env = make(map[string]string, len(job.Env))
		for k, v := range job.Env {
			newV, err := replace(v)
			if err != nil {
				return nil, err
			}
			resolved.Env[k] = newV
		}
	}

	return &resolved, nil
}
