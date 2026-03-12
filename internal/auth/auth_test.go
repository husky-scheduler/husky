// Package auth white-box tests.
// Using package auth (not auth_test) gives access to the unexported contextKey
// type so we can inject synthetic roles for RBAC testing.
package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/husky-scheduler/husky/internal/daemoncfg"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// discardLogger returns a slog.Logger that silently discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func doReq(mw func(http.Handler) http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rr, r)
	return rr
}

func errBody(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		return ""
	}
	return payload["error"]
}

func bearerCfg(token string) daemoncfg.AuthConfig {
	return daemoncfg.AuthConfig{
		Type:   "bearer",
		Bearer: daemoncfg.BearerAuthConfig{Token: token},
	}
}

func TestBearerMiddleware_ValidToken_OK(t *testing.T) {
	a, err := New(bearerCfg("secret123"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	assert.Equal(t, http.StatusOK, doReq(mw, req).Code)
}

func TestBearerMiddleware_InvalidToken_401(t *testing.T) {
	a, err := New(bearerCfg("secret123"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := doReq(mw, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, "invalid token", errBody(t, rr))
}

func TestBearerMiddleware_MissingToken_401(t *testing.T) {
	a, err := New(bearerCfg("secret123"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rr := doReq(mw, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, "authorization required", errBody(t, rr))
}

func TestBearerMiddleware_TokenViaQueryParam(t *testing.T) {
	a, err := New(bearerCfg("secret123"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs?token=secret123", nil)
	assert.Equal(t, http.StatusOK, doReq(mw, req).Code)
}

func TestBearerMiddleware_OPTIONS_Passthrough(t *testing.T) {
	a, err := New(bearerCfg("secret123"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodOptions, "/jobs", nil)
	assert.Equal(t, http.StatusOK, doReq(mw, req).Code)
}

func TestBearerMiddleware_SetsAdminRoleInContext(t *testing.T) {
	a, err := New(bearerCfg("tok"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	var gotRole string
	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotRole = RoleFromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tok")
	mw(capture).ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "admin", gotRole)
}

func basicCfg(t *testing.T, username, password string) daemoncfg.AuthConfig {
	t.Helper()
	raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return daemoncfg.AuthConfig{
		Type: "basic",
		Basic: daemoncfg.BasicAuthConfig{
			Users: []daemoncfg.BasicAuthUserEntry{
				{Username: username, PasswordHash: string(raw)},
			},
		},
	}
}

func TestBasicMiddleware_ValidCredentials_OK(t *testing.T) {
	a, err := New(basicCfg(t, "alice", "testpass"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.SetBasicAuth("alice", "testpass")
	assert.Equal(t, http.StatusOK, doReq(mw, req).Code)
}

func TestBasicMiddleware_WrongPassword_401(t *testing.T) {
	a, err := New(basicCfg(t, "alice", "testpass"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.SetBasicAuth("alice", "wrongpass")
	rr := doReq(mw, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("WWW-Authenticate"))
	assert.Equal(t, "invalid credentials", errBody(t, rr))
}

func TestBasicMiddleware_UnknownUser_401(t *testing.T) {
	a, err := New(basicCfg(t, "alice", "testpass"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.SetBasicAuth("bob", "testpass")
	assert.Equal(t, http.StatusUnauthorized, doReq(mw, req).Code)
}

func TestBasicMiddleware_MissingCredentials_401(t *testing.T) {
	a, err := New(basicCfg(t, "alice", "testpass"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rr := doReq(mw, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("WWW-Authenticate"))
}

func TestBasicMiddleware_OPTIONS_Passthrough(t *testing.T) {
	a, err := New(basicCfg(t, "alice", "testpass"), discardLogger())
	require.NoError(t, err)
	mw := a.Middleware()
	req := httptest.NewRequest(http.MethodOptions, "/jobs", nil)
	assert.Equal(t, http.StatusOK, doReq(mw, req).Code)
}

func TestBasicMiddleware_PlaintextPasswordRejectedAtLoad(t *testing.T) {
	cfg := daemoncfg.AuthConfig{
		Type: "basic",
		Basic: daemoncfg.BasicAuthConfig{
			Users: []daemoncfg.BasicAuthUserEntry{
				{Username: "alice", PasswordHash: "plaintext-not-a-hash"},
			},
		},
	}
	_, err := New(cfg, discardLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bcrypt")
}

func injectRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), contextKey{}, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newBearerAuthenticator(t *testing.T) *Authenticator {
	t.Helper()
	a, err := New(bearerCfg("tok"), discardLogger())
	require.NoError(t, err)
	return a
}

func TestRBACMiddleware_ViewerCanGET(t *testing.T) {
	a := newBearerAuthenticator(t)
	chain := injectRole("viewer")(a.RBACMiddleware()(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRBACMiddleware_ViewerCannotPOST(t *testing.T) {
	a := newBearerAuthenticator(t)
	chain := injectRole("viewer")(a.RBACMiddleware()(okHandler))
	req := httptest.NewRequest(http.MethodPost, "/jobs/trigger", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRBACMiddleware_OperatorCanPOST(t *testing.T) {
	a := newBearerAuthenticator(t)
	chain := injectRole("operator")(a.RBACMiddleware()(okHandler))
	req := httptest.NewRequest(http.MethodPost, "/jobs/trigger", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRBACMiddleware_AdminCanDELETE(t *testing.T) {
	a := newBearerAuthenticator(t)
	chain := injectRole("admin")(a.RBACMiddleware()(okHandler))
	req := httptest.NewRequest(http.MethodDelete, "/jobs/foo", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRBACMiddleware_NoRoleDefaultsToAdmin(t *testing.T) {
	a := newBearerAuthenticator(t)
	rbac := a.RBACMiddleware()
	req := httptest.NewRequest(http.MethodPost, "/anything", nil)
	assert.Equal(t, http.StatusOK, doReq(rbac, req).Code)
}

func TestRBACMiddleware_AuthNone_NoOpPassthrough(t *testing.T) {
	a, err := New(daemoncfg.AuthConfig{Type: "none"}, discardLogger())
	require.NoError(t, err)
	chain := injectRole("viewer")(a.RBACMiddleware()(okHandler))
	req := httptest.NewRequest(http.MethodDelete, "/anything", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}
