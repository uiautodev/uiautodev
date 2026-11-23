package webserver

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

const (
	authCookieName = "auth_token"
	authIssuer     = "uiauto.dev"
	authTokenTTL   = 30 * 24 * time.Hour
)

// authExemptPaths are public endpoints that skip JWT cookie auth.
var authExemptPaths = []string{
	"/healthz",
	"/plugin-runtime.d.ts",
	"/plugin.schema.json",
	"/api/v1/cluster/connect",
}

func isAuthExempt(path string) bool {
	for _, p := range authExemptPaths {
		if path == p {
			return true
		}
	}
	return false
}

func generateJWT(username string, secret []byte) (string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		Issuer:    authIssuer,
		Subject:   username,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(authTokenTTL)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func validateJWT(tokenStr string, secret []byte) (string, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secret, nil
		},
		jwt.WithIssuer(authIssuer),
	)
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	return claims.Subject, nil
}

func setJWTCookie(c echo.Context, token string) {
	cookie := &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Scheme() == "https",
	}
	c.SetCookie(cookie)
}

func extractBearerToken(c echo.Context) string {
	auth := c.Request().Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[7:])
}

// redactTokenQuery masks the token query parameter so JWT values are never
// written to request logs.
func redactTokenQuery(rawURI string) string {
	u, err := url.ParseRequestURI(rawURI)
	if err != nil || u.Query().Get("token") == "" {
		return rawURI
	}
	q := u.Query()
	q.Set("token", "REDACTED")
	u.RawQuery = q.Encode()
	return u.RequestURI()
}

func validateBasicAuth(c echo.Context, authUsers []string) (string, bool) {
	username, password, ok := c.Request().BasicAuth()
	if !ok || username == "" || password == "" {
		return "", false
	}
	for _, userPass := range authUsers {
		parts := strings.SplitN(userPass, ":", 2)
		if len(parts) == 2 &&
			subtle.ConstantTimeCompare([]byte(parts[0]), []byte(username)) == 1 &&
			subtle.ConstantTimeCompare([]byte(parts[1]), []byte(password)) == 1 {
			return username, true
		}
	}
	return "", false
}

func JWTAuthMiddleware(authUsers []string, jwtSecret []byte) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if isAuthExempt(c.Request().URL.Path) {
				return next(c)
			}

			// /mcp/ is governed by the MCP token middleware when a token is
			// configured. Only fall back to login auth when no MCP token is set.
			if strings.HasPrefix(c.Request().URL.Path, "/mcp/") {
				if token, err := getMCPToken(); err == nil && token != "" {
					return next(c)
				}
			}

			// Try JWT cookie first
			cookie, err := c.Cookie(authCookieName)
			if err == nil && cookie.Value != "" {
				username, err := validateJWT(cookie.Value, jwtSecret)
				if err == nil {
					c.Set("username", username)
					return next(c)
				}
			}

			// Try Authorization: Bearer header
			if token := extractBearerToken(c); token != "" {
				username, err := validateJWT(token, jwtSecret)
				if err == nil {
					c.Set("username", username)
					return next(c)
				}
			}

			// Fallback to Basic Auth
			username, ok := validateBasicAuth(c, authUsers)
			if ok {
				// Issue new JWT
				token, err := generateJWT(username, jwtSecret)
				if err != nil {
					slog.Error("failed to generate JWT", "error", err, "username", username)
					return c.NoContent(http.StatusInternalServerError)
				}
				setJWTCookie(c, token)
				c.Set("username", username)
				slog.Info("auth: Basic Auth passed, JWT issued", "username", username)
				return next(c)
			}

			// Neither JWT nor Basic Auth valid
			c.Response().Header().Set("WWW-Authenticate", `Basic realm="uiautodev"`)
			return c.JSONPretty(http.StatusUnauthorized, APIResponse{
				Code:    401,
				Message: "authentication required",
			}, "  ")
		}
	}
}
