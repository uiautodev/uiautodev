package webserver

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// sessionCookieName is the cookie that distinguishes anonymous browser
// sessions. It is not an authentication credential.
const sessionCookieName = "ua_sid"

// sessionCookieMaxAge is one year, in seconds.
const sessionCookieMaxAge = 365 * 24 * 60 * 60

// AuthSource describes where an identity came from.
type AuthSource string

const (
	AuthSourceProxy     AuthSource = "proxy"
	AuthSourceLogin     AuthSource = "login"
	AuthSourceMCP       AuthSource = "mcp"
	AuthSourceAnonymous AuthSource = "anonymous"
)

// Identity is the authenticated (or anonymous) caller behind a request.
type Identity struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	AvatarURL string   `json:"avatar_url,omitempty"`
	Anonymous bool     `json:"anonymous"`
}

// requestIdentity couples an Identity with the session ID and its source.
type requestIdentity struct {
	Identity
	SessionID string
	Source    AuthSource
}

const (
	identityContextKey = "request-identity"
	presenceCtxKey     = "presence-session"
)

// identityFrom returns the identity resolved by the identity middleware.
func identityFrom(c echo.Context) (*requestIdentity, bool) {
	id, ok := c.Get(identityContextKey).(*requestIdentity)
	return id, ok && id != nil
}

// clientIP returns the direct peer IP of the request, ignoring forwarding
// headers (which are only honored for trusted proxies).
func clientIP(c echo.Context) string {
	addr := c.Request().RemoteAddr
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// ipInNetworks reports whether the IP belongs to one of the networks.
func ipInNetworks(ip string, networks []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, network := range networks {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// proxyIdentity parses the X-Forwarded-* / X-Auth-Request-* identity headers.
func proxyIdentity(h http.Header) (Identity, bool) {
	user := h.Get("X-Forwarded-User")
	if user == "" {
		user = h.Get("X-Auth-Request-User")
	}
	if user == "" {
		return Identity{}, false
	}
	name := h.Get("X-Forwarded-Preferred-Username")
	if name == "" {
		name = user
	}
	email := h.Get("X-Forwarded-Email")
	if email == "" {
		email = h.Get("X-Auth-Request-Email")
	}
	var groups []string
	if raw := h.Get("X-Forwarded-Groups"); raw != "" {
		for _, group := range strings.Split(raw, ",") {
			if group = strings.TrimSpace(group); group != "" {
				groups = append(groups, group)
			}
		}
	}
	return Identity{
		ID:        "proxy:" + user,
		Name:      name,
		Email:     email,
		Groups:    groups,
		AvatarURL: h.Get("X-Forwarded-Avatar"),
		Anonymous: false,
	}, true
}

// hashShort returns the first 16 hex characters of the SHA-256 of value.
func hashShort(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

// newSessionID returns a 32 character hex session ID.
func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is fatal for security-sensitive code, but the
		// session ID is not a credential; fall back to a timestamp-free hash.
		return hashShort("fallback-session")
	}
	return hex.EncodeToString(buf)
}

// acceptsHTML reports whether the request looks like a browser navigation (as
// opposed to an API/script call).
func acceptsHTML(c echo.Context) bool {
	return strings.Contains(c.Request().Header.Get(echo.HeaderAccept), "text/html")
}

// resolveSessionID determines the caller's session ID from the cookie, a bearer
// token or, as a last resort, the client IP plus user agent. It returns an
// empty string for a browser navigation without a cookie: the middleware then
// creates a fresh random session and stores it in the cookie so distinct
// browsers get distinct identities even behind the same IP with the same user
// agent.
func resolveSessionID(c echo.Context) string {
	if cookie, err := c.Cookie(sessionCookieName); err == nil {
		if value := strings.TrimSpace(cookie.Value); len(value) >= 16 {
			return value
		}
	}
	if token := extractBearerToken(c); token != "" {
		return hashShort(token)
	}
	if acceptsHTML(c) {
		return ""
	}
	return hashShort(clientIP(c) + "|" + c.Request().UserAgent())
}

// resolveIdentity builds the request identity from the trusted proxy headers,
// the login session or the anonymous session cookie.
func resolveIdentity(c echo.Context, cfg AccessConfig) *requestIdentity {
	sessionID := resolveSessionID(c)
	if sessionID == "" {
		sessionID = newSessionID()
	}
	networks := compileTrustedProxies(cfg.Auth.TrustedProxies)
	if ipInNetworks(clientIP(c), networks) {
		if identity, ok := proxyIdentity(c.Request().Header); ok {
			return &requestIdentity{Identity: identity, SessionID: sessionID, Source: AuthSourceProxy}
		}
	}
	if username, ok := c.Get("username").(string); ok && username != "" {
		return &requestIdentity{
			Identity: Identity{
				ID:   "login:" + username,
				Name: username,
			},
			SessionID: sessionID,
			Source:    AuthSourceLogin,
		}
	}
	return &requestIdentity{
		Identity: Identity{
			ID:        "anon:" + sessionID[:8],
			Name:      "Anonymous",
			Anonymous: true,
		},
		SessionID: sessionID,
		Source:    AuthSourceAnonymous,
	}
}

// identityExemptPath lists the paths that stay reachable without an identity
// even when require_identity is on.
func identityExemptPath(path string) bool {
	switch path {
	case "/healthz", "/api/v1/me":
		return true
	}
	return false
}

// identityMiddleware resolves the caller identity, issues the anonymous session
// cookie and enforces require_identity on API traffic.
func identityMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cfg := getAccessConfig()
			identity := resolveIdentity(c, cfg)
			c.Set(identityContextKey, identity)

			// Keep the anonymous session stable across tabs and requests. A
			// cookie is only issued when the client has none.
			if _, err := c.Cookie(sessionCookieName); err != nil && extractBearerToken(c) == "" {
				c.SetCookie(&http.Cookie{
					Name:     sessionCookieName,
					Value:    identity.SessionID,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					Secure:   c.Scheme() == "https",
					MaxAge:   sessionCookieMaxAge,
				})
			}

			if cfg.Auth.RequireIdentity && identity.Anonymous && !identityExemptPath(c.Request().URL.Path) {
				return c.JSONPretty(http.StatusUnauthorized, APIResponse{
					Code:    401,
					Message: "identity required",
				}, "  ")
			}
			return next(c)
		}
	}
}

// meResponse is the payload of GET /api/v1/me.
type meResponse struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	Anonymous       bool       `json:"anonymous"`
	AuthSource      AuthSource `json:"auth_source"`
	AvatarURL       string     `json:"avatar_url"`
	SignoutURL      string     `json:"signout_url"`
	PresenceEnabled bool       `json:"presence_enabled"`
	AuthRequired    bool       `json:"auth_required"`
}

// meHandler handles GET /api/v1/me.
func meHandler(c echo.Context) error {
	identity, ok := identityFrom(c)
	if !ok {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "identity unavailable",
		}, "  ")
	}
	cfg := getAccessConfig()
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data: meResponse{
			ID:              identity.ID,
			Name:            identity.Name,
			Email:           identity.Email,
			Anonymous:       identity.Anonymous,
			AuthSource:      identity.Source,
			AvatarURL:       identity.AvatarURL,
			SignoutURL:      cfg.Auth.SignoutURL,
			PresenceEnabled: cfg.Presence.Enabled,
			AuthRequired:    cfg.Auth.RequireIdentity,
		},
	}, "  ")
}
