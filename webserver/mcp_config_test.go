package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPTokenHandlers(t *testing.T) {
	isolateConfig(t)
	e := echo.New()
	e.GET("/api/v1/mcp/token", mcpTokenGetHandler)
	e.PUT("/api/v1/mcp/token", mcpTokenSetHandler)

	get := func() string {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/mcp/token", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var resp struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Data.Token
	}

	put := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/mcp/token", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, "", get())

	assert.Equal(t, http.StatusOK, put(`{"token":"abc123"}`))
	assert.Equal(t, "abc123", get())

	assert.Equal(t, http.StatusOK, put(`{"token":"  spaced  "}`))
	assert.Equal(t, "spaced", get())

	assert.Equal(t, http.StatusBadRequest, put(`{"token":"`+strings.Repeat("x", maxMCPTokenLen+1)+`"}`))

	assert.Equal(t, http.StatusOK, put(`{"token":""}`))
	assert.Equal(t, "", get())
}

func TestLoadOrCreateMCPToken(t *testing.T) {
	isolateConfig(t)

	token, err := loadOrCreateMCPToken()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, "ua-"))
	assert.Len(t, token, len("ua-")+16)

	again, err := loadOrCreateMCPToken()
	require.NoError(t, err)
	assert.Equal(t, token, again)

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, token, cfg.MCPToken)
}
