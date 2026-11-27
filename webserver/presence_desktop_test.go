package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// useAudience sets the process audience for a test and restores the previous
// one, so desktop-specific tests do not leak presence settings into each other.
func useAudience(t *testing.T, audience Audience) {
	t.Helper()
	previous := currentAudience()
	SetAudience(audience)
	t.Cleanup(func() { SetAudience(previous) })
}

// The desktop app is a single-user client: device presence is always off there,
// even when the stored configuration enables it, so the webview and plugin
// iframes never compete for a device (which used to surface as read_only).
func TestDesktopAudienceDisablesPresence(t *testing.T) {
	useTempConfig(t)
	useAudience(t, AudienceServer)

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
	useAudience(t, AudienceDesktop)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true, Audience: AudienceDesktop})

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

	// Inspect the shared manager with presence enabled: the desktop endpoint
	// must not have created a session in the first place.
	SetAudience(AudienceServer)
	snapshot = httptest.NewRecorder()
	e.ServeHTTP(snapshot, httptest.NewRequest(http.MethodGet, "/api/v1/presence", nil))
	if err := json.Unmarshal(snapshot.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode server snapshot: %v", err)
	}
	if _, exists := snap.Data.Devices[deviceID]; exists {
		t.Fatal("desktop presence must not create a session in the manager")
	}
}

// The multi-device page renews every device with the batch endpoint. On the
// desktop app that request must be a no-op instead of creating sessions.
func TestDesktopBatchPresenceIsNoOp(t *testing.T) {
	useTempConfig(t)
	useAudience(t, AudienceDesktop)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true, Audience: AudienceDesktop})

	body := `{"devices":{"a-desktop-batch-1":{"mode":"control"},"a-desktop-batch-2":{"mode":"view"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/presence", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch presence failed: %d %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Data struct {
			Devices map[string]Occupancy `json:"devices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode batch presence: %v", err)
	}
	if len(res.Data.Devices) != 2 {
		t.Fatalf("expected two devices in the response, got %d", len(res.Data.Devices))
	}
	for deviceID, occ := range res.Data.Devices {
		if occ.State != OccupancyFree {
			t.Fatalf("expected free occupancy for %s, got %q", deviceID, occ.State)
		}
	}

	// The batch endpoint returns free without touching the manager; verify
	// under server mode, where the snapshot is not masked by the audience.
	SetAudience(AudienceServer)
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
	for _, deviceID := range []string{"a-desktop-batch-1", "a-desktop-batch-2"} {
		if _, exists := snap.Data.Devices[deviceID]; exists {
			t.Fatalf("desktop batch presence created a session for %s", deviceID)
		}
	}
}

// The pagehide handler sends DELETE for every visited device. On the desktop
// app it must leave the shared manager alone.
func TestDesktopPresenceDeleteIsNoOp(t *testing.T) {
	useTempConfig(t)
	useAudience(t, AudienceDesktop)

	const deviceID = "a-desktop-presence-delete"
	identity := testIdentity("anon:desktop-delete", "desktop-delete-session")
	// Seed the shared manager as the standalone server would, then switch the
	// desktop audience: the delete endpoint must not touch that session.
	SetAudience(AudienceServer)
	if _, err := presenceManagerInstance.Acquire(deviceID, identity, AccessModeControl, false, "127.0.0.1", "ua"); err != nil {
		t.Fatalf("seed presence session: %v", err)
	}
	t.Cleanup(func() { presenceManagerInstance.Release(deviceID, identity.SessionID) })
	SetAudience(AudienceDesktop)

	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true, Audience: AudienceDesktop})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+deviceID+"/presence", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: identity.SessionID})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("presence delete failed: %d %s", rec.Code, rec.Body.String())
	}

	if _, ok := presenceManagerInstance.Mode(deviceID, identity.SessionID); !ok {
		t.Fatal("desktop presence delete must not release a session")
	}
}

// The config endpoint reports the deployed audience so the UI can hide settings
// that only apply to the standalone server.
func TestConfigEndpointReportsAudience(t *testing.T) {
	useTempConfig(t)
	useAudience(t, AudienceDesktop)
	e := CreateEchoServerWithConfig(ServerConfig{DisablePluginAuth: true, Audience: AudienceDesktop})

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
