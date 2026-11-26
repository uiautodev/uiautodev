package cluster

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAppInstallRequest(t *testing.T) {
	assert.True(t, isAppInstallRequest(http.MethodPost, "/api/v1/devices/c-1/apps"))
	assert.False(t, isAppInstallRequest(http.MethodGet, "/api/v1/devices/c-1/apps"))
	assert.False(t, isAppInstallRequest(http.MethodPost, "/api/v1/devices/c-1/apps/com.x/start"))
	assert.False(t, isAppInstallRequest(http.MethodPost, "/api/v1/devices/c-1/actions/tap"))
}

func TestProxyTransportGivesAppInstallsALongerHeaderTimeout(t *testing.T) {
	master := &Master{}
	timeoutOf := func(method, path string) time.Duration {
		transport, ok := master.proxyTransport("s1", SessionControl, false, method, path).(*http.Transport)
		require.True(t, ok)
		return transport.ResponseHeaderTimeout
	}

	assert.Equal(t, appInstallProxyTimeout, timeoutOf(http.MethodPost, "/api/v1/devices/c-1/apps"))
	assert.Equal(t, slaveResponseHeaderTimeout, timeoutOf(http.MethodGet, "/api/v1/devices/c-1/apps"))
	assert.Equal(t, slaveResponseHeaderTimeout, timeoutOf(http.MethodPost, "/api/v1/devices/c-1/actions/tap"))
	// The installers bound an install at ten minutes, so the proxy must wait at
	// least that long for the slave's response.
	assert.GreaterOrEqual(t, appInstallProxyTimeout, 10*time.Minute)
}

func TestProxyTransportClonesDirectClientForAppInstalls(t *testing.T) {
	shared := &http.Transport{ResponseHeaderTimeout: slaveResponseHeaderTimeout}
	master := &Master{slaves: map[string]*slaveState{
		"s1": {
			record:       SlaveRecord{ID: "s1", Mode: SlaveModeDirect, Direct: &DirectConfig{URL: "http://slave.example.com"}},
			directClient: &http.Client{Transport: shared},
		},
	}}

	install, ok := master.proxyTransport("s1", SessionControl, true, http.MethodPost, "/api/v1/devices/c-1/apps").(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, appInstallProxyTimeout, install.ResponseHeaderTimeout)
	assert.NotSame(t, shared, install)

	list, ok := master.proxyTransport("s1", SessionControl, true, http.MethodGet, "/api/v1/devices/c-1/apps").(*http.Transport)
	require.True(t, ok)
	assert.Same(t, shared, list)
}

// TestProxyAppliesInstallTimeoutOnlyToAppUploads proves the proxy really uses
// the install timeout: with it shortened, a slow app upload times out while the
// same slow response on another route still succeeds with the default timeout.
func TestProxyAppliesInstallTimeoutOnlyToAppUploads(t *testing.T) {
	slave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer slave.Close()

	master := NewMaster(newMemoryStore(), MasterConfig{Version: "test"})
	defer master.Close()
	record := SlaveRecord{ID: "s9", Name: "slow", Mode: SlaveModeDirect, Direct: &DirectConfig{URL: slave.URL}}
	master.mu.Lock()
	master.slaves["s9"] = &slaveState{record: record, directOnline: true, directClient: newDirectClient(record)}
	master.mu.Unlock()

	original := appInstallProxyTimeout
	appInstallProxyTimeout = 50 * time.Millisecond
	t.Cleanup(func() { appInstallProxyTimeout = original })

	e := echo.New()
	e.Use(master.DeviceRouteMiddleware())
	notProxied := func(c echo.Context) error { return c.NoContent(http.StatusTeapot) }
	e.POST("/api/v1/devices/:device_id/apps", notProxied)
	e.POST("/api/v1/devices/:device_id/actions/tap", notProxied)
	server := httptest.NewServer(e)
	defer server.Close()

	status := func(path string) int {
		resp, err := http.Post(server.URL+path, "application/octet-stream", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	globalID := GlobalDeviceID("s9", "a-1")

	assert.Equal(t, http.StatusBadGateway, status("/api/v1/devices/"+globalID+"/apps"))
	assert.Equal(t, http.StatusOK, status("/api/v1/devices/"+globalID+"/actions/tap"))
}
