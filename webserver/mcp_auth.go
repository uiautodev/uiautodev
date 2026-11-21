package webserver

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// MCPAuthMiddleware guards /mcp/ with the configured long-lived MCP token. When
// no token is configured it passes through, leaving access to the login auth
// middleware (when configured) or open access otherwise.
func MCPAuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token, err := getMCPToken()
			if err != nil {
				slog.Error("failed to load MCP token", "error", err)
				return c.JSONPretty(http.StatusInternalServerError, APIResponse{
					Code:    500,
					Message: "failed to load MCP token",
				}, "  ")
			}
			if token == "" {
				return next(c)
			}
			if mcpTokenMatches(c, token) {
				c.Set("username", "mcp")
				return next(c)
			}
			return c.JSONPretty(http.StatusUnauthorized, APIResponse{
				Code:    401,
				Message: "authentication required",
			}, "  ")
		}
	}
}

// mcpTokenMatches reports whether the request carries the expected MCP token,
// either as ?token= or as an Authorization: Bearer header.
func mcpTokenMatches(c echo.Context, expected string) bool {
	provided := c.QueryParam("token")
	if provided == "" {
		provided = extractBearerToken(c)
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
