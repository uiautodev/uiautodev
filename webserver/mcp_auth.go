package webserver

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

const (
	// mcpIdentityID is the identity every MCP request authenticated with the
	// shared MCP token acts as.
	mcpIdentityID = "mcp:admin"
	// mcpIdentityName is the display name of the MCP admin identity.
	mcpIdentityName = "MCP"
)

// mcpIdentityContextKey carries the resolved request identity to MCP tool
// handlers, which only receive a context.Context.
type mcpIdentityContextKey struct{}

// mcpIdentityFromContext returns the identity injected by MCPAuthMiddleware.
func mcpIdentityFromContext(ctx context.Context) *requestIdentity {
	identity, _ := ctx.Value(mcpIdentityContextKey{}).(*requestIdentity)
	return identity
}

// mcpContextWithIdentity copies the identity from the HTTP request context into
// the context handed to MCP tool handlers. It is used as the SSE/streamable
// HTTP context function.
func mcpContextWithIdentity(ctx context.Context, r *http.Request) context.Context {
	identity, ok := r.Context().Value(mcpIdentityContextKey{}).(*requestIdentity)
	if !ok || identity == nil {
		return ctx
	}
	return context.WithValue(ctx, mcpIdentityContextKey{}, identity)
}

// MCPAuthMiddleware guards /mcp/ with the configured long-lived MCP token. When
// no token is configured it passes through, leaving access to the login auth
// middleware (when configured) or open access otherwise.
//
// When the token matches, the request acts as the shared MCP admin identity so
// device presence and conflict handling apply to MCP like any other user.
func MCPAuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Tool metadata is public: the MCP page lists it without holding the
			// token, and it only exposes names and descriptions.
			if c.Request().Method == http.MethodGet && c.Request().URL.Path == "/mcp/tools" {
				return next(c)
			}

			token, err := getMCPToken()
			if err != nil {
				slog.Error("failed to load MCP token", "error", err)
				return c.JSONPretty(http.StatusInternalServerError, APIResponse{
					Code:    500,
					Message: "failed to load MCP token",
				}, "  ")
			}

			identity, _ := identityFrom(c)
			if token != "" {
				if !mcpTokenMatches(c, token) {
					return c.JSONPretty(http.StatusUnauthorized, APIResponse{
						Code:    401,
						Message: "authentication required",
					}, "  ")
				}
				identity = &requestIdentity{
					Identity: Identity{
						ID:   mcpIdentityID,
						Name: mcpIdentityName,
					},
					SessionID: hashShort(token),
					Source:    AuthSourceMCP,
				}
				// Keep username populated for handlers that still read it.
				c.Set("username", "mcp")
				c.Set(identityContextKey, identity)
			}

			if identity != nil {
				ctx := context.WithValue(c.Request().Context(), mcpIdentityContextKey{}, identity)
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
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
