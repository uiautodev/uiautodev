package webserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// useTempConfig points the config loader at a fresh file for the duration of a
// test and clears the access config cache afterwards.
func useTempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	SetConfigFile(path)
	ResetAccessConfigCache()
	t.Cleanup(func() {
		SetConfigFile("")
		ResetAccessConfigCache()
	})
	return path
}

func TestAccessConfigDefaults(t *testing.T) {
	useTempConfig(t)
	cfg := getAccessConfig()
	if !cfg.Presence.Enabled {
		t.Fatalf("expected presence enabled by default")
	}
	if cfg.Presence.ConflictPolicy != ConflictPolicyPreempt {
		t.Fatalf("unexpected default policy: %s", cfg.Presence.ConflictPolicy)
	}
	if cfg.Presence.IdleTimeout != IdleTimeout10m {
		t.Fatalf("unexpected default idle timeout: %s", cfg.Presence.IdleTimeout)
	}
	if !cfg.Presence.AllowObservers {
		t.Fatalf("expected observers allowed by default")
	}
	if len(cfg.Auth.TrustedProxies) != 0 {
		t.Fatalf("expected no trusted proxies by default")
	}
}

func TestAccessConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  AccessConfig
	}{
		{name: "bad cidr", cfg: AccessConfig{Auth: AuthAccessConfig{TrustedProxies: []string{"not-an-ip"}}}},
		{name: "bad policy", cfg: AccessConfig{Presence: PresenceAccessConfig{ConflictPolicy: "nope", IdleTimeout: IdleTimeout10m}}},
		{name: "bad idle", cfg: AccessConfig{Presence: PresenceAccessConfig{ConflictPolicy: ConflictPolicyPreempt, IdleTimeout: "2m"}}},
		{name: "bad signout", cfg: AccessConfig{Auth: AuthAccessConfig{SignoutURL: "javascript:alert(1)"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateAccessConfig(tc.cfg); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestAccessConfigRoundTrip(t *testing.T) {
	path := useTempConfig(t)
	cfg := defaultAccessConfig()
	cfg.Auth.TrustedProxies = []string{"127.0.0.1/32", "10.0.0.1"}
	cfg.Auth.SignoutURL = "/oauth2/sign_out"
	cfg.Presence.ConflictPolicy = ConflictPolicyExclusive
	cfg.Presence.IdleTimeout = IdleTimeout5m
	cfg.Presence.AllowObservers = false

	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded := getAccessConfig()
	if loaded.Presence.ConflictPolicy != ConflictPolicyExclusive {
		t.Fatalf("policy not persisted: %s", loaded.Presence.ConflictPolicy)
	}
	if loaded.Presence.IdleTimeout != IdleTimeout5m {
		t.Fatalf("idle timeout not persisted: %s", loaded.Presence.IdleTimeout)
	}
	if loaded.Presence.AllowObservers {
		t.Fatalf("allow observers not persisted")
	}
	if len(loaded.Auth.TrustedProxies) != 2 {
		t.Fatalf("trusted proxies not persisted: %v", loaded.Auth.TrustedProxies)
	}

	// A fresh process must see the same values.
	ResetAccessConfigCache()
	reloaded := getAccessConfig()
	if reloaded.Presence.ConflictPolicy != ConflictPolicyExclusive {
		t.Fatalf("policy lost after cache reset: %s", reloaded.Presence.ConflictPolicy)
	}
	if _, err := parseCIDR("10.0.0.1"); err != nil {
		t.Fatalf("single IP should be accepted: %v", err)
	}
	if path == "" {
		t.Fatal("expected config path")
	}
}

func TestAccessConfigHTTP(t *testing.T) {
	useTempConfig(t)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true})

	get := httptest.NewRequest(http.MethodGet, "/api/v1/access/config", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET failed: %d %s", getRec.Code, getRec.Body.String())
	}
	var loaded struct {
		Data struct {
			Config     AccessConfig `json:"config"`
			ConfigFile string       `json:"config_file"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &loaded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !loaded.Data.Config.Presence.Enabled {
		t.Fatalf("expected presence enabled in response")
	}
	if loaded.Data.ConfigFile == "" {
		t.Fatalf("expected config_file in response")
	}

	invalid := AccessConfig{Auth: AuthAccessConfig{TrustedProxies: []string{"nope"}}}
	body, _ := json.Marshal(invalid)
	put := httptest.NewRequest(http.MethodPut, "/api/v1/access/config", bytes.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, put)
	if putRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid CIDR, got %d %s", putRec.Code, putRec.Body.String())
	}

	valid := defaultAccessConfig()
	valid.Auth.TrustedProxies = []string{"127.0.0.1/32"}
	valid.Presence.IdleTimeout = IdleTimeout30m
	body, _ = json.Marshal(valid)
	put = httptest.NewRequest(http.MethodPut, "/api/v1/access/config", bytes.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putRec = httptest.NewRecorder()
	e.ServeHTTP(putRec, put)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d %s", putRec.Code, putRec.Body.String())
	}
	if got := getAccessConfig().Presence.IdleTimeout; got != IdleTimeout30m {
		t.Fatalf("config not applied immediately: %s", got)
	}
}

func TestMeEndpoint(t *testing.T) {
	useTempConfig(t)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.RemoteAddr = "198.51.100.9:1000"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me failed: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data meResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if !body.Data.Anonymous || body.Data.AuthSource != AuthSourceAnonymous {
		t.Fatalf("expected anonymous identity: %+v", body.Data)
	}
	if !body.Data.PresenceEnabled {
		t.Fatalf("expected presence enabled")
	}
	if body.Data.ID == "" {
		t.Fatalf("expected identity id")
	}
}

// TestAccessConfigAuthOnlyKeepsPresenceDefaults covers the documented
// oauth2-proxy deployment: config.json only carries access.auth, so the
// presence section keeps its defaults instead of silently disabling occupancy.
func TestAccessConfigAuthOnlyKeepsPresenceDefaults(t *testing.T) {
	path := useTempConfig(t)
	raw := `{"access":{"auth":{"trusted_proxies":["127.0.0.1/32"]}}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ResetAccessConfigCache()

	cfg := getAccessConfig()
	if !cfg.Presence.Enabled {
		t.Fatalf("presence must stay enabled when only access.auth is configured")
	}
	if !cfg.Presence.AllowObservers {
		t.Fatalf("observers must stay allowed when only access.auth is configured")
	}
	if cfg.Presence.ConflictPolicy != ConflictPolicyPreempt {
		t.Fatalf("unexpected policy: %s", cfg.Presence.ConflictPolicy)
	}
	if cfg.Presence.IdleTimeout != IdleTimeout10m {
		t.Fatalf("unexpected idle timeout: %s", cfg.Presence.IdleTimeout)
	}
	if len(cfg.Auth.TrustedProxies) != 1 || cfg.Auth.TrustedProxies[0] != "127.0.0.1/32" {
		t.Fatalf("auth section lost: %+v", cfg.Auth)
	}
}

// TestAccessConfigExplicitFalseIsKept makes sure filling the defaults does not
// override values the user actually turned off.
func TestAccessConfigExplicitFalseIsKept(t *testing.T) {
	path := useTempConfig(t)
	raw := `{"access":{"presence":{"enabled":false,"allow_observers":false}}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ResetAccessConfigCache()

	cfg := getAccessConfig()
	if cfg.Presence.Enabled {
		t.Fatalf("presence.enabled=false was ignored")
	}
	if cfg.Presence.AllowObservers {
		t.Fatalf("presence.allow_observers=false was ignored")
	}
	if cfg.Presence.ConflictPolicy != ConflictPolicyPreempt || cfg.Presence.IdleTimeout != IdleTimeout10m {
		t.Fatalf("unspecified presence keys should keep their defaults: %+v", cfg.Presence)
	}
}

func TestAccessConfigInvalidFileFallsBackToDefaults(t *testing.T) {
	useTempConfig(t)
	cfg := defaultAccessConfig()
	cfg.Presence.IdleTimeout = "bogus"
	// Bypass validation to simulate a hand-edited bad file.
	if err := mutateConfig(func(app *appConfig) {
		app.Access = &cfg
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	ResetAccessConfigCache()
	loaded := getAccessConfig()
	if loaded.Presence.IdleTimeout != IdleTimeout10m {
		t.Fatalf("expected defaults after invalid file, got %s", loaded.Presence.IdleTimeout)
	}
}
