package webserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

// ConflictPolicy controls what happens when another identity already controls
// a device.
type ConflictPolicy string

const (
	ConflictPolicyPreempt   ConflictPolicy = "preempt"
	ConflictPolicyExclusive ConflictPolicy = "exclusive"
)

// ConflictPolicyList lists every supported conflict policy.
var ConflictPolicyList = []ConflictPolicy{ConflictPolicyPreempt, ConflictPolicyExclusive}

// Valid reports whether the policy is one of the supported values.
func (p ConflictPolicy) Valid() bool {
	switch p {
	case ConflictPolicyPreempt, ConflictPolicyExclusive:
		return true
	default:
		return false
	}
}

// DeviceAccessMode is the role a session holds on a device.
type DeviceAccessMode string

const (
	AccessModeControl DeviceAccessMode = "control"
	AccessModeView    DeviceAccessMode = "view"
)

// DeviceAccessModeList lists every supported access mode.
var DeviceAccessModeList = []DeviceAccessMode{AccessModeControl, AccessModeView}

// Valid reports whether the mode is one of the supported values.
func (m DeviceAccessMode) Valid() bool {
	switch m {
	case AccessModeControl, AccessModeView:
		return true
	default:
		return false
	}
}

// IdleTimeout is the configurable period after which an idle device session is
// released.
type IdleTimeout string

const (
	IdleTimeout1m  IdleTimeout = "1m"
	IdleTimeout5m  IdleTimeout = "5m"
	IdleTimeout10m IdleTimeout = "10m"
	IdleTimeout30m IdleTimeout = "30m"
	IdleTimeout1h  IdleTimeout = "1h"
)

// IdleTimeoutList lists every supported idle timeout.
var IdleTimeoutList = []IdleTimeout{IdleTimeout1m, IdleTimeout5m, IdleTimeout10m, IdleTimeout30m, IdleTimeout1h}

// Valid reports whether the timeout is one of the supported values.
func (t IdleTimeout) Valid() bool {
	switch t {
	case IdleTimeout1m, IdleTimeout5m, IdleTimeout10m, IdleTimeout30m, IdleTimeout1h:
		return true
	default:
		return false
	}
}

// Duration returns the timeout as a time.Duration. Unknown values fall back to
// the default (10 minutes).
func (t IdleTimeout) Duration() time.Duration {
	switch t {
	case IdleTimeout1m:
		return time.Minute
	case IdleTimeout5m:
		return 5 * time.Minute
	case IdleTimeout30m:
		return 30 * time.Minute
	case IdleTimeout1h:
		return time.Hour
	default:
		return 10 * time.Minute
	}
}

// AuthAccessConfig configures where identities come from.
type AuthAccessConfig struct {
	// TrustedProxies lists IPs or CIDRs whose X-Forwarded-* headers are trusted.
	TrustedProxies []string `json:"trusted_proxies"`
	// RequireIdentity rejects anonymous API requests when true.
	RequireIdentity bool `json:"require_identity"`
	// SignoutURL is shown in the frontend when non-empty.
	SignoutURL string `json:"signout_url"`
}

// PresenceAccessConfig configures device occupancy handling.
type PresenceAccessConfig struct {
	Enabled        bool           `json:"enabled"`
	ConflictPolicy ConflictPolicy `json:"conflict_policy"`
	IdleTimeout    IdleTimeout    `json:"idle_timeout"`
	AllowObservers bool           `json:"allow_observers"`
}

// UnmarshalJSON treats absent keys as "use the default" instead of as the bool
// zero value. Both Enabled and AllowObservers default to true, so a config.json
// that only sets access.auth (the oauth2-proxy example in the docs) must not
// silently disable presence. Values that are present, including explicit
// false, are kept.
func (p *PresenceAccessConfig) UnmarshalJSON(data []byte) error {
	type shadow struct {
		Enabled        *bool           `json:"enabled"`
		ConflictPolicy *ConflictPolicy `json:"conflict_policy"`
		IdleTimeout    *IdleTimeout    `json:"idle_timeout"`
		AllowObservers *bool           `json:"allow_observers"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	out := defaultAccessConfig().Presence
	if s.Enabled != nil {
		out.Enabled = *s.Enabled
	}
	if s.ConflictPolicy != nil {
		out.ConflictPolicy = *s.ConflictPolicy
	}
	if s.IdleTimeout != nil {
		out.IdleTimeout = *s.IdleTimeout
	}
	if s.AllowObservers != nil {
		out.AllowObservers = *s.AllowObservers
	}
	*p = out
	return nil
}

// AccessConfig is the persisted access section of config.json.
type AccessConfig struct {
	Auth     AuthAccessConfig     `json:"auth"`
	Presence PresenceAccessConfig `json:"presence"`
}

// UnmarshalJSON starts from the defaults so an access section that only carries
// auth (or only some presence keys) keeps the documented defaults for the rest.
func (a *AccessConfig) UnmarshalJSON(data []byte) error {
	type shadow struct {
		Auth     *AuthAccessConfig     `json:"auth"`
		Presence *PresenceAccessConfig `json:"presence"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	out := defaultAccessConfig()
	if s.Auth != nil {
		out.Auth = *s.Auth
		if out.Auth.TrustedProxies == nil {
			out.Auth.TrustedProxies = []string{}
		}
	}
	if s.Presence != nil {
		out.Presence = *s.Presence
	}
	*a = out
	return nil
}

// releaseGrace is the fixed delay between the last session leaving a device and
// the device resource being released.
const releaseGrace = 30 * time.Second

// evictionTTL bounds how long a preemption notice is kept.
const evictionTTL = 2 * time.Minute

// defaultAccessConfig returns the defaults used when the section is missing or
// invalid.
func defaultAccessConfig() AccessConfig {
	return AccessConfig{
		Auth: AuthAccessConfig{
			TrustedProxies:  []string{},
			RequireIdentity: false,
			SignoutURL:      "",
		},
		Presence: PresenceAccessConfig{
			Enabled:        true,
			ConflictPolicy: ConflictPolicyPreempt,
			IdleTimeout:    IdleTimeout10m,
			AllowObservers: true,
		},
	}
}

// normalizeAccessConfig fills missing values from the defaults and trims
// whitespace in list fields.
func normalizeAccessConfig(cfg AccessConfig) AccessConfig {
	def := defaultAccessConfig()
	if cfg.Auth.TrustedProxies == nil {
		cfg.Auth.TrustedProxies = []string{}
	}
	trimmed := make([]string, 0, len(cfg.Auth.TrustedProxies))
	for _, proxy := range cfg.Auth.TrustedProxies {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			trimmed = append(trimmed, proxy)
		}
	}
	cfg.Auth.TrustedProxies = trimmed
	cfg.Auth.SignoutURL = strings.TrimSpace(cfg.Auth.SignoutURL)
	if !cfg.Presence.ConflictPolicy.Valid() {
		cfg.Presence.ConflictPolicy = def.Presence.ConflictPolicy
	}
	if !cfg.Presence.IdleTimeout.Valid() {
		cfg.Presence.IdleTimeout = def.Presence.IdleTimeout
	}
	return cfg
}

// ValidateAccessConfig checks user supplied values and returns an error
// describing the first problem.
func ValidateAccessConfig(cfg AccessConfig) error {
	if !cfg.Presence.ConflictPolicy.Valid() {
		return fmt.Errorf("invalid conflict_policy: %s", cfg.Presence.ConflictPolicy)
	}
	if !cfg.Presence.IdleTimeout.Valid() {
		return fmt.Errorf("invalid idle_timeout: %s", cfg.Presence.IdleTimeout)
	}
	for _, proxy := range cfg.Auth.TrustedProxies {
		if _, err := parseCIDR(proxy); err != nil {
			return fmt.Errorf("invalid trusted_proxies entry %q: %w", proxy, err)
		}
	}
	if cfg.Auth.SignoutURL != "" && !validSignoutURL(cfg.Auth.SignoutURL) {
		return fmt.Errorf("invalid signout_url: %s", cfg.Auth.SignoutURL)
	}
	return nil
}

// validSignoutURL accepts relative paths and absolute http(s) URLs.
func validSignoutURL(raw string) bool {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// parseCIDR accepts either a bare IP (treated as a single host) or a CIDR.
func parseCIDR(value string) (*net.IPNet, error) {
	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		return network, err
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return nil, fmt.Errorf("not an IP or CIDR")
	}
	bits := 32
	if ip.To4() == nil {
		bits = 128
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
}

// compileTrustedProxies parses the configured list into networks.
func compileTrustedProxies(values []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		network, err := parseCIDR(value)
		if err != nil {
			slog.Warn("ignoring invalid trusted proxy", "value", value, "error", err)
			continue
		}
		out = append(out, network)
	}
	return out
}

var accessConfigCache atomic.Pointer[AccessConfig]

// ResetAccessConfigCache drops the cached configuration so the next read loads
// it from disk again. It is used when tests switch the config file.
func ResetAccessConfigCache() {
	accessConfigCache.Store(nil)
}

// getAccessConfig returns the effective configuration, loading and caching it
// from disk on first use.
func getAccessConfig() AccessConfig {
	if cached := accessConfigCache.Load(); cached != nil {
		return *cached
	}
	cfg, err := loadConfig()
	if err != nil {
		slog.Warn("failed to load access config, using defaults", "error", err)
		def := defaultAccessConfig()
		accessConfigCache.Store(&def)
		return def
	}
	if cfg.Access == nil {
		def := defaultAccessConfig()
		accessConfigCache.Store(&def)
		return def
	}
	normalized := normalizeAccessConfig(*cfg.Access)
	if err := ValidateAccessConfig(normalized); err != nil {
		slog.Warn("invalid access config, using defaults", "error", err)
		def := defaultAccessConfig()
		accessConfigCache.Store(&def)
		return def
	}
	accessConfigCache.Store(&normalized)
	return normalized
}

// setAccessConfig validates and persists the access configuration, then updates
// the in-memory cache without a restart.
func setAccessConfig(cfg AccessConfig) (AccessConfig, error) {
	if err := ValidateAccessConfig(cfg); err != nil {
		return AccessConfig{}, err
	}
	normalized := normalizeAccessConfig(cfg)
	if err := mutateConfig(func(app *appConfig) {
		app.Access = &normalized
	}); err != nil {
		return AccessConfig{}, err
	}
	accessConfigCache.Store(&normalized)
	return normalized, nil
}

// accessConfigGetHandler handles GET /api/v1/access/config.
func accessConfigGetHandler(c echo.Context) error {
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"config":      getAccessConfig(),
			"config_file": getConfigFile(),
		},
	}, "  ")
}

// accessConfigPutHandler handles PUT /api/v1/access/config.
func accessConfigPutHandler(c echo.Context) error {
	var cfg AccessConfig
	if err := c.Bind(&cfg); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	saved, err := setAccessConfig(cfg)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"config":      saved,
			"config_file": getConfigFile(),
		},
	}, "  ")
}
