package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The desktop app is a single-user client: device presence is always off there,
// even when the stored configuration enables it, so the webview and plugin
// iframes never compete for a device (which used to surface as read_only).
func TestDesktopAudienceDisablesPresence(t *testing.T) {
	useTempConfig(t)
	t.Cleanup(func() { SetAudience(AudienceServer) })

	cfg := defaultAccessConfig()
	cfg.Presence.Enabled = true
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save access config: %v", err)
	}

	SetAudience(AudienceServer)
	if !getAccessConfig().Presence.Enabled {
		t.Fatal("expected presence enabled for the standalone server")
	}

	SetAudience(AudienceDesktop)
	if getAccessConfig().Presence.Enabled {
		t.Fatal("expected presence disabled in the desktop app")
	}
	if !storedAccessConfig().Presence.Enabled {
		t.Fatal("expected the stored value to stay enabled for server mode")
	}
}

// Presence endpoints are no-ops in the desktop app: they report a free device
// instead of creating a session that could later block control.
func TestDesktopPresenceEndpointsAreNoOps(t *testing.T) {
	useTempConfig(t)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true, Audience: AudienceDesktop})
	t.Cleanup(func() { SetAudience(AudienceServer) })

	const deviceID = "a-desktop-presence-noop"

	req := httptest.NewRequest(http.MethodPut, "/api/v1/devices/"+deviceID+"/presence", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("presence put failed: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data Occupancy `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode occupancy: %v", err)
	}
	if body.Data.State != OccupancyFree {
		t.Fatalf("expected free occupancy, got %q", body.Data.State)
	}

	snapshot := httptest.NewRecorder()
	e.ServeHTTP(snapshot, httptest.NewRequest(http.MethodGet, "/api/v1/presence", nil))
	var snap struct {
		Data struct {
			Devices map[string]Occupancy `json:"devices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(snapshot.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if _, exists := snap.Data.Devices[deviceID]; exists {
		t.Fatal("desktop presence must not create a session")
	}

	me := httptest.NewRecorder()
	e.ServeHTTP(me, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	var meBody struct {
		Data meResponse `json:"data"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if meBody.Data.PresenceEnabled {
		t.Fatal("expected presence_enabled=false in the desktop app")
	}
}

// The config endpoint reports the deployed audience so the UI can hide settings
// that only apply to the standalone server.
func TestConfigEndpointReportsAudience(t *testing.T) {
	useTempConfig(t)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true, Audience: AudienceDesktop})
	t.Cleanup(func() { SetAudience(AudienceServer) })

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	var body struct {
		Data ServerConfig `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if body.Data.Audience != AudienceDesktop {
		t.Fatalf("expected desktop audience, got %q", body.Data.Audience)
	}
}
