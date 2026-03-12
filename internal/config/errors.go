package config

import (
	"fmt"
	"strings"
)

// ValidationError describes a single validation failure within a husky.yaml file.
type ValidationError struct {
	// Job is the name of the job in which the error occurred.
	// Empty when the error is at the top level (e.g. a missing version field).
	Job string

	// Field is the YAML field path where the error occurred (e.g. "frequency",
	// "notify.on_failure").
	Field string

	// Msg is a human-readable description of the error.
	Msg string
}

func (e *ValidationError) Error() string {
	if e.Job != "" {
		return fmt.Sprintf("jobs.%s.%s: %s", e.Job, e.Field, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Msg)
}

// ParseError is returned by Load when one or more validation failures are found.
// It collects all errors so that the caller can report them all at once rather
// than surfacing a single failure per parse attempt.
type ParseError struct {
	Errors []*ValidationError
}

func (p *ParseError) Error() string {
	msgs := make([]string, len(p.Errors))
	for i, e := range p.Errors {
		msgs[i] = "  • " + e.Error()
	}
	return fmt.Sprintf("husky.yaml has %d validation error(s):\n%s",
		len(p.Errors), strings.Join(msgs, "\n"))
}

// add appends a new ValidationError to the ParseError.
func (p *ParseError) add(job, field, msg string) {
	p.Errors = append(p.Errors, &ValidationError{Job: job, Field: field, Msg: msg})
}

// addTop appends a top-level (non-job) ValidationError.
func (p *ParseError) addTop(field, msg string) {
	p.add("", field, msg)
}

// hasErrors reports whether any errors have been collected.
func (p *ParseError) hasErrors() bool {
	return len(p.Errors) > 0
}
