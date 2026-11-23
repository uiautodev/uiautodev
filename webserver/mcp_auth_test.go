package webserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMCPTestServer(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.GET("/mcp/secure", func(c echo.Context) error {
		username, _ := c.Get("username").(string)
		return c.String(http.StatusOK, username)
	}, MCPAuthMiddleware())
	return e
}

func TestMCPAuthMiddleware(t *testing.T) {
	isolateConfig(t)
	e := newMCPTestServer(t)

	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("open when no token configured", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(httptest.NewRequest(http.MethodGet, "/mcp/secure", nil)).Code)
	})

	require.NoError(t, setMCPToken("long-lived-token"))
	t.Cleanup(func() { _ = setMCPToken("") })

	t.Run("requires token", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(httptest.NewRequest(http.MethodGet, "/mcp/secure", nil)).Code)
	})

	t.Run("accepts query token", func(t *testing.T) {
		rec := do(httptest.NewRequest(http.MethodGet, "/mcp/secure?token=long-lived-token", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "mcp", rec.Body.String())
	})

	t.Run("accepts bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp/secure", nil)
		req.Header.Set("Authorization", "Bearer long-lived-token")
		assert.Equal(t, http.StatusOK, do(req).Code)
	})

	t.Run("rejects wrong token", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(httptest.NewRequest(http.MethodGet, "/mcp/secure?token=nope", nil)).Code)
	})
}

func TestMCPToolsMetadataIsPublic(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, setMCPToken("mcp-token"))
	t.Cleanup(func() { _ = setMCPToken("") })

	e := echo.New()
	e.GET("/mcp/tools", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, MCPAuthMiddleware())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/tools", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "tool metadata should not require the MCP token")
}

func TestMCPAuthWithLoginFallback(t *testing.T) {
	isolateConfig(t)
	secret := []byte("test-secret-0123456789")
	e := echo.New()
	e.Use(JWTAuthMiddleware([]string{"alice:secret"}, secret))
	e.GET("/mcp/sse", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, MCPAuthMiddleware())

	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("empty mcp token falls back to login auth", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)).Code)

		req := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)
		req.SetBasicAuth("alice", "secret")
		assert.Equal(t, http.StatusOK, do(req).Code)
	})

	t.Run("mcp token bypasses login auth", func(t *testing.T) {
		require.NoError(t, setMCPToken("mcp-token"))
		t.Cleanup(func() { _ = setMCPToken("") })

		assert.Equal(t, http.StatusOK, do(httptest.NewRequest(http.MethodGet, "/mcp/sse?token=mcp-token", nil)).Code)

		// A login JWT is not accepted for MCP once an MCP token is configured.
		jwt, err := generateJWT("alice", secret)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/mcp/sse?token="+jwt, nil)
		assert.Equal(t, http.StatusUnauthorized, do(req).Code)
	})
}
