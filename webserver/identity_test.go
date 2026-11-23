package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// newIdentityProbe builds a minimal echo app exposing the resolved identity so
// tests can inspect the middleware output without a full server.
func newIdentityProbe(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.Use(identityMiddleware())
	e.GET("/api/v1/probe", func(c echo.Context) error {
		identity, ok := identityFrom(c)
		if !ok {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "no identity"})
		}
		return c.JSON(http.StatusOK, identity)
	})
	return e
}

func probe(t *testing.T, e *echo.Echo, remoteAddr string, headers map[string]string, cookies ...*http.Cookie) (*httptest.ResponseRecorder, requestIdentity) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.RemoteAddr = remoteAddr
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var identity requestIdentity
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &identity); err != nil {
			t.Fatalf("decode identity: %v", err)
		}
	}
	return rec, identity
}

func TestIdentityIgnoresUntrustedForwardedHeaders(t *testing.T) {
	useTempConfig(t)
	e := newIdentityProbe(t)
	_, identity := probe(t, e, "203.0.113.5:1234", map[string]string{"X-Forwarded-User": "alice"})
	if !identity.Anonymous {
		t.Fatalf("expected anonymous identity, got %+v", identity)
	}
	if identity.Source != AuthSourceAnonymous {
		t.Fatalf("unexpected source: %s", identity.Source)
	}
}

func TestIdentityTrustsProxyHeadersFromConfiguredCIDR(t *testing.T) {
	useTempConfig(t)
	cfg := defaultAccessConfig()
	cfg.Auth.TrustedProxies = []string{"127.0.0.1/32"}
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	e := newIdentityProbe(t)
	_, identity := probe(t, e, "127.0.0.1:5555", map[string]string{
		"X-Forwarded-User":               "alice",
		"X-Forwarded-Email":              "alice@example.com",
		"X-Forwarded-Preferred-Username": "Alice",
		"X-Forwarded-Groups":             "dev, ops",
		"X-Forwarded-Avatar":             "https://example.com/a.png",
	})
	if identity.ID != "proxy:alice" {
		t.Fatalf("unexpected id: %s", identity.ID)
	}
	if identity.Name != "Alice" {
		t.Fatalf("unexpected name: %s", identity.Name)
	}
	if identity.Email != "alice@example.com" {
		t.Fatalf("unexpected email: %s", identity.Email)
	}
	if len(identity.Groups) != 2 {
		t.Fatalf("unexpected groups: %v", identity.Groups)
	}
	if identity.Source != AuthSourceProxy {
		t.Fatalf("unexpected source: %s", identity.Source)
	}
}

func TestIdentityAnonymousCookieIsStable(t *testing.T) {
	useTempConfig(t)
	e := newIdentityProbe(t)
	rec, first := probe(t, e, "203.0.113.5:1234", nil)
	if !first.Anonymous {
		t.Fatalf("expected anonymous identity")
	}
	cookies := rec.Result().Cookies()
	var session *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == sessionCookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatalf("expected a %s cookie, got %v", sessionCookieName, cookies)
	}
	_, second := probe(t, e, "203.0.113.5:1234", nil, session)
	if second.ID != first.ID {
		t.Fatalf("session identity not stable: %s != %s", second.ID, first.ID)
	}
	other := &http.Cookie{Name: sessionCookieName, Value: "0123456789abcdef0123456789abcdef"}
	_, third := probe(t, e, "203.0.113.5:1234", nil, other)
	if third.ID == first.ID {
		t.Fatalf("different cookie should yield a different identity")
	}
}

func TestIdentityDistinctBrowsersGetDistinctSessions(t *testing.T) {
	useTempConfig(t)
	e := newIdentityProbe(t)
	firstRec, first := probe(t, e, "192.0.2.10:1111", map[string]string{"Accept": "text/html"})
	_, second := probe(t, e, "192.0.2.10:1111", map[string]string{"Accept": "text/html"})
	if first.ID == second.ID {
		t.Fatalf("two browsers with the same IP/UA should get distinct identities: %s", first.ID)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("expected distinct session ids")
	}
	firstCookie := firstRec.Result().Cookies()
	if len(firstCookie) == 0 || firstCookie[0].Value == "" {
		t.Fatalf("expected a session cookie for the browser navigation")
	}
	if firstCookie[0].Value != first.SessionID {
		t.Fatalf("cookie should carry the resolved session id")
	}
}

func TestIdentityBearerSession(t *testing.T) {
	useTempConfig(t)
	e := newIdentityProbe(t)
	_, first := probe(t, e, "203.0.113.5:1234", map[string]string{"Authorization": "Bearer token-1"})
	_, second := probe(t, e, "203.0.113.5:1235", map[string]string{"Authorization": "Bearer token-1"})
	_, other := probe(t, e, "203.0.113.5:1234", map[string]string{"Authorization": "Bearer token-2"})
	if first.ID != second.ID {
		t.Fatalf("bearer identity should be stable: %s != %s", first.ID, second.ID)
	}
	if first.ID == other.ID {
		t.Fatalf("different tokens should yield different identities")
	}
}

func TestIdentityRequireIdentity(t *testing.T) {
	useTempConfig(t)
	cfg := defaultAccessConfig()
	cfg.Auth.RequireIdentity = true
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	e := newIdentityProbe(t)
	rec, _ := probe(t, e, "203.0.113.5:1234", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for anonymous, got %d", rec.Code)
	}
	rec, _ = probe(t, e, "127.0.0.1:1234", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for untrusted forwarded headers, got %d", rec.Code)
	}
}
