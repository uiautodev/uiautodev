package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// Duration is a JSON-friendly time.Duration using formats like "1h" or "30m".
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		value, err := time.ParseDuration(text)
		if err != nil {
			return err
		}
		*d = Duration(value)
		return nil
	}
	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return err
	}
	*d = Duration(time.Duration(seconds) * time.Second)
	return nil
}

func (d Duration) value() time.Duration { return time.Duration(d) }

// ADBRelayConfig configures the master-side adb connect relay.
type ADBRelayConfig struct {
	Bind            string   `json:"bind,omitempty"`
	PublicHost      string   `json:"public_host,omitempty"`
	PortRange       [2]int   `json:"port_range,omitempty"`
	TTL             Duration `json:"ttl,omitempty"`
	IdleTimeout     Duration `json:"idle_timeout,omitempty"`
	AllowSameIPOnly bool     `json:"allow_same_ip_only,omitempty"`
	AllowedCIDRs    []string `json:"allowed_cidrs,omitempty"`
}

func DefaultADBRelayConfig() ADBRelayConfig {
	return ADBRelayConfig{
		Bind:            "0.0.0.0",
		PortRange:       [2]int{20000, 20100},
		TTL:             Duration(time.Hour),
		IdleTimeout:     Duration(30 * time.Minute),
		AllowSameIPOnly: true,
	}
}

type relay struct {
	manager *relayManager

	deviceID string
	slaveID  string
	localID  string

	mu            sync.Mutex
	slaveAddr     string
	listener      net.Listener
	port          int
	allowedIP     net.IP
	allowedNets   []*net.IPNet
	allowLoopback bool // lease created by an upstream master over the tunnel
	expiresAt     time.Time
	lastActive    time.Time
	refreshedAt   time.Time
	activeConns   int
	closed        bool
	closeOnce     sync.Once
}

type relayManager struct {
	master *Master
	cfg    ADBRelayConfig

	mu     sync.Mutex
	relays map[string]*relay

	done chan struct{}
	wg   sync.WaitGroup
}

func newRelayManager(master *Master, cfg ADBRelayConfig) *relayManager {
	defaults := DefaultADBRelayConfig()
	if cfg.Bind == "" {
		cfg.Bind = defaults.Bind
	}
	if cfg.PortRange == [2]int{} {
		cfg.PortRange = defaults.PortRange
	}
	if cfg.TTL == 0 {
		cfg.TTL = defaults.TTL
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = defaults.IdleTimeout
	}
	m := &relayManager{
		master: master,
		cfg:    cfg,
		relays: make(map[string]*relay),
		done:   make(chan struct{}),
	}
	m.wg.Add(1)
	go m.janitor()
	return m
}

func (m *relayManager) Close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	m.mu.Lock()
	relays := make([]*relay, 0, len(m.relays))
	for _, r := range m.relays {
		relays = append(relays, r)
	}
	m.relays = make(map[string]*relay)
	m.mu.Unlock()
	for _, r := range relays {
		r.close()
	}
	m.wg.Wait()
}

func (m *relayManager) CloseSlave(slaveID string) {
	m.mu.Lock()
	relays := make([]*relay, 0)
	for id, r := range m.relays {
		if r.slaveID == slaveID {
			relays = append(relays, r)
			delete(m.relays, id)
		}
	}
	m.mu.Unlock()
	for _, r := range relays {
		r.close()
	}
}

func (m *relayManager) CloseDevice(deviceID string) {
	m.mu.Lock()
	r := m.relays[deviceID]
	delete(m.relays, deviceID)
	m.mu.Unlock()
	if r != nil {
		r.close()
	}
}

func (m *relayManager) GetOrCreate(deviceID, slaveID, localID, slaveAddr string, requesterIP net.IP, allowLoopback bool) (*relay, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.relays[deviceID]; existing != nil && !existing.isClosed() {
		existing.touch(slaveAddr, requesterIP, allowLoopback)
		return existing, nil
	}

	listener, port, err := m.listen()
	if err != nil {
		return nil, err
	}
	nets, err := parseCIDRs(m.cfg.AllowedCIDRs)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	r := &relay{
		manager:       m,
		deviceID:      deviceID,
		slaveID:       slaveID,
		localID:       localID,
		slaveAddr:     slaveAddr,
		listener:      listener,
		port:          port,
		allowedIP:     requesterIP,
		allowedNets:   nets,
		allowLoopback: allowLoopback,
		expiresAt:     time.Now().Add(m.cfg.TTL.value()),
		lastActive:    time.Now(),
		refreshedAt:   time.Now(),
	}
	m.relays[deviceID] = r
	go r.acceptLoop()
	return r, nil
}

func (m *relayManager) listen() (net.Listener, int, error) {
	start, end := m.cfg.PortRange[0], m.cfg.PortRange[1]
	bind := m.cfg.Bind
	if start > 0 && end >= start {
		for port := start; port <= end; port++ {
			listener, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
			if err == nil {
				return listener, port, nil
			}
		}
		return nil, 0, fmt.Errorf("no free adb relay port in range %d-%d", start, end)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(bind, "0"))
	if err != nil {
		return nil, 0, err
	}
	return listener, listener.Addr().(*net.TCPAddr).Port, nil
}

func (m *relayManager) janitor() {
	defer m.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case now := <-ticker.C:
			m.mu.Lock()
			relays := make([]*relay, 0, len(m.relays))
			for _, r := range m.relays {
				relays = append(relays, r)
			}
			m.mu.Unlock()
			for _, r := range relays {
				if r.expired(now) {
					m.CloseDevice(r.deviceID)
					go m.release(r)
					continue
				}
				if r.needsRefresh(now) {
					go m.refresh(r)
				}
			}
		}
	}
}

func (m *relayManager) refresh(r *relay) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint, err := m.master.slaveConnect(ctx, r.slaveID, r.localID)
	if err != nil {
		slog.Warn("cluster: failed to refresh adb relay lease", "device", r.deviceID, "error", err)
		m.CloseDevice(r.deviceID)
		return
	}
	r.mu.Lock()
	r.slaveAddr = endpoint
	r.refreshedAt = time.Now()
	r.expiresAt = time.Now().Add(r.manager.cfg.TTL.value())
	r.mu.Unlock()
}

func (m *relayManager) release(r *relay) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.master.slaveDisconnect(ctx, r.slaveID, r.localID)
}

func (r *relay) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *relay) touch(slaveAddr string, requesterIP net.IP, allowLoopback bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slaveAddr != "" {
		r.slaveAddr = slaveAddr
	}
	if requesterIP != nil {
		r.allowedIP = requesterIP
	}
	if allowLoopback {
		r.allowLoopback = true
	}
	r.expiresAt = time.Now().Add(r.manager.cfg.TTL.value())
	r.lastActive = time.Now()
}

func (r *relay) expired(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return true
	}
	if now.After(r.expiresAt) {
		return true
	}
	if r.activeConns > 0 {
		return false
	}
	idle := r.manager.cfg.IdleTimeout.value()
	return idle > 0 && now.Sub(r.lastActive) > idle
}

func (r *relay) needsRefresh(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && now.Sub(r.refreshedAt) > 5*time.Minute
}

func (r *relay) close() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		listener := r.listener
		r.mu.Unlock()
		if listener != nil {
			_ = listener.Close()
		}
	})
}

func (r *relay) acceptLoop() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			return
		}
		if !r.allowed(conn.RemoteAddr()) {
			slog.Warn("cluster: rejected adb relay connection", "device", r.deviceID, "remote", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		r.mu.Lock()
		r.lastActive = time.Now()
		slaveAddr := r.slaveAddr
		r.mu.Unlock()
		go r.handle(conn, slaveAddr)
	}
}

func (r *relay) allowed(addr net.Addr) bool {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return false
	}
	ip := tcpAddr.IP
	r.mu.Lock()
	sameIP := r.allowedIP
	nets := r.allowedNets
	allowLoopback := r.allowLoopback
	r.mu.Unlock()

	// Leases requested by an upstream master arrive over the authenticated
	// tunnel, and the raw stream reaches this relay from the local loopback.
	// Public peers remain restricted by the same-IP/CIDR checks below.
	if allowLoopback && ip.IsLoopback() {
		return true
	}
	if r.manager.cfg.AllowSameIPOnly && sameIP != nil && sameIP.Equal(ip) {
		return true
	}
	for _, network := range nets {
		if network.Contains(ip) {
			return true
		}
	}
	return !r.manager.cfg.AllowSameIPOnly && len(nets) == 0
}

func (r *relay) handle(client net.Conn, slaveAddr string) {
	defer client.Close()
	r.connStarted()
	defer r.connFinished()

	host, portText, err := net.SplitHostPort(slaveAddr)
	if err != nil {
		slog.Warn("cluster: invalid slave adb endpoint", "device", r.deviceID, "endpoint", slaveAddr, "error", err)
		return
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		slog.Warn("cluster: invalid slave adb port", "device", r.deviceID, "endpoint", slaveAddr, "error", err)
		return
	}
	if _, direct := r.manager.master.slaveBaseURL(r.slaveID); direct != nil {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 10*time.Second)
		if err != nil {
			slog.Warn("cluster: failed to dial direct adb endpoint", "device", r.deviceID, "endpoint", slaveAddr, "error", err)
			return
		}
		bridgeConns(client, conn)
		return
	}

	session := r.manager.master.currentSession(r.slaveID, SessionMedia)
	if session == nil {
		slog.Warn("cluster: slave offline during adb relay", "device", r.deviceID, "slave", r.slaveID)
		return
	}
	stream, err := openRawStream(session, RawTarget{Host: host, Port: port})
	if err != nil {
		slog.Warn("cluster: failed to open adb relay stream", "device", r.deviceID, "error", err)
		return
	}
	bridgeConns(client, stream)
}

func (r *relay) connStarted() {
	r.mu.Lock()
	r.activeConns++
	r.lastActive = time.Now()
	r.expiresAt = time.Now().Add(r.manager.cfg.TTL.value())
	r.mu.Unlock()
}

func (r *relay) connFinished() {
	r.mu.Lock()
	if r.activeConns > 0 {
		r.activeConns--
	}
	r.lastActive = time.Now()
	r.mu.Unlock()
}

func parseCIDRs(values []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", value, err)
		}
		out = append(out, network)
	}
	return out, nil
}

func (m *Master) handleRemoteConnect(c echo.Context, slaveID, localID string) error {
	deviceID := GlobalDeviceID(slaveID, localID)
	ctx := c.Request().Context()
	endpoint, err := m.slaveConnect(ctx, slaveID, localID)
	if err != nil {
		var responseErr *slaveResponseError
		if errors.As(err, &responseErr) && responseErr.Status >= 400 && responseErr.Status < 500 {
			m.relays.CloseDevice(deviceID)
			return writeAPI(c, http.StatusNotFound, 404, "device not found", nil)
		}
		return writeAPI(c, http.StatusBadGateway, 502, "failed to connect slave device: "+err.Error(), nil)
	}
	requesterIP := requestIP(c)
	r, err := m.relays.GetOrCreate(deviceID, slaveID, localID, endpoint, requesterIP, isTunnelRequest(ctx))
	if err != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = m.slaveDisconnect(releaseCtx, slaveID, localID)
		cancel()
		return writeAPI(c, http.StatusServiceUnavailable, 503, err.Error(), nil)
	}
	publicHost := strings.TrimSpace(m.relays.cfg.PublicHost)
	if publicHost == "" {
		publicHost = hostWithoutPort(c.Request().Host)
	}
	info := map[string]any{
		"type":      "tcp",
		"transport": "adb",
		"endpoint":  net.JoinHostPort(publicHost, strconv.Itoa(r.port)),
	}
	return writeAPI(c, http.StatusOK, 0, "ok", info)
}

func (m *Master) handleRemoteDisconnect(c echo.Context, slaveID, localID string) error {
	deviceID := GlobalDeviceID(slaveID, localID)
	m.relays.CloseDevice(deviceID)
	ctx := c.Request().Context()
	if err := m.slaveDisconnect(ctx, slaveID, localID); err != nil {
		slog.Warn("cluster: failed to release slave connect endpoint", "device", deviceID, "error", err)
	}
	return writeAPI(c, http.StatusOK, 0, "ok", nil)
}

func (m *Master) slaveConnect(ctx context.Context, slaveID, localID string) (string, error) {
	var data device.DeviceConnectInfo
	if err := m.getJSON(ctx, slaveID, "/api/v1/devices/"+localID+"/connect", &data); err != nil {
		return "", err
	}
	if data.Endpoint == "" {
		return "", fmt.Errorf("slave returned an empty endpoint")
	}
	_, port, err := net.SplitHostPort(data.Endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid slave endpoint %q: %w", data.Endpoint, err)
	}
	// Push mode reaches the slave's adbkit through the tunnel's loopback host.
	// Direct mode dials the slave's public host instead.
	if base, direct := m.slaveBaseURL(slaveID); direct != nil {
		u, err := url.Parse(base)
		if err != nil || u.Hostname() == "" {
			return "", fmt.Errorf("invalid direct slave URL %q", base)
		}
		return net.JoinHostPort(u.Hostname(), port), nil
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

func (m *Master) slaveDisconnect(ctx context.Context, slaveID, localID string) error {
	req, err := m.newSlaveRequest(ctx, http.MethodDelete, slaveID, "/api/v1/devices/"+localID+"/connect", nil)
	if err != nil {
		return err
	}
	resp, err := m.newClient(slaveID, SessionControl).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("slave returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func requestIP(c echo.Context) net.IP {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(c.Request().Header.Get(header))
		if value == "" {
			continue
		}
		if idx := strings.IndexByte(value, ','); idx >= 0 {
			value = value[:idx]
		}
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
			return ip
		}
	}
	return net.ParseIP(c.RealIP())
}

func hostWithoutPort(host string) string {
	if host == "" {
		return host
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}
