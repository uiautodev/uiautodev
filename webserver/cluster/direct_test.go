package cluster

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDirectURL(t *testing.T) {
	got, err := NormalizeDirectURL("https://slave.example.com:33299/")
	require.NoError(t, err)
	assert.Equal(t, "https://slave.example.com:33299", got)

	got, err = NormalizeDirectURL("http://10.0.0.10:33299")
	require.NoError(t, err)
	assert.Equal(t, "http://10.0.0.10:33299", got)

	for _, invalid := range []string{
		"",
		"ftp://slave.example.com",
		"https://slave.example.com/api",
		"https://slave.example.com?x=1",
		"https://user:pass@slave.example.com",
	} {
		_, err := NormalizeDirectURL(invalid)
		assert.Error(t, err, invalid)
	}
}

func directTestServer(t *testing.T, adbPort int, username, password string) *httptest.Server {
	t.Helper()
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	writeData := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data":    data,
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		writeData(w, map[string]any{"version": "test"})
	})
	mux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		if r.URL.Query().Get("platform") == "demo" {
			writeData(w, []map[string]any{{
				"platform": "demo",
				"id":       "d-demo",
				"props":    map[string]any{"serial": "demo", "model": "Direct Demo"},
			}})
			return
		}
		writeData(w, []any{})
	})
	mux.HandleFunc("/api/v1/devices/d-demo", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		writeData(w, map[string]any{
			"platform": "demo",
			"id":       "d-demo",
			"props":    map[string]any{"serial": "demo", "model": "Direct Demo"},
		})
	})
	mux.HandleFunc("/api/v1/devices/d-demo/shell", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		writeData(w, map[string]any{"output": "direct-shell-ok"})
	})
	mux.HandleFunc("/api/v1/devices/d-demo/connect", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		if r.Method == http.MethodDelete {
			writeData(w, nil)
			return
		}
		writeData(w, map[string]any{
			"type":      "tcp",
			"transport": "adb",
			"endpoint":  net.JoinHostPort("127.0.0.1", strconv.Itoa(adbPort)),
		})
	})
	return httptest.NewServer(mux)
}

func TestDirectRecordRejectsPushConnect(t *testing.T) {
	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:   "cdir",
		Name: "Direct",
		Mode: SlaveModeDirect,
		Direct: &DirectConfig{
			URL:      "http://127.0.0.1:33299",
			Username: "user",
			Password: "pass",
		},
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{Relay: ADBRelayConfig{Bind: "127.0.0.1", PortRange: [2]int{0, 0}}})
	defer master.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, ConnectPath, nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer u-cdir-secret")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, master.ConnectHandler()(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDirectSlaveConnectAndRelay(t *testing.T) {
	adbListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer adbListener.Close()
	adbPort := adbListener.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := adbListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	slaveServer := directTestServer(t, adbPort, "user", "pass")
	defer slaveServer.Close()

	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:   "cdir",
		Name: "Direct Demo",
		Mode: SlaveModeDirect,
		Direct: &DirectConfig{
			URL:      slaveServer.URL,
			Username: "user",
			Password: "pass",
		},
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{
		Relay: ADBRelayConfig{Bind: "127.0.0.1", PortRange: [2]int{0, 0}},
	})
	defer master.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := master.slaveConnect(ctx, "cdir", "d-demo")
	require.NoError(t, err)
	assert.Equal(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(adbPort)), endpoint)

	r, err := master.relays.GetOrCreate("r-cdir-d-demo", "cdir", "d-demo", endpoint, net.ParseIP("127.0.0.1"), false)
	require.NoError(t, err)
	defer master.relays.CloseDevice(r.deviceID)

	relayAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(r.port))
	conn, err := net.DialTimeout("tcp", relayAddr, 5*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte("hello-direct"))
	require.NoError(t, err)
	buf := make([]byte, len("hello-direct"))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "hello-direct", string(buf))
}

func TestRelayTTLSliding(t *testing.T) {
	adbListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer adbListener.Close()
	adbPort := adbListener.Addr().(*net.TCPAddr).Port

	slaveServer := directTestServer(t, adbPort, "user", "pass")
	defer slaveServer.Close()

	store := newMemoryStore()
	require.NoError(t, store.SaveSlave(SlaveRecord{
		ID:   "cdir",
		Name: "Direct Demo",
		Mode: SlaveModeDirect,
		Direct: &DirectConfig{
			URL:      slaveServer.URL,
			Username: "user",
			Password: "pass",
		},
		CreatedAt: time.Now(),
	}))
	master := NewMaster(store, MasterConfig{
		Relay: ADBRelayConfig{
			Bind:        "127.0.0.1",
			PortRange:   [2]int{0, 0},
			TTL:         Duration(time.Hour),
			IdleTimeout: Duration(time.Hour),
		},
	})
	defer master.Close()

	endpoint, err := master.slaveConnect(context.Background(), "cdir", "d-demo")
	require.NoError(t, err)
	r, err := master.relays.GetOrCreate("r-cdir-d-demo", "cdir", "d-demo", endpoint, net.ParseIP("127.0.0.1"), false)
	require.NoError(t, err)
	defer master.relays.CloseDevice(r.deviceID)

	r.mu.Lock()
	r.expiresAt = time.Now().Add(-time.Minute)
	r.mu.Unlock()

	before := time.Now()
	master.relays.refresh(r)

	r.mu.Lock()
	expiresAt := r.expiresAt
	r.mu.Unlock()
	assert.True(t, expiresAt.After(before), "refresh should extend the relay TTL")
}

func TestRelayIdleIgnoresActiveConnections(t *testing.T) {
	manager := &relayManager{cfg: ADBRelayConfig{
		TTL:         Duration(time.Hour),
		IdleTimeout: Duration(time.Minute),
	}}
	r := &relay{
		manager:     manager,
		expiresAt:   time.Now().Add(time.Hour),
		lastActive:  time.Now().Add(-time.Hour),
		activeConns: 1,
	}
	assert.False(t, r.expired(time.Now()))

	r.activeConns = 0
	assert.True(t, r.expired(time.Now()))
}
