// Package config handles loading, parsing, and validating husky.yaml.
//
// # Overview
//
// The primary entry point is [Load], which:
//  1. Reads and unmarshals the YAML file
//  2. Runs JSON Schema structural validation
//  3. Runs semantic validation (enum values, time format, field co-dependencies)
//  4. Applies [Defaults] to every job that does not override a field
//  5. Interpolates ${env:HOST_VAR} references in all env maps
//
// All validation errors are collected and returned together as a [ParseError]
// so that every problem surfaces in a single pass.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

//go:embed schema/husky.schema.json
var schemaJSON string

// schemaURL is the synthetic URI used to register the embedded schema.
const schemaURL = "https://github.com/husky-scheduler/husky/schema/husky.schema.json"

// compiledSchema holds the compiled JSON Schema, initialised once at package
// load time (see init). Panics on invalid embedded schema (programmer error).
var compiledSchema *jsonschema.Schema

func init() {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	if err := compiler.AddResource(schemaURL, strings.NewReader(schemaJSON)); err != nil {
		panic(fmt.Sprintf("config: failed to add embedded JSON Schema resource: %v", err))
	}
	var err error
	compiledSchema, err = compiler.Compile(schemaURL)
	if err != nil {
		panic(fmt.Sprintf("config: failed to compile embedded JSON Schema: %v", err))
	}
}

// Load reads path, validates it, applies defaults, and interpolates env vars.
// It returns a fully-resolved *Config ready for use by the scheduler, or a
// *ParseError describing every validation failure found.
//
// Before reading path, Load attempts to source a .env file from the same
// directory. Variables already present in the process environment take
// precedence over .env values (non-destructive loading).
func Load(path string) (*Config, error) {
	// Source .env from the same directory as the config file. Errors are
	// intentionally non-fatal — a missing .env is perfectly valid.
	if dir := filepath.Dir(path); dir != "" {
		_ = LoadDotEnv(dir) // best-effort; callers can read .env explicitly if needed
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return LoadBytes(data)
}

// LoadBytes parses and validates a husky.yaml document from raw bytes.
// This is the testable core of Load.
func LoadBytes(data []byte) (*Config, error) {
	// ── 1. Unmarshal to generic map for JSON Schema validation ────────────

	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml parse error: %w", err)
	}

	// JSON Schema requires a non-nil document.
	if raw == nil {
		return nil, &ParseError{Errors: []*ValidationError{
			{Field: "(document)", Msg: "file is empty or contains only comments"},
		}}
	}

	// ── 2. JSON Schema structural validation ─────────────────────────────

	if err := compiledSchema.Validate(raw); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return nil, schemaErrorsToParseError(ve)
		}
		return nil, fmt.Errorf("schema validation: %w", err)
	}

	// ── 3. Unmarshal to typed Config ──────────────────────────────────────

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}

	// Populate Job.Name from map key.
	for name, job := range cfg.Jobs {
		job.Name = name
	}

	// ── 4. Semantic validation ────────────────────────────────────────────

	errs := &ParseError{}
	validateConfig(&cfg, errs)
	if errs.hasErrors() {
		return nil, errs
	}

	// ── 5. Apply defaults ─────────────────────────────────────────────────

	applyDefaults(&cfg)

	// ── 6. Interpolate env vars ───────────────────────────────────────────

	interpolateEnv(&cfg)

	return &cfg, nil
}

// schemaErrorsToParseError converts a jsonschema.ValidationError tree into a
// flat *ParseError with normalised job/field names.
func schemaErrorsToParseError(ve *jsonschema.ValidationError) *ParseError {
	pe := &ParseError{}
	collectSchemaErrors(ve, pe)
	return pe
}

// collectSchemaErrors walks the jsonschema error tree and adds a
// ValidationError for every leaf node.  It normalises two common error shapes:
//
//   - "missing properties: 'x', 'y'" at "/jobs/<name>" becomes per-field
//     required errors scoped to that job.
//   - Any error at "/jobs/<name>/<field>" is scoped to that job and field.
func collectSchemaErrors(ve *jsonschema.ValidationError, pe *ParseError) {
	if len(ve.Causes) == 0 {
		loc := strings.TrimPrefix(ve.InstanceLocation, "/")
		msg := ve.Message

		// Normalise "missing properties: 'a', 'b'" into individual field errors.
		if strings.HasPrefix(msg, "missing properties:") {
			fields := parseMissingPropertiesMsg(msg)
			for _, f := range fields {
				if strings.HasPrefix(loc, "jobs/") {
					parts := strings.SplitN(loc, "/", 3)
					jobName := ""
					if len(parts) >= 2 {
						jobName = parts[1]
					}
					// When there is a sub-path (e.g. "healthcheck"), prefix the
					// field name so callers see "healthcheck.command" not "command".
					fieldPrefix := ""
					if len(parts) >= 3 && parts[2] != "" {
						fieldPrefix = strings.ReplaceAll(parts[2], "/", ".") + "."
					}
					pe.add(jobName, fieldPrefix+f, "field is required")
				} else {
					pe.addTop(f, "field is required")
				}
			}
			return
		}

		// Normalise "/jobs/<name>/<field>" paths into job-scoped errors.
		if strings.HasPrefix(loc, "jobs/") {
			parts := strings.SplitN(loc, "/", 3)
			if len(parts) == 3 {
				field := strings.ReplaceAll(parts[2], "/", ".")
				pe.add(parts[1], field, msg)
				return
			}
		}

		pe.addTop(loc, msg)
		return
	}
	for _, cause := range ve.Causes {
		collectSchemaErrors(cause, pe)
	}
}

// parseMissingPropertiesMsg extracts the property names from a jsonschema
// "missing properties: 'a', 'b'" error message.
func parseMissingPropertiesMsg(msg string) []string {
	rest, _ := strings.CutPrefix(msg, "missing properties:")
	var names []string
	for _, part := range strings.Split(rest, ",") {
		name := strings.Trim(strings.TrimSpace(part), "'")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
