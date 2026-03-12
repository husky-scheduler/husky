// Package auth provides pluggable HTTP authentication and RBAC for huskyd.
//
// Three auth strategies are supported:
//
//   - none   — no authentication (default); all requests are allowed.
//   - bearer — token-based; accepts a static token (or token file).
//   - basic  — HTTP Basic Auth with bcrypt-hashed passwords.
//
// OIDC is declared in config but returns an error at startup (not yet
// implemented).
//
// RBAC is layered on top of authentication: after the auth check the request
// context carries the authenticated role ("admin" by default for bearer/basic).
// RBACMiddleware then enforces per-role method+path grants.
package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/husky-scheduler/husky/internal/daemoncfg"
)

// ── Context key ───────────────────────────────────────────────────────────────

type contextKey struct{}

// RoleFromContext retrieves the authenticated role stored in the request
// context by the auth middleware.  Returns "" when no role is present (i.e.
// when auth.type is "none").
func RoleFromContext(ctx context.Context) string {
	r, _ := ctx.Value(contextKey{}).(string)
	return r
}

// ── Authenticator ─────────────────────────────────────────────────────────────

// Authenticator holds the active authentication strategy and any
// hot-reloadable state (bearer token set).
type Authenticator struct {
	cfg    daemoncfg.AuthConfig
	logger *slog.Logger
	mu     sync.RWMutex
	tokens map[string]struct{} // bearer: current valid token set
}

// New creates an Authenticator from cfg.
//
//   - For "bearer": reads the initial token set from file + inline token.
//   - For "basic":  validates that all password_hash values look like bcrypt
//     hashes; plaintext passwords are rejected at load time.
//   - For "oidc":   returns an error — OIDC is not yet supported.
//   - For "none":   returns a no-op authenticator immediately.
func New(cfg daemoncfg.AuthConfig, logger *slog.Logger) (*Authenticator, error) {
	a := &Authenticator{cfg: cfg, logger: logger}
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "oidc":
		return nil, fmt.Errorf(
			"auth.type=oidc is not yet supported in this release; " +
				"use 'none', 'bearer', or 'basic'",
		)
	case "bearer":
		if cfg.Bearer.Token != "" {
			logger.Warn("auth.bearer.token is set inline; consider using auth.bearer.token_file instead")
		}
		tokens, err := loadBearerTokens(cfg.Bearer)
		if err != nil {
			return nil, fmt.Errorf("auth: load bearer tokens: %w", err)
		}
		a.tokens = tokens
	case "basic":
		for _, u := range cfg.Basic.Users {
			// A valid bcrypt hash starts with "$2" and is at least 60 chars.
			if !strings.HasPrefix(u.PasswordHash, "$2") || len(u.PasswordHash) < 60 {
				return nil, fmt.Errorf(
					"auth: user %q: password_hash does not look like a bcrypt hash; "+
						"plaintext passwords are not accepted",
					u.Username,
				)
			}
		}
	}
	return a, nil
}

// ReloadTokens re-reads the bearer token file and atomically replaces the
// in-memory token set.  Safe to call concurrently (e.g. on SIGHUP).
// No-op when auth.type is not "bearer".
func (a *Authenticator) ReloadTokens() error {
	if !strings.EqualFold(strings.TrimSpace(a.cfg.Type), "bearer") {
		return nil
	}
	tokens, err := loadBearerTokens(a.cfg.Bearer)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.tokens = tokens
	a.mu.Unlock()
	a.logger.Info("bearer tokens hot-reloaded")
	return nil
}

// Middleware returns the HTTP middleware that enforces the configured auth
// strategy.  When auth.type is "none" the returned middleware is a transparent
// pass-through with zero allocation overhead.
func (a *Authenticator) Middleware() func(http.Handler) http.Handler {
	switch strings.ToLower(strings.TrimSpace(a.cfg.Type)) {
	case "bearer":
		return a.bearerMiddleware()
	case "basic":
		return a.basicMiddleware()
	default: // "none" or empty
		return func(next http.Handler) http.Handler { return next }
	}
}

// RBACMiddleware returns an HTTP middleware that enforces the RBAC rules
// configured in a.cfg.RBAC.  The authenticated role is read from the request
// context (set by Middleware).
//
// Built-in role semantics when no explicit rules are configured:
//
//	admin    — all methods, all paths (default for bearer / basic)
//	operator — all methods, all paths (same as admin when no rules set)
//	viewer   — GET, HEAD, OPTIONS only
//
// When explicit RBAC rules are configured they fully replace the built-in
// defaults.  An authenticated request whose role matches no rule is rejected
// with 403 Forbidden.
func (a *Authenticator) RBACMiddleware() func(http.Handler) http.Handler {
	// No-op when auth is disabled.
	if strings.ToLower(strings.TrimSpace(a.cfg.Type)) == "none" || a.cfg.Type == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	rules := buildEffectiveRules(a.cfg.RBAC)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := RoleFromContext(r.Context())
			if role == "" {
				role = "admin"
			}
			if !rbacAllows(rules, role, r.Method, r.URL.Path) {
				jsonError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── bearer middleware ─────────────────────────────────────────────────────────

func (a *Authenticator) bearerMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for CORS preflight requests.
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			token := extractBearerToken(r)
			if token == "" {
				jsonError(w, http.StatusUnauthorized, "authorization required")
				return
			}
			a.mu.RLock()
			_, valid := a.tokens[token]
			a.mu.RUnlock()
			if !valid {
				jsonError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), contextKey{}, "admin")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── basic auth middleware ─────────────────────────────────────────────────────

func (a *Authenticator) basicMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			username, password, ok := r.BasicAuth()
			if !ok || username == "" {
				w.Header().Set("WWW-Authenticate", `Basic realm="husky"`)
				jsonError(w, http.StatusUnauthorized, "authorization required")
				return
			}
			if !a.checkBasic(username, password) {
				w.Header().Set("WWW-Authenticate", `Basic realm="husky"`)
				jsonError(w, http.StatusUnauthorized, "invalid credentials")
				return
			}
			ctx := context.WithValue(r.Context(), contextKey{}, "admin")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// checkBasic returns true when username/password match a configured user entry.
func (a *Authenticator) checkBasic(username, password string) bool {
	for _, u := range a.cfg.Basic.Users {
		if u.Username != username {
			continue
		}
		err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
		return err == nil
	}
	return false
}

// ── bearer token extraction ───────────────────────────────────────────────────

// extractBearerToken returns the bearer token from the Authorization header,
// or from the ?token= query parameter (WebSocket upgrade requests cannot set
// custom headers easily in all browsers).
func extractBearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if after, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	// Fallback for WebSocket clients that cannot set Authorization headers.
	return r.URL.Query().Get("token")
}

// ── token file loader ─────────────────────────────────────────────────────────

// loadBearerTokens assembles the valid token set from the token_file (one
// token per non-blank, non-comment line) and the optional inline token value.
func loadBearerTokens(cfg daemoncfg.BearerAuthConfig) (map[string]struct{}, error) {
	tokens := make(map[string]struct{})
	if cfg.TokenFile != "" {
		f, err := os.Open(cfg.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("bearer token_file %q: %w", cfg.TokenFile, err)
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			tokens[line] = struct{}{}
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("bearer token_file %q: %w", cfg.TokenFile, err)
		}
	}
	if t := strings.TrimSpace(cfg.Token); t != "" {
		tokens[t] = struct{}{}
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("auth.bearer: no tokens configured (set auth.bearer.token or auth.bearer.token_file)")
	}
	return tokens, nil
}

// ── RBAC ─────────────────────────────────────────────────────────────────────

// rbacRule is the internal, normalised form of a daemoncfg.RBACRule.
type rbacRule struct {
	role    string
	methods map[string]struct{}
	paths   []string // glob prefixes ending with "*" are prefix-matched
}

// buildEffectiveRules converts the user-configured RBAC rules into the
// internal representation.  When the user provides no rules the built-in
// defaults are used:
//
//	admin    — GET/HEAD/OPTIONS/POST/PUT/PATCH/DELETE on all paths
//	operator — GET/HEAD/OPTIONS/POST/PUT/PATCH/DELETE on all paths
//	viewer   — GET/HEAD/OPTIONS on all paths
func buildEffectiveRules(cfgRules []daemoncfg.RBACRule) []rbacRule {
	if len(cfgRules) == 0 {
		return defaultRules()
	}
	rules := make([]rbacRule, 0, len(cfgRules))
	for _, cr := range cfgRules {
		r := rbacRule{
			role:    strings.ToLower(cr.Role),
			methods: make(map[string]struct{}),
			paths:   cr.Paths,
		}
		for _, m := range cr.Methods {
			r.methods[strings.ToUpper(m)] = struct{}{}
		}
		rules = append(rules, r)
	}
	return rules
}

func defaultRules() []rbacRule {
	allMethods := map[string]struct{}{
		"GET": {}, "HEAD": {}, "OPTIONS": {},
		"POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {},
	}
	readMethods := map[string]struct{}{
		"GET": {}, "HEAD": {}, "OPTIONS": {},
	}
	return []rbacRule{
		{role: "admin", methods: allMethods, paths: []string{"*"}},
		{role: "operator", methods: allMethods, paths: []string{"*"}},
		{role: "viewer", methods: readMethods, paths: []string{"*"}},
	}
}

// rbacAllows returns true if any rule for role grants method on path.
func rbacAllows(rules []rbacRule, role, method, path string) bool {
	role = strings.ToLower(role)
	method = strings.ToUpper(method)
	for _, r := range rules {
		if r.role != role {
			continue
		}
		if _, ok := r.methods[method]; !ok {
			continue
		}
		for _, p := range r.paths {
			if p == "*" || p == path {
				return true
			}
			if strings.HasSuffix(p, "*") && strings.HasPrefix(path, p[:len(p)-1]) {
				return true
			}
		}
	}
	return false
}

// ── helpers ───────────────────────────────────────────────────────────────────

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	b, _ := json.Marshal(map[string]string{"error": msg})
	_, _ = w.Write(b)
}
