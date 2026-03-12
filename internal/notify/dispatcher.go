package notify

// Package notify provides production notification dispatch for Husky job events.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/store"
)

// Event identifies a job lifecycle notification event.
type Event string

const (
	EventSuccess   Event = "on_success"
	EventFailure   Event = "on_failure"
	EventSLABreach Event = "on_sla_breach"
	EventRetry     Event = "on_retry"
)

// Dispatcher sends notifications and writes alert audit rows.
type Dispatcher struct {
	store  *store.Store
	logger *slog.Logger
	http   *http.Client
}

// New creates a notification dispatcher.
func New(st *store.Store, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		store:  st,
		logger: logger,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Dispatch sends one event notification for a job run according to cfg.
func (d *Dispatcher) Dispatch(ctx context.Context, cfg *config.Config, jobName string, job *config.Job, run *store.Run, event Event) error {
	if cfg == nil || job == nil || run == nil || job.Notify == nil {
		return nil
	}
	notifyEvent := pickEvent(job.Notify, event)
	if notifyEvent == nil || strings.TrimSpace(notifyEvent.Channel) == "" {
		return nil
	}

	if event == EventSuccess && notifyEvent.OnlyAfterFailure {
		prev, err := d.store.GetPreviousCompletedRun(ctx, jobName, run.ID)
		if err != nil {
			return fmt.Errorf("lookup previous run for only_after_failure: %w", err)
		}
		if prev == nil || prev.Status != store.StatusFailed {
			return nil
		}
	}

	message, err := renderMessage(notifyEvent.Message, jobName, job, run)
	if err != nil {
		return err
	}
	if message == "" {
		message = fmt.Sprintf("[%s] job %s status=%s", event, jobName, run.Status)
	}

	logs, err := d.renderLogs(ctx, notifyEvent.AttachLogs, run.ID)
	if err != nil {
		return err
	}
	if logs != "" {
		message = message + "\n\n" + logs
	}

	provider, channelTarget, integration, err := resolveIntegration(cfg, notifyEvent.Channel)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"event":   event,
		"job":     jobName,
		"run_id":  run.ID,
		"status":  run.Status,
		"channel": notifyEvent.Channel,
		"message": message,
	}

	err = d.sendWithRetry(ctx, provider, channelTarget, integration, message, run)
	payload["ok"] = err == nil
	if err != nil {
		payload["error"] = err.Error()
	}
	_ = d.recordAlert(ctx, jobName, run.ID, string(event), notifyEvent.Channel, payload, err)
	return err
}

// TestIntegration sends a live test event to a configured integration key.
func (d *Dispatcher) TestIntegration(ctx context.Context, cfg *config.Config, integrationName string) error {
	if cfg == nil {
		return fmt.Errorf("configuration is nil")
	}
	intg, ok := cfg.Integrations[integrationName]
	if !ok || intg == nil {
		return fmt.Errorf("integration %q not found", integrationName)
	}
	provider := effectiveProvider(integrationName, intg)
	message := fmt.Sprintf("husky integration test: %s (%s)", integrationName, provider)

	switch provider {
	case "slack", "discord":
		return d.sendWithRetry(ctx, provider, "#test", intg, message, nil)
	case "webhook":
		return d.sendWithRetry(ctx, provider, intg.WebhookURL, intg, message, nil)
	case "pagerduty":
		return d.sendWithRetry(ctx, provider, "p3", intg, message, nil)
	case "smtp":
		if intg.From == "" {
			return fmt.Errorf("smtp integration %q requires from address", integrationName)
		}
		return d.sendEmail(intg, intg.From, "husky integration test", message)
	default:
		return fmt.Errorf("unsupported integration provider %q", provider)
	}
}

func pickEvent(n *config.Notify, event Event) *config.NotifyEvent {
	switch event {
	case EventSuccess:
		return n.OnSuccess
	case EventFailure:
		return n.OnFailure
	case EventSLABreach:
		if n.OnSLABreach != nil {
			return n.OnSLABreach
		}
		return n.OnFailure
	case EventRetry:
		return n.OnRetry
	default:
		return nil
	}
}

func renderMessage(raw string, jobName string, job *config.Job, run *store.Run) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	jobRetries := 0
	if job.Retries != nil {
		jobRetries = *job.Retries
	}
	normalized := normalizeTemplateSyntax(raw)
	tpl, err := template.New("notify").Option("missingkey=zero").Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("message template parse: %w", err)
	}
	data := map[string]any{
		"job": map[string]any{
			"name":        jobName,
			"description": job.Description,
			"frequency":   job.Frequency,
			"retries":     jobRetries,
			"tags":        job.Tags,
		},
		"run": map[string]any{
			"id":           run.ID,
			"status":       run.Status,
			"attempt":      run.Attempt,
			"trigger":      run.Trigger,
			"reason":       run.Reason,
			"sla_breached": run.SLABreached,
		},
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("message template execute: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

func normalizeTemplateSyntax(raw string) string {
	res := strings.ReplaceAll(raw, "{{ job.", "{{ .job.")
	res = strings.ReplaceAll(res, "{{ run.", "{{ .run.")
	res = strings.ReplaceAll(res, "{{job.", "{{.job.")
	res = strings.ReplaceAll(res, "{{run.", "{{.run.")
	return res
}

var lastNLinesRe = regexp.MustCompile(`^last_(\d+)_lines$`)

func (d *Dispatcher) renderLogs(ctx context.Context, mode string, runID int64) (string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" || mode == "none" {
		return "", nil
	}
	lines, err := d.store.GetRunLogs(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("load logs for attach_logs: %w", err)
	}
	if len(lines) == 0 {
		return "", nil
	}

	var selected []store.LogLine
	switch {
	case mode == "all":
		selected = lines
	case lastNLinesRe.MatchString(mode):
		m := lastNLinesRe.FindStringSubmatch(mode)
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			return "", nil
		}
		if n > len(lines) {
			n = len(lines)
		}
		selected = lines[len(lines)-n:]
	default:
		return "", nil
	}

	var b strings.Builder
	b.WriteString("Attached logs:\n")
	for _, line := range selected {
		b.WriteString(line.Line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String()), nil
}

func resolveIntegration(cfg *config.Config, channel string) (provider string, target string, intg *config.Integration, err error) {
	parts := strings.SplitN(strings.TrimSpace(channel), ":", 2)
	if len(parts) != 2 {
		return "", "", nil, fmt.Errorf("invalid channel format %q; expected <integration_or_provider>:<target>", channel)
	}
	key := parts[0]
	target = parts[1]

	// Named integration lookup (also handles bare provider name e.g. "slack").
	if cfg != nil && cfg.Integrations != nil {
		if named, ok := cfg.Integrations[key]; ok && named != nil {
			return effectiveProvider(key, named), target, named, nil
		}
	}

	if key == "webhook" && strings.HasPrefix(target, "http") {
		return "webhook", target, &config.Integration{WebhookURL: target, EffectiveProvider: "webhook"}, nil
	}

	if key == "email" {
		if cfg != nil && cfg.Integrations != nil {
			if smtpCfg, ok := cfg.Integrations["smtp"]; ok && smtpCfg != nil {
				return "smtp", target, smtpCfg, nil
			}
		}
	}

	return "", "", nil, fmt.Errorf("no integration found for channel prefix %q", key)
}

func effectiveProvider(name string, intg *config.Integration) string {
	if intg.EffectiveProvider != "" {
		return intg.EffectiveProvider
	}
	if intg.Provider != "" {
		return intg.Provider
	}
	return name
}

func (d *Dispatcher) sendWithRetry(
	ctx context.Context,
	provider string,
	target string,
	intg *config.Integration,
	message string,
	run *store.Run,
) error {
	var sendErr error
	for attempt := 1; attempt <= 3; attempt++ {
		sendErr = d.sendOnce(ctx, provider, target, intg, message, run)
		if sendErr == nil {
			return nil
		}
		if d.logger != nil {
			d.logger.Warn("notification delivery failed",
				"provider", provider,
				"target", target,
				"attempt", attempt,
				"error", sendErr)
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return sendErr
}

func (d *Dispatcher) sendOnce(
	ctx context.Context,
	provider string,
	target string,
	intg *config.Integration,
	message string,
	run *store.Run,
) error {
	switch provider {
	case "slack":
		return d.postJSON(ctx, intg.WebhookURL, map[string]any{"text": message, "channel": target})
	case "discord":
		return d.postJSON(ctx, intg.WebhookURL, map[string]any{"content": message})
	case "webhook":
		url := intg.WebhookURL
		if strings.HasPrefix(target, "http") {
			url = target
		}
		return d.postJSON(ctx, url, map[string]any{"message": message})
	case "pagerduty":
		severity := pagerDutySeverity(target)
		dedup := ""
		if run != nil {
			dedup = fmt.Sprintf("husky-%s-%d", run.JobName, run.ID)
		}
		payload := map[string]any{
			"routing_key":  intg.RoutingKey,
			"event_action": "trigger",
			"dedup_key":    dedup,
			"payload": map[string]any{
				"summary":  message,
				"source":   "husky",
				"severity": severity,
			},
		}
		return d.postJSON(ctx, "https://events.pagerduty.com/v2/enqueue", payload)
	case "smtp":
		return d.sendEmail(intg, target, "Husky notification", message)
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
}

func pagerDutySeverity(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "p1":
		return "critical"
	case "p2":
		return "error"
	case "p3":
		return "warning"
	default:
		return "info"
	}
}

func (d *Dispatcher) postJSON(ctx context.Context, endpoint string, body map[string]any) error {
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("missing endpoint URL")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal notification payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendEmail(intg *config.Integration, toAddr string, subject string, body string) error {
	if intg == nil {
		return fmt.Errorf("smtp integration is nil")
	}
	if intg.Host == "" || intg.From == "" {
		return fmt.Errorf("smtp integration requires host and from")
	}
	port := intg.Port
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", intg.Host, port)

	msg := strings.Join([]string{
		"From: " + intg.From,
		"To: " + toAddr,
		"Subject: " + subject,
		"",
		body,
	}, "\r\n")

	var auth smtp.Auth
	if intg.Username != "" || intg.Password != "" {
		auth = smtp.PlainAuth("", intg.Username, intg.Password, intg.Host)
	}
	return smtp.SendMail(addr, auth, intg.From, []string{toAddr}, []byte(msg))
}

func (d *Dispatcher) recordAlert(ctx context.Context, jobName string, runID int64, event string, channel string, payload map[string]any, deliveryErr error) error {
	raw, _ := json.Marshal(payload)
	rid := runID
	status := "delivered"
	lastError := ""
	if deliveryErr != nil {
		status = "failed"
		lastError = deliveryErr.Error()
	}
	return d.store.RecordAlert(ctx, store.Alert{
		JobName:       jobName,
		RunID:         &rid,
		Event:         event,
		Channel:       channel,
		Status:        status,
		DeliveryError: lastError,
		SentAt:        time.Now().UTC(),
		Payload:       string(raw),
	})
}
