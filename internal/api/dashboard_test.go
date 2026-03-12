package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardRoutes(t *testing.T) {
	s := New("127.0.0.1:0", Dependencies{})

	// / must serve index.html directly with no redirect loop.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rw, req)
	if rw.Code != 200 {
		t.Errorf("GET /: got %d, want 200 (location=%q)", rw.Code, rw.Header().Get("Location"))
	}
	if !strings.Contains(rw.Body.String(), "<html") {
		t.Errorf("GET /: body does not look like HTML: %q", truncate(rw.Body.String(), 120))
	}

	// Every JS/CSS asset in the embedded bundle must be reachable at /assets/<name>.
	sub, _ := fs.Sub(dashboardAssets, "dashboard")
	_ = fs.WalkDir(sub, "assets", func(path string, d fs.DirEntry, _ error) error {
		if d == nil || d.IsDir() {
			return nil
		}
		reqPath := "/" + path
		r := httptest.NewRequest(http.MethodGet, reqPath, nil)
		w := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("GET %s: got %d, want 200", reqPath, w.Code)
		}
		return nil
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
