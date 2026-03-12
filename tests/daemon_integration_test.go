package tests_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/husky-scheduler/husky/internal/auth"
	"github.com/husky-scheduler/husky/internal/daemoncfg"
)

// ── bearer auth + HUSKY_TOKEN ─────────────────────────────────────────────────

// newBearerServer starts an httptest.Server whose single route ("/status")
// is protected by bearer auth using token.
func newBearerServer(t *testing.T, token string) (*httptest.Server, func()) {
	t.Helper()
	cfg := daemoncfg.AuthConfig{
		Type:   "bearer",
		Bearer: daemoncfg.BearerAuthConfig{Token: token},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := auth.New(cfg, logger)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(a.Middleware()(mux))
	return srv, srv.Close
}

func TestIntegration_BearerAuth_WithoutToken_401(t *testing.T) {
	srv, teardown := newBearerServer(t, "correct-token")
	defer teardown()

	resp, err := http.Get(srv.URL + "/status")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"request without Authorization header should be rejected")
}

func TestIntegration_BearerAuth_WrongToken_401(t *testing.T) {
	srv, teardown := newBearerServer(t, "correct-token")
	defer teardown()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_BearerAuth_WithHUSKY_TOKEN_200(t *testing.T) {
	const token = "correct-token"
	srv, teardown := newBearerServer(t, token)
	defer teardown()

	// Simulate what the husky CLI does: read HUSKY_TOKEN and inject as Bearer.
	t.Setenv("HUSKY_TOKEN", token)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	require.NoError(t, err)
	if tok := os.Getenv("HUSKY_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"request with correct HUSKY_TOKEN should succeed")
}

// ── huskyd.yaml absent → defaults, no error ───────────────────────────────────

func TestIntegration_NoDaemonConfig_EmptyPath_ReturnsDefaults(t *testing.T) {
	// Load("", "") → both path and huskyConfigDir empty: returns defaults immediately.
	cfg, err := daemoncfg.Load("", "")
	require.NoError(t, err, "absent huskyd.yaml must not produce an error")

	defaults := daemoncfg.Defaults()

	// Spot-check a handful of default values.
	assert.Equal(t, defaults.Executor.Shell, cfg.Executor.Shell)
	assert.Equal(t, defaults.Executor.PoolSize, cfg.Executor.PoolSize)
	assert.Equal(t, defaults.Log.Level, cfg.Log.Level)
}

func TestIntegration_NoDaemonConfig_NonexistentDir_ReturnsDefaults(t *testing.T) {
	// Load("", "/nonexistent") → file discovery path doesn't exist: return defaults.
	cfg, err := daemoncfg.Load("", "/nonexistent/dir/that/cannot/exist")
	require.NoError(t, err, "absent huskyd.yaml must not produce an error")

	defaults := daemoncfg.Defaults()
	assert.Equal(t, defaults.Executor.Shell, cfg.Executor.Shell)
}

func TestIntegration_NoDaemonConfig_ExplicitAbsentPath_ReturnsError(t *testing.T) {
	// Load("/explicit/path", "") → explicit path that doesn't exist must return ErrNoDaemonConfig.
	_, err := daemoncfg.Load("/nonexistent/explicit/huskyd.yaml", "")
	require.ErrorIs(t, err, daemoncfg.ErrNoDaemonConfig,
		"explicit missing path must return ErrNoDaemonConfig")
}
