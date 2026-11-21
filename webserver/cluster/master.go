package cluster

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/labstack/echo/v4"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// MasterConfig configures the master side of the cluster.
type MasterConfig struct {
	Version string
	// NodeID is this node's stable cluster identity. It is reported to upstream
	// masters and used to detect aggregation cycles.
	NodeID string
	Relay  ADBRelayConfig
}

type slaveState struct {
	record      SlaveRecord
	hello       Hello
	sessions    map[string]*yamux.Session
	connectedAt time.Time
	lastSeen    time.Time

	// directOnline is maintained by the direct-mode poller. version is fetched
	// from the slave's /api/v1/config endpoint in direct mode.
	directOnline bool
	version      string
	nodeID       string
	directClient *http.Client

	snapshot   Snapshot
	snapshotAt time.Time
	refreshing bool
}

func (s *slaveState) online() bool {
	if s.record.IsDirect() {
		return s.directOnline
	}
	return len(s.sessions) > 0
}

func (s *slaveState) displayName() string {
	if s.hello.Name != "" {
		return s.hello.Name
	}
	return s.record.Name
}

func (s *slaveState) session(kind string) *yamux.Session {
	session := s.sessions[kind]
	if session == nil && kind == SessionMedia {
		session = s.sessions[SessionControl]
	}
	return session
}

// Master aggregates slaves, proxies their API and records registrations.
type Master struct {
	store Store
	cfg   MasterConfig

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.RWMutex
	slaves  map[string]*slaveState
	pollers map[string]*directPoller

	relays *relayManager
}

type directPoller struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewMaster(store Store, cfg MasterConfig) *Master {
	if cfg.Relay.Bind == "" && cfg.Relay.PortRange == [2]int{} && cfg.Relay.TTL == 0 {
		cfg.Relay = DefaultADBRelayConfig()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Master{
		store:   store,
		cfg:     cfg,
		ctx:     ctx,
		cancel:  cancel,
		slaves:  make(map[string]*slaveState),
		pollers: make(map[string]*directPoller),
	}
	m.relays = newRelayManager(m, cfg.Relay)
	m.loadRecords()
	return m
}

// NodeID returns this node's stable cluster identity.
func (m *Master) NodeID() string {
	return m.cfg.NodeID
}

// RefreshSnapshot re-fetches a slave's device inventory when it is online.
func (m *Master) RefreshSnapshot(slaveID string) {
	m.mu.RLock()
	state := m.slaves[slaveID]
	online := state != nil && state.online()
	m.mu.RUnlock()
	if online {
		go m.refreshSnapshot(slaveID)
	}
}

func (m *Master) loadRecords() {
	records, err := m.store.ListSlaves()
	if err != nil {
		slog.Warn("cluster: failed to load slave records", "error", err)
		return
	}
	for _, record := range records {
		m.reconcileRecord(record)
	}
}

func (m *Master) Close() {
	m.cancel()

	m.mu.Lock()
	pollers := make([]*directPoller, 0, len(m.pollers))
	for _, p := range m.pollers {
		pollers = append(pollers, p)
	}
	m.pollers = make(map[string]*directPoller)
	sessions := make([]*yamux.Session, 0)
	for _, state := range m.slaves {
		for _, session := range state.sessions {
			sessions = append(sessions, session)
		}
	}
	m.mu.Unlock()

	for _, p := range pollers {
		<-p.done
	}
	for _, session := range sessions {
		_ = session.Close()
	}
	m.relays.Close()
}

func (m *Master) ensureState(record SlaveRecord) *slaveState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.slaves[record.ID]
	if state == nil {
		state = &slaveState{sessions: make(map[string]*yamux.Session)}
		m.slaves[record.ID] = state
	}
	state.record = record
	if record.IsDirect() {
		if state.directClient != nil {
			state.directClient.CloseIdleConnections()
		}
		state.directClient = newDirectClient(record)
	} else {
		state.directClient = nil
	}
	return state
}

// reconcileRecord keeps the in-memory state and direct poller in sync with a
// persisted slave record.
func (m *Master) reconcileRecord(record SlaveRecord) {
	m.ensureState(record)
	if record.IsDirect() && !record.Disabled {
		m.startDirectPoller(record.ID)
		return
	}
	m.stopDirectPoller(record.ID)
}

func (m *Master) startDirectPoller(slaveID string) {
	m.stopDirectPoller(slaveID)

	m.mu.RLock()
	state := m.slaves[slaveID]
	ok := state != nil && state.record.IsDirect() && !state.record.Disabled
	m.mu.RUnlock()
	if !ok {
		return
	}

	ctx, cancel := context.WithCancel(m.ctx)
	poller := &directPoller{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	m.pollers[slaveID] = poller
	m.mu.Unlock()

	go func() {
		defer close(poller.done)
		m.runDirectPoller(ctx, slaveID)
	}()
}

func (m *Master) stopDirectPoller(slaveID string) {
	m.mu.Lock()
	poller := m.pollers[slaveID]
	delete(m.pollers, slaveID)
	m.mu.Unlock()
	if poller != nil {
		poller.cancel()
	}
}

func (m *Master) runDirectPoller(ctx context.Context, slaveID string) {
	m.refreshSnapshot(slaveID)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshSnapshot(slaveID)
		}
	}
}

func newDirectClient(record SlaveRecord) *http.Client {
	transport := &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}
	if record.Direct != nil && record.Direct.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &http.Client{Transport: transport}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// ConnectHandler upgrades a slave's outbound WebSocket and starts its yamux
// session.
func (m *Master) ConnectHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := strings.TrimSpace(strings.TrimPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer "))
		slaveID, secret, err := ParseToken(token)
		if err != nil {
			return writeAPI(c, http.StatusUnauthorized, 401, "invalid slave token", nil)
		}
		record, ok, err := m.store.GetSlave(slaveID)
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		if !ok {
			return writeAPI(c, http.StatusUnauthorized, 401, "unknown slave", nil)
		}
		if record.IsDirect() {
			return writeAPI(c, http.StatusForbidden, 403, "slave is configured for direct mode", nil)
		}
		if record.Disabled {
			return writeAPI(c, http.StatusForbidden, 403, "slave is disabled", nil)
		}
		if subtle.ConstantTimeCompare([]byte(record.TokenHash), []byte(HashSecret(secret))) != 1 {
			return writeAPI(c, http.StatusUnauthorized, 401, "invalid slave token", nil)
		}

		conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return nil
		}

		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		var hello Hello
		if err := conn.ReadJSON(&hello); err != nil {
			slog.Warn("cluster: invalid slave hello", "slave", slaveID, "error", err)
			_ = conn.Close()
			return nil
		}
		_ = conn.SetReadDeadline(time.Time{})

		if hello.ProtocolVersion != ProtocolVersion {
			slog.Warn("cluster: incompatible slave protocol", "slave", slaveID, "got", hello.ProtocolVersion, "want", ProtocolVersion)
			_ = conn.Close()
			return nil
		}
		if hello.SlaveID != "" && hello.SlaveID != slaveID {
			slog.Warn("cluster: slave ID mismatch", "token_id", slaveID, "hello_id", hello.SlaveID)
			_ = conn.Close()
			return nil
		}
		kind := hello.Kind
		if kind != SessionControl && kind != SessionMedia {
			kind = SessionControl
		}
		hello.SlaveID = slaveID
		if hello.Name == "" {
			hello.Name = record.Name
		}

		session, err := yamux.Client(newWSConn(conn), yamuxConfig())
		if err != nil {
			slog.Warn("cluster: failed to start yamux", "slave", slaveID, "error", err)
			_ = conn.Close()
			return nil
		}

		m.attach(record, hello, kind, session)
		go func() {
			<-session.CloseChan()
			m.detach(record.ID, kind, session)
			_ = conn.Close()
		}()
		if kind == SessionControl || m.currentSession(record.ID, SessionControl) != nil {
			go m.refreshSnapshot(record.ID)
		}
		return nil
	}
}

func (m *Master) attach(record SlaveRecord, hello Hello, kind string, session *yamux.Session) {
	m.mu.Lock()
	state := m.slaves[record.ID]
	if state == nil {
		state = &slaveState{sessions: make(map[string]*yamux.Session)}
		m.slaves[record.ID] = state
	}
	old := state.sessions[kind]
	state.record = record
	state.hello = hello
	if hello.NodeID != "" {
		state.nodeID = hello.NodeID
	}
	state.sessions[kind] = session
	now := time.Now()
	state.lastSeen = now
	if state.connectedAt.IsZero() {
		state.connectedAt = now
	}
	m.mu.Unlock()

	if old != nil && old != session {
		_ = old.Close()
	}
	slog.Info("cluster: slave session attached", "slave", record.ID, "kind", kind, "name", hello.Name)
}

func (m *Master) detach(slaveID, kind string, session *yamux.Session) {
	m.mu.Lock()
	state := m.slaves[slaveID]
	if state == nil {
		m.mu.Unlock()
		return
	}
	if state.sessions[kind] != session {
		m.mu.Unlock()
		return
	}
	delete(state.sessions, kind)
	state.lastSeen = time.Now()
	offline := len(state.sessions) == 0
	m.mu.Unlock()

	if offline {
		m.relays.CloseSlave(slaveID)
	}
	slog.Info("cluster: slave session detached", "slave", slaveID, "kind", kind)
}

func (m *Master) currentSession(slaveID, kind string) *yamux.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.slaves[slaveID]
	if state == nil {
		return nil
	}
	return state.session(kind)
}

func (m *Master) transport(slaveID, kind string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			session := m.currentSession(slaveID, kind)
			if session == nil {
				return nil, fmt.Errorf("%w: %s", ErrSlaveOffline, slaveID)
			}
			return openHTTPStream(session)
		},
		DisableKeepAlives:     true,
		MaxIdleConns:          -1,
		IdleConnTimeout:       0,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}

func (m *Master) newClient(slaveID, kind string) *http.Client {
	m.mu.RLock()
	state := m.slaves[slaveID]
	if state != nil && state.record.IsDirect() && state.directClient != nil {
		client := state.directClient
		m.mu.RUnlock()
		return client
	}
	m.mu.RUnlock()
	return &http.Client{Transport: m.transport(slaveID, kind)}
}

// slaveBaseURL returns the request base URL and Basic Auth credentials for a
// slave. Push slaves use the synthetic "http://slave" host carried over the
// yamux tunnel; direct slaves use the configured public origin.
func (m *Master) slaveBaseURL(slaveID string) (string, *DirectConfig) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.slaves[slaveID]
	if state == nil || !state.record.IsDirect() || state.record.Direct == nil {
		return "http://slave", nil
	}
	direct := *state.record.Direct
	return strings.TrimRight(direct.URL, "/"), &direct
}

func (m *Master) newSlaveRequest(ctx context.Context, method, slaveID, path string, body io.Reader) (*http.Request, error) {
	base, direct := m.slaveBaseURL(slaveID)
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	if direct != nil && strings.TrimSpace(direct.Username) != "" {
		req.SetBasicAuth(direct.Username, direct.Password)
	}
	return req, nil
}

func (m *Master) slaveOnline(slaveID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.slaves[slaveID]
	return state != nil && state.online()
}

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// slaveResponseError carries the HTTP status returned by a slave so callers can
// distinguish a missing device from an offline tunnel.
type slaveResponseError struct {
	Status  int
	Message string
}

func (e *slaveResponseError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("slave returned HTTP %d", e.Status)
}

func (m *Master) getJSON(ctx context.Context, slaveID, path string, out any) error {
	req, err := m.newSlaveRequest(ctx, http.MethodGet, slaveID, path, nil)
	if err != nil {
		return err
	}
	resp, err := m.newClient(slaveID, SessionControl).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		if resp.StatusCode/100 != 2 {
			return &slaveResponseError{Status: resp.StatusCode}
		}
		return err
	}
	if resp.StatusCode/100 != 2 {
		return &slaveResponseError{Status: resp.StatusCode, Message: envelope.Message}
	}
	if envelope.Code != 0 {
		status := envelope.Code
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return &slaveResponseError{Status: status, Message: envelope.Message}
	}
	if out != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func (m *Master) refreshSnapshot(slaveID string) {
	m.mu.Lock()
	state := m.slaves[slaveID]
	if state == nil || state.refreshing {
		m.mu.Unlock()
		return
	}
	state.refreshing = true
	direct := state.record.IsDirect()
	record := state.record
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer cancel()

	var snapshot Snapshot
	var version string
	var nodeID string
	var err error
	if direct {
		snapshot, version, nodeID, err = m.fetchDirectSnapshot(ctx, slaveID, record.IncludeNested)
	} else {
		path := "/api/v1/cluster/devices?" + m.snapshotQuery(record).Encode()
		err = m.getJSON(ctx, slaveID, path, &snapshot)
	}

	m.mu.Lock()
	state = m.slaves[slaveID]
	if state == nil {
		m.mu.Unlock()
		return
	}
	state.refreshing = false
	state.snapshotAt = time.Now()
	if err != nil {
		wasOnline := state.directOnline
		if direct {
			state.directOnline = false
		}
		m.mu.Unlock()
		slog.Warn("cluster: refresh devices failed", "slave", slaveID, "direct", direct, "error", err)
		if direct && wasOnline {
			m.relays.CloseSlave(slaveID)
		}
		return
	}
	snapshot.UpdatedAt = time.Now()
	state.snapshot = snapshot
	state.lastSeen = time.Now()
	if direct {
		state.directOnline = true
		if version != "" {
			state.version = version
		}
		if nodeID != "" {
			state.nodeID = nodeID
		}
	}
	m.mu.Unlock()
}

// snapshotQuery builds the query for a slave's device inventory. The requester
// node id lets the slave drop devices that would loop back, and scope=all asks
// for devices the slave aggregated from its own slaves.
func (m *Master) snapshotQuery(record SlaveRecord) url.Values {
	query := url.Values{}
	if m.cfg.NodeID != "" {
		query.Set("visited", m.cfg.NodeID)
	}
	if record.IncludeNested {
		query.Set("scope", "all")
	}
	return query
}

// fetchDirectSnapshot aggregates devices from a slave's standard API. Direct
// slaves run the normal uiautodev-server and therefore expose /api/v1/devices
// rather than the cluster-specific /api/v1/cluster/devices endpoint.
func (m *Master) fetchDirectSnapshot(ctx context.Context, slaveID string, includeNested bool) (Snapshot, string, string, error) {
	var snapshot Snapshot
	var info struct {
		NodeID  string `json:"node_id"`
		Version string `json:"version"`
	}
	if err := m.getJSON(ctx, slaveID, "/api/v1/cluster/info", &info); err != nil {
		var respErr *slaveResponseError
		if errors.As(err, &respErr) && (respErr.Status == http.StatusUnauthorized || respErr.Status == http.StatusForbidden) {
			return snapshot, "", "", err
		}
		// Older slaves do not expose /api/v1/cluster/info (some serve the SPA
		// fallback page instead); fall back to the config endpoint for the
		// version and treat the node as legacy.
		var cfg struct {
			Version string `json:"version"`
		}
		if err := m.getJSON(ctx, slaveID, "/api/v1/config", &cfg); err == nil {
			info.Version = cfg.Version
		}
	}

	// Without the slave's node identity a device chain cannot be built, so
	// nested aggregation would be unable to detect cycles. Treat older slaves
	// as leaf nodes instead.
	if includeNested && info.NodeID == "" {
		slog.Warn("cluster: slave does not report a node id, nested devices are skipped", "slave", slaveID)
		includeNested = false
	}

	ownChain := info.NodeID
	if ownChain == "" {
		// Fall back to the master-assigned id so cycles between this master and
		// a legacy direct slave still resolve (legacy slaves have no nested
		// devices, so the id only needs to be unique locally).
		ownChain = slaveID
	}

	visited := url.Values{}
	if m.cfg.NodeID != "" {
		visited.Set("visited", m.cfg.NodeID)
	}
	querySuffix := ""
	if encoded := visited.Encode(); encoded != "" {
		querySuffix = "&" + encoded
	}

	// Identity-aware slaves are masters too and can serve their aggregated
	// inventory in one call. Prefer it when nested devices are requested: unlike
	// /api/v1/devices it keeps reporting nested devices even when the node's own
	// device enumeration fails (e.g. no working adb), and it applies the visited
	// filter itself.
	if includeNested && info.NodeID != "" {
		var nested Snapshot
		path := "/api/v1/cluster/devices?scope=all" + querySuffix
		if err := m.getJSON(ctx, slaveID, path, &nested); err == nil {
			appendDirectDevices(&snapshot, nested.Devices, ownChain, true)
			return snapshot, info.Version, info.NodeID, nil
		}
		// Fall back to the per-platform API below for nodes that do not support
		// the cluster inventory endpoint.
	}

	for _, platform := range []string{"android", "ios", "harmony", "demo"} {
		var devices []Device
		err := m.getJSON(ctx, slaveID, "/api/v1/devices?platform="+platform+querySuffix, &devices)
		if err != nil {
			var respErr *slaveResponseError
			if errors.As(err, &respErr) && respErr.Status != http.StatusUnauthorized && respErr.Status != http.StatusForbidden {
				// Unsupported platform or device listing disabled; skip it.
				continue
			}
			return snapshot, "", "", err
		}
		appendDirectDevices(&snapshot, devices, ownChain, includeNested)
	}
	return snapshot, info.Version, info.NodeID, nil
}

// appendDirectDevices copies devices reported by a direct slave into a snapshot,
// skipping nested ones (unless enabled) and filling in the node chain when the
// slave did not.
func appendDirectDevices(snapshot *Snapshot, devices []Device, ownChain string, includeNested bool) {
	for _, dev := range devices {
		if dev.Remote && !includeNested {
			continue
		}
		chain := dev.Chain
		if len(chain) == 0 {
			chain = []string{ownChain}
		}
		snapshot.Devices = append(snapshot.Devices, Device{
			ID:       dev.ID,
			Platform: dev.Platform,
			Props:    dev.Props,
			Disabled: dev.Disabled,
			Chain:    chain,
		})
	}
}

// RemoteDevices returns the aggregated devices of all online slaves.
func (m *Master) RemoteDevices(platform string) []Device {
	return m.RemoteDevicesFiltered(platform, nil)
}

// RemoteDevicesFiltered returns aggregated devices, skipping any device whose
// chain already contains a visited node id. Chains are extended with this
// node's identity so an upstream master can detect aggregation cycles.
func (m *Master) RemoteDevicesFiltered(platform string, visited map[string]bool) []Device {
	type refreshState struct {
		id         string
		online     bool
		snapshotAt time.Time
	}
	m.mu.RLock()
	states := make([]refreshState, 0, len(m.slaves))
	for _, state := range m.slaves {
		states = append(states, refreshState{
			id:         state.record.ID,
			online:     state.online(),
			snapshotAt: state.snapshotAt,
		})
	}
	m.mu.RUnlock()

	now := time.Now()
	for _, state := range states {
		if state.online && (state.snapshotAt.IsZero() || now.Sub(state.snapshotAt) > 5*time.Second) {
			go m.refreshSnapshot(state.id)
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Device, 0)
	for _, state := range m.slaves {
		for _, dev := range state.snapshot.Devices {
			if platform != "" && dev.Platform != platform {
				continue
			}
			// The setting can be turned off while the slave is offline, so apply
			// it to cached entries instead of relying on the next refresh.
			if !state.record.IncludeNested && len(dev.Chain) > 1 {
				continue
			}
			// Drop devices that would loop back to this node, even if the slave
			// did not apply the visited filter itself.
			if chainIntersects(dev.Chain, visited) || chainContains(dev.Chain, m.cfg.NodeID) {
				continue
			}
			dev.ID = GlobalDeviceID(state.record.ID, dev.ID)
			dev.SlaveID = state.record.ID
			dev.SlaveName = state.displayName()
			dev.Remote = true
			dev.SlaveOnline = state.online()
			dev.Chain = appendChain(dev.Chain, m.cfg.NodeID)
			if !state.online() {
				dev.Disabled = true
			}
			out = append(out, dev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// appendChain returns a copy of chain with nodeID appended. Empty nodeIDs are
// ignored so nodes without an identity can still aggregate (without cycle
// detection).
func appendChain(chain []string, nodeID string) []string {
	if nodeID == "" {
		return chain
	}
	out := make([]string, 0, len(chain)+1)
	out = append(out, chain...)
	out = append(out, nodeID)
	return out
}

// chainIntersects reports whether any node id in chain is in visited.
func chainIntersects(chain []string, visited map[string]bool) bool {
	if len(visited) == 0 {
		return false
	}
	for _, id := range chain {
		if visited[id] {
			return true
		}
	}
	return false
}

// chainContains reports whether chain includes nodeID.
func chainContains(chain []string, nodeID string) bool {
	if nodeID == "" {
		return false
	}
	for _, id := range chain {
		if id == nodeID {
			return true
		}
	}
	return false
}

// Device returns a single cached remote device by global ID.
func (m *Master) Device(globalID string) (Device, bool) {
	slaveID, localID, ok := ParseGlobalDeviceID(globalID)
	if !ok {
		return Device{}, false
	}
	m.mu.RLock()
	state := m.slaves[slaveID]
	if state == nil {
		m.mu.RUnlock()
		return Device{}, false
	}
	online := state.online()
	record := state.record
	displayName := state.displayName()
	var found *Device
	for _, dev := range state.snapshot.Devices {
		if dev.ID == localID {
			copy := dev
			found = &copy
			break
		}
	}
	m.mu.RUnlock()

	if found != nil {
		dev := *found
		dev.ID = globalID
		dev.SlaveID = record.ID
		dev.SlaveName = displayName
		dev.Remote = true
		dev.SlaveOnline = online
		if !online {
			dev.Disabled = true
		}
		return dev, true
	}
	if !online {
		return Device{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var data device.DeviceInfo
	if err := m.getJSON(ctx, slaveID, "/api/v1/devices/"+localID, &data); err != nil {
		return Device{}, false
	}
	return Device{
		ID:          globalID,
		Platform:    data.Platform,
		Props:       data.Props,
		Disabled:    data.Disabled,
		SlaveID:     record.ID,
		SlaveName:   displayName,
		Remote:      true,
		SlaveOnline: true,
	}, true
}

// ResolveDevice implements the device resolver hook for MCP and other internal
// callers.
func (m *Master) ResolveDevice(globalID string) (device.Device, bool) {
	slaveID, localID, ok := ParseGlobalDeviceID(globalID)
	if !ok {
		return nil, false
	}
	m.mu.RLock()
	_, exists := m.slaves[slaveID]
	m.mu.RUnlock()
	if !exists {
		return nil, false
	}
	return newRemoteDevice(m, slaveID, localID, globalID), true
}

// RegisterRoutes mounts the master's cluster endpoints.
func (m *Master) RegisterRoutes(e *echo.Echo, managementAuth echo.MiddlewareFunc) {
	if managementAuth == nil {
		managementAuth = func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	e.GET(ConnectPath, m.ConnectHandler())
	e.GET("/api/v1/cluster/slaves", m.listSlavesHandler(), managementAuth)
	e.POST("/api/v1/cluster/slaves", m.createSlaveHandler(), managementAuth)
	e.PATCH("/api/v1/cluster/slaves/:slave_id", m.updateSlaveHandler(), managementAuth)
	e.DELETE("/api/v1/cluster/slaves/:slave_id", m.deleteSlaveHandler(), managementAuth)
	e.POST("/api/v1/cluster/slaves/:slave_id/rotate-token", m.rotateTokenHandler(), managementAuth)
}

func (m *Master) listSlavesHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		records, err := m.store.ListSlaves()
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		views := make([]SlaveView, 0, len(records))
		for _, record := range records {
			views = append(views, m.view(record))
		}
		sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
		return writeAPI(c, http.StatusOK, 0, "ok", views)
	}
}

func slaveNameExists(records []SlaveRecord, name, excludeID string) bool {
	for _, record := range records {
		if record.ID == excludeID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(record.Name), name) {
			return true
		}
	}
	return false
}

func (m *Master) createSlaveHandler() echo.HandlerFunc {
	type request struct {
		Name          string        `json:"name"`
		Mode          string        `json:"mode"`
		Direct        *DirectConfig `json:"direct"`
		IncludeNested *bool         `json:"include_nested"`
	}
	return func(c echo.Context) error {
		var req request
		if err := c.Bind(&req); err != nil {
			return writeAPI(c, http.StatusBadRequest, 400, "invalid request body", nil)
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			return writeAPI(c, http.StatusBadRequest, 400, "name is required", nil)
		}

		direct := req.Mode == string(SlaveModeDirect) || req.Direct != nil
		var directCfg *DirectConfig
		if direct {
			if req.Direct == nil {
				return writeAPI(c, http.StatusBadRequest, 400, "direct connection settings are required", nil)
			}
			url, err := NormalizeDirectURL(req.Direct.URL)
			if err != nil {
				return writeAPI(c, http.StatusBadRequest, 400, err.Error(), nil)
			}
			username := strings.TrimSpace(req.Direct.Username)
			password := req.Direct.Password
			if username == "" {
				// Direct slaves without authentication are valid: only send
				// Basic Auth when a username is configured.
				password = ""
			}
			directCfg = &DirectConfig{
				URL:         url,
				Username:    username,
				Password:    password, // pragma: allowlist secret
				InsecureTLS: req.Direct.InsecureTLS,
			}
		}

		records, err := m.store.ListSlaves()
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		if slaveNameExists(records, req.Name, "") {
			return writeAPI(c, http.StatusConflict, 409, "name already exists", nil)
		}
		used := make(map[string]bool, len(records))
		for _, record := range records {
			used[record.ID] = true
		}
		var id string
		for {
			id, err = GenerateSlaveID()
			if err != nil {
				return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
			}
			if !used[id] {
				break
			}
		}

		record := SlaveRecord{
			ID:            id,
			Name:          req.Name,
			IncludeNested: req.IncludeNested != nil && *req.IncludeNested,
			CreatedAt:     time.Now(),
		}
		if direct {
			record.Mode = SlaveModeDirect
			record.Direct = directCfg
			if err := m.store.SaveSlave(record); err != nil {
				return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
			}
			m.reconcileRecord(record)
			return writeAPI(c, http.StatusOK, 0, "ok", map[string]any{
				"slave": m.view(record),
			})
		}

		secret, err := GenerateSecret()
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		record.Mode = SlaveModePush
		record.TokenHash = HashSecret(secret)
		if err := m.store.SaveSlave(record); err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		m.reconcileRecord(record)
		return writeAPI(c, http.StatusOK, 0, "ok", map[string]any{
			"slave": m.view(record),
			"token": NewToken(id, secret),
		})
	}
}

func (m *Master) updateSlaveHandler() echo.HandlerFunc {
	type directRequest struct {
		URL         *string `json:"url"`
		Username    *string `json:"username"`
		Password    *string `json:"password"`
		InsecureTLS *bool   `json:"insecure_tls"`
	}
	type request struct {
		Name          *string        `json:"name"`
		Disabled      *bool          `json:"disabled"`
		Direct        *directRequest `json:"direct"`
		IncludeNested *bool          `json:"include_nested"`
	}
	return func(c echo.Context) error {
		id := c.Param("slave_id")
		record, ok, err := m.store.GetSlave(id)
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		if !ok {
			return writeAPI(c, http.StatusNotFound, 404, "slave not found", nil)
		}
		var req request
		if err := c.Bind(&req); err != nil {
			return writeAPI(c, http.StatusBadRequest, 400, "invalid request body", nil)
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return writeAPI(c, http.StatusBadRequest, 400, "name is required", nil)
			}
			records, err := m.store.ListSlaves()
			if err != nil {
				return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
			}
			if slaveNameExists(records, name, record.ID) {
				return writeAPI(c, http.StatusConflict, 409, "name already exists", nil)
			}
			record.Name = name
		}
		if req.Direct != nil {
			if !record.IsDirect() || record.Direct == nil {
				return writeAPI(c, http.StatusBadRequest, 400, "slave is not in direct mode", nil)
			}
			direct := *record.Direct
			if req.Direct.URL != nil {
				url, err := NormalizeDirectURL(*req.Direct.URL)
				if err != nil {
					return writeAPI(c, http.StatusBadRequest, 400, err.Error(), nil)
				}
				direct.URL = url
			}
			if req.Direct.Username != nil {
				direct.Username = strings.TrimSpace(*req.Direct.Username)
				if direct.Username == "" {
					direct.Password = ""
				}
			}
			if req.Direct.Password != nil && *req.Direct.Password != "" {
				direct.Password = *req.Direct.Password
			}
			if req.Direct.InsecureTLS != nil {
				direct.InsecureTLS = *req.Direct.InsecureTLS
			}
			record.Direct = &direct
		}
		if req.Disabled != nil {
			record.Disabled = *req.Disabled
		}
		nestedChanged := false
		if req.IncludeNested != nil && record.IncludeNested != *req.IncludeNested {
			record.IncludeNested = *req.IncludeNested
			nestedChanged = true
		}
		if err := m.store.SaveSlave(record); err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		if record.Disabled {
			m.closeSlave(record.ID)
		} else {
			m.reconcileRecord(record)
			if nestedChanged {
				m.RefreshSnapshot(record.ID)
			}
		}
		return writeAPI(c, http.StatusOK, 0, "ok", m.view(record))
	}
}

func (m *Master) deleteSlaveHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("slave_id")
		if _, ok, err := m.store.GetSlave(id); err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		} else if !ok {
			return writeAPI(c, http.StatusNotFound, 404, "slave not found", nil)
		}
		m.closeSlave(id)
		if err := m.store.DeleteSlave(id); err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		return writeAPI(c, http.StatusOK, 0, "ok", nil)
	}
}

func (m *Master) rotateTokenHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("slave_id")
		record, ok, err := m.store.GetSlave(id)
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		if !ok {
			return writeAPI(c, http.StatusNotFound, 404, "slave not found", nil)
		}
		if record.IsDirect() {
			return writeAPI(c, http.StatusBadRequest, 400, "direct slaves do not use tokens", nil)
		}
		secret, err := GenerateSecret()
		if err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		record.TokenHash = HashSecret(secret)
		if err := m.store.SaveSlave(record); err != nil {
			return writeAPI(c, http.StatusInternalServerError, 500, err.Error(), nil)
		}
		m.closeSlave(id)
		return writeAPI(c, http.StatusOK, 0, "ok", map[string]any{
			"slave": m.view(record),
			"token": NewToken(id, secret),
		})
	}
}

func (m *Master) view(record SlaveRecord) SlaveView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	view := SlaveView{
		ID:            record.ID,
		Name:          record.Name,
		Mode:          record.DisplayMode(),
		Disabled:      record.Disabled,
		IncludeNested: record.IncludeNested,
		CreatedAt:     record.CreatedAt,
	}
	if record.Direct != nil {
		view.URL = record.Direct.URL
		view.Username = record.Direct.Username
		view.InsecureTLS = record.Direct.InsecureTLS
	}
	state := m.slaves[record.ID]
	if state != nil {
		view.Name = state.displayName()
		view.Online = state.online()
		view.NodeID = state.nodeID
		view.LastSeen = state.lastSeen
		for _, dev := range state.snapshot.Devices {
			// Cached nested devices must not be counted once the setting is off.
			if !record.IncludeNested && len(dev.Chain) > 1 {
				continue
			}
			view.DeviceCount++
		}
		if record.IsDirect() {
			view.Version = state.version
		} else {
			view.Version = state.hello.Version
		}
	}
	return view
}

func (m *Master) closeSlave(slaveID string) {
	m.stopDirectPoller(slaveID)
	m.mu.Lock()
	state := m.slaves[slaveID]
	delete(m.slaves, slaveID)
	var sessions []*yamux.Session
	if state != nil {
		for _, session := range state.sessions {
			sessions = append(sessions, session)
		}
	}
	m.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	m.relays.CloseSlave(slaveID)
}

func writeAPI(c echo.Context, status, code int, message string, data any) error {
	body := map[string]any{
		"code":    code,
		"message": message,
	}
	if data != nil {
		body["data"] = data
	}
	return c.JSON(status, body)
}

// DeviceIDsByPlatform returns remote device IDs grouped by platform.
func (m *Master) DeviceIDsByPlatform(platforms []string) map[string][]string {
	out := make(map[string][]string, len(platforms))
	for _, platform := range platforms {
		out[platform] = []string{}
	}
	allowed := make(map[string]bool, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = true
	}
	for _, dev := range m.RemoteDevices("") {
		if !allowed[dev.Platform] {
			continue
		}
		out[dev.Platform] = append(out[dev.Platform], dev.ID)
	}
	return out
}
