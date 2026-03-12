package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/store"
)

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func openNotifyStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "notify-test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fakeJob(name string) *config.Job {
	return &config.Job{
		Name:        name,
		Description: "test job",
		Frequency:   "daily",
		Tags:        []string{"etl"},
	}
}

func fakeRun(runID int64, jobName string, status store.RunStatus) *store.Run {
	now := time.Now().UTC()
	return &store.Run{
		ID:        runID,
		JobName:   jobName,
		Status:    status,
		Attempt:   1,
		Trigger:   store.TriggerManual,
		Reason:    "test reason",
		StartedAt: &now,
	}
}

func newDisp(st *store.Store) *Dispatcher {
	return New(st, nil)
}

// capturingServer returns an httptest.Server that records every request body
// into the returned channel (buffered, length 4).
func capturingServer(t *testing.T) (*httptest.Server, chan []byte) {
	t.Helper()
	ch := make(chan []byte, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ch <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)
	return ts, ch
}

// ──────────────────────────────────────────────────────────────────────────────
// normalizeTemplateSyntax
// ──────────────────────────────────────────────────────────────────────────────

func TestNormalizeTemplateSyntax(t *testing.T) {
	cases := []struct{ in, want string }{
		{"{{ job.name }}", "{{ .job.name }}"},
		{"{{ run.status }}", "{{ .run.status }}"},
		{"{{job.name}}", "{{.job.name}}"},
		{"{{run.id}}", "{{.run.id}}"},
		{"no template here", "no template here"},
		{"{{ job.name }} / {{ run.status }}", "{{ .job.name }} / {{ .run.status }}"},
	}
	for _, c := range cases {
		got := normalizeTemplateSyntax(c.in)
		assert.Equal(t, c.want, got, "input: %q", c.in)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// renderMessage — template variable coverage
// ──────────────────────────────────────────────────────────────────────────────

func TestRenderMessage_JobVariables(t *testing.T) {
	job := fakeJob("ingest")
	retries := 3
	job.Retries = &retries
	run := fakeRun(42, "ingest", store.StatusFailed)

	msg, err := renderMessage(
		"job={{ job.name }} desc={{ job.description }} freq={{ job.frequency }} retries={{ job.retries }}",
		"ingest", job, run,
	)
	require.NoError(t, err)
	assert.Equal(t, "job=ingest desc=test job freq=daily retries=3", msg)
}

func TestRenderMessage_RunVariables(t *testing.T) {
	job := fakeJob("ingest")
	run := fakeRun(7, "ingest", store.StatusFailed)

	msg, err := renderMessage(
		"id={{ run.id }} status={{ run.status }} attempt={{ run.attempt }} trigger={{ run.trigger }} reason={{ run.reason }}",
		"ingest", job, run,
	)
	require.NoError(t, err)
	assert.Equal(t, "id=7 status=FAILED attempt=1 trigger=manual reason=test reason", msg)
}

func TestRenderMessage_SLABreached(t *testing.T) {
	job := fakeJob("ingest")
	run := fakeRun(1, "ingest", store.StatusSuccess)
	run.SLABreached = true

	msg, err := renderMessage("sla={{ run.sla_breached }}", "ingest", job, run)
	require.NoError(t, err)
	assert.Equal(t, "sla=true", msg)
}

func TestRenderMessage_EmptyTemplate_ReturnsEmpty(t *testing.T) {
	job := fakeJob("ingest")
	run := fakeRun(1, "ingest", store.StatusSuccess)

	msg, err := renderMessage("", "ingest", job, run)
	require.NoError(t, err)
	assert.Equal(t, "", msg)
}

func TestRenderMessage_MissingKeyisSilent(t *testing.T) {
	job := fakeJob("ingest")
	run := fakeRun(1, "ingest", store.StatusSuccess)

	// missingkey=zero — should not error.
	msg, err := renderMessage("x={{ run.nonexistent }}", "ingest", job, run)
	require.NoError(t, err)
	assert.Equal(t, "x=<no value>", msg)
}

// ──────────────────────────────────────────────────────────────────────────────
// pickEvent — SLA breach falls back to on_failure when on_sla_breach is nil
// ──────────────────────────────────────────────────────────────────────────────

func TestPickEvent_SLABreachFallsBackToOnFailure(t *testing.T) {
	n := &config.Notify{
		OnFailure: &config.NotifyEvent{Channel: "slack:#alerts"},
	}
	got := pickEvent(n, EventSLABreach)
	require.NotNil(t, got)
	assert.Equal(t, "slack:#alerts", got.Channel)
}

func TestPickEvent_SLABreachUsesOwnEvent(t *testing.T) {
	n := &config.Notify{
		OnFailure:   &config.NotifyEvent{Channel: "slack:#failures"},
		OnSLABreach: &config.NotifyEvent{Channel: "pagerduty:p1"},
	}
	got := pickEvent(n, EventSLABreach)
	require.NotNil(t, got)
	assert.Equal(t, "pagerduty:p1", got.Channel)
}

func TestPickEvent_RetryEvent(t *testing.T) {
	n := &config.Notify{
		OnRetry: &config.NotifyEvent{Channel: "slack:#retries"},
	}
	got := pickEvent(n, EventRetry)
	require.NotNil(t, got)
	assert.Equal(t, "slack:#retries", got.Channel)
}

func TestPickEvent_NilEventReturnsNil(t *testing.T) {
	n := &config.Notify{} // nothing configured
	assert.Nil(t, pickEvent(n, EventSuccess))
	assert.Nil(t, pickEvent(n, EventFailure))
}

// ──────────────────────────────────────────────────────────────────────────────
// resolveIntegration — named key, bare provider fallback, missing integration
// ──────────────────────────────────────────────────────────────────────────────

func TestResolveIntegration_NamedKey(t *testing.T) {
	cfg := &config.Config{Integrations: map[string]*config.Integration{
		"slack_ops": {WebhookURL: "https://hooks.slack.com/test", EffectiveProvider: "slack"},
	}}
	provider, target, intg, err := resolveIntegration(cfg, "slack_ops:#data-alerts")
	require.NoError(t, err)
	assert.Equal(t, "slack", provider)
	assert.Equal(t, "#data-alerts", target)
	assert.Equal(t, "https://hooks.slack.com/test", intg.WebhookURL)
}

func TestResolveIntegration_BareProviderKey(t *testing.T) {
	cfg := &config.Config{Integrations: map[string]*config.Integration{
		"slack": {WebhookURL: "https://hooks.slack.com/test"},
	}}
	provider, target, intg, err := resolveIntegration(cfg, "slack:#channel")
	require.NoError(t, err)
	assert.Equal(t, "slack", provider)
	assert.Equal(t, "#channel", target)
	require.NotNil(t, intg)
}

func TestResolveIntegration_WehookWithURL(t *testing.T) {
	cfg := &config.Config{} // no integrations
	provider, target, intg, err := resolveIntegration(cfg, "webhook:https://example.com/hook")
	require.NoError(t, err)
	assert.Equal(t, "webhook", provider)
	assert.Equal(t, "https://example.com/hook", target)
	require.NotNil(t, intg)
}

func TestResolveIntegration_EmailResolvesSmtp(t *testing.T) {
	cfg := &config.Config{Integrations: map[string]*config.Integration{
		"smtp": {Host: "mail.example.com", From: "noreply@example.com"},
	}}
	provider, target, intg, err := resolveIntegration(cfg, "email:ops@example.com")
	require.NoError(t, err)
	assert.Equal(t, "smtp", provider)
	assert.Equal(t, "ops@example.com", target)
	require.NotNil(t, intg)
}

func TestResolveIntegration_Missing(t *testing.T) {
	cfg := &config.Config{}
	_, _, _, err := resolveIntegration(cfg, "discord:#announcements")
	assert.Error(t, err)
}

func TestResolveIntegration_InvalidFormat(t *testing.T) {
	_, _, _, err := resolveIntegration(nil, "noconjunction")
	assert.Error(t, err)
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload construction — Slack
// ──────────────────────────────────────────────────────────────────────────────

func TestSendOnce_SlackPayload(t *testing.T) {
	capSrv, ch := capturingServer(t)
	d := newDisp(nil)
	intg := &config.Integration{WebhookURL: capSrv.URL, EffectiveProvider: "slack"}

	err := d.sendOnce(context.Background(), "slack", "#alerts", intg, "hello slack", nil)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(<-ch, &payload))
	assert.Equal(t, "hello slack", payload["text"])
	assert.Equal(t, "#alerts", payload["channel"])
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload construction — Discord
// ──────────────────────────────────────────────────────────────────────────────

func TestSendOnce_DiscordPayload(t *testing.T) {
	capSrv, ch := capturingServer(t)
	d := newDisp(nil)
	intg := &config.Integration{WebhookURL: capSrv.URL, EffectiveProvider: "discord"}

	err := d.sendOnce(context.Background(), "discord", "#general", intg, "hello discord", nil)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(<-ch, &payload))
	assert.Equal(t, "hello discord", payload["content"])
	// Discord webhooks do not take a "channel" field.
	_, hasChannel := payload["channel"]
	assert.False(t, hasChannel)
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload construction — Custom webhook
// ──────────────────────────────────────────────────────────────────────────────

func TestSendOnce_WebhookPayload(t *testing.T) {
	capSrv, ch := capturingServer(t)
	d := newDisp(nil)
	intg := &config.Integration{WebhookURL: capSrv.URL, EffectiveProvider: "webhook"}

	// When target does NOT start with "http", sendOnce falls back to intg.WebhookURL.
	err := d.sendOnce(context.Background(), "webhook", "my-webhook", intg, "custom event", nil)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(<-ch, &payload))
	assert.Equal(t, "custom event", payload["message"])
}

func TestSendOnce_WebhookPayload_TargetURL(t *testing.T) {
	capSrv, ch := capturingServer(t)
	d := newDisp(nil)
	// When target starts with "http", sendOnce uses target as the URL directly.
	intg := &config.Integration{WebhookURL: "http://unused", EffectiveProvider: "webhook"}

	err := d.sendOnce(context.Background(), "webhook", capSrv.URL, intg, "direct url event", nil)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(<-ch, &payload))
	assert.Equal(t, "direct url event", payload["message"])
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload construction — PagerDuty
// ──────────────────────────────────────────────────────────────────────────────

func TestSendOnce_PagerDutyPayload(t *testing.T) {
	capSrv, ch := capturingServer(t)
	d := &Dispatcher{
		store:  nil,
		logger: nil,
		http:   &http.Client{Transport: &fixedURLTransport{target: capSrv.URL}},
	}
	intg := &config.Integration{RoutingKey: "key-abc", EffectiveProvider: "pagerduty"}
	run := fakeRun(5, "myjob", store.StatusFailed)

	err := d.sendOnce(context.Background(), "pagerduty", "p1", intg, "pagerduty test", run)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(<-ch, &payload))
	assert.Equal(t, "key-abc", payload["routing_key"])
	assert.Equal(t, "trigger", payload["event_action"])
	inner, ok := payload["payload"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "critical", inner["severity"]) // p1 → critical
	assert.Equal(t, "pagerduty test", inner["summary"])
}

// fixedURLTransport rewrites the request host to a test server URL so that
// PagerDuty calls (which hardcode the events API URL) can be intercepted.
type fixedURLTransport struct{ target string }

func (f *fixedURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	parsed, err := url.Parse(f.target)
	if err != nil {
		return nil, fmt.Errorf("fixedURLTransport: bad target: %w", err)
	}
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = parsed.Scheme
	req2.URL.Host = parsed.Host
	return http.DefaultTransport.RoundTrip(req2)
}

// ──────────────────────────────────────────────────────────────────────────────
// pagerDutySeverity mapping
// ──────────────────────────────────────────────────────────────────────────────

func TestPagerDutySeverity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"p1", "critical"},
		{"p2", "error"},
		{"p3", "warning"},
		{"p4", "info"},
		{"unknown", "info"},
		{"P1", "critical"}, // case-insensitive
	}
	for _, c := range cases {
		assert.Equal(t, c.want, pagerDutySeverity(c.in), "severity for %q", c.in)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// sendWithRetry — retries on HTTP error
// ──────────────────────────────────────────────────────────────────────────────

func TestSendWithRetry_RetriesOnHTTPError(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := newDisp(nil)
	intg := &config.Integration{WebhookURL: ts.URL, EffectiveProvider: "slack"}
	err := d.sendWithRetry(context.Background(), "slack", "#ch", intg, "msg", nil)
	assert.Error(t, err)
	assert.Equal(t, 3, calls, "expected exactly 3 delivery attempts")
}

// ──────────────────────────────────────────────────────────────────────────────
// renderLogs — attach_logs modes
// ──────────────────────────────────────────────────────────────────────────────

func TestRenderLogs_None(t *testing.T) {
	d := newDisp(openNotifyStore(t))
	out, err := d.renderLogs(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Equal(t, "", out)
}

func TestRenderLogs_All(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	runID, err := st.RecordRunStart(ctx, store.Run{JobName: "j", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now})
	require.NoError(t, err)
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 0, Stream: "stdout", Line: "line-one", TS: now}))
	require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: 1, Stream: "stdout", Line: "line-two", TS: now}))

	d := newDisp(st)
	out, err := d.renderLogs(ctx, "all", runID)
	require.NoError(t, err)
	assert.Contains(t, out, "line-one")
	assert.Contains(t, out, "line-two")
}

func TestRenderLogs_LastNLines(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	runID, err := st.RecordRunStart(ctx, store.Run{JobName: "j", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now})
	require.NoError(t, err)
	lines := []string{"line-A", "line-B", "line-C", "line-D", "line-E"}
	for i, l := range lines {
		require.NoError(t, st.RecordLog(ctx, store.LogLine{RunID: runID, Seq: i, Stream: "stdout", Line: l, TS: now}))
	}

	d := newDisp(st)
	out, err := d.renderLogs(ctx, "last_2_lines", runID)
	require.NoError(t, err)
	assert.NotContains(t, out, "line-A")
	assert.Contains(t, out, "line-D")
	assert.Contains(t, out, "line-E")
}

// ──────────────────────────────────────────────────────────────────────────────
// only_after_failure suppression
// ──────────────────────────────────────────────────────────────────────────────

func TestDispatch_OnlyAfterFailure_SuppressedWhenPrevWasSuccess(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Record a previous SUCCESS run.
	prevID, err := st.RecordRunStart(ctx, store.Run{JobName: "ingest", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now})
	require.NoError(t, err)
	exitCode := 0
	require.NoError(t, st.RecordRunEnd(ctx, prevID, store.StatusSuccess, "", &exitCode, now.Add(time.Second), false, nil))

	// Record the current run.
	currID, err := st.RecordRunStart(ctx, store.Run{JobName: "ingest", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerManual, StartedAt: &now})
	require.NoError(t, err)
	require.NoError(t, st.RecordRunEnd(ctx, currID, store.StatusSuccess, "", &exitCode, now.Add(2*time.Second), false, nil))
	run, err := st.GetRun(ctx, currID)
	require.NoError(t, err)

	// The notification server should NOT be called.
	notifyCalled := false
	capSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notifyCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer capSrv.Close()

	job := fakeJob("ingest")
	job.Notify = &config.Notify{
		OnSuccess: &config.NotifyEvent{
			Channel:          "slack:#ch",
			OnlyAfterFailure: true,
		},
	}
	cfg := &config.Config{
		Jobs: map[string]*config.Job{"ingest": job},
		Integrations: map[string]*config.Integration{
			"slack": {WebhookURL: capSrv.URL},
		},
	}

	d := newDisp(st)
	err = d.Dispatch(ctx, cfg, "ingest", job, run, EventSuccess)
	require.NoError(t, err)
	assert.False(t, notifyCalled, "notification should be suppressed when prev run was SUCCESS")
}

func TestDispatch_OnlyAfterFailure_FiredWhenPrevWasFailed(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Record a previous FAILED run.
	prevID, err := st.RecordRunStart(ctx, store.Run{JobName: "ingest", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerSchedule, StartedAt: &now})
	require.NoError(t, err)
	exitCode := 1
	require.NoError(t, st.RecordRunEnd(ctx, prevID, store.StatusFailed, "", &exitCode, now.Add(time.Second), false, nil))

	// Record the current (SUCCESS) run.
	exitCode = 0
	currID, err := st.RecordRunStart(ctx, store.Run{JobName: "ingest", Status: store.StatusRunning, Attempt: 1, Trigger: store.TriggerManual, StartedAt: &now})
	require.NoError(t, err)
	require.NoError(t, st.RecordRunEnd(ctx, currID, store.StatusSuccess, "", &exitCode, now.Add(2*time.Second), false, nil))
	run, err := st.GetRun(ctx, currID)
	require.NoError(t, err)

	notifyCalled := false
	capSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notifyCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer capSrv.Close()

	job := fakeJob("ingest")
	job.Notify = &config.Notify{
		OnSuccess: &config.NotifyEvent{
			Channel:          "slack:#ch",
			OnlyAfterFailure: true,
		},
	}
	cfg := &config.Config{
		Jobs: map[string]*config.Job{"ingest": job},
		Integrations: map[string]*config.Integration{
			"slack": {WebhookURL: capSrv.URL},
		},
	}

	d := newDisp(st)
	err = d.Dispatch(ctx, cfg, "ingest", job, run, EventSuccess)
	require.NoError(t, err)
	assert.True(t, notifyCalled, "notification should fire when previous run was FAILED")
}

func TestDispatch_RecordsAlertEventAndStatus(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	run := fakeRun(11, "ingest", store.StatusRetrying)

	capSrv, _ := capturingServer(t)
	job := fakeJob("ingest")
	job.Notify = &config.Notify{
		OnRetry: &config.NotifyEvent{Channel: "webhook:" + capSrv.URL, Message: "retry"},
	}
	cfg := &config.Config{Jobs: map[string]*config.Job{"ingest": job}}

	d := newDisp(st)
	err := d.Dispatch(ctx, cfg, "ingest", job, run, EventRetry)
	require.NoError(t, err)

	alerts, err := st.ListAlerts(ctx, 10)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
	assert.Equal(t, "on_retry", alerts[0].Event)
	assert.Equal(t, "delivered", alerts[0].Status)
	assert.Equal(t, "webhook:"+capSrv.URL, alerts[0].Channel)
	assert.Empty(t, alerts[0].DeliveryError)
	assert.Contains(t, alerts[0].Payload, "\"event\":\"on_retry\"")
}
