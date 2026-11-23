package webserver_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"github.com/uiautodev/uiautodev/webserver"
)

// freeAddr returns a loopback address with an OS-assigned free port so tests do
// not collide with a fixed port already in use.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

// waitForHealthz polls the health endpoint until the server is ready or the
// deadline is exceeded, instead of relying on a fixed sleep.
func waitForHealthz(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			return resp
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server not ready: %v", lastErr)
	return nil
}

func TestHealthz(t *testing.T) {
	addr := freeAddr(t)
	shutdown := webserver.StartHTTPServer(addr)
	defer shutdown()
	resp := waitForHealthz(t, fmt.Sprintf("http://%s/healthz", addr))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if ok, _ := m["ok"].(bool); !ok {
		t.Fatalf("expected ok=true got %v", m["ok"])
	}
}

func corsHeader(t *testing.T, cfg webserver.ServerConfig) string {
	t.Helper()
	e := webserver.CreateEchoServerWithConfig(cfg)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Header().Get("Access-Control-Allow-Origin")
}

func TestCORSEnabled(t *testing.T) {
	origin := corsHeader(t, webserver.ServerConfig{CORS: true})
	if origin != "http://example.com" {
		t.Fatalf("expected CORS origin to be echoed, got %q", origin)
	}
}

func TestCORSDisabled(t *testing.T) {
	origin := corsHeader(t, webserver.ServerConfig{CORS: false})
	if origin != "" {
		t.Fatalf("expected no CORS header when disabled, got %q", origin)
	}
}
