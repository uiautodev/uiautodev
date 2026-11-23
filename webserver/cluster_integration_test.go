package webserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uiautodev/uiautodev/webserver/cluster"
	"github.com/uiautodev/uiautodev/webserver/device"
)

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
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

func TestClusterDeviceAggregation(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	const (
		slaveID = "slave001"
		secret  = "integration-secret" // pragma: allowlist secret
	)
	store := newClusterStore()
	require.NoError(t, store.SaveSlave(cluster.SlaveRecord{
		ID:        slaveID,
		Name:      "Office LAN",
		TokenHash: cluster.HashSecret(secret),
		CreatedAt: time.Now(),
	}))

	e := CreateEchoServerWithConfig(ServerConfig{
		DisablePluginAuth: true,
		Audience:          "server",
	})
	server := httptest.NewServer(e)
	defer server.Close()
	defer func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	}()

	slaveHandler := http.NewServeMux()
	slaveHandler.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": cluster.Snapshot{Devices: []cluster.Device{{
				ID:       "d-demo",
				Platform: "demo",
				Props:    map[string]any{"serial": "demo", "model": "Remote Demo"},
			}}},
		})
	})
	slaveHandler.HandleFunc("/api/v1/devices/d-demo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"platform": "demo",
				"id":       "d-demo",
				"props":    map[string]any{"serial": "demo", "model": "Remote Demo"},
			},
		})
	})
	slaveHandler.HandleFunc("/api/v1/devices/d-demo/shell", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"output": "remote-shell-ok"},
		})
	})

	agent, err := cluster.NewAgent(cluster.AgentConfig{
		MasterURL: server.URL,
		Token:     cluster.NewToken(slaveID, secret),
		Name:      "Office LAN",
	}, slaveHandler)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()

	remoteID := cluster.GlobalDeviceID(slaveID, "d-demo")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if master := getActiveClusterMaster(); master != nil {
			for _, dev := range master.RemoteDevices("demo") {
				if dev.ID == remoteID {
					goto ready
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("remote device was not aggregated")
ready:

	resp, err := http.Get(server.URL + "/api/v1/devices?platform=demo")
	require.NoError(t, err)
	defer resp.Body.Close()
	var list struct {
		Code int         `json:"code"`
		Data []APIDevice `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	var found *APIDevice
	for i := range list.Data {
		if list.Data[i].ID == remoteID {
			found = &list.Data[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.True(t, found.Remote)
	assert.Equal(t, "Office LAN", found.SlaveName)
	assert.Equal(t, "demo", found.Props["serial"])

	resp, err = http.Get(server.URL + "/api/v1/devices/" + remoteID)
	require.NoError(t, err)
	defer resp.Body.Close()
	var info struct {
		Data APIDevice `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&info))
	assert.Equal(t, remoteID, info.Data.ID)
	assert.True(t, info.Data.Remote)

	remoteDev, err := device.NewDevice(remoteID)
	require.NoError(t, err)
	output, err := runDeviceShell(remoteDev, "echo hi", 5000)
	require.NoError(t, err)
	assert.Equal(t, "remote-shell-ok", output)

	resp, err = http.Get(server.URL + "/api/v1/config")
	require.NoError(t, err)
	defer resp.Body.Close()
	var cfg struct {
		Data ServerConfig `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.NotNil(t, getActiveClusterMaster())
}

func TestClusterDemoDeviceEndToEnd(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	const (
		slaveID = "demoslave"
		secret  = "demo-secret" // pragma: allowlist secret
	)
	store := newClusterStore()
	require.NoError(t, store.SaveSlave(cluster.SlaveRecord{
		ID:        slaveID,
		Name:      "Demo LAN",
		TokenHash: cluster.HashSecret(secret),
		CreatedAt: time.Now(),
	}))

	snapshotCache.mu.Lock()
	snapshotCache.at = time.Time{}
	snapshotCache.data = cluster.Snapshot{}
	snapshotCache.mu.Unlock()

	slaveEcho := CreateEchoServerWithConfig(ServerConfig{
		NodeID:            "node-slave",
		Audience:          "server",
		DisablePluginAuth: true,
	})
	masterEcho := CreateEchoServerWithConfig(ServerConfig{
		NodeID:            "node-master",
		Audience:          "server",
		DisablePluginAuth: true,
	})
	server := httptest.NewServer(masterEcho)
	defer server.Close()
	defer func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	}()

	agent, err := cluster.NewAgent(cluster.AgentConfig{
		MasterURL: server.URL,
		Token:     cluster.NewToken(slaveID, secret),
		Name:      "Demo LAN",
		NodeID:    "node-slave",
	}, slaveEcho)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()

	remoteID := cluster.GlobalDeviceID(slaveID, "d-demo")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if master := getActiveClusterMaster(); master != nil {
			for _, dev := range master.RemoteDevices("demo") {
				if dev.ID == remoteID {
					goto ready
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("demo device was not aggregated")
ready:

	resp, err := http.Get(server.URL + "/api/v1/devices?platform=demo")
	require.NoError(t, err)
	defer resp.Body.Close()
	var list struct {
		Data []APIDevice `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	var found *APIDevice
	for i := range list.Data {
		if list.Data[i].ID == remoteID {
			found = &list.Data[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.True(t, found.Remote)
	assert.Equal(t, "Demo LAN", found.SlaveName)

	resp, err = http.Get(server.URL + "/api/v1/devices/" + remoteID + "/screenshot")
	require.NoError(t, err)
	defer resp.Body.Close()
	var screenshot struct {
		Data string `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&screenshot))
	raw, err := base64.StdEncoding.DecodeString(screenshot.Data)
	require.NoError(t, err)
	_, err = png.Decode(bytes.NewReader(raw))
	require.NoError(t, err)

	resp, err = http.Post(server.URL+"/api/v1/devices/"+remoteID+"/actions/tap", "application/json", strings.NewReader(`{"x":10,"y":20}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var tap struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tap))
	assert.Equal(t, 0, tap.Code)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices/" + remoteID + "/screen/frames"
	ws, wsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if wsResp != nil {
		defer wsResp.Body.Close()
	}
	require.NoError(t, err)
	defer ws.Close()
	_, frame, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.NotEmpty(t, frame)

	cancel()
	waitForCondition(t, 5*time.Second, func() bool {
		master := getActiveClusterMaster()
		if master == nil {
			return false
		}
		for _, dev := range master.RemoteDevices("demo") {
			if dev.ID == remoteID {
				return !dev.SlaveOnline
			}
		}
		return false
	})
	resp, err = http.Get(server.URL + "/api/v1/devices?platform=demo")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	found = nil
	for i := range list.Data {
		if list.Data[i].ID == remoteID {
			found = &list.Data[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.False(t, found.SlaveOnline)
	assert.True(t, found.Disabled)
}
