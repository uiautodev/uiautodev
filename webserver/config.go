package webserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/webserver/cluster"
)

// PluginRecord is the persisted per-plugin state: the enabled flag plus the
// install metadata (version / source / sha256 / installed_at). A nil Enabled
// means the plugin has no explicit state and defaults to enabled.
type PluginRecord struct {
	Enabled     *bool      `json:"enabled,omitempty"`
	Version     string     `json:"version,omitempty"`
	Source      string     `json:"source,omitempty"`
	Sha256      string     `json:"sha256,omitempty"`
	InstalledAt *time.Time `json:"installed_at,omitempty"`
}

// UnmarshalJSON accepts either the legacy bool form (e.g. {"a":false}) or the
// record object form so older config files keep loading after the migration.
func (r *PluginRecord) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && (data[0] == 't' || data[0] == 'f') {
		var enabled bool
		if err := json.Unmarshal(data, &enabled); err != nil {
			return err
		}
		r.Enabled = &enabled
		return nil
	}
	type alias PluginRecord
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = PluginRecord(a)
	return nil
}

// DeviceMeta holds user-defined metadata for a device, keyed by device id in
// appConfig. It is an object (not a bare string) so new fields can be added
// later without breaking existing configs.
type DeviceMeta struct {
	Remark string `json:"remark,omitempty"`
	// WDAPort and WDAMJPEGPort override the WebDriverAgent HTTP and MJPEG ports
	// for iOS devices. Zero means the default (8100 / 9100).
	WDAPort      int `json:"wda_port,omitempty"`
	WDAMJPEGPort int `json:"wda_mjpeg_port,omitempty"`
	// WDABundleID overrides the WebDriverAgent runner bundle id launched on iOS
	// devices. Empty means the default runner bundle id.
	WDABundleID string `json:"wda_bundle_id,omitempty"`
}

// VersionCheckRecord caches the last upstream version check so the app queries
// the release service at most once per day (across restarts). LastCheckAt is
// the time of the last attempt, recorded even when the refresh failed, so an
// outage does not trigger a retry on every request. LatestVersion is the last
// successfully fetched version and is left untouched by failed attempts.
type VersionCheckRecord struct {
	LastCheckAt   *time.Time `json:"last_check_at,omitempty"`
	LatestVersion string     `json:"latest_version,omitempty"`
}

// OpenAIConfig holds the OpenAI-compatible provider settings persisted to disk.
type OpenAIConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	// Proxy is an optional proxy URL (http, https, socks5 or socks5h) used for
	// provider requests. Empty means a direct connection.
	Proxy string `json:"proxy"`
}

// JevConfig holds the TypeSafe Jev (System One decision model) settings
// persisted to disk. Unlike OpenAI there is no versioned path convention, so
// the full decision endpoint is configured directly.
type JevConfig struct {
	// EndpointURL is the complete decision endpoint, e.g.
	// https://api.typesafe.ai/v1/systemone.
	EndpointURL string `json:"endpoint_url"`
	APIKey      string `json:"api_key"`
	Model       string `json:"model"`
	// Proxy is an optional proxy URL (http, https, socks5 or socks5h) used for
	// provider requests. Empty means a direct connection.
	Proxy string `json:"proxy"`
}

// appConfig is the application-level config persisted to disk. It holds the
// management password, the per-plugin records (enabled state plus install
// metadata) and the provider settings (OpenAI and Jev); further sections may be
// added here over time as the app grows.
type appConfig struct {
	Password           string                  `json:"password"`
	Plugins            map[string]PluginRecord `json:"plugins"`
	OpenAI             *OpenAIConfig           `json:"openai,omitempty"`
	Jev                *JevConfig              `json:"jev,omitempty"`
	PluginMarketSource string                  `json:"plugin_market_source,omitempty"`
	// JWTSecret is the base64-encoded signing key for JWT auth. It lives here so
	// it follows the same config dir as the rest of the app state (and honors
	// -config-dir isolation).
	JWTSecret string `json:"jwt_secret,omitempty"`
	// MCPToken is the long-lived, user-configurable token that guards /mcp/.
	// Empty means no MCP token: MCP then falls back to the login auth when
	// configured, or stays open otherwise.
	MCPToken string `json:"mcp_token,omitempty"`
	// DeviceMeta holds per-device user metadata (e.g. a remark) keyed by device id.
	DeviceMeta map[string]DeviceMeta `json:"device_meta,omitempty"`
	// VersionCheck caches the last upstream version check result.
	VersionCheck *VersionCheckRecord `json:"version_check,omitempty"`
	// Locale is the UI language selected in the app ("zh-CN" or "en-US"). Native
	// menus and dialogs use it so they match the web UI.
	Locale string `json:"locale,omitempty"`
	// Cluster holds master cluster state: registered slaves and adb relay
	// settings.
	Cluster *ClusterConfig `json:"cluster,omitempty"`
	// Access holds the identity and device presence settings.
	Access *AccessConfig `json:"access,omitempty"`
}

// ClusterConfig is the persisted master-side cluster configuration.
type ClusterConfig struct {
	Slaves   map[string]cluster.SlaveRecord `json:"slaves,omitempty"`
	ADBRelay *cluster.ADBRelayConfig        `json:"adb_relay,omitempty"`
	// NodeID is this installation's stable cluster identity. It is generated on
	// first use and lets upstream masters detect aggregation cycles.
	NodeID string `json:"node_id,omitempty"`
}

const (
	// configFileName is the preferred config file going forward.
	configFileName = "config.json"
	// legacyConfigFileName is where older versions stored the config.
	legacyConfigFileName = "plugins_config.json"
)

var (
	configMu   sync.Mutex
	configFile string // explicit config path override (for tests)
)

// SetConfigFile overrides the config file path (for tests). Passing an empty
// string restores the default resolution under the user config dir.
func SetConfigFile(path string) {
	configMu.Lock()
	defer configMu.Unlock()
	configFile = path
	ResetAccessConfigCache()
}

// defaultConfigPaths returns the preferred and legacy config paths under the
// user config dir.
func defaultConfigPaths() (preferred, legacy string) {
	dir := GetUserConfigDir()
	return filepath.Join(dir, configFileName), filepath.Join(dir, legacyConfigFileName)
}

// resolveConfigPathLocked returns the effective path to read from or surface to
// users: an explicit override wins; otherwise an existing config.json, then an
// existing legacy plugins_config.json, then config.json for fresh setups.
// Caller must hold configMu.
func resolveConfigPathLocked() string {
	if configFile != "" {
		return configFile
	}
	preferred, legacy := defaultConfigPaths()
	if fileExists(preferred) {
		return preferred
	}
	if fileExists(legacy) {
		return legacy
	}
	return preferred
}

// writeConfigPathLocked returns the path config is persisted to: an explicit
// override when set, otherwise config.json (the preferred file going forward).
// Caller must hold configMu.
func writeConfigPathLocked() string {
	if configFile != "" {
		return configFile
	}
	preferred, _ := defaultConfigPaths()
	return preferred
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// saveConfigLocked writes cfg to path. Caller must hold configMu. After a
// successful write to the preferred config.json, the legacy file is removed so
// existing setups migrate to the new file.
func saveConfigLocked(path string, cfg appConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	if configFile == "" {
		_, legacy := defaultConfigPaths()
		if legacy != path && fileExists(legacy) {
			if err := os.Remove(legacy); err != nil {
				slog.Warn("failed to remove legacy config file", "path", legacy, "error", err)
			}
		}
	}
	return nil
}

// loadConfigLocked reads and normalizes the app config. Caller must hold
// configMu.
func loadConfigLocked() (appConfig, error) {
	path := resolveConfigPathLocked()
	var cfg appConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return appConfig{Plugins: make(map[string]PluginRecord)}, nil
		}
		return appConfig{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return appConfig{}, err
	}
	if cfg.Plugins == nil {
		cfg.Plugins = make(map[string]PluginRecord)
	}
	return cfg, nil
}

// loadConfig reads the app config from disk. A missing file yields an empty
// config. If only the legacy file exists it is read until the next save
// migrates it to config.json.
func loadConfig() (appConfig, error) {
	configMu.Lock()
	defer configMu.Unlock()
	return loadConfigLocked()
}

// mutateConfig loads, mutates and persists the app config under a single lock so
// concurrent writers cannot lose each other's updates.
func mutateConfig(mutate func(cfg *appConfig)) error {
	configMu.Lock()
	defer configMu.Unlock()
	cfg, err := loadConfigLocked()
	if err != nil {
		return err
	}
	mutate(&cfg)
	return saveConfigLocked(writeConfigPathLocked(), cfg)
}

// saveConfig persists the app config to disk.
func saveConfig(cfg appConfig) error {
	configMu.Lock()
	defer configMu.Unlock()
	return saveConfigLocked(writeConfigPathLocked(), cfg)
}

// getConfigFile returns the effective config file path (where existing config
// currently lives, otherwise the preferred config.json). It is exposed so the
// UI can tell users where to edit settings such as the password.
func getConfigFile() string {
	configMu.Lock()
	defer configMu.Unlock()
	return resolveConfigPathLocked()
}

// loadOrCreateJWTSecret returns the JWT signing key, generating and persisting a
// new random one (base64-encoded) when the config has none.
func loadOrCreateJWTSecret() ([]byte, error) {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil {
		return nil, err
	}
	if cfg.JWTSecret != "" {
		secret, err := base64.StdEncoding.DecodeString(cfg.JWTSecret)
		if err == nil {
			return secret, nil
		}
		slog.Warn("invalid JWT secret in config, regenerating", "error", err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate jwt secret: %w", err)
	}
	cfg.JWTSecret = base64.StdEncoding.EncodeToString(secret)
	if err := saveConfigLocked(writeConfigPathLocked(), cfg); err != nil {
		return nil, fmt.Errorf("persist jwt secret: %w", err)
	}
	slog.Info("JWT secret generated and saved", "path", writeConfigPathLocked())
	return secret, nil
}

// loadOrCreateNodeID returns this node's stable cluster identity, generating and
// persisting a random one on first use. It is reported to upstream masters so
// nested device aggregation can detect cycles.
func loadOrCreateNodeID() (string, error) {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil {
		return "", err
	}
	if cfg.Cluster != nil && cfg.Cluster.NodeID != "" {
		return cfg.Cluster.NodeID, nil
	}

	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate node id: %w", err)
	}
	nodeID := hex.EncodeToString(buf)
	if cfg.Cluster == nil {
		cfg.Cluster = &ClusterConfig{}
	}
	cfg.Cluster.NodeID = nodeID
	if err := saveConfigLocked(writeConfigPathLocked(), cfg); err != nil {
		return "", fmt.Errorf("persist node id: %w", err)
	}
	return nodeID, nil
}

// getOpenAIConfig returns the persisted OpenAI provider settings. A nil section
// is treated as an empty configuration so callers can always read a value.
func getOpenAIConfig() (OpenAIConfig, error) {
	cfg, err := loadConfig()
	if err != nil {
		return OpenAIConfig{}, err
	}
	if cfg.OpenAI == nil {
		return OpenAIConfig{}, nil
	}
	return *cfg.OpenAI, nil
}

// updateOpenAIConfig persists the OpenAI provider settings to disk.
func updateOpenAIConfig(openAI OpenAIConfig) error {
	return mutateConfig(func(cfg *appConfig) {
		cfg.OpenAI = &openAI
	})
}

// getJevConfig returns the persisted Jev provider settings. A nil section is
// treated as an empty configuration so callers can always read a value.
func getJevConfig() (JevConfig, error) {
	cfg, err := loadConfig()
	if err != nil {
		return JevConfig{}, err
	}
	if cfg.Jev == nil {
		return JevConfig{}, nil
	}
	return *cfg.Jev, nil
}

// updateJevConfig persists the Jev provider settings to disk.
func updateJevConfig(jev JevConfig) error {
	return mutateConfig(func(cfg *appConfig) {
		cfg.Jev = &jev
	})
}

// getMarketSource returns the configured custom plugin market source, or an
// empty string when the default source is in use.
func getMarketSource() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	return cfg.PluginMarketSource, nil
}

// setMarketSource persists the plugin market source. An empty url restores the
// default source.
func setMarketSource(url string) error {
	return mutateConfig(func(cfg *appConfig) {
		cfg.PluginMarketSource = url
	})
}

// getMCPToken returns the configured MCP token. An empty string means no MCP
// token is configured.
func getMCPToken() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	return cfg.MCPToken, nil
}

// mcpTokenCharset is the alphabet used for generated MCP tokens.
const mcpTokenCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// generateMCPToken returns a short random MCP token prefixed with "ua-".
func generateMCPToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i, v := range b {
		b[i] = mcpTokenCharset[int(v)%len(mcpTokenCharset)]
	}
	return "ua-" + string(b), nil
}

// loadOrCreateMCPToken returns the configured MCP token, generating and
// persisting a new one when it is missing. It is used when login auth is
// enabled so /mcp/ is always guarded.
func loadOrCreateMCPToken() (string, error) {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil {
		return "", err
	}
	if cfg.MCPToken != "" {
		return cfg.MCPToken, nil
	}

	token, err := generateMCPToken()
	if err != nil {
		return "", fmt.Errorf("generate mcp token: %w", err)
	}
	cfg.MCPToken = token // pragma: allowlist secret
	if err := saveConfigLocked(writeConfigPathLocked(), cfg); err != nil {
		return "", fmt.Errorf("persist mcp token: %w", err)
	}
	slog.Info("MCP token generated and saved", "path", writeConfigPathLocked())
	return token, nil
}

// setMCPToken persists the MCP token. An empty token disables MCP token auth.
func setMCPToken(token string) error {
	return mutateConfig(func(cfg *appConfig) {
		cfg.MCPToken = token
	})
}

// getDeviceMetas returns the per-device metadata keyed by device id. A missing
// section yields an empty (non-nil) map.
func getDeviceMetas() (map[string]DeviceMeta, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.DeviceMeta == nil {
		return map[string]DeviceMeta{}, nil
	}
	return cfg.DeviceMeta, nil
}

// setDeviceMeta replaces the metadata for a device. An empty meta drops the
// device entry so the config stays clean.
func setDeviceMeta(deviceID string, meta DeviceMeta) error {
	return mutateConfig(func(cfg *appConfig) {
		if meta == (DeviceMeta{}) {
			delete(cfg.DeviceMeta, deviceID)
			return
		}
		if cfg.DeviceMeta == nil {
			cfg.DeviceMeta = make(map[string]DeviceMeta)
		}
		cfg.DeviceMeta[deviceID] = meta
	})
}

// deleteDeviceMeta removes the metadata for a device.
func deleteDeviceMeta(deviceID string) error {
	return mutateConfig(func(cfg *appConfig) {
		delete(cfg.DeviceMeta, deviceID)
	})
}

// getVersionCheck returns the persisted version check record, or an empty one
// when it has never been run.
func getVersionCheck() (VersionCheckRecord, error) {
	cfg, err := loadConfig()
	if err != nil {
		return VersionCheckRecord{}, err
	}
	if cfg.VersionCheck == nil {
		return VersionCheckRecord{}, nil
	}
	return *cfg.VersionCheck, nil
}

// setVersionCheck persists the version check record.
func setVersionCheck(rec VersionCheckRecord) error {
	return mutateConfig(func(cfg *appConfig) {
		cfg.VersionCheck = &rec
	})
}

const (
	LocaleChinese = "zh-CN"
	LocaleEnglish = "en-US"
)

// normalizeLocale maps a locale tag to one of the supported UI languages.
func normalizeLocale(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(value, "en"):
		return LocaleEnglish
	case strings.HasPrefix(value, "zh"):
		return LocaleChinese
	default:
		return ""
	}
}

// GetLocale returns the persisted UI language, defaulting to Chinese.
func GetLocale() string {
	cfg, err := loadConfig()
	if err != nil {
		return LocaleChinese
	}
	if locale := normalizeLocale(cfg.Locale); locale != "" {
		return locale
	}
	return LocaleChinese
}

// SetLocale persists the UI language selected in the frontend.
func SetLocale(value string) (string, error) {
	locale := normalizeLocale(value)
	if locale == "" {
		return "", fmt.Errorf("unsupported locale: %s", value)
	}
	if err := mutateConfig(func(cfg *appConfig) {
		cfg.Locale = locale
	}); err != nil {
		return "", err
	}
	return locale, nil
}

// getPluginRecords returns the persisted plugin records keyed by plugin id.
func getPluginRecords() (map[string]PluginRecord, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return cfg.Plugins, nil
}

// recordPluginInstall records or updates the install metadata for a plugin,
// preserving its enabled state.
func recordPluginInstall(id string, rec PluginRecord) error {
	return mutateConfig(func(cfg *appConfig) {
		cur := cfg.Plugins[id]
		cur.Version = rec.Version
		cur.Source = rec.Source
		cur.Sha256 = rec.Sha256
		cur.InstalledAt = rec.InstalledAt
		cfg.Plugins[id] = cur
	})
}

// setPluginEnabledStates persists the enabled state for the given plugin ids,
// preserving their install metadata.
func setPluginEnabledStates(states map[string]bool) error {
	return mutateConfig(func(cfg *appConfig) {
		for id, enabled := range states {
			rec := cfg.Plugins[id]
			v := enabled
			rec.Enabled = &v
			cfg.Plugins[id] = rec
		}
	})
}

// deletePluginRecord drops the record for a plugin, e.g. after it has been
// uninstalled.
func deletePluginRecord(id string) error {
	return mutateConfig(func(cfg *appConfig) {
		delete(cfg.Plugins, id)
	})
}
