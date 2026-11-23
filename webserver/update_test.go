package webserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uiautodev/uiautodev/internal/update"
	"github.com/uiautodev/uiautodev/version"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupUpdateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	SetUserConfigDir(dir)
	t.Cleanup(func() {
		SetConfigFile("")
		SetUserConfigDir("")
	})
}

func setTestUpdateClient(t *testing.T, manifestURL string, desktop bool) {
	t.Helper()
	setUpdateClient(update.New(update.Options{
		ManifestURL: manifestURL,
		BaseURL:     manifestURL,
		DataDir:     t.TempDir(),
		Desktop:     desktop,
		GOOS:        "darwin",
		GOARCH:      "arm64",
		Install:     func(string, string) error { return nil },
		Quit:        func() {},
	}))
}

func updateContext(method, path string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestUpdateInfoHandler(t *testing.T) {
	setupUpdateConfig(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"99.0.0","notes":"hello","assets":[{"name":"UiautodevDesktop-mac-99.0.0.dmg","size":1,"sha256":"abc"}]}`))
	}))
	defer upstream.Close()
	setTestUpdateClient(t, upstream.URL, true)

	c, rec := updateContext(http.MethodGet, "/api/v1/update")
	require.NoError(t, updateInfoHandler()(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	data := envelopeData(t, rec)
	var info update.Info
	require.NoError(t, json.Unmarshal(data, &info))
	assert.Equal(t, "99.0.0", info.Latest)
	assert.True(t, info.Available)
	assert.True(t, info.Desktop)
}

func TestUpdateApplyRejectsServerMode(t *testing.T) {
	setupUpdateConfig(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"99.0.0","assets":[]}`))
	}))
	defer upstream.Close()
	setTestUpdateClient(t, upstream.URL, false)

	c, rec := updateContext(http.MethodPost, "/api/v1/update")
	require.NoError(t, updateApplyHandler()(c))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestUpdateApplyRejectsUpToDate(t *testing.T) {
	setupUpdateConfig(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(map[string]any{"version": version.Version})
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer upstream.Close()
	setTestUpdateClient(t, upstream.URL, true)

	c, rec := updateContext(http.MethodPost, "/api/v1/update")
	require.NoError(t, updateApplyHandler()(c))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestCheckForUpdate(t *testing.T) {
	setupUpdateConfig(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"99.0.0","assets":[]}`))
	}))
	defer upstream.Close()
	setTestUpdateClient(t, upstream.URL, false)

	info, err := CheckForUpdate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "99.0.0", info.Latest)
	assert.True(t, info.Available)
}

func TestUpdateStatusHandler(t *testing.T) {
	setupUpdateConfig(t)
	setTestUpdateClient(t, "https://example.invalid/updates/latest.json", true)

	c, rec := updateContext(http.MethodGet, "/api/v1/update/status")
	require.NoError(t, updateStatusHandler()(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var progress update.Progress
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &progress))
	assert.False(t, progress.Active)
	assert.Zero(t, progress.Percent)
}

func TestUpdateApplyRunsInBackground(t *testing.T) {
	setupUpdateConfig(t)
	body := "update payload"
	sum := sha256.Sum256([]byte(body))
	name := "UiautodevDesktop-mac-99.0.0.dmg"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/release.json") || strings.HasSuffix(r.URL.Path, "/latest.json") {
			_ = json.NewEncoder(w).Encode(update.Manifest{
				Version: "99.0.0",
				Assets:  []update.Asset{{Name: name, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}},
			})
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	setUpdateClient(update.New(update.Options{
		ManifestURL: upstream.URL + "/latest.json",
		BaseURL:     upstream.URL,
		DataDir:     t.TempDir(),
		Executable:  filepath.Join(t.TempDir(), "uiautodev-desktop"),
		Desktop:     true,
		GOOS:        "darwin",
		GOARCH:      "arm64",
		Install:     func(string, string) error { return nil },
		Relaunch:    func(string) error { return nil },
		Quit:        func() {},
	}))
	c, rec := updateContext(http.MethodPost, "/api/v1/update")
	require.NoError(t, updateApplyHandler()(c))
	assert.Equal(t, http.StatusAccepted, rec.Code)

	client, err := currentUpdateClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		progress := client.Progress()
		return !progress.Active && progress.Percent == 100 && progress.Error == ""
	}, 3*time.Second, 50*time.Millisecond)
	// Let the handler's restart delay finish so the apply mutex is released
	// before the next test starts.
	time.Sleep(1500 * time.Millisecond)
}
