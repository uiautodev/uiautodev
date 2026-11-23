package webserver

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// OccupancyState describes the caller's relationship to a device.
type OccupancyState string

const (
	OccupancyFree      OccupancyState = "free"
	OccupancyMine      OccupancyState = "mine"
	OccupancyShared    OccupancyState = "shared"
	OccupancyViewing   OccupancyState = "viewing"
	OccupancyBusy      OccupancyState = "busy"
	OccupancyPreempted OccupancyState = "preempted"
)

// OccupancyStateList lists every supported occupancy state.
var OccupancyStateList = []OccupancyState{
	OccupancyFree, OccupancyMine, OccupancyShared, OccupancyViewing, OccupancyBusy, OccupancyPreempted,
}

// Valid reports whether the state is one of the supported values.
func (s OccupancyState) Valid() bool {
	for _, state := range OccupancyStateList {
		if s == state {
			return true
		}
	}
	return false
}

// Occupant is one identity using a device, aggregated across its sessions.
type Occupant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Anonymous bool      `json:"anonymous"`
	Sessions  int       `json:"sessions"`
	LastSeen  time.Time `json:"last_seen"`
}

// Notice is attached to an occupancy after the caller was preempted.
type Notice struct {
	Reason string    `json:"reason"`
	By     Occupant  `json:"by"`
	At     time.Time `json:"at"`
}

// Occupancy is the occupancy snapshot of one device for one caller.
type Occupancy struct {
	DeviceID    string         `json:"device_id"`
	State       OccupancyState `json:"state"`
	InUse       bool           `json:"in_use"`
	Mine        bool           `json:"mine"`
	Since       *time.Time     `json:"since,omitempty"`
	Occupants   []Occupant     `json:"occupants,omitempty"`
	Observers   []Occupant     `json:"observers,omitempty"`
	Policy      ConflictPolicy `json:"policy"`
	CanTakeover bool           `json:"can_takeover"`
	CanObserve  bool           `json:"can_observe"`
	Notice      *Notice        `json:"notice,omitempty"`
}

// PresenceError carries the HTTP status and occupancy for a rejected presence
// operation.
type PresenceError struct {
	Status    int
	Code      string
	Message   string
	Occupancy Occupancy
}

func (e *PresenceError) Error() string { return e.Code }

type presenceSession struct {
	SessionID string
	Identity  Identity
	Mode      DeviceAccessMode
	StartedAt time.Time
	LastSeen  time.Time
	Remote    string
	UA        string
	kicks     map[int]func(reason string)
	// streams counts the websocket streams attached to the session. A session
	// with an open stream is activity even when it sends no HTTP requests, so
	// the idle sweeper leaves it alone.
	streams int
}

type devicePresence struct {
	sessions map[string]*presenceSession
}

type evictionRecord struct {
	DeviceID string
	Reason   string
	By       Occupant
	At       time.Time
}

// PresenceManager tracks device sessions in memory. Occupancy is not
// persisted: restarting the process clears it.
type PresenceManager struct {
	mu         sync.Mutex
	devices    map[string]*devicePresence
	emptySince map[string]time.Time
	evicted    map[string]evictionRecord
	onIdle     func(deviceID string)
	nextKickID int
	now        func() time.Time

	// beforeIdleRelease is a test seam that runs after the sweeper decided to
	// release a device and before the idle claim is re-checked, so a concurrent
	// re-acquire can be reproduced deterministically. Nil in production.
	beforeIdleRelease func(deviceID string)

	stop chan struct{}
	once sync.Once
}

// NewPresenceManager creates an empty manager. onIdle is invoked (outside the
// lock) when a device becomes idle and its resource should be released.
func NewPresenceManager(onIdle func(deviceID string)) *PresenceManager {
	return &PresenceManager{
		devices:    make(map[string]*devicePresence),
		emptySince: make(map[string]time.Time),
		evicted:    make(map[string]evictionRecord),
		onIdle:     onIdle,
		now:        time.Now,
		stop:       make(chan struct{}),
	}
}

// Start launches the background sweeper. It is safe to call more than once.
func (m *PresenceManager) Start() {
	m.once.Do(func() {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-m.stop:
					return
				case <-ticker.C:
					m.sweep(m.now())
				}
			}
		}()
	})
}

// Close stops the background sweeper.
func (m *PresenceManager) Close() {
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
}

func (m *PresenceManager) sessionLocked(deviceID, sessionID string) *presenceSession {
	if device := m.devices[deviceID]; device != nil {
		return device.sessions[sessionID]
	}
	return nil
}

// Mode returns the access mode of an existing session, if any.
func (m *PresenceManager) Mode(deviceID, sessionID string) (DeviceAccessMode, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessionLocked(deviceID, sessionID); session != nil {
		return session.Mode, true
	}
	return "", false
}

// Eviction returns a preemption notice for the session, if it was preempted
// within the retention window.
func (m *PresenceManager) Eviction(deviceID, sessionID string) *Notice {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.evicted[sessionID]
	if !ok || record.DeviceID != deviceID {
		return nil
	}
	occ := record.By
	return &Notice{Reason: record.Reason, By: occ, At: record.At}
}

// Acquire creates or refreshes a session, optionally taking the device over
// from the current controller.
func (m *PresenceManager) Acquire(
	deviceID string,
	identity requestIdentity,
	mode DeviceAccessMode,
	takeover bool,
	remote, ua string,
) (Occupancy, *PresenceError) {
	cfg := getAccessConfig().Presence
	now := m.now()

	var kicks []func(string)

	m.mu.Lock()
	if !cfg.Enabled {
		occ := Occupancy{
			DeviceID:   deviceID,
			State:      OccupancyFree,
			Policy:     cfg.ConflictPolicy,
			CanObserve: cfg.AllowObservers,
		}
		m.mu.Unlock()
		return occ, nil
	}

	device := m.devices[deviceID]
	controllers := make([]*presenceSession, 0)
	if device != nil {
		controllers = make([]*presenceSession, 0, len(device.sessions))
		for _, session := range device.sessions {
			if session.Mode == AccessModeControl {
				controllers = append(controllers, session)
			}
		}
	}

	if mode == AccessModeView && !cfg.AllowObservers {
		occ := m.occupancyLocked(deviceID, &identity.Identity, identity.SessionID, cfg)
		m.mu.Unlock()
		return occ, &PresenceError{Status: http.StatusLocked, Code: "device_busy", Message: "observers are disabled", Occupancy: occ}
	}

	if mode == AccessModeControl {
		others := make([]*presenceSession, 0, len(controllers))
		for _, session := range controllers {
			if session.SessionID == identity.SessionID || session.Identity.ID == identity.ID {
				continue
			}
			others = append(others, session)
		}
		if len(others) > 0 {
			if !takeover || cfg.ConflictPolicy != ConflictPolicyPreempt {
				occ := m.occupancyLocked(deviceID, &identity.Identity, identity.SessionID, cfg)
				m.mu.Unlock()
				return occ, &PresenceError{Status: http.StatusLocked, Code: "device_busy", Message: "device is controlled by another user", Occupancy: occ}
			}
			by := occupantFromSessions(identity.Identity, 1, now)
			for _, session := range others {
				delete(device.sessions, session.SessionID)
				m.evicted[session.SessionID] = evictionRecord{
					DeviceID: deviceID,
					Reason:   "preempted",
					By:       by,
					At:       now,
				}
				for _, kick := range session.kicks {
					kicks = append(kicks, kick)
				}
			}
		}
	}

	if device == nil {
		device = &devicePresence{sessions: make(map[string]*presenceSession)}
		m.devices[deviceID] = device
	}

	existing := device.sessions[identity.SessionID]
	if existing == nil {
		device.sessions[identity.SessionID] = &presenceSession{
			SessionID: identity.SessionID,
			Identity:  identity.Identity,
			Mode:      mode,
			StartedAt: now,
			LastSeen:  now,
			Remote:    remote,
			UA:        ua,
			kicks:     make(map[int]func(string)),
		}
	} else {
		existing.Mode = mode
		existing.LastSeen = now
		existing.Identity = identity.Identity
		existing.Remote = remote
		existing.UA = ua
	}
	delete(m.evicted, identity.SessionID)
	delete(m.emptySince, deviceID)

	occ := m.occupancyLocked(deviceID, &identity.Identity, identity.SessionID, cfg)
	m.mu.Unlock()

	for _, kick := range kicks {
		kick("device taken over by " + identity.Name)
	}
	return occ, nil
}

// AcquireAuto implements the fallback used by requests that carry no explicit
// presence intent: take control when the device is free, otherwise observe when
// observers are allowed.
func (m *PresenceManager) AcquireAuto(
	deviceID string,
	identity requestIdentity,
	remote, ua string,
) (Occupancy, *PresenceError) {
	occ, err := m.Acquire(deviceID, identity, AccessModeControl, false, remote, ua)
	if err == nil {
		return occ, nil
	}
	if err.Code == "device_busy" && getAccessConfig().Presence.AllowObservers {
		return m.Acquire(deviceID, identity, AccessModeView, false, remote, ua)
	}
	return occ, err
}

// Touch refreshes the last-seen time of a session.
func (m *PresenceManager) Touch(deviceID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessionLocked(deviceID, sessionID); session != nil {
		session.LastSeen = m.now()
	}
}

// Release removes one session and closes its streams. When the last session
// leaves, the device enters the release grace period.
func (m *PresenceManager) Release(deviceID, sessionID string) {
	now := m.now()
	var kicks []func(string)
	m.mu.Lock()
	device := m.devices[deviceID]
	if device == nil {
		m.mu.Unlock()
		return
	}
	session, ok := device.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return
	}
	for _, kick := range session.kicks {
		kicks = append(kicks, kick)
	}
	delete(device.sessions, sessionID)
	if len(device.sessions) == 0 {
		delete(m.devices, deviceID)
		m.emptySince[deviceID] = now
	}
	m.mu.Unlock()

	// Without closing the streams a released session keeps its screen and input
	// connection alive; the device is then handed to the next user while the
	// previous one can still operate it.
	for _, kick := range kicks {
		kick("device session released")
	}
}

// ReleaseDevice forcefully removes every session of a device and releases the
// device resource immediately.
func (m *PresenceManager) ReleaseDevice(deviceID string) {
	var kicks []func(string)
	m.mu.Lock()
	device := m.devices[deviceID]
	if device != nil {
		for _, session := range device.sessions {
			for _, kick := range session.kicks {
				kicks = append(kicks, kick)
			}
		}
		delete(m.devices, deviceID)
	}
	delete(m.emptySince, deviceID)
	m.mu.Unlock()

	for _, kick := range kicks {
		kick("device released")
	}
	if m.onIdle != nil {
		m.onIdle(deviceID)
	}
}

// Occupancy returns the occupancy of a device for the given caller.
func (m *PresenceManager) Occupancy(deviceID string, identity *Identity, sessionID string) Occupancy {
	cfg := getAccessConfig().Presence
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.occupancyLocked(deviceID, identity, sessionID, cfg)
}

// Snapshot returns the occupancy of every tracked device for the caller.
func (m *PresenceManager) Snapshot(identity *Identity, sessionID string) map[string]Occupancy {
	cfg := getAccessConfig().Presence
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Occupancy, len(m.devices))
	for deviceID := range m.devices {
		out[deviceID] = m.occupancyLocked(deviceID, identity, sessionID, cfg)
	}
	return out
}

func occupantFromSessions(identity Identity, sessions int, lastSeen time.Time) Occupant {
	return Occupant{
		ID:        identity.ID,
		Name:      identity.Name,
		Email:     identity.Email,
		AvatarURL: identity.AvatarURL,
		Anonymous: identity.Anonymous,
		Sessions:  sessions,
		LastSeen:  lastSeen,
	}
}

// occupancyLocked computes the snapshot. Caller must hold m.mu.
func (m *PresenceManager) occupancyLocked(
	deviceID string,
	self *Identity,
	sessionID string,
	cfg PresenceAccessConfig,
) Occupancy {
	occ := Occupancy{
		DeviceID:   deviceID,
		State:      OccupancyFree,
		Policy:     cfg.ConflictPolicy,
		CanObserve: cfg.AllowObservers,
	}
	device := m.devices[deviceID]
	if device == nil || len(device.sessions) == 0 {
		return occ
	}

	controlByID := make(map[string][]*presenceSession)
	viewByID := make(map[string][]*presenceSession)
	var earliest *time.Time
	for _, session := range device.sessions {
		if session.Mode == AccessModeControl {
			controlByID[session.Identity.ID] = append(controlByID[session.Identity.ID], session)
			started := session.StartedAt
			if earliest == nil || started.Before(*earliest) {
				earliest = &started
			}
		} else {
			viewByID[session.Identity.ID] = append(viewByID[session.Identity.ID], session)
		}
	}

	occ.Occupants = occupantsFrom(controlByID)
	occ.Observers = occupantsFrom(viewByID)
	occ.InUse = len(occ.Occupants) > 0
	occ.Since = earliest

	selfID := ""
	if self != nil {
		selfID = self.ID
	}
	myControl := controlByID[selfID]
	myView := viewByID[selfID]

	switch {
	case len(myControl) > 0:
		occ.Mine = true
		if len(myControl) > 1 {
			occ.State = OccupancyShared
		} else {
			occ.State = OccupancyMine
		}
	case len(myView) > 0:
		occ.State = OccupancyViewing
	case occ.InUse:
		occ.State = OccupancyBusy
	}

	if occ.InUse && !occ.Mine && cfg.ConflictPolicy == ConflictPolicyPreempt {
		occ.CanTakeover = true
	}
	if record, ok := m.evicted[sessionID]; ok && record.DeviceID == deviceID {
		occ.State = OccupancyPreempted
		occ.Notice = &Notice{Reason: record.Reason, By: record.By, At: record.At}
	}
	return occ
}

func occupantsFrom(groups map[string][]*presenceSession) []Occupant {
	out := make([]Occupant, 0, len(groups))
	for _, sessions := range groups {
		lastSeen := sessions[0].LastSeen
		for _, session := range sessions[1:] {
			if session.LastSeen.After(lastSeen) {
				lastSeen = session.LastSeen
			}
		}
		out = append(out, occupantFromSessions(sessions[0].Identity, len(sessions), lastSeen))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].ID < out[j].ID
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// sweep reaps idle sessions, releases grace-expired devices and drops old
// eviction notices. Callbacks run outside the lock.
func (m *PresenceManager) sweep(now time.Time) {
	cfg := getAccessConfig().Presence
	if !cfg.Enabled {
		return
	}
	idle := cfg.IdleTimeout.Duration()

	var kicks []func(string)
	var releases []string

	m.mu.Lock()
	for deviceID, device := range m.devices {
		idleReaped := false
		for sessionID, session := range device.sessions {
			// An open stream is activity: a session that keeps rendering must
			// not be reaped just because it sends no HTTP requests.
			if session.streams > 0 {
				continue
			}
			if now.Sub(session.LastSeen) > idle {
				delete(device.sessions, sessionID)
				idleReaped = true
				for _, kick := range session.kicks {
					kicks = append(kicks, kick)
				}
			}
		}
		if len(device.sessions) == 0 {
			delete(m.devices, deviceID)
			if idleReaped {
				// Reaching the idle timeout releases the device immediately;
				// only an explicit leave gets the grace period.
				releases = append(releases, deviceID)
			}
		}
	}
	for deviceID, since := range m.emptySince {
		if now.Sub(since) < releaseGrace {
			continue
		}
		delete(m.emptySince, deviceID)
		if _, active := m.devices[deviceID]; !active {
			releases = append(releases, deviceID)
		}
	}
	for sessionID, record := range m.evicted {
		if now.Sub(record.At) > evictionTTL {
			delete(m.evicted, sessionID)
		}
	}
	m.mu.Unlock()

	for _, kick := range kicks {
		kick("device released after idle timeout")
	}
	for _, deviceID := range releases {
		if m.beforeIdleRelease != nil {
			m.beforeIdleRelease(deviceID)
		}
		// The release candidate was collected while the device was empty. A
		// request may have acquired it since: releasing now would close the adb
		// connection of the new session.
		if !m.claimIdleRelease(deviceID) {
			continue
		}
		if m.onIdle != nil {
			m.onIdle(deviceID)
		}
	}
}

// claimIdleRelease re-checks under the lock that the device is still idle.
func (m *PresenceManager) claimIdleRelease(deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if device := m.devices[deviceID]; device != nil && len(device.sessions) > 0 {
		return false
	}
	delete(m.emptySince, deviceID)
	return true
}

// presenceSessionRef identifies the session a request acts on.
type presenceSessionRef struct {
	DeviceID  string
	SessionID string
}

type presenceContextKey struct{}

// presenceSessionFromContext returns the session a stream request holds.
func presenceSessionFromContext(ctx context.Context) *presenceSessionRef {
	ref, _ := ctx.Value(presenceContextKey{}).(*presenceSessionRef)
	return ref
}

// RegisterStreamConn registers a websocket connection with the request's
// presence session so preemption and idle release can close it. It returns an
// unregister function (nil when the request has no session).
//
// While the stream is attached the session is exempt from idle reaping: a
// screen stream that keeps rendering is activity even though it carries no HTTP
// requests.
func (m *PresenceManager) RegisterStreamConn(ctx context.Context, conn *websocket.Conn) func() {
	ref := presenceSessionFromContext(ctx)
	if ref == nil {
		return nil
	}
	kick := func(reason string) {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(4001, reason),
			time.Now().Add(2*time.Second),
		)
		_ = conn.Close()
	}
	return m.attachStream(ref, kick)
}

// attachStream registers the kick callback of one stream connection and marks
// the session as streaming. It returns an unregister function (nil when the
// session no longer exists).
func (m *PresenceManager) attachStream(ref *presenceSessionRef, kick func(string)) func() {
	m.mu.Lock()
	session := m.sessionLocked(ref.DeviceID, ref.SessionID)
	if session == nil {
		m.mu.Unlock()
		return nil
	}
	if session.kicks == nil {
		session.kicks = make(map[int]func(string))
	}
	m.nextKickID++
	id := m.nextKickID
	session.kicks[id] = kick
	session.streams++
	session.LastSeen = m.now()
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if session := m.sessionLocked(ref.DeviceID, ref.SessionID); session != nil {
				delete(session.kicks, id)
				if session.streams > 0 {
					session.streams--
				}
				session.LastSeen = m.now()
			}
		})
	}
}

// RegisterStreamConn is the package-level hook installed on the device package.
func RegisterStreamConn(ctx context.Context, conn *websocket.Conn) func() {
	return presenceManagerInstance.RegisterStreamConn(ctx, conn)
}

// StreamReadOnly reports whether the stream request belongs to a view-only
// session. The scrcpy and screen-frames websockets are bidirectional, so the
// device layer must drop input messages from observers instead of relying on the
// frontend to disable controls.
func StreamReadOnly(ctx context.Context) bool {
	ref := presenceSessionFromContext(ctx)
	if ref == nil {
		return false
	}
	mode, ok := presenceManagerInstance.Mode(ref.DeviceID, ref.SessionID)
	return ok && mode == AccessModeView
}

// isControlRequest reports whether the request mutates or controls a device.
func isControlRequest(c echo.Context) bool {
	method := c.Request().Method
	path := c.Request().URL.Path
	if method == http.MethodGet || method == http.MethodHead {
		return strings.HasSuffix(path, "/connect")
	}
	if method == http.MethodOptions {
		return false
	}
	switch {
	case strings.HasPrefix(path, "/proxy/devices/"):
		return true
	case strings.Contains(path, "/actions/"):
		return true
	case strings.HasSuffix(path, "/shell"):
		return true
	case strings.HasSuffix(path, "/connect"):
		return true
	case strings.HasSuffix(path, "/clipboard"):
		return true
	case strings.HasSuffix(path, "/start"), strings.HasSuffix(path, "/stop"):
		return true
	}
	return false
}

// isPresenceEndpoint reports whether the path is served by the presence API
// itself and must bypass the presence middleware.
func isPresenceEndpoint(c echo.Context) bool {
	return strings.HasSuffix(c.Request().URL.Path, "/presence")
}

// isPassiveRequest reports device requests that must not claim a device by
// themselves, such as editing metadata from the device list. They still refresh
// an existing session.
func isPassiveRequest(c echo.Context) bool {
	return strings.HasSuffix(c.Request().URL.Path, "/meta")
}

func writePresenceError(c echo.Context, err *PresenceError) error {
	return c.JSONPretty(err.Status, APIResponse{
		Code:    err.Status,
		Message: err.Code,
		Data:    err.Occupancy,
	}, "  ")
}

// presenceMiddleware auto-creates sessions for device requests, refreshes their
// last-seen time and enforces the read-only/observer rules.
func presenceMiddleware(manager *PresenceManager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			deviceID := c.Param("device_id")
			if deviceID == "" || isPresenceEndpoint(c) {
				return next(c)
			}
			if c.Request().Method == http.MethodOptions {
				return next(c)
			}
			cfg := getAccessConfig().Presence
			if !cfg.Enabled {
				return next(c)
			}
			identity, ok := identityFrom(c)
			if !ok {
				return next(c)
			}

			if notice := manager.Eviction(deviceID, identity.SessionID); notice != nil {
				occ := manager.Occupancy(deviceID, &identity.Identity, identity.SessionID)
				occ.State = OccupancyPreempted
				occ.Notice = notice
				return writePresenceError(c, &PresenceError{
					Status:    http.StatusConflict,
					Code:      "preempted",
					Message:   "device session was preempted",
					Occupancy: occ,
				})
			}

			if isPassiveRequest(c) {
				if _, exists := manager.Mode(deviceID, identity.SessionID); exists {
					manager.Touch(deviceID, identity.SessionID)
					ref := &presenceSessionRef{DeviceID: deviceID, SessionID: identity.SessionID}
					c.Set(presenceCtxKey, ref)
					ctx := context.WithValue(c.Request().Context(), presenceContextKey{}, ref)
					c.SetRequest(c.Request().WithContext(ctx))
				}
				return next(c)
			}

			if mode, exists := manager.Mode(deviceID, identity.SessionID); exists && mode == AccessModeView && isControlRequest(c) {
				occ := manager.Occupancy(deviceID, &identity.Identity, identity.SessionID)
				return writePresenceError(c, &PresenceError{
					Status:    http.StatusForbidden,
					Code:      "read_only",
					Message:   "view-only session cannot control the device",
					Occupancy: occ,
				})
			}

			remote := clientIP(c)
			ua := c.Request().UserAgent()
			var perr *PresenceError
			if isControlRequest(c) {
				_, perr = manager.Acquire(deviceID, *identity, AccessModeControl, false, remote, ua)
			} else {
				_, perr = manager.AcquireAuto(deviceID, *identity, remote, ua)
			}
			if perr != nil {
				return writePresenceError(c, perr)
			}

			ref := &presenceSessionRef{DeviceID: deviceID, SessionID: identity.SessionID}
			c.Set(presenceCtxKey, ref)
			ctx := context.WithValue(c.Request().Context(), presenceContextKey{}, ref)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// presencePutRequest is the body of the single-device presence endpoint.
type presencePutRequest struct {
	Mode     DeviceAccessMode `json:"mode"`
	Takeover bool             `json:"takeover"`
}

// presenceBatchRequest is the body of the batch presence endpoint.
type presenceBatchRequest struct {
	Devices map[string]presencePutRequest `json:"devices"`
}

// presencePutHandler handles PUT /api/v1/devices/:device_id/presence.
func presencePutHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	identity, ok := identityFrom(c)
	if !ok {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "identity unavailable"}, "  ")
	}
	var req presencePutRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	if req.Mode == "" {
		req.Mode = AccessModeControl
	}
	if !req.Mode.Valid() {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid mode"}, "  ")
	}
	occ, perr := presenceManagerInstance.Acquire(deviceID, *identity, req.Mode, req.Takeover, clientIP(c), c.Request().UserAgent())
	if perr != nil {
		return writePresenceError(c, perr)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: occ}, "  ")
}

// presenceDeleteHandler handles DELETE /api/v1/devices/:device_id/presence.
func presenceDeleteHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	if identity, ok := identityFrom(c); ok {
		presenceManagerInstance.Release(deviceID, identity.SessionID)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok"}, "  ")
}

// presenceSnapshotHandler handles GET /api/v1/presence.
func presenceSnapshotHandler(c echo.Context) error {
	identity, ok := identityFrom(c)
	if !ok {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "identity unavailable"}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"devices": presenceManagerInstance.Snapshot(&identity.Identity, identity.SessionID)},
	}, "  ")
}

// presenceBatchPutHandler handles PUT /api/v1/presence. Devices that are busy
// are reported in the response instead of failing the whole batch.
func presenceBatchPutHandler(c echo.Context) error {
	identity, ok := identityFrom(c)
	if !ok {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "identity unavailable"}, "  ")
	}
	var req presenceBatchRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	result := make(map[string]Occupancy, len(req.Devices))
	for deviceID, entry := range req.Devices {
		mode := entry.Mode
		if mode == "" {
			mode = AccessModeControl
		}
		if !mode.Valid() {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid mode for " + deviceID}, "  ")
		}
		occ, perr := presenceManagerInstance.Acquire(deviceID, *identity, mode, entry.Takeover, clientIP(c), c.Request().UserAgent())
		if perr != nil {
			result[deviceID] = perr.Occupancy
			continue
		}
		result[deviceID] = occ
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"devices": result},
	}, "  ")
}

var presenceManagerInstance = NewPresenceManager(func(deviceID string) {
	deviceManagerInstance.ReleaseDevice(deviceID)
})

func init() {
	presenceManagerInstance.Start()
}
