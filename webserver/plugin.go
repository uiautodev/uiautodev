package webserver

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/codeskyblue/jsonc"
	"github.com/labstack/echo/v4"
)

//go:embed plugin_runtime.js
var pluginRuntimeJS []byte

//go:embed plugin_runtime.d.ts
var pluginRuntimeDTS []byte

//go:embed plugin.schema.json
var pluginSchemaJSON []byte

// getPluginDirs returns the directories plugins are loaded from, in priority
// order: the user plugin dir (highest), then the builtin dir next to the
// executable (lowest). Same-name plugins in higher-priority dirs shadow those
// in lower-priority dirs.
func getPluginDirs() []string {
	wd, _ := os.Getwd()
	return []string{GetUserPluginDir(), filepath.Join(wd, "plugins")}
}

// GetUserConfigDir returns the app config dir (~/.config/uiautodev), unless it
// has been overridden with SetUserConfigDir.
func GetUserConfigDir() string {
	if userConfigDirOverride != "" {
		return userConfigDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to get user home dir", "error", err)
		home = "~"
	}
	return filepath.Join(home, ".config", "uiautodev")
}

// SetUserConfigDir overrides the app config directory. It relocates config.json
// and the default plugin dir (config-dir/plugins) so that multiple servers on
// the same machine keep their state isolated. Passing an empty string restores
// the default (~/.config/uiautodev).
func SetUserConfigDir(dir string) {
	if dir == "" {
		userConfigDirOverride = ""
		return
	}
	userConfigDirOverride = filepath.Clean(dir)
}

// GetUserPluginDir returns the directory where user-installed plugins live. It
// defaults to <config-dir>/plugins and can be overridden with SetUserPluginDir
// (e.g. via the -plugin-dir server flag) so that multiple servers on the same
// machine keep their plugins isolated.
func GetUserPluginDir() string {
	if userPluginDirOverride != "" {
		return userPluginDirOverride
	}
	return filepath.Join(GetUserConfigDir(), "plugins")
}

// isUserPluginDir reports whether dir is located under the user plugin directory.
func isUserPluginDir(dir string) bool {
	return strings.HasPrefix(dir, GetUserPluginDir()+string(filepath.Separator))
}

// SetUserPluginDir overrides the user plugin directory, i.e. where plugins are
// installed and loaded from with the highest priority. Passing an empty string
// restores the default (<config-dir>/plugins).
func SetUserPluginDir(path string) {
	if path == "" {
		userPluginDirOverride = ""
		return
	}
	userPluginDirOverride = filepath.Clean(path)
}

var (
	userConfigDirOverride string
	userPluginDirOverride string
)

// DisplayMode represents how a plugin is displayed in the UI
type DisplayMode string

const (
	DisplayCard DisplayMode = "card" // 网格卡片模式（默认）
	DisplayTab  DisplayMode = "tab"  // 独立标签页模式
)

// PluginManifest represents the plugin.json of a plugin
//
//go:generate go run ../cmd/generate-plugin-schema plugin.go
type PluginManifest struct {
	ID           string      `json:"id,omitempty"`            // 插件唯一ID，zip安装时用作目录名，默认使用name
	Name         string      `json:"name,omitempty"`          // 插件名称，默认使用目录名
	Description  string      `json:"description,omitempty"`   // 插件描述
	Version      string      `json:"version,omitempty"`       // 版本号
	Homepage     string      `json:"homepage,omitempty"`      // 插件主页链接
	Main         string      `json:"main,omitempty"`          // 入口文件，默认 index.html
	Height       int         `json:"height,omitempty"`        // 插件窗口高度，默认自适应
	Platforms    []string    `json:"platforms,omitempty"`     // 支持的平台，空数组或不填表示全平台
	Display      DisplayMode `json:"display,omitempty"`       // 显示模式: "card"(默认,网格卡片) 或 "tab"(独立标签页)
	AllowedHosts []string    `json:"allowed_hosts,omitempty"` // 允许代理的目标host，支持通配符 *
}

// PluginInfo represents the plugin information returned to the client
type PluginInfo struct {
	ID       string         `json:"id"`  // Unique identifier for the plugin (dir basename)
	Dir      string         `json:"-"`   // Plugin directory (internal only)
	Src      string         `json:"src"` // https or /plugins/{id}/...
	Manifest PluginManifest `json:"manifest"`
	Enabled  bool           `json:"enabled"` // Whether the plugin is enabled
	Builtin  bool           `json:"builtin"` // Whether the plugin ships with the app (cannot be uninstalled)
}

// PluginManager manages plugin loading, registration, and file serving (singleton)
type PluginManager struct {
	plugins map[string]*PluginInfo // ID -> PluginInfo
	state   map[string]bool        // ID -> enabled (persisted)
	stateMu sync.Mutex
	mutex   sync.RWMutex
}

var globalPluginManager *PluginManager
var globalPluginManagerOnce sync.Once

// GetPluginManager returns the singleton PluginManager instance
func GetPluginManager() *PluginManager {
	globalPluginManagerOnce.Do(func() {
		globalPluginManager = &PluginManager{
			plugins: make(map[string]*PluginInfo),
			state:   make(map[string]bool),
		}
		globalPluginManager.loadState()
	})
	return globalPluginManager
}

// loadState loads the persisted enabled/disabled state from disk.
func (pm *PluginManager) loadState() {
	pm.stateMu.Lock()
	defer pm.stateMu.Unlock()
	cfg, err := loadConfig()
	if err != nil {
		slog.Warn("failed to read app config", "error", err)
		pm.state = make(map[string]bool)
		return
	}
	state := make(map[string]bool, len(cfg.Plugins))
	for id, rec := range cfg.Plugins {
		if rec.Enabled != nil {
			state[id] = *rec.Enabled
		}
	}
	pm.state = state
}

// saveState writes the enabled/disabled state to disk, preserving the install
// metadata recorded for each plugin.
func (pm *PluginManager) saveState() {
	pm.stateMu.Lock()
	states := make(map[string]bool, len(pm.state))
	for id, enabled := range pm.state {
		states[id] = enabled
	}
	pm.stateMu.Unlock()
	if err := setPluginEnabledStates(states); err != nil {
		slog.Warn("failed to write app config", "error", err)
	}
}

// isEnabled reports whether the plugin with the given ID is enabled.
// Plugins not present in the state map default to enabled.
func (pm *PluginManager) isEnabled(id string) bool {
	pm.stateMu.Lock()
	defer pm.stateMu.Unlock()
	enabled, ok := pm.state[id]
	if !ok {
		return true
	}
	return enabled
}

// setEnabled updates and persists the enabled state for a plugin ID.
func (pm *PluginManager) setEnabled(id string, enabled bool) {
	pm.stateMu.Lock()
	pm.state[id] = enabled
	pm.stateMu.Unlock()
	pm.saveState()
}

// removeState drops the in-memory enabled state for a plugin without persisting.
func (pm *PluginManager) removeState(id string) {
	pm.stateMu.Lock()
	delete(pm.state, id)
	pm.stateMu.Unlock()
}

// generateID returns the plugin ID, derived from the plugin directory basename.
// Using the directory name guarantees uniqueness within a parent directory, so
// plugins with the same manifest id but different directories do not collide.
func (pm *PluginManager) generateID(pluginDir string) string {
	return filepath.Base(pluginDir)
}

// genPluginSrc generates the source URL for a plugin
func (pm *PluginManager) genPluginSrc(pluginID, main string) string {
	if strings.HasPrefix(main, "http://") || strings.HasPrefix(main, "https://") {
		return main
	}
	main = strings.TrimPrefix(main, "/")
	if main == "" {
		main = "index.html"
	}
	return "/plugins/" + pluginID + "/" + main
}

// readPluginManifest reads and parses the plugin.json from a plugin directory,
// using the directory name as the fallback plugin name.
func readPluginManifest(pluginDir string) (*PluginManifest, error) {
	name := filepath.Base(pluginDir)
	data, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.json: %w", err)
	}
	var manifest PluginManifest
	if err := jsonc.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}
	if manifest.Name == "" {
		manifest.Name = name
	}
	if manifest.Main == "" {
		manifest.Main = "index.html"
	}
	switch manifest.Display {
	case "", DisplayCard:
		manifest.Display = DisplayCard
	case DisplayTab:
		// ok
	default:
		manifest.Display = DisplayCard
	}
	if manifest.Main != "" && !strings.HasPrefix(manifest.Main, "http://") && !strings.HasPrefix(manifest.Main, "https://") {
		if strings.Contains(manifest.Main, "..") {
			return nil, fmt.Errorf("invalid main path: %s", manifest.Main)
		}
	}
	return &manifest, nil
}

// resolveEntryDir checks if a directory entry is a directory or a symlink to one.
// Returns the entry path and true if it is a valid plugin directory.
// For symlinks, returns the symlink path (not the resolved target) so each symlink
// gets a distinct identity.
func resolveEntryDir(entry os.DirEntry, parentDir string) (string, bool) {
	// Dot-prefixed entries are internal bookkeeping (e.g. install backups), not
	// plugins.
	if strings.HasPrefix(entry.Name(), ".") {
		return "", false
	}
	if entry.IsDir() {
		return filepath.Join(parentDir, entry.Name()), true
	}
	if entry.Type()&os.ModeSymlink != 0 {
		path := filepath.Join(parentDir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			return "", false
		}
		if info.IsDir() {
			return path, true
		}
	}
	return "", false
}

// isPluginDir reports whether dir is itself a plugin directory, i.e. it
// directly contains a plugin.json.
func isPluginDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "plugin.json"))
	return err == nil && !info.IsDir()
}

// loadPlugin reads the manifest from pluginDir and registers it, returning the
// plugin ID on success.
func (pm *PluginManager) loadPlugin(pluginDir string) (string, error) {
	manifest, err := readPluginManifest(pluginDir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(pluginDir), err)
	}

	// The plugin ID is the directory basename, which serves as the priority
	// override key: same-basename plugins in different directories are
	// deduplicated.
	pluginID := pm.generateID(pluginDir)

	pm.mutex.Lock()
	pm.plugins[pluginID] = &PluginInfo{
		ID:       pluginID,
		Dir:      pluginDir,
		Src:      pm.genPluginSrc(pluginID, manifest.Main),
		Manifest: *manifest,
		Enabled:  pm.isEnabled(pluginID),
	}
	pm.mutex.Unlock()

	slog.Info("loaded plugin", "name", manifest.Name, "version", manifest.Version, "id", pluginID)
	return pluginID, nil
}

// LoadFromDir loads all plugins from the specified directory. If the directory
// is itself a plugin (contains plugin.json), it is loaded directly.
func (pm *PluginManager) LoadFromDir(pluginsDir string) []error {
	var errors []error

	if isPluginDir(pluginsDir) {
		if _, err := pm.loadPlugin(pluginsDir); err != nil {
			slog.Warn("failed to load plugin", "dir", pluginsDir, "error", err)
			return append(errors, err)
		}
		return errors
	}

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("plugins directory not found", "dir", pluginsDir)
			return errors
		}
		slog.Error("failed to read plugins directory", "dir", pluginsDir, "error", err)
		return append(errors, fmt.Errorf("failed to read plugins directory: %w", err))
	}

	for _, entry := range entries {
		pluginPath, ok := resolveEntryDir(entry, pluginsDir)
		if !ok {
			continue
		}
		if _, err := pm.loadPlugin(pluginPath); err != nil {
			slog.Warn("failed to read plugin.json", "plugin", filepath.Base(pluginPath), "error", err)
			errors = append(errors, err)
		}
	}

	return errors
}

// LoadAll loads plugins from multiple directories, removing plugins that no
// longer exist. pluginDirs is in priority order (highest first). Plugin IDs are
// directory basenames, so a same-ID plugin in different directories is
// deduplicated: dirs are loaded from the lowest priority first, letting the
// highest-priority directory overwrite in the plugin map.
func (pm *PluginManager) LoadAll(pluginDirs []string) []error {
	// Track found plugins to detect removed ones later
	foundPlugins := make(map[string]bool)

	var allErrors []error

	for i := len(pluginDirs) - 1; i >= 0; i-- {
		dir := pluginDirs[i]
		errs := pm.LoadFromDir(dir)
		allErrors = append(allErrors, errs...)

		// Mark plugins in this directory as found. When the directory is itself
		// a plugin, only that single plugin counts.
		if isPluginDir(dir) {
			foundPlugins[pm.generateID(dir)] = true
			continue
		}
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if pluginPath, ok := resolveEntryDir(entry, dir); ok {
				foundPlugins[pm.generateID(pluginPath)] = true
			}
		}
	}

	// Remove plugins that were not found
	pm.mutex.Lock()
	var removedCount int
	for id := range pm.plugins {
		if !foundPlugins[id] {
			delete(pm.plugins, id)
			removedCount++
		}
	}

	// Mark plugins under any user dir as user-installed (uninstallable);
	// everything else is considered builtin.
	for _, dir := range pluginDirs {
		dirPrefix := dir + string(filepath.Separator)
		for id := range pm.plugins {
			p := pm.plugins[id]
			if strings.HasPrefix(p.Dir, dirPrefix) {
				p.Builtin = !isUserPluginDir(p.Dir)
			}
		}
	}
	pm.mutex.Unlock()

	for _, err := range allErrors {
		slog.Warn("plugin loading error", "error", err)
	}

	slog.Info("plugins loaded", "count", len(pm.plugins), "removed", removedCount)
	return allErrors
}

// List returns all loaded plugins
func (pm *PluginManager) List() []PluginInfo {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	result := make([]PluginInfo, 0, len(pm.plugins))
	for _, p := range pm.plugins {
		result = append(result, *p)
	}
	return result
}

// GetDir retrieves the directory for a given plugin ID
func (pm *PluginManager) GetDir(id string) (string, bool) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	p, ok := pm.plugins[id]
	if !ok {
		return "", false
	}
	return p.Dir, true
}

// GetPlugin retrieves a plugin by ID
func (pm *PluginManager) GetPlugin(id string) (*PluginInfo, bool) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	p, ok := pm.plugins[id]
	return p, ok
}

// pluginInjectedCSS is injected into plugin HTML files before </head>
const pluginInjectedCSS = `<style></style>`

// serveRuntimeJS serves the plugin runtime JavaScript with embedded plugin info
func (pm *PluginManager) serveRuntimeJS(c echo.Context, pluginID string) error {
	p, ok := pm.GetPlugin(pluginID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "plugin not found")
	}

	manifestJSON, _ := json.Marshal(p.Manifest)
	js := fmt.Sprintf("window.__plugin_id__=%q;\nwindow.__plugin__=%s;\n%s", pluginID, manifestJSON, pluginRuntimeJS)

	c.Response().Header().Set("Cache-Control", "no-cache")
	return c.Blob(http.StatusOK, "application/javascript", []byte(js))
}

// serveHTML reads an HTML file, injects plugin CSS and JS, and serves it
func serveHTML(c echo.Context, filePath, pluginID string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	s := string(content)

	scriptTag := fmt.Sprintf(`<script src="/plugins/%s/__runtime.js"></script>`, pluginID)

	// Inject CSS before </head>
	if i := strings.Index(s, "</head>"); i != -1 {
		s = s[:i] + pluginInjectedCSS + scriptTag + s[i:]
	} else {
		s = pluginInjectedCSS + scriptTag + s
	}

	c.Response().Header().Set("Cache-Control", "no-cache")
	return c.Blob(http.StatusOK, "text/html; charset=utf-8", []byte(s))
}

// FileHandler returns an echo.HandlerFunc that serves plugin files.
// Route pattern: /plugins/:id/*
func (pm *PluginManager) FileHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		pluginID := c.Param("id")
		filePath := c.Param("*")

		dir, ok := pm.GetDir(pluginID)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "plugin not found")
		}
		pluginInfo, ok := pm.GetPlugin(pluginID)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "plugin not found")
		}

		// No file specified, serve main file with injected CSS/JS
		if filePath == "" || filePath == "/" {
			return serveHTML(c, filepath.Join(dir, pluginInfo.Manifest.Main), pluginID)
		}

		// Security check: ensure the file is within the plugin directory
		absPath, err := filepath.Abs(filepath.Join(dir, filePath))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid file path")
		}
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "server error")
		}
		if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) && absPath != absDir {
			return echo.NewHTTPError(http.StatusForbidden, "access denied")
		}

		slog.Debug("plugin file request", "pluginID", pluginID, "filePath", filePath, "absPath", absPath)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "file not found")
		}
		if filePath == pluginInfo.Manifest.Main {
			return serveHTML(c, absPath, pluginID)
		}
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.File(absPath)
	}
}

// serverRuntimeDTS serves the plugin runtime TypeScript declarations
func serveRuntimeDTS(c echo.Context) error {
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", pluginRuntimeDTS)
}

func servePluginSchema(c echo.Context) error {
	return c.Blob(http.StatusOK, "application/json; charset=utf-8", pluginSchemaJSON)
}
func FilterPluginsByPlatform(plugins []PluginInfo, platform string) []PluginInfo {
	if platform == "" {
		return plugins
	}

	var filtered []PluginInfo
	for _, plugin := range plugins {
		if len(plugin.Manifest.Platforms) == 0 || slices.Contains(plugin.Manifest.Platforms, platform) {
			filtered = append(filtered, plugin)
		}
	}
	return filtered
}

// pluginsHandler handles the /api/v1/plugins endpoint
func pluginsHandler(c echo.Context) error {
	platform := c.QueryParam("platform")
	enabledFilter := c.QueryParam("enabled")

	pm := GetPluginManager()

	// Load plugins from multiple directories (only loads new plugins)
	pm.LoadAll(getPluginDirs())

	allPlugins := pm.List()
	filteredPlugins := FilterPluginsByPlatform(allPlugins, platform)
	if enabledFilter == "enabled" {
		var enabled []PluginInfo
		for _, p := range filteredPlugins {
			if p.Enabled {
				enabled = append(enabled, p)
			}
		}
		filteredPlugins = enabled
	}
	slices.SortFunc(filteredPlugins, func(a, b PluginInfo) int {
		return strings.Compare(a.Manifest.Name, b.Manifest.Name)
	})

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    filteredPlugins,
	}, "  ")
}

type pluginSetEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// pluginSetEnabledHandler handles PUT /api/v1/plugins/:id
func pluginSetEnabledHandler(c echo.Context) error {
	pluginID := c.Param("id")
	var req pluginSetEnabledRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	pm := GetPluginManager()
	pm.LoadAll(getPluginDirs())
	if _, ok := pm.GetPlugin(pluginID); !ok {
		return c.JSONPretty(http.StatusNotFound, APIResponse{
			Code:    404,
			Message: "plugin not found",
		}, "  ")
	}
	pm.setEnabled(pluginID, req.Enabled)
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"id": pluginID, "enabled": req.Enabled},
	}, "  ")
}

// pluginUninstallHandler handles DELETE /api/v1/plugins/:id
func pluginUninstallHandler(c echo.Context) error {
	pluginID := c.Param("id")
	pm := GetPluginManager()
	pm.LoadAll(getPluginDirs())

	p, ok := pm.GetPlugin(pluginID)
	if !ok {
		return c.JSONPretty(http.StatusNotFound, APIResponse{
			Code:    404,
			Message: "plugin not found",
		}, "  ")
	}
	if p.Builtin {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "cannot uninstall a builtin plugin",
		}, "  ")
	}
	if err := os.RemoveAll(p.Dir); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to remove plugin directory: " + err.Error(),
		}, "  ")
	}
	pm.mutex.Lock()
	delete(pm.plugins, pluginID)
	pm.mutex.Unlock()
	pm.removeState(pluginID)
	if err := deletePluginRecord(pluginID); err != nil {
		slog.Warn("failed to remove plugin record", "id", pluginID, "error", err)
	}
	slog.Info("uninstalled plugin", "id", pluginID, "dir", p.Dir)
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
	}, "  ")
}

// pluginInstallHandler handles POST /api/v1/plugins (multipart zip upload)
func pluginInstallHandler(c echo.Context) error {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "missing file field: " + err.Error(),
		}, "  ")
	}
	if fileHeader.Size > pluginZipMaxSize {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "plugin archive too large",
		}, "  ")
	}

	// Save the uploaded file to a temp location for extraction.
	tmp, err := os.CreateTemp("", "uiautodev-plugin-*.zip")
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to create temp file: " + err.Error(),
		}, "  ")
	}
	defer os.Remove(tmp.Name())

	src, err := fileHeader.Open()
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "failed to open upload: " + err.Error(),
		}, "  ")
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), src); err != nil {
		src.Close()
		tmp.Close()
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to save upload: " + err.Error(),
		}, "  ")
	}
	src.Close()
	tmp.Close()

	userDir := GetUserPluginDir()
	pluginName, pluginDir, err := extractPluginZip(tmp.Name(), userDir)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "failed to install plugin: " + err.Error(),
		}, "  ")
	}

	// Validate the extracted plugin
	manifest, err := readPluginManifest(pluginDir)
	if err != nil {
		os.RemoveAll(pluginDir)
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid plugin: " + err.Error(),
		}, "  ")
	}

	pm := GetPluginManager()
	pm.LoadAll(getPluginDirs())

	pluginID := manifest.ID
	if pluginID == "" {
		pluginID = pluginName
	}
	now := time.Now()
	if err := recordPluginInstall(pluginID, PluginRecord{
		Version:     manifest.Version,
		Source:      pluginSourceUpload,
		Sha256:      hex.EncodeToString(hasher.Sum(nil)),
		InstalledAt: &now,
	}); err != nil {
		slog.Warn("failed to record plugin install", "id", pluginID, "error", err)
	}

	slog.Info("installed plugin", "name", pluginName, "dir", pluginDir)
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]string{"name": pluginName},
	}, "  ")
}
