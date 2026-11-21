package webserver

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryNodeExposesClusterAPI covers the desktop app's server config
// (trusted, no management password). Every node is a cluster master, so the
// settings page can always poll /api/v1/cluster/slaves; it used to fail with
// 404 when the desktop ran with cluster mode "off".
func TestEveryNodeExposesClusterAPI(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	e := CreateEchoServerWithConfig(ServerConfig{
		DisablePluginAuth: true,
		Audience:          "desktop",
	})
	t.Cleanup(func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// No slaves yet: the list is empty, not an error.
	rec := get("/api/v1/cluster/slaves")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"code":0,"message":"ok","data":[]}`, rec.Body.String())

	// The inventory endpoint used by upstream push-mode masters is always on.
	rec = get("/api/v1/cluster/devices")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDesktopClusterRoutesRequireAPIToken(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	e := CreateEchoServerWithConfig(ServerConfig{
		DisablePluginAuth: true,
		Audience:          "desktop",
		APIToken:          "desktop-secret",
	})
	t.Cleanup(func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	})

	do := func(method, path, token string) int {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set(apiTokenHeader, token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	// Reads stay available (the settings page polls before the token is set).
	assert.Equal(t, http.StatusOK, do(http.MethodGet, "/api/v1/cluster/slaves", ""))
	// Mutations require the per-process token.
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodDelete, "/api/v1/cluster/slaves/unknown", ""))
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodDelete, "/api/v1/cluster/slaves/unknown", "wrong"))
	assert.Equal(t, http.StatusNotFound, do(http.MethodDelete, "/api/v1/cluster/slaves/unknown", "desktop-secret"))
}

func TestCORSPreflightAllowsPatch(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	e := CreateEchoServerWithConfig(ServerConfig{
		CORS:              true,
		DisablePluginAuth: true,
		Audience:          "desktop",
		ClusterStore:      newMemClusterStore(),
	})
	t.Cleanup(func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/cluster/slaves/abc", nil)
	req.Header.Set(echo.HeaderOrigin, "http://wails.localhost")
	req.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodPatch)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.NotEmpty(t, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderAccessControlAllowMethods), http.MethodPatch)
}
