package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerTestPlugin adds a plugin to the global PluginManager for testing.
// The returned function removes the plugin when called.
func registerTestPlugin(id string, manifest PluginManifest) func() {
	pm := GetPluginManager()
	pm.mutex.Lock()
	pm.plugins[id] = &PluginInfo{
		ID:       id,
		Manifest: manifest,
	}
	pm.mutex.Unlock()
	return func() {
		pm.mutex.Lock()
		delete(pm.plugins, id)
		pm.mutex.Unlock()
	}
}

// newProxyEcho returns an Echo instance with the proxyRequestHandler registered.
func newProxyEcho() *echo.Echo {
	e := echo.New()
	e.Any("/proxy/request/*", proxyRequestHandler)
	return e
}

func TestIsHostAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowedHosts []string
		targetHost   string
		want         bool
	}{
		{
			name:         "empty allowed list",
			allowedHosts: []string{},
			targetHost:   "https://example.com",
			want:         false,
		},
		{
			name:         "exact match with scheme",
			allowedHosts: []string{"https://example.com"},
			targetHost:   "https://example.com",
			want:         true,
		},
		{
			name:         "host-only pattern matches any scheme",
			allowedHosts: []string{"example.com"},
			targetHost:   "https://example.com",
			want:         true,
		},
		{
			name:         "wildcard subdomain matches",
			allowedHosts: []string{"*.example.com"},
			targetHost:   "https://sub.example.com",
			want:         true,
		},
		{
			name:         "wildcard subdomain does not match apex",
			allowedHosts: []string{"*.example.com"},
			targetHost:   "https://example.com",
			want:         false,
		},
		{
			name:         "scheme mismatch",
			allowedHosts: []string{"http://example.com"},
			targetHost:   "https://example.com",
			want:         false,
		},
		{
			name:         "different host",
			allowedHosts: []string{"https://other.com"},
			targetHost:   "https://example.com",
			want:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHostAllowed(tt.allowedHosts, tt.targetHost)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProxyRequestHandler_MissingTargetHost(t *testing.T) {
	e := newProxyEcho()
	req := httptest.NewRequest(http.MethodGet, "/proxy/request/path", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 400, resp.Code)
	assert.Contains(t, resp.Message, "X-Target-Host")
}

func TestProxyRequestHandler_InvalidTargetHost(t *testing.T) {
	e := newProxyEcho()

	cases := []struct {
		name   string
		header string
	}{
		{"no scheme", "example.com"},
		{"file scheme", "file:///etc/passwd"},
		{"with path", "https://example.com/path"},
		{"with query", "https://example.com?q=1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/proxy/request/path", nil)
			req.Header.Set("X-Target-Host", tc.header)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestProxyRequestHandler_MissingPluginID(t *testing.T) {
	e := newProxyEcho()
	req := httptest.NewRequest(http.MethodGet, "/proxy/request/path", nil)
	req.Header.Set("X-Target-Host", "https://example.com")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 403, resp.Code)
	assert.Contains(t, resp.Message, "X-Plugin-ID")
}

func TestProxyRequestHandler_PluginNotFound(t *testing.T) {
	e := newProxyEcho()
	req := httptest.NewRequest(http.MethodGet, "/proxy/request/path", nil)
	req.Header.Set("X-Target-Host", "https://example.com")
	req.Header.Set("X-Plugin-ID", "nonexistent-plugin-id")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 403, resp.Code)
	assert.Contains(t, resp.Message, "plugin not found")
}

func TestProxyRequestHandler_HostNotAllowed(t *testing.T) {
	cleanup := registerTestPlugin("test-proxy-plugin-forbidden", PluginManifest{
		Name:         "test-proxy-plugin",
		AllowedHosts: []string{"https://allowed.example.com"},
	})
	defer cleanup()

	e := newProxyEcho()
	req := httptest.NewRequest(http.MethodGet, "/proxy/request/path", nil)
	req.Header.Set("X-Target-Host", "https://blocked.example.com")
	req.Header.Set("X-Plugin-ID", "test-proxy-plugin-forbidden")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 403, resp.Code)
	assert.Contains(t, resp.Message, "not allowed")
}

func TestProxyRequestHandler_ForwardsAndStripsControlHeaders(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cleanup := registerTestPlugin("test-proxy-plugin-ok", PluginManifest{
		Name:         "test-proxy-plugin",
		AllowedHosts: []string{upstream.URL},
	})
	defer cleanup()

	e := newProxyEcho()
	req := httptest.NewRequest(http.MethodGet, "/proxy/request/api/data", nil)
	req.Header.Set("X-Target-Host", upstream.URL)
	req.Header.Set("X-Plugin-ID", "test-proxy-plugin-ok")
	req.Header.Set("Cookie", "session=secret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, receivedHeaders.Get("X-Target-Host"), "X-Target-Host must not be forwarded to upstream")
	assert.Empty(t, receivedHeaders.Get("X-Plugin-ID"), "X-Plugin-ID must not be forwarded to upstream")
	assert.Empty(t, receivedHeaders.Get("Cookie"), "Cookie must not be forwarded to upstream")
}
