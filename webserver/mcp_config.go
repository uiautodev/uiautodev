package webserver

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// maxMCPTokenLen bounds the user-configured MCP token length.
const maxMCPTokenLen = 256

// mcpTokenGetHandler handles GET /api/v1/mcp/token. It returns the configured
// MCP token so the UI can embed it into MCP client configs.
func mcpTokenGetHandler(c echo.Context) error {
	token, err := getMCPToken()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load MCP token: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]string{"token": token},
	}, "  ")
}

// mcpTokenSetHandler handles PUT /api/v1/mcp/token. An empty token disables MCP
// token auth.
func mcpTokenSetHandler(c echo.Context) error {
	var req struct {
		Token string `json:"token"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	token := strings.TrimSpace(req.Token)
	if len(token) > maxMCPTokenLen {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "token too long",
		}, "  ")
	}
	if err := setMCPToken(token); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to save MCP token: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]string{"token": token},
	}, "  ")
}
