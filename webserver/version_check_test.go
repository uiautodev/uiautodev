package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uiautodev/uiautodev/version"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupVersionCheckConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })
}

func newVersionCheckContext() (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version/latest", nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestVersionLatestHandlerUpdateAvailable(t *testing.T) {
	setupVersionCheckConfig(t)

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"99.0.0","files":[]}`))
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	c, rec := newVersionCheckContext()
	require.NoError(t, versionLatestHandler()(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	data := envelopeData(t, rec)
	var result VersionCheckResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, version.Version, result.CurrentVersion)
	assert.Equal(t, "99.0.0", result.LatestVersion)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestVersionLatestHandlerUpToDate(t *testing.T) {
	setupVersionCheckConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"` + version.Version + `"}`))
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	c, rec := newVersionCheckContext()
	require.NoError(t, versionLatestHandler()(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	var result VersionCheckResult
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &result))
	assert.Equal(t, version.Version, result.LatestVersion)
	assert.False(t, result.UpdateAvailable)
}

func TestVersionLatestHandlerCachesWithinTTL(t *testing.T) {
	setupVersionCheckConfig(t)

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"version":"99.0.0"}`))
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	for i := 0; i < 3; i++ {
		c, rec := newVersionCheckContext()
		require.NoError(t, versionLatestHandler()(c))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "upstream should be queried only once within TTL")
}

func TestVersionLatestHandlerFallsBackToCache(t *testing.T) {
	setupVersionCheckConfig(t)

	// An expired cache entry: the refresh is attempted and fails, so the stale
	// value must still be served instead of erroring out.
	stale := time.Now().Add(-2 * versionCheckTTL)
	require.NoError(t, setVersionCheck(VersionCheckRecord{LastCheckAt: &stale, LatestVersion: "1.2.3"}))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	c, rec := newVersionCheckContext()
	require.NoError(t, versionLatestHandler()(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	var result VersionCheckResult
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &result))
	assert.Equal(t, "1.2.3", result.LatestVersion)
}

func TestVersionLatestHandlerUpstreamError(t *testing.T) {
	setupVersionCheckConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	c, rec := newVersionCheckContext()
	require.NoError(t, versionLatestHandler()(c))
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestVersionLatestHandlerOutageDoesNotRetryWithinTTL(t *testing.T) {
	setupVersionCheckConfig(t)

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	for i := 0; i < 3; i++ {
		c, rec := newVersionCheckContext()
		require.NoError(t, versionLatestHandler()(c))
		assert.Equal(t, http.StatusBadGateway, rec.Code)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "a failed refresh must be recorded so later requests do not retry within TTL")
}

func TestVersionLatestHandlerOutageServesCacheWithoutRetry(t *testing.T) {
	setupVersionCheckConfig(t)

	stale := time.Now().Add(-2 * versionCheckTTL)
	require.NoError(t, setVersionCheck(VersionCheckRecord{LastCheckAt: &stale, LatestVersion: "1.2.3"}))

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	prev := versionCheckURL
	versionCheckURL = upstream.URL
	t.Cleanup(func() { versionCheckURL = prev })

	for i := 0; i < 3; i++ {
		c, rec := newVersionCheckContext()
		require.NoError(t, versionLatestHandler()(c))
		assert.Equal(t, http.StatusOK, rec.Code)
		var result VersionCheckResult
		require.NoError(t, json.Unmarshal(envelopeData(t, rec), &result))
		assert.Equal(t, "1.2.3", result.LatestVersion)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "upstream should be attempted only once per TTL even while failing")
}
