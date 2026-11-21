package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uiautodev/uiautodev/webserver/cluster"
)

func newDirectSlaveServer(t *testing.T, username, password string, onRequest ...func()) *httptest.Server {
	t.Helper()
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		for _, fn := range onRequest {
			fn()
		}
		if username == "" && password == "" {
			return true
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	writeData := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		writeData(w, map[string]any{"version": "direct-test"})
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
	return httptest.NewServer(mux)
}

func TestClusterDirectMode(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	slaveServer := newDirectSlaveServer(t, "user", "pass")
	defer slaveServer.Close()

	const slaveID = "cdir"
	store := newClusterStore()
	require.NoError(t, store.SaveSlave(cluster.SlaveRecord{
		ID:   slaveID,
		Name: "Direct LAN",
		Mode: cluster.SlaveModeDirect,
		Direct: &cluster.DirectConfig{
			URL:      slaveServer.URL,
			Username: "user",
			Password: "pass",
		},
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

	remoteID := cluster.GlobalDeviceID(slaveID, "d-demo")
	waitForCondition(t, 5*time.Second, func() bool {
		master := getActiveClusterMaster()
		if master == nil {
			return false
		}
		for _, dev := range master.RemoteDevices("demo") {
			if dev.ID == remoteID {
				return true
			}
		}
		return false
	})

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
	assert.Equal(t, "Direct LAN", found.SlaveName)

	resp, err = http.Post(
		server.URL+"/api/v1/devices/"+remoteID+"/shell",
		"application/json",
		strings.NewReader(`{"command":"echo hi"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	var shell struct {
		Data struct {
			Output string `json:"output"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&shell))
	assert.Equal(t, "direct-shell-ok", shell.Data.Output)

	resp, err = http.Get(server.URL + "/api/v1/cluster/slaves")
	require.NoError(t, err)
	defer resp.Body.Close()
	var slaves struct {
		Data []cluster.SlaveView `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&slaves))
	require.Len(t, slaves.Data, 1)
	assert.Equal(t, cluster.SlaveModeDirect, slaves.Data[0].Mode)
	assert.Equal(t, slaveServer.URL, slaves.Data[0].URL)
	assert.True(t, slaves.Data[0].Online)
}

func TestClusterDirectCreateAndEdit(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	slaveServer := newDirectSlaveServer(t, "user", "pass")
	defer slaveServer.Close()

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

	body := fmt.Sprintf(
		`{"name":"Created Direct","mode":"direct","direct":{"url":%q,"username":"user","password":"pass"}}`,
		slaveServer.URL,
	)
	resp, err := http.Post(server.URL+"/api/v1/cluster/slaves", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var created struct {
		Data struct {
			Slave cluster.SlaveView `json:"slave"`
			Token string            `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.Equal(t, cluster.SlaveModeDirect, created.Data.Slave.Mode)
	assert.Equal(t, slaveServer.URL, created.Data.Slave.URL)
	assert.Empty(t, created.Data.Token)

	patch := fmt.Sprintf(`{"direct":{"url":%q,"username":"user","password":""}}`, slaveServer.URL)
	req, err := http.NewRequest(
		http.MethodPatch,
		server.URL+"/api/v1/cluster/slaves/"+created.Data.Slave.ID,
		strings.NewReader(patch),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var updated struct {
		Data cluster.SlaveView `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
	assert.Equal(t, slaveServer.URL, updated.Data.URL)
	assert.Equal(t, "user", updated.Data.Username)

	resp, err = http.Post(
		server.URL+"/api/v1/cluster/slaves/"+created.Data.Slave.ID+"/rotate-token",
		"application/json",
		nil,
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestClusterDirectModeAuthFailure(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	var requests atomic.Int32
	slaveServer := newDirectSlaveServer(t, "user", "pass", func() { requests.Add(1) })
	defer slaveServer.Close()

	const slaveID = "cfail"
	store := newClusterStore()
	require.NoError(t, store.SaveSlave(cluster.SlaveRecord{
		ID:   slaveID,
		Name: "Wrong Creds",
		Mode: cluster.SlaveModeDirect,
		Direct: &cluster.DirectConfig{
			URL:      slaveServer.URL,
			Username: "user",
			Password: "wrong",
		},
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

	waitForCondition(t, 5*time.Second, func() bool { return requests.Load() > 0 })

	waitForCondition(t, 5*time.Second, func() bool {
		resp, err := http.Get(server.URL + "/api/v1/cluster/slaves")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var slaves struct {
			Data []cluster.SlaveView `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&slaves); err != nil {
			return false
		}
		return len(slaves.Data) == 1 && !slaves.Data[0].Online
	})
}

func TestClusterDirectModeWithoutAuth(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	slaveServer := newDirectSlaveServer(t, "", "")
	defer slaveServer.Close()

	const slaveID = "cnoauth"
	store := newClusterStore()
	require.NoError(t, store.SaveSlave(cluster.SlaveRecord{
		ID:   slaveID,
		Name: "Direct No Auth",
		Mode: cluster.SlaveModeDirect,
		Direct: &cluster.DirectConfig{
			URL: slaveServer.URL,
		},
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

	remoteID := cluster.GlobalDeviceID(slaveID, "d-demo")
	waitForCondition(t, 5*time.Second, func() bool {
		master := getActiveClusterMaster()
		if master == nil {
			return false
		}
		for _, dev := range master.RemoteDevices("demo") {
			if dev.ID == remoteID {
				return true
			}
		}
		return false
	})

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
	assert.True(t, found.SlaveOnline)
}
