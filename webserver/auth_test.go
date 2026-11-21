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

func TestGenerateAndValidateJWT(t *testing.T) {
	secret := []byte("secret-key")
	token, err := generateJWT("alice", secret)
	require.NoError(t, err)

	username, err := validateJWT(token, secret)
	require.NoError(t, err)
	assert.Equal(t, "alice", username)

	_, err = validateJWT(token, []byte("wrong-secret"))
	assert.Error(t, err)

	_, err = validateJWT("not-a-token", secret)
	assert.Error(t, err)
}

// isolateConfig points the config file at a temp dir so tests never touch the
// developer's real config.
func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })
}

func newAuthTestServer(t *testing.T, users []string, secret []byte) *echo.Echo {
	t.Helper()
	isolateConfig(t)
	e := echo.New()
	e.Use(JWTAuthMiddleware(users, secret))
	e.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	e.GET("/api/v1/ping", func(c echo.Context) error {
		username, _ := c.Get("username").(string)
		return c.String(http.StatusOK, username)
	})
	e.GET("/mcp/sse", func(c echo.Context) error {
		username, _ := c.Get("username").(string)
		return c.String(http.StatusOK, username)
	})
	return e
}

func TestJWTAuthMiddleware(t *testing.T) {
	secret := []byte("test-secret-0123456789")
	e := newAuthTestServer(t, []string{"alice:secret"}, secret)

	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("healthz is public", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		assert.Equal(t, http.StatusOK, do(req).Code)
	})

	t.Run("no credentials is unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		rec := do(req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Basic")
	})

	t.Run("basic auth issues jwt cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.SetBasicAuth("alice", "secret")
		rec := do(req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", rec.Body.String())
		assert.Contains(t, rec.Header().Get("Set-Cookie"), authCookieName)
	})

	t.Run("basic auth wrong password is unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.SetBasicAuth("alice", "nope")
		assert.Equal(t, http.StatusUnauthorized, do(req).Code)
	})

	t.Run("bearer token", func(t *testing.T) {
		token, err := generateJWT("alice", secret)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := do(req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", rec.Body.String())
	})

	t.Run("cookie token", func(t *testing.T) {
		token, err := generateJWT("alice", secret)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
		assert.Equal(t, http.StatusOK, do(req).Code)
	})

	t.Run("mcp falls back to login auth when no mcp token", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)).Code)

		req := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)
		req.SetBasicAuth("alice", "secret")
		assert.Equal(t, http.StatusOK, do(req).Code)
	})

	t.Run("mcp is skipped by login auth when mcp token is set", func(t *testing.T) {
		require.NoError(t, setMCPToken("mcp-token"))
		t.Cleanup(func() { _ = setMCPToken("") })

		assert.Equal(t, http.StatusOK, do(httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)).Code)
	})

	t.Run("token signed with another secret is rejected", func(t *testing.T) {
		token, err := generateJWT("alice", []byte("other-secret"))
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		assert.Equal(t, http.StatusUnauthorized, do(req).Code)
	})
}

func TestRedactTokenQuery(t *testing.T) {
	assert.Equal(t, "/mcp/sse?token=REDACTED", redactTokenQuery("/mcp/sse?token=abc.def"))
	assert.Equal(t, "/mcp/sse?a=1&token=REDACTED", redactTokenQuery("/mcp/sse?a=1&token=abc"))
	assert.Equal(t, "/api/v1/devices", redactTokenQuery("/api/v1/devices"))
}

func TestLoadOrCreateJWTSecret(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })

	s1, err := loadOrCreateJWTSecret()
	require.NoError(t, err)
	require.NotEmpty(t, s1)

	s2, err := loadOrCreateJWTSecret()
	require.NoError(t, err)
	assert.Equal(t, s1, s2)

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.JWTSecret)
}

func TestLoadOrCreateJWTSecretPreservesConfig(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })

	require.NoError(t, saveConfig(appConfig{Password: "pw", Plugins: map[string]PluginRecord{}}))
	_, err := loadOrCreateJWTSecret()
	require.NoError(t, err)

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "pw", cfg.Password)
	assert.NotEmpty(t, cfg.JWTSecret)
}
