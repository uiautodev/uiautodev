package webserver

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	sessionTTL = 24 * time.Hour
)

// errEmptyPassword is returned when the management password would be empty.
var errEmptyPassword = errors.New("password must not be empty")

// generatePasswordSuggestion returns a fresh random password suggestion of at
// most 6 characters (3 random bytes as hex). It is NOT persisted: the user
// decides on first setup whether to keep it or type their own password.
func generatePasswordSuggestion() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// passwordSet reports whether a management password has been configured.
func passwordSet() (bool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return false, err
	}
	return cfg.Password != "", nil
}

// saveNewPassword persists the management password on first setup. The password
// must be non-empty after trimming; no further complexity rules are enforced.
func saveNewPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return errEmptyPassword
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Password = password // pragma: allowlist secret
	return saveConfig(cfg)
}

// verifyPassword checks the password against the stored one.
func verifyPassword(password string) (bool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return false, err
	}
	return cfg.Password != "" && subtle.ConstantTimeCompare([]byte(cfg.Password), []byte(password)) == 1, nil // pragma: allowlist secret
}

// pluginSessionManager tracks issued session tokens in memory.
type pluginSessionManager struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

var globalSessionManager = &pluginSessionManager{
	sessions: make(map[string]time.Time),
}

// issue creates a new session token valid for sessionTTL.
func (s *pluginSessionManager) issue() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic cleanup of expired sessions.
	now := time.Now()
	for t, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, t)
		}
	}
	s.sessions[token] = now.Add(sessionTTL)
	return token
}

// valid reports whether the token is currently valid.
func (s *pluginSessionManager) valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, token)
		return false
	}
	return true
}

// managementAuthDisabled indicates management operations (plugins, openai
// config) require no password (set when running inside the trusted wails
// desktop environment).
var managementAuthDisabled bool

// managementAuthMiddleware guards management write operations (plugin
// install/enable and openai config) with a session token, unless management
// auth is disabled.
func managementAuthMiddleware(config ServerConfig) echo.MiddlewareFunc {
	managementAuthDisabled = config.DisablePluginAuth
	if config.DisablePluginAuth {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	return requireManagementAuth
}

// requireManagementAuth is an echo middleware that guards management write
// operations with a session token issued after password verification.
func requireManagementAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if !globalSessionManager.valid(token) {
			return c.JSONPretty(http.StatusUnauthorized, APIResponse{
				Code:    401,
				Message: "unauthorized: valid management session required",
			}, "  ")
		}
		return next(c)
	}
}

// managementAuthStatusHandler handles GET /api/v1/plugins/auth.
// Until a password is configured it never persists one: it only reports the
// initialized flag, the config file the user can edit, and a suggested
// password for the first-run setup screen. When auth is disabled, auth_required
// is false and no password is needed.
func managementAuthStatusHandler(c echo.Context) error {
	if managementAuthDisabled {
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    map[string]any{"initialized": false, "auth_required": false, "plugin_dir": GetUserPluginDir()},
		}, "  ")
	}
	set, err := passwordSet()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load plugin auth: " + err.Error(),
		}, "  ")
	}
	data := map[string]any{
		"initialized":   set,
		"auth_required": true,
		"config_file":   getConfigFile(),
		"plugin_dir":    GetUserPluginDir(),
	}
	if !set {
		suggestion, err := generatePasswordSuggestion()
		if err != nil {
			return c.JSONPretty(http.StatusInternalServerError, APIResponse{
				Code:    500,
				Message: "failed to generate password suggestion: " + err.Error(),
			}, "  ")
		}
		data["suggested_password"] = suggestion // pragma: allowlist secret
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: data}, "  ")
}

type pluginAuthSetupRequest struct {
	Password string `json:"password"`
}

// managementAuthSetupHandler handles POST /api/v1/plugins/auth/setup.
// It persists the first management password (chosen by the user, possibly
// derived from a suggestion) and issues a session token. It only succeeds while
// no password is configured yet.
func managementAuthSetupHandler(c echo.Context) error {
	if managementAuthDisabled {
		token := globalSessionManager.issue()
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    map[string]string{"token": token},
		}, "  ")
	}
	var req pluginAuthSetupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	set, err := passwordSet()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load plugin auth: " + err.Error(),
		}, "  ")
	}
	if set {
		return c.JSONPretty(http.StatusConflict, APIResponse{
			Code:    409,
			Message: "password already configured",
		}, "  ")
	}
	if err := saveNewPassword(req.Password); err != nil {
		if err == errEmptyPassword {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{
				Code:    400,
				Message: "password must not be empty",
			}, "  ")
		}
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to save password: " + err.Error(),
		}, "  ")
	}
	token := globalSessionManager.issue()
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]string{"token": token},
	}, "  ")
}

type pluginAuthLoginRequest struct {
	Password string `json:"password"`
}

// managementAuthLoginHandler handles POST /api/v1/plugins/auth.
// Verifies the password and issues a session token on success.
func managementAuthLoginHandler(c echo.Context) error {
	if managementAuthDisabled {
		token := globalSessionManager.issue()
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    map[string]string{"token": token},
		}, "  ")
	}
	var req pluginAuthLoginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	ok, err := verifyPassword(req.Password)
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to verify password: " + err.Error(),
		}, "  ")
	}
	if !ok {
		return c.JSONPretty(http.StatusUnauthorized, APIResponse{
			Code:    401,
			Message: "invalid password",
		}, "  ")
	}
	token := globalSessionManager.issue()
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]string{"token": token},
	}, "  ")
}
