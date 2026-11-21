package webserver

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost":   true,
		"LOCALHOST":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"10.0.0.1":    false,
		"example.com": false,
		"":            false,
	}
	for host, want := range cases {
		assert.Equal(t, want, isLoopbackHost(host), "host %q", host)
	}
}

func TestValidateMarketSourceURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"empty resets default", "", "", false},
		{"default url", defaultPluginMarketURL, defaultPluginMarketURL, false},
		{"default url trailing slash", defaultPluginMarketURL + "/", defaultPluginMarketURL, false},
		{"custom https", "https://example.com", "https://example.com", false},
		{"custom https trailing slash", "https://example.com/", "https://example.com", false},
		{"custom http rejected", "http://example.com", "", true},
		{"loopback http", "http://localhost:8080", "http://localhost:8080", false},
		{"loopback ip", "http://127.0.0.1:1234", "http://127.0.0.1:1234", false},
		{"bad scheme", "ftp://example.com", "", true},
		{"missing host", "not a url", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateMarketSourceURL(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveMarketSource(t *testing.T) {
	_, _ = setupMarketTest(t)

	source, err := resolveMarketSource()
	assert.NoError(t, err)
	assert.Equal(t, defaultPluginMarketURL, source)

	require.NoError(t, setMarketSource("https://mirror.example.com"))
	source, err = resolveMarketSource()
	assert.NoError(t, err)
	assert.Equal(t, "https://mirror.example.com", source)
}

func TestMergeMarketItems(t *testing.T) {
	_, pluginDir := setupMarketTest(t)

	id := "com.example.merge"
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, id), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, id, "plugin.json"),
		[]byte(`{"id":"`+id+`","name":"Merge","version":"1.0.0"}`),
		0644,
	))
	pm := GetPluginManager()
	pm.LoadAll(getPluginDirs())
	require.NoError(t, recordPluginInstall(id, PluginRecord{Version: "1.0.0", Source: defaultPluginMarketURL}))

	items := mergeMarketItems([]PluginSummary{{ID: id, LatestVersion: "1.2.0"}})
	require.Len(t, items, 1)
	assert.True(t, items[0].Installed)
	assert.Equal(t, "1.0.0", items[0].InstalledVersion)
	assert.True(t, items[0].UpdateAvailable)

	// Unknown plugin is not installed.
	items = mergeMarketItems([]PluginSummary{{ID: "com.example.absent", LatestVersion: "1.0.0"}})
	require.Len(t, items, 1)
	assert.False(t, items[0].Installed)
	assert.False(t, items[0].UpdateAvailable)
}

func TestIsNewerVersion(t *testing.T) {
	assert.True(t, isNewerVersion("1.2.0", "1.0.0"))
	assert.False(t, isNewerVersion("1.0.0", "1.0.0"))
	assert.False(t, isNewerVersion("1.0.0", "1.2.0"))
	assert.False(t, isNewerVersion("", "1.0.0"))
	assert.False(t, isNewerVersion("1.0.0", ""))
}

func TestMarketListHandlerAlwaysFetches(t *testing.T) {
	_, _ = setupMarketTest(t)

	reg := newFakeRegistry(t,
		[]PluginSummary{{ID: "com.example.foo", Name: "Foo", LatestVersion: "1.0.0"}},
		nil, nil,
	)
	require.NoError(t, setMarketSource(reg.URL))
	handler := marketListHandler()

	rec := callMarketHandler(t, handler, http.MethodGet, "/api/v1/market/plugins", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "com.example.foo")
	assert.Equal(t, 1, reg.listCalls)

	// No local cache: every request hits the registry.
	callMarketHandler(t, handler, http.MethodGet, "/api/v1/market/plugins", nil)
	assert.Equal(t, 2, reg.listCalls)
}

func TestLoadAllIgnoresDotDirs(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, filepath.Join(dir, "foo"), "foo", "1.0.0")
	// Install backups are dot-prefixed and must not be loaded as plugins.
	writeTestPlugin(t, filepath.Join(dir, ".foo.bak-123"), "foo", "1.0.0")

	pm := &PluginManager{plugins: make(map[string]*PluginInfo), state: make(map[string]bool)}
	pm.LoadAll([]string{dir})

	plugins := pm.List()
	require.Len(t, plugins, 1)
	assert.Equal(t, "foo", plugins[0].ID)
}

func TestUninstallClearsPluginRecord(t *testing.T) {
	_, pluginDir := setupMarketTest(t)

	id := "com.example.gone"
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, id), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, id, "plugin.json"),
		[]byte(`{"id":"`+id+`","name":"Gone","version":"1.0.0"}`), 0644))
	require.NoError(t, recordPluginInstall(id, PluginRecord{Version: "1.0.0", Source: defaultPluginMarketURL}))

	rec := callMarketHandler(t, pluginUninstallHandler, http.MethodDelete, "/api/v1/plugins/"+id, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	records, err := getPluginRecords()
	require.NoError(t, err)
	_, ok := records[id]
	assert.False(t, ok, "plugin record should be removed on uninstall")
}

func TestMarketInstallHandler(t *testing.T) {
	_, pluginDir := setupMarketTest(t)

	id := "com.example.foo"
	zipBytes := buildZipBytes(t, map[string]string{
		"plugin.json": `{"id":"` + id + `","name":"Foo","version":"1.0.0"}`,
		"index.html":  "<html></html>",
	})
	sum := sha256.Sum256(zipBytes)

	reg := newFakeRegistry(t,
		nil,
		map[string]PluginVersion{id: {
			Version:     "1.0.0",
			Sha256:      hex.EncodeToString(sum[:]),
			DownloadURL: "/zips/" + id + ".zip",
		}},
		map[string][]byte{id: zipBytes},
	)
	require.NoError(t, setMarketSource(reg.URL))

	handler := marketInstallHandler()
	rec := callMarketHandler(t, handler, http.MethodPost, "/api/v1/market/plugins/"+id+"/install",
		strings.NewReader(`{"version":"1.0.0"}`))

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "/api/v1/plugins/"+id, rec.Header().Get("Location"))
	assert.FileExists(t, filepath.Join(pluginDir, id, "plugin.json"))
	assert.FileExists(t, filepath.Join(pluginDir, id, "index.html"))

	records, err := getPluginRecords()
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", records[id].Version)
	assert.Equal(t, reg.URL, records[id].Source)

	// Re-installing the same version is a no-op and returns 200.
	rec = callMarketHandler(t, handler, http.MethodPost, "/api/v1/market/plugins/"+id+"/install", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMarketInstallChecksumMismatchKeepsOldVersion(t *testing.T) {
	_, pluginDir := setupMarketTest(t)

	id := "com.example.old"
	// Pre-existing installation that must survive a failed install.
	target := filepath.Join(pluginDir, id)
	require.NoError(t, os.MkdirAll(target, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "plugin.json"),
		[]byte(`{"id":"`+id+`","name":"Old","version":"0.9.0"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(target, "marker.txt"), []byte("keep"), 0644))

	zipBytes := buildZipBytes(t, map[string]string{
		"plugin.json": `{"id":"` + id + `","name":"New","version":"1.0.0"}`,
	})
	reg := newFakeRegistry(t, nil,
		map[string]PluginVersion{id: {
			Version:     "1.0.0",
			Sha256:      strings.Repeat("0", 64), // wrong checksum
			DownloadURL: "/zips/" + id + ".zip",
		}},
		map[string][]byte{id: zipBytes},
	)
	require.NoError(t, setMarketSource(reg.URL))

	handler := marketInstallHandler()
	rec := callMarketHandler(t, handler, http.MethodPost, "/api/v1/market/plugins/"+id+"/install", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.FileExists(t, filepath.Join(target, "marker.txt"), "old version must be preserved")
	manifest, err := readPluginManifest(target)
	require.NoError(t, err)
	assert.Equal(t, "0.9.0", manifest.Version)
}

func TestMarketInstallRejectsInvalidID(t *testing.T) {
	_, _ = setupMarketTest(t)
	handler := marketInstallHandler()

	for _, id := range []string{"Bad_ID", "UPPER", ".", "..", ".hidden"} {
		rec := callMarketHandler(t, handler, http.MethodPost,
			"/api/v1/market/plugins/"+id+"/install", nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "id %q", id)
	}
}

func TestMarketInstallRejectsIDMismatch(t *testing.T) {
	_, _ = setupMarketTest(t)

	requested := "com.example.foo"
	zipBytes := buildZipBytes(t, map[string]string{
		"plugin.json": `{"id":"com.example.other","name":"Other","version":"1.0.0"}`,
	})
	sum := sha256.Sum256(zipBytes)
	reg := newFakeRegistry(t, nil,
		map[string]PluginVersion{requested: {
			Version:     "1.0.0",
			Sha256:      hex.EncodeToString(sum[:]),
			DownloadURL: "/zips/" + requested + ".zip",
		}},
		map[string][]byte{requested: zipBytes},
	)
	require.NoError(t, setMarketSource(reg.URL))

	handler := marketInstallHandler()
	rec := callMarketHandler(t, handler, http.MethodPost, "/api/v1/market/plugins/"+requested+"/install", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "id mismatch")
}

func TestMarketInstallRejectsHighMinAppVersion(t *testing.T) {
	_, _ = setupMarketTest(t)

	id := "com.example.future"
	zipBytes := buildZipBytes(t, map[string]string{
		"plugin.json": `{"id":"` + id + `","name":"Future","version":"1.0.0"}`,
	})
	sum := sha256.Sum256(zipBytes)
	reg := newFakeRegistry(t, nil,
		map[string]PluginVersion{id: {
			Version:       "1.0.0",
			Sha256:        hex.EncodeToString(sum[:]),
			MinAppVersion: "999.0.0",
			DownloadURL:   "/zips/" + id + ".zip",
		}},
		map[string][]byte{id: zipBytes},
	)
	require.NoError(t, setMarketSource(reg.URL))

	handler := marketInstallHandler()
	rec := callMarketHandler(t, handler, http.MethodPost, "/api/v1/market/plugins/"+id+"/install", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "requires app version")
}

func TestMarketSourceHandlers(t *testing.T) {
	_, _ = setupMarketTest(t)

	// GET returns the default source.
	rec := callMarketHandler(t, marketSourceGetHandler(), http.MethodGet, "/api/v1/market/source", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var info MarketSourceInfo
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &info))
	assert.Equal(t, defaultPluginMarketURL, info.URL)
	assert.Equal(t, defaultPluginMarketURL, info.DefaultURL)
	assert.False(t, info.IsCustom)

	// PUT a custom https source.
	rec = callMarketHandler(t, marketSourcePutHandler(), http.MethodPut, "/api/v1/market/source",
		strings.NewReader(`{"url":"https://mirror.example.com"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &info))
	assert.Equal(t, "https://mirror.example.com", info.URL)
	assert.True(t, info.IsCustom)

	// PUT a loopback source.
	rec = callMarketHandler(t, marketSourcePutHandler(), http.MethodPut, "/api/v1/market/source",
		strings.NewReader(`{"url":"http://127.0.0.1:8080"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &info))
	assert.Equal(t, "http://127.0.0.1:8080", info.URL)

	// PUT the official source is always allowed.
	rec = callMarketHandler(t, marketSourcePutHandler(), http.MethodPut, "/api/v1/market/source",
		strings.NewReader(`{"url":"`+defaultPluginMarketURL+`"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &info))
	assert.Equal(t, defaultPluginMarketURL, info.URL)
	assert.False(t, info.IsCustom)

	// Empty url resets to the default.
	rec = callMarketHandler(t, marketSourcePutHandler(), http.MethodPut, "/api/v1/market/source",
		strings.NewReader(`{"url":""}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &info))
	assert.Equal(t, defaultPluginMarketURL, info.URL)
	assert.False(t, info.IsCustom)
}

// --- test helpers ---

// setupMarketTest isolates config and plugin state for market tests.
func setupMarketTest(t *testing.T) (configDir, pluginDir string) {
	t.Helper()
	dir := t.TempDir()
	configDir = filepath.Join(dir, "config")
	pluginDir = filepath.Join(dir, "plugins")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	SetConfigFile(filepath.Join(configDir, configFileName))
	SetUserPluginDir(pluginDir)

	t.Cleanup(func() {
		SetConfigFile("")
		SetUserPluginDir("")
	})
	return configDir, pluginDir
}

// callMarketHandler invokes an echo handler directly with optional path params.
func callMarketHandler(t *testing.T, h echo.HandlerFunc, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, target, body)
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Derive path params from the target: last segment is not enough, so callers
	// pass ids via the path and we set the conventional ":id" param.
	if id := marketIDFromPath(target); id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	require.NoError(t, h(c))
	return rec
}

func marketIDFromPath(target string) string {
	parts := strings.Split(strings.Trim(target, "/"), "/")
	for i, p := range parts {
		if p == "plugins" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// envelopeData extracts the "data" field from an APIResponse envelope body.
func envelopeData(t *testing.T, rec *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env.Data
}

// buildZipBytes builds an in-memory zip archive from a file map.
func buildZipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := w.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// fakeRegistry is a minimal in-process stand-in for the plugin registry.
type fakeRegistry struct {
	*httptest.Server
	summaries []PluginSummary
	versions  map[string]PluginVersion
	zips      map[string][]byte
	listCalls int
}

func newFakeRegistry(t *testing.T, summaries []PluginSummary, versions map[string]PluginVersion, zips map[string][]byte) *fakeRegistry {
	t.Helper()
	fr := &fakeRegistry{summaries: summaries, versions: versions, zips: zips}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/market/plugins", func(w http.ResponseWriter, r *http.Request) {
		fr.listCalls++
		writeMarketEnvelope(w, map[string]any{
			"total":     len(fr.summaries),
			"page":      1,
			"page_size": 20,
			"items":     fr.summaries,
		})
	})

	mux.HandleFunc("/api/v1/market/plugins/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/market/plugins/")
		parts := strings.Split(rest, "/")
		if len(parts) != 3 || parts[1] != "versions" {
			http.NotFound(w, r)
			return
		}
		pv, ok := fr.versions[parts[0]]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "message": "plugin not found"})
			return
		}
		writeMarketEnvelope(w, pv)
	})

	mux.HandleFunc("/zips/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/zips/"), ".zip")
		data, ok := fr.zips[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(data)
	})

	fr.Server = httptest.NewServer(mux)
	t.Cleanup(fr.Close)
	return fr
}

func writeMarketEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
}
