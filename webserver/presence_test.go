package webserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func testIdentity(id, session string) requestIdentity {
	return requestIdentity{
		Identity:  Identity{ID: id, Name: id, Anonymous: true},
		SessionID: session,
		Source:    AuthSourceAnonymous,
	}
}

func newTestPresenceManager(t *testing.T) *PresenceManager {
	t.Helper()
	useTempConfig(t)
	manager := NewPresenceManager(nil)
	t.Cleanup(manager.Close)
	return manager
}

func TestPresenceSameIdentitySharesDevice(t *testing.T) {
	manager := newTestPresenceManager(t)
	first := testIdentity("anon:abc", "sess-1")
	second := testIdentity("anon:abc", "sess-2")

	occ, err := manager.Acquire("a-1", first, AccessModeControl, false, "127.0.0.1", "ua")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if occ.State != OccupancyMine {
		t.Fatalf("expected mine, got %s", occ.State)
	}
	occ, err = manager.Acquire("a-1", second, AccessModeControl, false, "127.0.0.1", "ua")
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	if occ.State != OccupancyShared {
		t.Fatalf("expected shared, got %s", occ.State)
	}
	if len(occ.Occupants) != 1 || occ.Occupants[0].Sessions != 2 {
		t.Fatalf("expected one occupant with two sessions: %+v", occ.Occupants)
	}
}

func TestPresenceConflictAndPreempt(t *testing.T) {
	manager := newTestPresenceManager(t)
	alice := testIdentity("anon:alice", "sess-a")
	bob := testIdentity("anon:bob", "sess-b")

	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("alice acquire failed: %v", err)
	}

	occ, err := manager.Acquire("a-1", bob, AccessModeControl, false, "ip", "ua")
	if err == nil {
		t.Fatalf("expected device_busy")
	}
	if err.Status != http.StatusLocked || err.Code != "device_busy" {
		t.Fatalf("unexpected error: %+v", err)
	}
	if !occ.CanTakeover {
		t.Fatalf("expected can_takeover under preempt policy")
	}

	occ, err = manager.Acquire("a-1", bob, AccessModeControl, true, "ip", "ua")
	if err != nil {
		t.Fatalf("takeover failed: %v", err)
	}
	if occ.State != OccupancyMine || !occ.Mine {
		t.Fatalf("expected bob to own the device, got %+v", occ)
	}

	notice := manager.Eviction("a-1", alice.SessionID)
	if notice == nil {
		t.Fatalf("expected an eviction notice for alice")
	}
	if notice.By.ID != bob.ID {
		t.Fatalf("unexpected preemptor: %s", notice.By.ID)
	}
	occ = manager.Occupancy("a-1", &alice.Identity, alice.SessionID)
	if occ.State != OccupancyPreempted {
		t.Fatalf("expected preempted state, got %s", occ.State)
	}
}

func TestPresenceExclusivePolicyRejectsTakeover(t *testing.T) {
	manager := newTestPresenceManager(t)
	cfg := defaultAccessConfig()
	cfg.Presence.ConflictPolicy = ConflictPolicyExclusive
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	alice := testIdentity("anon:alice", "sess-a")
	bob := testIdentity("anon:bob", "sess-b")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("alice acquire failed: %v", err)
	}
	occ, err := manager.Acquire("a-1", bob, AccessModeControl, true, "ip", "ua")
	if err == nil || err.Code != "device_busy" {
		t.Fatalf("expected busy, got %+v", err)
	}
	if occ.CanTakeover {
		t.Fatalf("exclusive policy must not offer takeover")
	}
}

func TestPresenceObserverKeepsController(t *testing.T) {
	manager := newTestPresenceManager(t)
	alice := testIdentity("anon:alice", "sess-a")
	bob := testIdentity("anon:bob", "sess-b")

	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("alice acquire failed: %v", err)
	}
	occ, err := manager.Acquire("a-1", bob, AccessModeView, false, "ip", "ua")
	if err != nil {
		t.Fatalf("observer acquire failed: %v", err)
	}
	if occ.State != OccupancyViewing {
		t.Fatalf("expected viewing, got %s", occ.State)
	}
	if len(occ.Occupants) != 1 || occ.Occupants[0].ID != alice.ID {
		t.Fatalf("controller should be preserved: %+v", occ.Occupants)
	}
	if len(occ.Observers) != 1 || occ.Observers[0].ID != bob.ID {
		t.Fatalf("observer missing: %+v", occ.Observers)
	}

	// Preemption removes controllers but keeps observers.
	carol := testIdentity("anon:carol", "sess-c")
	if _, err := manager.Acquire("a-1", carol, AccessModeControl, true, "ip", "ua"); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}
	if notice := manager.Eviction("a-1", bob.SessionID); notice != nil {
		t.Fatalf("observer must not be preempted")
	}
	if mode, ok := manager.Mode("a-1", bob.SessionID); !ok || mode != AccessModeView {
		t.Fatalf("observer session should survive takeover")
	}
}

func TestPresenceObserversDisabled(t *testing.T) {
	manager := newTestPresenceManager(t)
	cfg := defaultAccessConfig()
	cfg.Presence.AllowObservers = false
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	alice := testIdentity("anon:alice", "sess-a")
	bob := testIdentity("anon:bob", "sess-b")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("alice acquire failed: %v", err)
	}
	if _, err := manager.Acquire("a-1", bob, AccessModeView, false, "ip", "ua"); err == nil || err.Code != "device_busy" {
		t.Fatalf("expected busy when observers are disabled, got %+v", err)
	}
	if _, err := manager.AcquireAuto("a-1", bob, "ip", "ua"); err == nil || err.Code != "device_busy" {
		t.Fatalf("auto acquire should fail when observers are disabled, got %+v", err)
	}

	// On a free device auto acquire still takes control.
	if _, err := manager.AcquireAuto("a-2", bob, "ip", "ua"); err != nil {
		t.Fatalf("auto acquire on free device failed: %v", err)
	}
}

func TestPresenceIdleSweepReleasesDevice(t *testing.T) {
	manager := newTestPresenceManager(t)
	now := time.Now()
	manager.now = func() time.Time { return now }

	var released []string
	manager.onIdle = func(deviceID string) { released = append(released, deviceID) }

	cfg := defaultAccessConfig()
	cfg.Presence.IdleTimeout = IdleTimeout1m
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	alice := testIdentity("anon:alice", "sess-a")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	manager.sweep(now.Add(30 * time.Second))
	if len(released) != 0 {
		t.Fatalf("device released before idle timeout: %v", released)
	}

	later := now.Add(2 * time.Minute)
	manager.now = func() time.Time { return later }
	manager.sweep(later)
	if len(released) != 1 || released[0] != "a-1" {
		t.Fatalf("expected a-1 release, got %v", released)
	}
	if _, ok := manager.Mode("a-1", alice.SessionID); ok {
		t.Fatalf("session should be removed after idle sweep")
	}
}

func TestPresenceReleaseGrace(t *testing.T) {
	manager := newTestPresenceManager(t)
	now := time.Now()
	manager.now = func() time.Time { return now }

	var released []string
	manager.onIdle = func(deviceID string) { released = append(released, deviceID) }

	alice := testIdentity("anon:alice", "sess-a")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	manager.Release("a-1", alice.SessionID)

	manager.sweep(now.Add(10 * time.Second))
	if len(released) != 0 {
		t.Fatalf("device released inside the grace period")
	}

	// Rejoining inside the grace period cancels the release.
	rejoinAt := now.Add(20 * time.Second)
	manager.now = func() time.Time { return rejoinAt }
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("re-acquire failed: %v", err)
	}
	manager.sweep(now.Add(40 * time.Second))
	if len(released) != 0 {
		t.Fatalf("release was not cancelled by rejoining")
	}

	// The grace period restarts on the next release.
	releaseAt := now.Add(50 * time.Second)
	manager.now = func() time.Time { return releaseAt }
	manager.Release("a-1", alice.SessionID)
	manager.sweep(now.Add(70 * time.Second))
	if len(released) != 0 {
		t.Fatalf("device released too early: grace should restart from the last release")
	}
	manager.sweep(now.Add(81 * time.Second))
	if len(released) != 1 {
		t.Fatalf("expected one release after grace, got %v", released)
	}
}

func TestPresenceEvictionExpires(t *testing.T) {
	manager := newTestPresenceManager(t)
	now := time.Now()
	manager.now = func() time.Time { return now }

	alice := testIdentity("anon:alice", "sess-a")
	bob := testIdentity("anon:bob", "sess-b")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if _, err := manager.Acquire("a-1", bob, AccessModeControl, true, "ip", "ua"); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}
	if manager.Eviction("a-1", alice.SessionID) == nil {
		t.Fatalf("expected eviction notice")
	}

	later := now.Add(evictionTTL + time.Second)
	manager.now = func() time.Time { return later }
	manager.sweep(later)
	if manager.Eviction("a-1", alice.SessionID) != nil {
		t.Fatalf("eviction notice should expire")
	}
}

func TestPresenceHTTPFlow(t *testing.T) {
	useTempConfig(t)
	e := CreateEchoServerWithConfig(ServerConfig{DisableDeviceListing: true})
	t.Cleanup(func() { presenceManagerInstance.ReleaseDevice("a-1") })

	request := func(method, path string, cookie *http.Cookie, body string, ua ...string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "198.51.100.7:4321"
		if len(ua) > 0 {
			req.Header.Set("User-Agent", ua[0])
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	sessionCookie := func(rec *httptest.ResponseRecorder) *http.Cookie {
		for _, cookie := range rec.Result().Cookies() {
			if cookie.Name == sessionCookieName {
				return cookie
			}
		}
		return nil
	}

	// First client controls the device explicitly.
	first := request(http.MethodPut, "/api/v1/devices/a-1/presence", nil, `{"mode":"control"}`, "browser-a")
	alice := sessionCookie(first)
	if alice == nil {
		t.Fatalf("expected a session cookie")
	}
	if occ := decodeOccupancy(t, first); occ.State != OccupancyMine {
		t.Fatalf("expected alice to control the device, got %s", occ.State)
	}

	// A metadata edit is passive: it must not claim the device for a new user.
	second := request(http.MethodPut, "/api/v1/devices/a-1/meta", nil, `{"remark":"bob"}`, "browser-b")
	bob := sessionCookie(second)
	if bob == nil || bob.Value == alice.Value {
		t.Fatalf("expected a distinct session cookie")
	}
	passive := request(http.MethodGet, "/api/v1/presence", bob, "")
	if occ := decodeSnapshot(t, passive); occ.State != OccupancyBusy {
		t.Fatalf("metadata edit must not claim the device, got %s", occ.State)
	}

	// Reading device content makes bob an observer.
	request(http.MethodGet, "/api/v1/devices/a-1/apps", bob, "")
	if occ := decodeSnapshot(t, request(http.MethodGet, "/api/v1/presence", bob, "")); occ.State != OccupancyViewing {
		t.Fatalf("expected bob to observe, got %s", occ.State)
	}

	// Observers are read-only.
	denied := request(http.MethodPost, "/api/v1/devices/a-1/actions/tap", bob, `{"x":1,"y":1}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for observer control, got %d %s", denied.Code, denied.Body.String())
	}

	// Without takeover, control is rejected with 423.
	blocked := request(http.MethodPut, "/api/v1/devices/a-1/presence", bob, `{"mode":"control"}`)
	if blocked.Code != http.StatusLocked {
		t.Fatalf("expected 423, got %d %s", blocked.Code, blocked.Body.String())
	}

	// Takeover succeeds and notifies the previous controller.
	taken := request(http.MethodPut, "/api/v1/devices/a-1/presence", bob, `{"mode":"control","takeover":true}`)
	if taken.Code != http.StatusOK {
		t.Fatalf("expected takeover, got %d %s", taken.Code, taken.Body.String())
	}
	if occ := decodeOccupancy(t, taken); occ.State != OccupancyMine {
		t.Fatalf("expected bob to own the device, got %s", occ.State)
	}
	preempted := request(http.MethodGet, "/api/v1/devices/a-1/meta", alice, "")
	if preempted.Code != http.StatusConflict {
		t.Fatalf("expected 409 for preempted session, got %d %s", preempted.Code, preempted.Body.String())
	}
}

// decodeOccupancy extracts the occupancy payload from a single-device response.
func decodeOccupancy(t *testing.T, rec *httptest.ResponseRecorder) Occupancy {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data Occupancy `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode occupancy: %v", err)
	}
	return body.Data
}

// decodeSnapshot extracts the occupancy of device a-1 from a snapshot response.
func decodeSnapshot(t *testing.T, rec *httptest.ResponseRecorder) Occupancy {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Devices map[string]Occupancy `json:"devices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return body.Data.Devices["a-1"]
}

// TestPresenceReleaseKicksStreams makes sure an explicit release closes the
// session's websockets. Otherwise the released session keeps its screen and
// input stream while the device can already be acquired by someone else.
func TestPresenceReleaseKicksStreams(t *testing.T) {
	manager := newTestPresenceManager(t)
	alice := testIdentity("anon:alice", "sess-a")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	var reasons []string
	ref := &presenceSessionRef{DeviceID: "a-1", SessionID: alice.SessionID}
	unregister := manager.attachStream(ref, func(reason string) { reasons = append(reasons, reason) })
	if unregister == nil {
		t.Fatalf("expected a registered stream")
	}
	t.Cleanup(unregister)

	manager.Release("a-1", alice.SessionID)
	if len(reasons) != 1 {
		t.Fatalf("expected the release to close the stream, got %v", reasons)
	}
	if _, ok := manager.Mode("a-1", alice.SessionID); ok {
		t.Fatalf("session should be gone after release")
	}
}

// TestPresenceActiveStreamPreventsIdleRelease covers a screen stream that keeps
// rendering but sends no HTTP requests: it must not be reaped by idle_timeout.
func TestPresenceActiveStreamPreventsIdleRelease(t *testing.T) {
	manager := newTestPresenceManager(t)
	now := time.Now()
	manager.now = func() time.Time { return now }

	var released []string
	manager.onIdle = func(deviceID string) { released = append(released, deviceID) }

	cfg := defaultAccessConfig()
	cfg.Presence.IdleTimeout = IdleTimeout1m
	if _, err := setAccessConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	alice := testIdentity("anon:alice", "sess-a")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	ref := &presenceSessionRef{DeviceID: "a-1", SessionID: alice.SessionID}
	unregister := manager.attachStream(ref, func(string) {})
	if unregister == nil {
		t.Fatalf("expected a registered stream")
	}

	manager.sweep(now.Add(5 * time.Minute))
	if len(released) != 0 {
		t.Fatalf("a streaming session must not be released: %v", released)
	}
	if _, ok := manager.Mode("a-1", alice.SessionID); !ok {
		t.Fatalf("streaming session disappeared")
	}

	unregister()
	manager.sweep(now.Add(2 * time.Minute))
	if len(released) != 1 || released[0] != "a-1" {
		t.Fatalf("expected the device release after the stream ended, got %v", released)
	}
}

// TestPresenceIdleReleaseSkipsReacquiredDevice reproduces the race between the
// sweeper's release decision and a new session: the release callback must not
// run once the device has been acquired again, or it closes the new session's
// adb connection.
func TestPresenceIdleReleaseSkipsReacquiredDevice(t *testing.T) {
	manager := newTestPresenceManager(t)
	now := time.Now()
	manager.now = func() time.Time { return now }

	var released []string
	manager.onIdle = func(deviceID string) { released = append(released, deviceID) }

	alice := testIdentity("anon:alice", "sess-a")
	if _, err := manager.Acquire("a-1", alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	manager.Release("a-1", alice.SessionID)

	bob := testIdentity("anon:bob", "sess-b")
	manager.beforeIdleRelease = func(deviceID string) {
		manager.beforeIdleRelease = nil
		if _, perr := manager.Acquire(deviceID, bob, AccessModeControl, false, "ip", "ua"); perr != nil {
			t.Fatalf("re-acquire failed: %v", perr)
		}
	}
	manager.sweep(now.Add(releaseGrace + time.Second))

	if len(released) != 0 {
		t.Fatalf("device was released despite a new session: %v", released)
	}
	if _, ok := manager.Mode("a-1", bob.SessionID); !ok {
		t.Fatalf("the new session must survive the stale release")
	}
}

// TestPresenceStreamReadOnlyForViewSession checks the flag the device layer uses
// to drop input messages from a bidirectional streaming websocket.
func TestPresenceStreamReadOnlyForViewSession(t *testing.T) {
	useTempConfig(t)
	const deviceID = "a-1-view"
	t.Cleanup(func() { presenceManagerInstance.ReleaseDevice(deviceID) })

	alice := testIdentity("anon:alice", "sess-a")
	bob := testIdentity("anon:bob", "sess-b")
	if _, err := presenceManagerInstance.Acquire(deviceID, alice, AccessModeControl, false, "ip", "ua"); err != nil {
		t.Fatalf("alice acquire failed: %v", err)
	}
	if _, err := presenceManagerInstance.Acquire(deviceID, bob, AccessModeView, false, "ip", "ua"); err != nil {
		t.Fatalf("bob acquire failed: %v", err)
	}

	viewCtx := context.WithValue(context.Background(), presenceContextKey{}, &presenceSessionRef{DeviceID: deviceID, SessionID: bob.SessionID})
	controlCtx := context.WithValue(context.Background(), presenceContextKey{}, &presenceSessionRef{DeviceID: deviceID, SessionID: alice.SessionID})
	if !StreamReadOnly(viewCtx) {
		t.Fatalf("view session stream should be read-only")
	}
	if StreamReadOnly(controlCtx) {
		t.Fatalf("control session stream must not be read-only")
	}
	if StreamReadOnly(context.Background()) {
		t.Fatalf("requests without a presence session are not read-only")
	}
}

func TestIsControlRequest(t *testing.T) {
	e := echo.New()
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/api/v1/devices/a-1", false},
		{http.MethodGet, "/api/v1/devices/a-1/uitree", false},
		{http.MethodGet, "/api/v1/devices/a-1/connect", true},
		{http.MethodDelete, "/api/v1/devices/a-1/connect", true},
		{http.MethodPost, "/api/v1/devices/a-1/actions/tap", true},
		{http.MethodPost, "/api/v1/devices/a-1/shell", true},
		{http.MethodPut, "/api/v1/devices/a-1/clipboard", true},
		{http.MethodPost, "/api/v1/devices/a-1/apps/com.x/start", true},
		{http.MethodGet, "/api/v1/devices/a-1/apps", false},
		{http.MethodPut, "/api/v1/devices/a-1/meta", false},
		{http.MethodDelete, "/api/v1/devices/a-1/meta", false},
		{"ANY", "/proxy/devices/a-1/tcp:8100/status", true},
		{http.MethodGet, "/stream/devices/a-1/screen/scrcpy", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		c := e.NewContext(req, httptest.NewRecorder())
		if got := isControlRequest(c); got != tc.want {
			t.Errorf("%s %s: got %v want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
