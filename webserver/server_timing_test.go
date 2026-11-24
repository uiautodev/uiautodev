package webserver_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/uiautodev/uiautodev/webserver"
)

// serverTimingPattern matches the single `app` metric this server exposes.
var serverTimingPattern = regexp.MustCompile(`^app;desc="total";dur=\d+\.\d$`)

// requestIsolatedServer serves one request against a server whose persisted
// config lives in a temp dir, so the tests never touch the user's config.
func requestIsolatedServer(t *testing.T, cfg webserver.ServerConfig, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	webserver.SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	t.Cleanup(func() { webserver.SetConfigFile("") })

	e := webserver.CreateEchoServerWithConfig(cfg)
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestServerTimingOnHealthz(t *testing.T) {
	rec := requestIsolatedServer(t, webserver.ServerConfig{}, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	got := rec.Header().Get("Server-Timing")
	if !serverTimingPattern.MatchString(got) {
		t.Fatalf("unexpected Server-Timing %q, want app;desc=\"total\";dur=<number>", got)
	}
}

func TestServerTimingOnNotFound(t *testing.T) {
	rec := requestIsolatedServer(t, webserver.ServerConfig{}, http.MethodGet, "/api/v1/definitely-missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Server-Timing"); !serverTimingPattern.MatchString(got) {
		t.Fatalf("unexpected Server-Timing %q on 404", got)
	}
}

func TestServerTimingExposedByCORS(t *testing.T) {
	rec := requestIsolatedServer(t, webserver.ServerConfig{CORS: true}, http.MethodGet, "/healthz",
		map[string]string{"Origin": "http://example.com"})

	exposed := rec.Header().Get("Access-Control-Expose-Headers")
	found := false
	for _, name := range strings.Split(exposed, ",") {
		if strings.EqualFold(strings.TrimSpace(name), "Server-Timing") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Server-Timing is not exposed to cross-origin callers, got %q", exposed)
	}
}

func TestServerTimingNotExposedWithoutCORS(t *testing.T) {
	rec := requestIsolatedServer(t, webserver.ServerConfig{}, http.MethodGet, "/healthz",
		map[string]string{"Origin": "http://example.com"})
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "" {
		t.Fatalf("expected no expose header without CORS, got %q", got)
	}
}
