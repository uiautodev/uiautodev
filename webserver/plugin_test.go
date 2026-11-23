package webserver

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LoadPluginsFromDir is a test helper function that creates a temporary PluginManager
// and loads plugins from the specified directory
func LoadPluginsFromDir(pluginsDir string) ([]PluginInfo, []error) {
	pm := &PluginManager{
		plugins: make(map[string]*PluginInfo),
	}
	errors := pm.LoadFromDir(pluginsDir)
	plugins := pm.List()
	return plugins, errors
}

func TestLoadPluginsFromDir(t *testing.T) {
	pluginsDir := "testdata/plugins"

	// Load plugins
	plugins, errors := LoadPluginsFromDir(pluginsDir)

	// Assertions - should load 7 plugins (valid-plugin, incomplete-plugin, no-name-plugin, invalid-plugin-no-index, android-only-plugin, ios-only-plugin, multi-platform-plugin)
	assert.Equal(t, 7, len(plugins), "should load 7 plugins")

	// Check errors - only 1 error (bad-json)
	assert.Equal(t, 1, len(errors), "should have 1 error for plugin with invalid json")

	// Find plugins by name since order may vary
	pluginsMap := make(map[string]*PluginInfo)
	for _, plugin := range plugins {
		name := plugin.Manifest.Name
		p := plugin
		pluginsMap[name] = &p
	}

	validPlugin := pluginsMap["valid-plugin"]
	incompletePlugin := pluginsMap["incomplete-plugin"]
	noNamePlugin := pluginsMap["no-name-plugin"]
	invalidPluginNoIndex := pluginsMap["invalid-plugin-no-index"]

	// Check valid plugin exists
	assert.NotNil(t, validPlugin, "valid-plugin should be found")
	assert.Equal(t, "valid-plugin", validPlugin.Manifest.Name)
	assert.Equal(t, "1.0.0", validPlugin.Manifest.Version)
	// Src uses the plugin ID (directory basename)
	assert.Contains(t, validPlugin.Src, "/plugins/")
	assert.Contains(t, validPlugin.Src, "/index.html")
	assert.Equal(t, "valid-plugin", validPlugin.ID, "ID should be the directory basename")
	assert.Equal(t, "/plugins/"+validPlugin.ID+"/index.html", validPlugin.Src)

	// Check incomplete plugin - Name should default to directory name
	assert.NotNil(t, incompletePlugin, "incomplete-plugin should be found")
	assert.Equal(t, "incomplete-plugin", incompletePlugin.Manifest.Name)
	assert.Equal(t, "", incompletePlugin.Manifest.Version)
	assert.Contains(t, incompletePlugin.Src, "/plugins/")
	assert.Contains(t, incompletePlugin.Src, "/index.html")
	assert.Equal(t, "/plugins/"+incompletePlugin.ID+"/index.html", incompletePlugin.Src)

	// Check no-name plugin - Name should default to directory name
	assert.NotNil(t, noNamePlugin, "no-name-plugin should be found")
	assert.Equal(t, "no-name-plugin", noNamePlugin.Manifest.Name)
	assert.Equal(t, "", noNamePlugin.Manifest.Version)
	assert.Contains(t, noNamePlugin.Src, "/plugins/")
	assert.Contains(t, noNamePlugin.Src, "/index.html")

	// Check invalid-plugin-no-index - should load since it has a valid plugin.json
	assert.NotNil(t, invalidPluginNoIndex, "invalid-plugin-no-index should be found")
	assert.Equal(t, "invalid-plugin-no-index", invalidPluginNoIndex.Manifest.Name)
	assert.Equal(t, "1.0.0", invalidPluginNoIndex.Manifest.Version)
	assert.Contains(t, invalidPluginNoIndex.Src, "/plugins/")
	assert.Contains(t, invalidPluginNoIndex.Src, "/index.html")

	// Check errors contain expected messages
	assert.Contains(t, errors[0].Error(), "bad-json-plugin")
	assert.Contains(t, errors[0].Error(), "invalid plugin.json")
}

func TestLoadPluginsFromNonExistentDir(t *testing.T) {
	plugins, errors := LoadPluginsFromDir("/non/existent/directory")

	assert.Equal(t, 0, len(plugins))
	assert.Equal(t, 0, len(errors))
}

func TestFilterPluginsByPlatform(t *testing.T) {
	plugins := []PluginInfo{
		{
			Manifest: PluginManifest{Name: "universal-plugin", Platforms: []string{}},
		},
		{
			Manifest: PluginManifest{Name: "android-plugin", Platforms: []string{"android"}},
		},
		{
			Manifest: PluginManifest{Name: "ios-plugin", Platforms: []string{"ios"}},
		},
		{
			Manifest: PluginManifest{Name: "multi-platform-plugin", Platforms: []string{"android", "ios"}},
		},
	}

	// Test no filter - should return all plugins
	allPlugins := FilterPluginsByPlatform(plugins, "")
	assert.Equal(t, 4, len(allPlugins), "should return all plugins when no platform filter")

	// Test Android filter
	androidPlugins := FilterPluginsByPlatform(plugins, "android")
	assert.Equal(t, 3, len(androidPlugins), "should return 3 plugins for Android")
	pluginNames := make(map[string]bool)
	for _, p := range androidPlugins {
		pluginNames[p.Manifest.Name] = true
	}
	assert.True(t, pluginNames["universal-plugin"], "universal plugin should be available on Android")
	assert.True(t, pluginNames["android-plugin"], "android-only plugin should be available on Android")
	assert.True(t, pluginNames["multi-platform-plugin"], "multi-platform plugin should be available on Android")
	assert.False(t, pluginNames["ios-plugin"], "ios-only plugin should NOT be available on Android")

	// Test iOS filter
	iosPlugins := FilterPluginsByPlatform(plugins, "ios")
	assert.Equal(t, 3, len(iosPlugins), "should return 3 plugins for iOS")
	pluginNames = make(map[string]bool)
	for _, p := range iosPlugins {
		pluginNames[p.Manifest.Name] = true
	}
	assert.True(t, pluginNames["universal-plugin"], "universal plugin should be available on iOS")
	assert.True(t, pluginNames["ios-plugin"], "ios-only plugin should be available on iOS")
	assert.True(t, pluginNames["multi-platform-plugin"], "multi-platform plugin should be available on iOS")
	assert.False(t, pluginNames["android-plugin"], "android-only plugin should NOT be available on iOS")
}

func TestLoadPluginsFromSymlinkDir(t *testing.T) {
	// Create a temp directory structure:
	//   tmpDir/
	//     real-plugin/
	//       plugin.json
	//     plugins/           <- scanned directory
	//       linked-plugin -> ../real-plugin  (symlink)

	tmpDir := t.TempDir()

	realDir := filepath.Join(tmpDir, "real-plugin")
	err := os.MkdirAll(realDir, 0755)
	assert.NoError(t, err)

	pluginJSON := `{"name": "symlinked-plugin", "version": "1.0.0"}`
	err = os.WriteFile(filepath.Join(realDir, "plugin.json"), []byte(pluginJSON), 0644)
	assert.NoError(t, err)

	pluginsDir := filepath.Join(tmpDir, "plugins")
	err = os.MkdirAll(pluginsDir, 0755)
	assert.NoError(t, err)

	symlinkPath := filepath.Join(pluginsDir, "linked-plugin")
	err = os.Symlink(realDir, symlinkPath)
	if err != nil {
		t.Skipf("symlink not supported on this platform/filesystem: %v", err)
	}

	// Load plugins from the plugins directory
	plugins, errs := LoadPluginsFromDir(pluginsDir)
	assert.Empty(t, errs)

	// The loaded plugin should use the symlink path for ID
	pm := &PluginManager{plugins: make(map[string]*PluginInfo)}
	expectedID := pm.generateID(symlinkPath)
	assert.Equal(t, expectedID, plugins[0].ID)
	assert.Equal(t, "symlinked-plugin", plugins[0].Manifest.Name)
	assert.Equal(t, "1.0.0", plugins[0].Manifest.Version)
}

// newTestPluginManager creates a PluginManager with an isolated config file.
func newTestPluginManager(t *testing.T) *PluginManager {
	t.Helper()
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, "plugins_config.json"))
	pm := &PluginManager{
		plugins: make(map[string]*PluginInfo),
		state:   make(map[string]bool),
	}
	pm.loadState()
	return pm
}

func writeTestZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	assert.NoError(t, err)
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	for name, content := range files {
		entry, err := w.Create(name)
		assert.NoError(t, err)
		_, err = entry.Write([]byte(content))
		assert.NoError(t, err)
	}
}

func TestExtractPluginZip_NoTopLevelDir(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "myplugin.zip")
	writeTestZip(t, zipPath, map[string]string{
		"plugin.json": `{"name":"myplugin","version":"1.0.0"}`,
		"index.html":  "<html></html>",
	})

	name, dir, err := extractPluginZip(zipPath, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "myplugin", name)
	assert.Equal(t, filepath.Join(userDir, "myplugin"), dir)
	assert.FileExists(t, filepath.Join(dir, "plugin.json"))
	assert.FileExists(t, filepath.Join(dir, "index.html"))
}

func TestExtractPluginZip_WithTopLevelDir(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "myplugin.zip")
	// No id: the zip's top-level directory name wins over the manifest name.
	writeTestZip(t, zipPath, map[string]string{
		"myplugin/plugin.json": `{"name":"other-name","version":"1.0.0"}`,
		"myplugin/index.html":  "<html></html>",
	})

	name, dir, err := extractPluginZip(zipPath, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "myplugin", name, "top-level zip dir name should be used")
	assert.Equal(t, filepath.Join(userDir, "myplugin"), dir)
	assert.FileExists(t, filepath.Join(dir, "plugin.json"))
	assert.FileExists(t, filepath.Join(dir, "index.html"))
}

func TestExtractPluginZip_WithTopLevelDirAndNoName(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "wrapped.zip")
	// No id and no name in the manifest: fall back to the top-level dir name.
	writeTestZip(t, zipPath, map[string]string{
		"myplugin/plugin.json": `{"version":"1.0.0"}`,
	})

	name, dir, err := extractPluginZip(zipPath, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "myplugin", name)
	assert.Equal(t, filepath.Join(userDir, "myplugin"), dir)
}

func TestDetectZipRootDir(t *testing.T) {
	mk := func(names ...string) []*zip.File {
		files := make([]*zip.File, 0, len(names))
		for _, name := range names {
			files = append(files, &zip.File{FileHeader: zip.FileHeader{Name: name}})
		}
		return files
	}

	assert.Equal(t, "myplugin/", detectZipRootDir(mk("myplugin/", "myplugin/plugin.json", "myplugin/index.html")))
	assert.Equal(t, "", detectZipRootDir(mk("plugin.json", "index.html")), "flat archive has no root dir")
	assert.Equal(t, "", detectZipRootDir(mk("a/plugin.json", "b/plugin.json")), "differing top dirs are not a root")
	assert.Equal(t, "", detectZipRootDir(mk("myplugin/plugin.json", "other.txt")), "root must wrap every entry")
	assert.Equal(t, "", detectZipRootDir(nil))
}

func TestExtractPluginZip_ZipSlipRejected(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "evil.zip")
	writeTestZip(t, zipPath, map[string]string{
		"plugin.json":    `{"name":"evil","version":"1.0.0"}`,
		"../../evil.txt": "pwned",
	})

	_, _, err := extractPluginZip(zipPath, userDir)
	assert.Error(t, err)
	// The partially extracted target dir should be cleaned up.
	assert.NoDirExists(t, filepath.Join(userDir, "evil"))
}

func TestExtractPluginZip_NotZip(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "notzip.txt")
	_, _, err := extractPluginZip(zipPath, userDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "only .zip files")
}

func TestExtractPluginZip_DirNameFromManifest(t *testing.T) {
	userDir := t.TempDir()
	// The archive filename must not influence the target directory name.
	zipPath := filepath.Join(userDir, "archive-20260811.zip")
	writeTestZip(t, zipPath, map[string]string{
		"plugin.json": `{"name":"myplugin","version":"1.0.0"}`,
		"index.html":  "<html></html>",
	})

	name, dir, err := extractPluginZip(zipPath, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "myplugin", name)
	assert.Equal(t, filepath.Join(userDir, "myplugin"), dir)
	assert.FileExists(t, filepath.Join(dir, "index.html"))
}

func TestExtractPluginZip_DirNamePrefersID(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "whatever.zip")
	writeTestZip(t, zipPath, map[string]string{
		"plugin.json": `{"id":"cool-plugin","name":"My Cool Plugin","version":"1.0.0"}`,
		"index.html":  "<html></html>",
	})

	name, dir, err := extractPluginZip(zipPath, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "cool-plugin", name, "id should take priority over name")
	assert.Equal(t, filepath.Join(userDir, "cool-plugin"), dir)
	assert.FileExists(t, filepath.Join(dir, "index.html"))
}

func TestExtractPluginZip_DirNameSanitized(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "spaces.zip")
	writeTestZip(t, zipPath, map[string]string{
		"plugin.json": `{"name":"My Plugin v1","version":"1.0.0"}`,
	})
	name, dir, err := extractPluginZip(zipPath, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "My Plugin v1", name)
	assert.Equal(t, filepath.Join(userDir, "My Plugin v1"), dir)

	// Illegal path characters are replaced with dashes.
	zipPath2 := filepath.Join(userDir, "illegal.zip")
	writeTestZip(t, zipPath2, map[string]string{
		"plugin.json": `{"name":"a/b\\c:d","version":"1.0.0"}`,
	})
	name2, dir2, err := extractPluginZip(zipPath2, userDir)
	assert.NoError(t, err)
	assert.Equal(t, "a-b-c-d", name2)
	assert.Equal(t, filepath.Join(userDir, "a-b-c-d"), dir2)
}

func TestExtractPluginZip_MissingIDAndNameRejected(t *testing.T) {
	userDir := t.TempDir()
	zipPath := filepath.Join(userDir, "noname.zip")
	// No top-level dir, no id, no name: nothing to name the plugin directory.
	writeTestZip(t, zipPath, map[string]string{
		"plugin.json": `{"version":"1.0.0"}`,
	})

	_, _, err := extractPluginZip(zipPath, userDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid plugin name")
}

func TestPluginEnabledStatePersistence(t *testing.T) {
	pm := newTestPluginManager(t)
	pm.setEnabled("myplugin-1234", false)
	assert.False(t, pm.isEnabled("myplugin-1234"))

	// Simulate a fresh manager loading the same state file.
	pm2 := &PluginManager{
		plugins: make(map[string]*PluginInfo),
		state:   make(map[string]bool),
	}
	pm2.loadState()
	assert.False(t, pm2.isEnabled("myplugin-1234"))
	assert.True(t, pm2.isEnabled("unknown"), "missing IDs default to enabled")
}

func TestPluginSetEnabled(t *testing.T) {
	pm := newTestPluginManager(t)
	pm.setEnabled("plugin-a", true)
	assert.True(t, pm.isEnabled("plugin-a"))
	pm.setEnabled("plugin-a", false)
	assert.False(t, pm.isEnabled("plugin-a"))
}

func TestPluginRecordUnmarshalLegacyBool(t *testing.T) {
	var records map[string]PluginRecord
	require.NoError(t, json.Unmarshal([]byte(`{"a":false,"b":true}`), &records))
	require.NotNil(t, records["a"].Enabled)
	assert.False(t, *records["a"].Enabled)
	require.NotNil(t, records["b"].Enabled)
	assert.True(t, *records["b"].Enabled)
}

func TestSaveStatePreservesInstallMetadata(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })

	installedAt := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, recordPluginInstall("plugin-a", PluginRecord{
		Version:     "1.2.0",
		Source:      defaultPluginMarketURL,
		Sha256:      "abc",
		InstalledAt: &installedAt,
	}))

	pm := &PluginManager{plugins: make(map[string]*PluginInfo), state: make(map[string]bool)}
	pm.setEnabled("plugin-a", false)

	records, err := getPluginRecords()
	require.NoError(t, err)
	rec := records["plugin-a"]
	require.NotNil(t, rec.Enabled)
	assert.False(t, *rec.Enabled)
	assert.Equal(t, "1.2.0", rec.Version)
	assert.Equal(t, defaultPluginMarketURL, rec.Source)
	assert.Equal(t, "abc", rec.Sha256)
	require.NotNil(t, rec.InstalledAt)
	assert.True(t, installedAt.Equal(*rec.InstalledAt))
}

func TestRecordPluginInstallPreservesEnabled(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })

	pm := &PluginManager{plugins: make(map[string]*PluginInfo), state: make(map[string]bool)}
	pm.setEnabled("plugin-a", false)

	require.NoError(t, recordPluginInstall("plugin-a", PluginRecord{Version: "2.0.0"}))

	records, err := getPluginRecords()
	require.NoError(t, err)
	rec := records["plugin-a"]
	require.NotNil(t, rec.Enabled)
	assert.False(t, *rec.Enabled)
	assert.Equal(t, "2.0.0", rec.Version)
}

func TestPluginUploadRecordsInstall(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	pluginDir := filepath.Join(dir, "plugins")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	SetConfigFile(filepath.Join(configDir, configFileName))
	SetUserPluginDir(pluginDir)
	t.Cleanup(func() {
		SetConfigFile("")
		SetUserPluginDir("")
	})

	zipBytes := buildZipBytes(t, map[string]string{
		"plugin.json": `{"id":"com.example.up","name":"Up","version":"3.1.0"}`,
		"index.html":  "<html></html>",
	})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "up.zip")
	require.NoError(t, err)
	_, err = fw.Write(zipBytes)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", &body)
	req.Header.Set(echo.HeaderContentType, mw.FormDataContentType())
	rec := httptest.NewRecorder()
	require.NoError(t, pluginInstallHandler(e.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	records, err := getPluginRecords()
	require.NoError(t, err)
	uploaded := records["com.example.up"]
	assert.Equal(t, "3.1.0", uploaded.Version)
	assert.Equal(t, pluginSourceUpload, uploaded.Source)
	sum := sha256.Sum256(zipBytes)
	assert.Equal(t, hex.EncodeToString(sum[:]), uploaded.Sha256)
	require.NotNil(t, uploaded.InstalledAt)
}

func TestGetPluginDirsPriorityOrder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, ".config", "uiautodev", "plugins")
	SetUserPluginDir(configDir)
	t.Cleanup(func() { SetUserPluginDir("") })

	dirs := getPluginDirs()
	assert.Len(t, dirs, 2)
	assert.Equal(t, configDir, dirs[0], "user config dir should have highest priority")
	wd, err := os.Getwd()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "plugins"), dirs[1], "builtin dir should have lowest priority")
}

func TestSetUserConfigDir(t *testing.T) {
	SetConfigFile("")
	root := t.TempDir()
	configDir := filepath.Join(root, "conf")
	t.Cleanup(func() { SetUserConfigDir("") })

	SetUserConfigDir(configDir)
	assert.Equal(t, configDir, GetUserConfigDir())
	assert.Equal(t, filepath.Join(configDir, "plugins"), GetUserPluginDir())
	assert.Equal(t, filepath.Join(configDir, configFileName), getConfigFile())
}

func TestGetPluginDirsWithUserDirOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)

	customDir := filepath.Join(root, "server1-plugins")
	SetUserPluginDir(customDir)
	t.Cleanup(func() { SetUserPluginDir("") })

	dirs := getPluginDirs()
	assert.Len(t, dirs, 2)
	assert.Equal(t, customDir, dirs[0], "overridden user dir should have the highest priority")
	wd, err := os.Getwd()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "plugins"), dirs[1], "builtin dir should have lowest priority")
}

func TestGetPluginDirsExcludesLegacyDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	SetUserPluginDir(filepath.Join(root, "user-plugins"))
	t.Cleanup(func() { SetUserPluginDir("") })

	legacyDir := filepath.Join(root, ".uiautodev", "plugins")
	for _, dir := range getPluginDirs() {
		assert.NotEqual(t, legacyDir, dir, "the legacy ~/.uiautodev/plugins dir should not be scanned")
	}
}

func TestLoadAllUserDirOverridePriority(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	customDir := filepath.Join(root, "server1-plugins")
	SetUserPluginDir(customDir)
	t.Cleanup(func() { SetUserPluginDir("") })

	// Same plugin name in the builtin dir and the user dir: the user dir wins.
	writeTestPlugin(t, filepath.Join(customDir, "foo"), "foo", "2.0.0")
	writeTestPlugin(t, filepath.Join(root, ".uiautodev", "plugins", "foo"), "foo", "1.0.0")

	pm := &PluginManager{
		plugins: make(map[string]*PluginInfo),
		state:   make(map[string]bool),
	}
	pm.LoadAll(getPluginDirs())
	plugins := pm.List()
	assert.Len(t, plugins, 1, "the legacy dir is not scanned, so only one foo is loaded")
	assert.Equal(t, "2.0.0", plugins[0].Manifest.Version, "overridden user dir plugin should win")
	assert.False(t, plugins[0].Builtin, "overridden user dir plugin should be user-installable")
}

// writeTestPlugin writes a minimal plugin.json into dir.
func writeTestPlugin(t *testing.T, dir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"name":%q,"version":%q}`, name, version)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllDedupSameNameByPriority(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, ".config", "uiautodev", "plugins")
	lowerDir := filepath.Join(root, "lower-priority-plugins")
	SetUserPluginDir(configDir)
	t.Cleanup(func() { SetUserPluginDir("") })

	// Same plugin name in both dirs: the config dir (higher priority) should win.
	writeTestPlugin(t, filepath.Join(configDir, "foo"), "foo", "1.0.0")
	writeTestPlugin(t, filepath.Join(lowerDir, "foo"), "foo", "2.0.0")
	// Only present in the lower-priority dir: should be kept.
	writeTestPlugin(t, filepath.Join(lowerDir, "bar"), "bar", "1.0.0")

	pm := &PluginManager{
		plugins: make(map[string]*PluginInfo),
		state:   make(map[string]bool),
	}
	pm.LoadAll([]string{configDir, lowerDir})
	plugins := pm.List()
	assert.Len(t, plugins, 2, "same-name plugin should be deduplicated")

	byName := make(map[string]PluginInfo, len(plugins))
	for _, p := range plugins {
		byName[p.Manifest.Name] = p
	}
	foo, ok := byName["foo"]
	assert.True(t, ok, "foo should still exist")
	assert.Equal(t, "1.0.0", foo.Manifest.Version, "higher-priority config dir plugin should win")
	assert.True(t, strings.HasPrefix(foo.Dir, configDir+string(filepath.Separator)))

	bar, ok := byName["bar"]
	assert.True(t, ok, "bar should be kept")
	assert.True(t, strings.HasPrefix(bar.Dir, lowerDir+string(filepath.Separator)))
}

// TestLoadAllSameManifestIDCoexist verifies that two plugins sharing the same
// manifest id but in different directories coexist, since the plugin key is the
// directory basename.
func TestLoadAllSameManifestIDCoexist(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, ".config", "uiautodev", "plugins")
	SetUserPluginDir(configDir)
	t.Cleanup(func() { SetUserPluginDir("") })

	for _, name := range []string{"analyzer-a", "analyzer-b"} {
		dir := filepath.Join(configDir, name)
		assert.NoError(t, os.MkdirAll(dir, 0755))
		assert.NoError(t, os.WriteFile(
			filepath.Join(dir, "plugin.json"),
			[]byte(`{"id":"shared-id","name":"analyzer","version":"1.0.0"}`),
			0644,
		))
	}

	pm := &PluginManager{
		plugins: make(map[string]*PluginInfo),
		state:   make(map[string]bool),
	}
	pm.LoadAll([]string{configDir})
	plugins := pm.List()
	assert.Len(t, plugins, 2, "same manifest id in different dirs should coexist")

	ids := map[string]bool{}
	for _, p := range plugins {
		ids[p.ID] = true
	}
	assert.True(t, ids["analyzer-a"], "dir basename should be the plugin ID")
	assert.True(t, ids["analyzer-b"], "dir basename should be the plugin ID")
}

func TestLoadAllMarksBuiltinAndEnabled(t *testing.T) {
	userDir := t.TempDir()
	SetConfigFile(filepath.Join(userDir, "plugins_config.json"))

	// Create a user plugin dir.
	userPluginDir := filepath.Join(userDir, "user-plugins")
	SetUserPluginDir(userPluginDir)
	t.Cleanup(func() { SetUserPluginDir("") })
	assert.NoError(t, os.MkdirAll(filepath.Join(userPluginDir, "u1"), 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(userPluginDir, "u1", "plugin.json"), []byte(`{"name":"u1"}`), 0644))

	pm := &PluginManager{
		plugins: make(map[string]*PluginInfo),
		state:   make(map[string]bool),
	}
	pm.loadState()
	pm.LoadAll([]string{userPluginDir})
	plugins := pm.List()
	assert.Equal(t, 1, len(plugins))
	assert.False(t, plugins[0].Builtin, "user plugin should not be builtin")
	assert.True(t, plugins[0].Enabled, "default enabled")
}

func TestPluginPasswordSuggestionAndSave(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	// Before setup no password is configured.
	set, err := passwordSet()
	assert.NoError(t, err)
	assert.False(t, set)

	// Suggestions are random, non-empty and NOT persisted.
	s1, err := generatePasswordSuggestion()
	assert.NoError(t, err)
	assert.Len(t, s1, 6, "3 random bytes encoded as 6 hex chars")
	s2, err := generatePasswordSuggestion()
	assert.NoError(t, err)
	assert.NotEqual(t, s1, s2)

	set, err = passwordSet()
	assert.NoError(t, err)
	assert.False(t, set, "suggestion must not be persisted")

	// Empty passwords are rejected.
	assert.ErrorIs(t, saveNewPassword(""), errEmptyPassword)
	assert.ErrorIs(t, saveNewPassword("   "), errEmptyPassword)

	// Saving a password makes it verifiable.
	assert.NoError(t, saveNewPassword(s1))
	set, err = passwordSet()
	assert.NoError(t, err)
	assert.True(t, set)

	ok, err := verifyPassword(s1)
	assert.NoError(t, err)
	assert.True(t, ok)
	ok, err = verifyPassword("wrong")
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestConfigMigrationFromLegacyFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	SetConfigFile("")
	t.Cleanup(func() { SetConfigFile("") })

	configDir := filepath.Join(root, ".config", "uiautodev")
	preferred := filepath.Join(configDir, configFileName)
	legacy := filepath.Join(configDir, legacyConfigFileName)

	// Seed an old-format file carrying both password and plugin state.
	assert.NoError(t, os.MkdirAll(configDir, 0755))
	assert.NoError(t, os.WriteFile(legacy, []byte(`{"password":"secret","plugins":{"a":false}}`), 0600)) // pragma: allowlist secret

	// Reading picks up the legacy file and reports it until migrated.
	cfg, err := loadConfig()
	assert.NoError(t, err)
	assert.Equal(t, "secret", cfg.Password)
	require.NotNil(t, cfg.Plugins["a"].Enabled)
	assert.False(t, *cfg.Plugins["a"].Enabled)
	assert.Equal(t, legacy, getConfigFile(), "legacy path should be reported until migrated")

	// Any save migrates to config.json and drops the legacy file.
	assert.NoError(t, saveConfig(cfg))
	assert.FileExists(t, preferred)
	assert.NoFileExists(t, legacy)
	assert.Equal(t, preferred, getConfigFile())

	// Data survives the migration.
	cfg2, err := loadConfig()
	assert.NoError(t, err)
	assert.Equal(t, "secret", cfg2.Password)
	require.NotNil(t, cfg2.Plugins["a"].Enabled)
	assert.False(t, *cfg2.Plugins["a"].Enabled)
}

func TestPluginSessionManager(t *testing.T) {
	sm := &pluginSessionManager{sessions: make(map[string]time.Time)}
	token := sm.issue()
	assert.NotEmpty(t, token)
	assert.True(t, sm.valid(token))
	assert.False(t, sm.valid("bogus"))
	assert.False(t, sm.valid(""))

	// Expired token should be invalid.
	sm.mu.Lock()
	sm.sessions[token] = time.Now().Add(-time.Second)
	sm.mu.Unlock()
	assert.False(t, sm.valid(token))
}

func TestPluginAuthStatusSetupAndLoginHandlers(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	// Status handler: not initialized, no persistence, returns a suggestion.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/auth", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthStatusHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			Initialized       bool   `json:"initialized"`
			AuthRequired      bool   `json:"auth_required"`
			SuggestedPassword string `json:"suggested_password"`
			ConfigFile        string `json:"config_file"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Data.Initialized)
	assert.True(t, resp.Data.AuthRequired)
	assert.Len(t, resp.Data.SuggestedPassword, 6)
	assert.Equal(t, filepath.Join(dir, configFileName), resp.Data.ConfigFile)

	// Setup with an empty password is rejected.
	body, _ := json.Marshal(map[string]string{"password": ""})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/auth/setup", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthSetupHandler(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Setup persists the password and issues a token.
	body, _ = json.Marshal(map[string]string{"password": "my-own-pass"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/auth/setup", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthSetupHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &loginResp))
	assert.NotEmpty(t, loginResp.Data.Token)
	assert.True(t, globalSessionManager.valid(loginResp.Data.Token))

	// Status now reports initialized and no longer exposes a suggestion.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/auth", nil)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthStatusHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp2 struct {
		Data struct {
			Initialized       bool   `json:"initialized"`
			AuthRequired      bool   `json:"auth_required"`
			SuggestedPassword string `json:"suggested_password"`
			ConfigFile        string `json:"config_file"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp2))
	assert.True(t, resp2.Data.Initialized)
	assert.Empty(t, resp2.Data.SuggestedPassword)

	// Setup again is rejected with 409.
	body, _ = json.Marshal(map[string]string{"password": "another"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/auth/setup", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthSetupHandler(c))
	assert.Equal(t, http.StatusConflict, rec.Code)

	// Login with the configured password works.
	body, _ = json.Marshal(map[string]string{"password": "my-own-pass"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/auth", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthLoginHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Login with a wrong password is rejected.
	body, _ = json.Marshal(map[string]string{"password": "nope"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/auth", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, managementAuthLoginHandler(c))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireManagementAuthMiddleware(t *testing.T) {
	handler := requireManagementAuth(func(c echo.Context) error {
		return c.JSON(http.StatusOK, APIResponse{Code: 0, Message: "ok"})
	})

	// No token -> 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	assert.NoError(t, handler(c))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Valid token -> 200.
	token := globalSessionManager.issue()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, handler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
}
