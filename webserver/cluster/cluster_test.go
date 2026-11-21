package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uiautodev/uiautodev/webserver/device"
)

type memoryStore struct {
	mu      sync.Mutex
	records map[string]SlaveRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: make(map[string]SlaveRecord)}
}

func (s *memoryStore) ListSlaves() ([]SlaveRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SlaveRecord, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, record)
	}
	return out, nil
}

func (s *memoryStore) GetSlave(id string) (SlaveRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	return record, ok, nil
}

func (s *memoryStore) SaveSlave(record SlaveRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = record
	return nil
}

func (s *memoryStore) DeleteSlave(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

func TestNormalizeMasterURL(t *testing.T) {
	got, err := NormalizeMasterURL("https://master.example.com")
	require.NoError(t, err)
	assert.Equal(t, "wss://master.example.com/api/v1/cluster/connect", got)

	got, err = NormalizeMasterURL("http://10.0.0.10:33299/")
	require.NoError(t, err)
	assert.Equal(t, "ws://10.0.0.10:33299/api/v1/cluster/connect", got)

	for _, invalid := range []string{
		"",
		"ftp://master.example.com",
		"https://master.example.com/api/v1/cluster/connect",
		"https://master.example.com?x=1",
		"https://master.example.com#frag",
	} {
		_, err := NormalizeMasterURL(invalid)
		assert.Error(t, err, invalid)
	}
}

func TestParseTokenAndGlobalID(t *testing.T) {
	token := NewToken("7f3a9c21", "abc")
	assert.Equal(t, "u-7f3a9c21-abc", token)
	id, secret, err := ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "7f3a9c21", id)
	assert.Equal(t, "abc", secret)

	id, secret, err = ParseToken("uia-7f3a9c21-abc")
	require.NoError(t, err)
	assert.Equal(t, "7f3a9c21", id)
	assert.Equal(t, "abc", secret)

	_, _, err = ParseToken("bad-token")
	assert.Error(t, err)

	global := GlobalDeviceID("7f3a9c21", "a-SERIAL")
	assert.Equal(t, "r-7f3a9c21-a-SERIAL", global)
	slave, local, ok := ParseGlobalDeviceID(global)
	require.True(t, ok)
	assert.Equal(t, "7f3a9c21", slave)
	assert.Equal(t, "a-SERIAL", local)

	_, _, ok = ParseGlobalDeviceID("a-SERIAL")
	assert.False(t, ok)
}

func TestDefaultSlaveName(t *testing.T) {
	name := DefaultSlaveName("abc")
	assert.NotEmpty(t, name)
}

func TestGeneratedTokenLength(t *testing.T) {
	id, err := GenerateSlaveID()
	require.NoError(t, err)
	assert.Len(t, id, 4)

	secret, err := GenerateSecret()
	require.NoError(t, err)
	assert.Len(t, secret, 20)

	assert.Len(t, NewToken(id, secret), 27)
}

func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestTunnelDeviceProxyAndRemoteDevice(t *testing.T) {
	const (
		slaveID = "7f3a9c21"
		secret  = "test-secret" // pragma: allowlist secret
		localID = "a-test"
	)
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "test-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))

	master := NewMaster(store, MasterConfig{
		Version: "test",
		Relay:   ADBRelayConfig{Bind: "127.0.0.1", PortRange: [2]int{0, 0}},
	})
	defer master.Close()

	masterEcho := echo.New()
	master.RegisterRoutes(masterEcho, nil)
	masterEcho.GET("/api/v1/devices/:device_id/window/size", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"code": 0, "message": "ok", "data": map[string]int{"width": 1, "height": 1}})
	})
	masterEcho.GET("/stream/devices/:device_id/screen/frames", func(c echo.Context) error {
		return c.NoContent(http.StatusNotImplemented)
	})
	masterEcho.Use(master.DeviceRouteMiddleware())
	masterServer := httptest.NewServer(masterEcho)
	defer masterServer.Close()

	slaveHandler := http.NewServeMux()
	slaveHandler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": Snapshot{
				Devices: []Device{{
					ID:       localID,
					Platform: "android",
					Props:    map[string]any{"serial": "test"},
				}},
			},
		})
	})
	slaveHandler.HandleFunc("/api/v1/devices/"+localID, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"platform": "android",
				"id":       localID,
				"props":    map[string]any{"serial": "test"},
			},
		})
	})
	slaveHandler.HandleFunc("/api/v1/devices/"+localID+"/window/size", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]int{"width": 1080, "height": 1920}})
	})
	slaveHandler.HandleFunc("/api/v1/devices/"+localID+"/actions/tap", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok"})
	})
	slaveHandler.HandleFunc("/api/v1/devices/"+localID+"/uitree", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": device.Node{ID: "n-1", TagName: "node"},
		})
	})
	slaveHandler.HandleFunc("/stream/devices/"+localID+"/screen/frames", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("frame"))
	})
	slaveAgent, err := NewAgent(AgentConfig{
		MasterURL: masterServer.URL,
		Token:     NewToken(slaveID, secret),
		Name:      "test-slave",
	}, slaveHandler)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slaveAgent.Start(ctx)
	defer slaveAgent.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) != nil
	})
	waitFor(t, 5*time.Second, func() bool {
		_, ok := master.Device(GlobalDeviceID(slaveID, localID))
		return ok
	})

	remote, ok := master.ResolveDevice(GlobalDeviceID(slaveID, localID))
	require.True(t, ok)
	w, h, err := remote.GetScreenSize()
	require.NoError(t, err)
	assert.Equal(t, 1080, w)
	assert.Equal(t, 1920, h)

	require.NoError(t, remote.Tap(10, 20))

	tree, err := remote.GetUITree()
	require.NoError(t, err)
	assert.Equal(t, "node", tree.TagName)
	assert.Equal(t, "android", remote.Info().Platform)

	// REST proxy through the master.
	resp, err := http.Get(masterServer.URL + "/api/v1/devices/" + GlobalDeviceID(slaveID, localID) + "/window/size")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "1080")

	// WebSocket proxy through the master.
	wsURL := "ws" + strings.TrimPrefix(masterServer.URL, "http") + "/stream/devices/" + GlobalDeviceID(slaveID, localID) + "/screen/frames"
	wsConn, wsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if wsResp != nil {
		defer wsResp.Body.Close()
	}
	require.NoError(t, err)
	defer wsConn.Close()
	_, message, err := wsConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "frame", string(message))
}

func TestRawStreamOverTunnel(t *testing.T) {
	const (
		slaveID = "rawslave"
		secret  = "raw-secret" // pragma: allowlist secret
	)
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "raw-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer master.Close()

	masterEcho := echo.New()
	master.RegisterRoutes(masterEcho, nil)
	masterServer := httptest.NewServer(masterEcho)
	defer masterServer.Close()

	// A TCP echo server reachable from the slave handler (same process).
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoListener.Close()
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(conn)
		}
	}()

	slaveHandler := http.NewServeMux()
	slaveHandler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": Snapshot{}})
	})
	agent, err := NewAgent(AgentConfig{
		MasterURL: masterServer.URL,
		Token:     NewToken(slaveID, secret),
		Name:      "raw-slave",
	}, slaveHandler)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) != nil
	})

	host, portText, err := net.SplitHostPort(echoListener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	stream, err := openRawStream(master.currentSession(slaveID, SessionControl), RawTarget{Host: host, Port: port})
	require.NoError(t, err)
	defer stream.Close()

	_, err = stream.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(stream, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))
}

func TestADBRelay(t *testing.T) {
	const (
		slaveID = "adbslave"
		secret  = "adb-secret" // pragma: allowlist secret
		localID = "a-test"
	)
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "adb-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{
		Version: "test",
		Relay:   ADBRelayConfig{Bind: "127.0.0.1", AllowSameIPOnly: true},
	})
	defer master.Close()

	masterEcho := echo.New()
	master.RegisterRoutes(masterEcho, nil)
	masterEcho.GET("/api/v1/devices/:device_id/connect", func(c echo.Context) error {
		return c.NoContent(http.StatusNotImplemented)
	})
	masterEcho.DELETE("/api/v1/devices/:device_id/connect", func(c echo.Context) error {
		return c.NoContent(http.StatusNotImplemented)
	})
	masterEcho.Use(master.DeviceRouteMiddleware())
	masterServer := httptest.NewServer(masterEcho)
	defer masterServer.Close()

	// Fake adbkit endpoint on the slave side.
	adbListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer adbListener.Close()
	go func() {
		for {
			conn, err := adbListener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(conn)
		}
	}()

	var releases atomic.Int32
	slaveHandler := http.NewServeMux()
	slaveHandler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": Snapshot{Devices: []Device{{ID: localID, Platform: "android"}}},
		})
	})
	slaveHandler.HandleFunc("/api/v1/devices/"+localID+"/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			releases.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"type":      "tcp",
				"transport": "adb",
				"endpoint":  adbListener.Addr().String(),
			},
		})
	})
	agent, err := NewAgent(AgentConfig{
		MasterURL: masterServer.URL,
		Token:     NewToken(slaveID, secret),
		Name:      "adb-slave",
	}, slaveHandler)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) != nil
	})

	resp, err := http.Get(masterServer.URL + "/api/v1/devices/" + GlobalDeviceID(slaveID, localID) + "/connect")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Endpoint string `json:"endpoint"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	require.Equal(t, 0, envelope.Code)
	require.NotEmpty(t, envelope.Data.Endpoint)

	relayConn, err := net.DialTimeout("tcp", envelope.Data.Endpoint, 5*time.Second)
	require.NoError(t, err)
	defer relayConn.Close()
	_, err = relayConn.Write([]byte("adb!"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(relayConn, buf)
	require.NoError(t, err)
	assert.Equal(t, "adb!", string(buf))

	const concurrent = 3
	errs := make(chan error, concurrent)
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", envelope.Data.Endpoint, 5*time.Second)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			payload := fmt.Sprintf("adb-%d", i)
			if _, err := conn.Write([]byte(payload)); err != nil {
				errs <- err
				return
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, got); err != nil {
				errs <- err
				return
			}
			if string(got) != payload {
				errs <- fmt.Errorf("got %q, want %q", got, payload)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	deviceID := GlobalDeviceID(slaveID, localID)
	req, err := http.NewRequest(http.MethodDelete, masterServer.URL+"/api/v1/devices/"+deviceID+"/connect", nil)
	require.NoError(t, err)
	delResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()
	require.Equal(t, http.StatusOK, delResp.StatusCode)
	waitFor(t, 5*time.Second, func() bool { return releases.Load() == 1 })
	master.relays.mu.Lock()
	_, relayExists := master.relays.relays[deviceID]
	master.relays.mu.Unlock()
	assert.False(t, relayExists)

	resp, err = http.Get(masterServer.URL + "/api/v1/devices/" + deviceID + "/connect")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	cancel()
	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) == nil
	})
	waitFor(t, 5*time.Second, func() bool {
		master.relays.mu.Lock()
		defer master.relays.mu.Unlock()
		return len(master.relays.relays) == 0
	})
}

func TestTokenRotationAndDuplicateSession(t *testing.T) {
	const (
		slaveID = "rotateslave"
		secret  = "rotate-secret" // pragma: allowlist secret
	)
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "rotate-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer master.Close()

	e := echo.New()
	master.RegisterRoutes(e, nil)
	server := httptest.NewServer(e)
	defer server.Close()

	handler := http.NewServeMux()
	handler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": Snapshot{}})
	})
	newAgent := func(token string) *Agent {
		agent, err := NewAgent(AgentConfig{
			MasterURL: server.URL,
			Token:     token,
			Name:      "rotate-slave",
		}, handler)
		require.NoError(t, err)
		agent.Start(context.Background())
		return agent
	}

	token := NewToken(slaveID, secret)
	agent1 := newAgent(token)
	defer agent1.Stop()
	waitFor(t, 5*time.Second, func() bool { return master.currentSession(slaveID, SessionControl) != nil })
	first := master.currentSession(slaveID, SessionControl)

	agent2 := newAgent(token)
	defer agent2.Stop()
	waitFor(t, 5*time.Second, func() bool {
		current := master.currentSession(slaveID, SessionControl)
		return current != nil && current != first
	})

	resp, err := http.Post(server.URL+"/api/v1/cluster/slaves/"+slaveID+"/rotate-token", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rotated struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rotated))
	require.NotEmpty(t, rotated.Data.Token)
	assert.NotEqual(t, token, rotated.Data.Token)

	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) == nil
	})

	agent3 := newAgent(rotated.Data.Token)
	defer agent3.Stop()
	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) != nil
	})
}

func TestNormalizeSlaveAddr(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		":33299":            "127.0.0.1:33299",
		"0.0.0.0:33299":     "127.0.0.1:33299",
		"[::]:33299":        "127.0.0.1:33299",
		"localhost:33299":   "localhost:33299",
		"127.0.0.1:33299":   "127.0.0.1:33299",
		"192.168.1.2:33299": "192.168.1.2:33299",
	}
	for input, want := range cases {
		got, err := NormalizeSlaveAddr(input)
		require.NoError(t, err, input)
		assert.Equal(t, want, got, input)
	}
	_, err := NormalizeSlaveAddr("33299")
	assert.Error(t, err)
}

func TestConnectRejectsInvalidAndDisabledTokens(t *testing.T) {
	const (
		slaveID  = "authslave"
		secret   = "auth-secret" // pragma: allowlist secret
		disabled = "disabledslave"
	)
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "auth-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        disabled,
		Name:      "disabled-slave",
		TokenHash: HashSecret("disabled-secret"),
		Disabled:  true,
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer master.Close()

	e := echo.New()
	master.RegisterRoutes(e, nil)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + ConnectPath
	dial := func(token string) (int, error) {
		headers := http.Header{}
		if token != "" {
			headers.Set("Authorization", "Bearer "+token)
		}
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if resp != nil {
			defer resp.Body.Close()
			return resp.StatusCode, err
		}
		if conn != nil {
			_ = conn.Close()
		}
		return 0, err
	}

	for _, token := range []string{"", "bad-token", NewToken(slaveID, "wrong"), NewToken("unknown", "x")} {
		status, err := dial(token)
		require.Error(t, err, token)
		assert.Equal(t, http.StatusUnauthorized, status, token)
	}

	status, err := dial(NewToken(disabled, "disabled-secret"))
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, status)
}

func TestRelayAccessControlAndExpiry(t *testing.T) {
	sameIP := DefaultADBRelayConfig()
	sameIP.AllowSameIPOnly = true
	manager := newRelayManager(&Master{}, sameIP)
	defer manager.Close()

	r := &relay{manager: manager, allowedIP: net.ParseIP("10.1.2.3")}
	assert.True(t, r.allowed(&net.TCPAddr{IP: net.ParseIP("10.1.2.3")}))
	assert.False(t, r.allowed(&net.TCPAddr{IP: net.ParseIP("10.1.2.4")}))

	cidrCfg := DefaultADBRelayConfig()
	cidrCfg.AllowSameIPOnly = false
	cidrCfg.AllowedCIDRs = []string{"192.168.0.0/16"}
	nets, err := parseCIDRs(cidrCfg.AllowedCIDRs)
	require.NoError(t, err)
	cidrManager := newRelayManager(&Master{}, cidrCfg)
	defer cidrManager.Close()
	cidrRelay := &relay{manager: cidrManager, allowedNets: nets}
	assert.True(t, cidrRelay.allowed(&net.TCPAddr{IP: net.ParseIP("192.168.1.9")}))
	assert.False(t, cidrRelay.allowed(&net.TCPAddr{IP: net.ParseIP("8.8.8.8")}))

	expiring := DefaultADBRelayConfig()
	expiring.Bind = "127.0.0.1"
	expiring.PortRange = [2]int{0, 0}
	expiring.TTL = Duration(20 * time.Millisecond)
	expiring.IdleTimeout = Duration(20 * time.Millisecond)
	expiringManager := newRelayManager(&Master{}, expiring)
	defer expiringManager.Close()
	lease, err := expiringManager.GetOrCreate("d-expire", "s", "l", "127.0.0.1:1", nil, false)
	require.NoError(t, err)
	assert.False(t, lease.expired(time.Now()))
	time.Sleep(50 * time.Millisecond)
	assert.True(t, lease.expired(time.Now()))
}

func TestRelayPortExhaustionReleasesSlaveEndpoint(t *testing.T) {
	const (
		slaveID = "exhaustslave"
		secret  = "exhaust-secret" // pragma: allowlist secret
		localID = "d-demo"
	)
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer blocker.Close()
	_, portText, err := net.SplitHostPort(blocker.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "exhaust-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{
		Version: "test",
		Relay:   ADBRelayConfig{Bind: "127.0.0.1", PortRange: [2]int{port, port}},
	})
	defer master.Close()

	e := echo.New()
	master.RegisterRoutes(e, nil)
	e.GET("/api/v1/devices/:device_id/connect", func(c echo.Context) error {
		return c.NoContent(http.StatusNotImplemented)
	})
	e.DELETE("/api/v1/devices/:device_id/connect", func(c echo.Context) error {
		return c.NoContent(http.StatusNotImplemented)
	})
	e.Use(master.DeviceRouteMiddleware())
	server := httptest.NewServer(e)
	defer server.Close()

	var releases atomic.Int32
	handler := http.NewServeMux()
	handler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": Snapshot{Devices: []Device{{ID: localID, Platform: "demo"}}},
		})
	})
	handler.HandleFunc("/api/v1/devices/"+localID+"/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			releases.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"type": "tcp", "transport": "adb", "endpoint": "127.0.0.1:12345"},
		})
	})
	agent, err := NewAgent(AgentConfig{
		MasterURL: server.URL,
		Token:     NewToken(slaveID, secret),
		Name:      "exhaust-slave",
	}, handler)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()
	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) != nil
	})

	resp, err := http.Get(server.URL + "/api/v1/devices/" + GlobalDeviceID(slaveID, localID) + "/connect")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	waitFor(t, 5*time.Second, func() bool { return releases.Load() == 1 })
}

func TestAgentReconnectsAfterMasterRestart(t *testing.T) {
	const (
		slaveID = "restartslave"
		secret  = "restart-secret" // pragma: allowlist secret
	)
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:        slaveID,
		Name:      "restart-slave",
		TokenHash: HashSecret(secret),
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer master.Close()

	e := echo.New()
	master.RegisterRoutes(e, nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: e}
	go func() { _ = server.Serve(listener) }()
	addr := listener.Addr().String()

	handler := http.NewServeMux()
	handler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": Snapshot{}})
	})
	agent, err := NewAgent(AgentConfig{
		MasterURL: "http://" + addr,
		Token:     NewToken(slaveID, secret),
		Name:      "restart-slave",
	}, handler)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return master.currentSession(slaveID, SessionControl) != nil
	})

	master.Close()
	_ = server.Close()

	listener2, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	restarted := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer restarted.Close()
	restartedEcho := echo.New()
	restarted.RegisterRoutes(restartedEcho, nil)
	server2 := &http.Server{Handler: restartedEcho}
	go func() { _ = server2.Serve(listener2) }()
	defer server2.Close()

	waitFor(t, 10*time.Second, func() bool {
		return restarted.currentSession(slaveID, SessionControl) != nil
	})
}

func TestSlaveNameMustBeUnique(t *testing.T) {
	store := newMemoryStore()
	master := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer master.Close()

	e := echo.New()
	master.RegisterRoutes(e, nil)
	server := httptest.NewServer(e)
	defer server.Close()

	create := func(name string) *http.Response {
		body := strings.NewReader(fmt.Sprintf(`{"name":%q}`, name))
		resp, err := http.Post(server.URL+"/api/v1/cluster/slaves", "application/json", body)
		require.NoError(t, err)
		return resp
	}

	resp := create("office")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp = create("office")
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()

	resp = create(" OFFICE ")
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()

	resp = create("lab")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var created struct {
		Data struct {
			Slave SlaveView `json:"slave"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	require.NotEmpty(t, created.Data.Slave.ID)

	rename := func(name string) *http.Response {
		req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/cluster/slaves/"+created.Data.Slave.ID, strings.NewReader(fmt.Sprintf(`{"name":%q}`, name)))
		require.NoError(t, err)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		return resp
	}

	resp = rename("office")
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()

	resp = rename("lab-2")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp = rename("lab-2")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
