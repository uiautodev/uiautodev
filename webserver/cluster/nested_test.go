package cluster

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNestedSlaveServer fakes a direct-mode slave that reports its own device,
// a device aggregated from one of its slaves, and a device that loops back to
// the requesting master.
func newNestedSlaveServer(t *testing.T, nodeID string, devices []Device, seenVisited *string) *httptest.Server {
	t.Helper()
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"node_id": nodeID, "version": "9.9.9", "is_master": true})
	})
	mux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		if seenVisited != nil {
			*seenVisited = r.URL.Query().Get("visited")
		}
		platform := r.URL.Query().Get("platform")
		matched := make([]Device, 0, len(devices))
		for _, dev := range devices {
			if platform == "" || dev.Platform == platform {
				matched = append(matched, dev)
			}
		}
		write(w, matched)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDirectSlaveNestedChainAndCycleFiltering(t *testing.T) {
	const (
		masterNode = "node-a"
		slaveNode  = "node-c"
	)
	var visited string
	srv := newNestedSlaveServer(t, slaveNode, []Device{
		{ID: "d-local", Platform: "demo", Chain: []string{slaveNode}},
		{ID: "r-x-d-nested", Platform: "demo", Remote: true, Chain: []string{"node-x", slaveNode}},
		{ID: "r-a-d-loop", Platform: "demo", Remote: true, Chain: []string{masterNode, slaveNode}},
	}, &visited)

	record := SlaveRecord{
		ID:            "c",
		Name:          "C",
		Mode:          SlaveModeDirect,
		Direct:        &DirectConfig{URL: srv.URL},
		IncludeNested: true,
		CreatedAt:     time.Now(),
	}

	master := NewMaster(newMemoryStore(), MasterConfig{NodeID: masterNode})
	defer master.Close()
	master.ensureState(record)
	master.refreshSnapshot(record.ID)

	// The requester identity is forwarded so the slave can drop devices that
	// would loop back.
	assert.Equal(t, masterNode, visited)

	devices := master.RemoteDevices("demo")
	require.Len(t, devices, 2, "loop device must be filtered out")

	byID := map[string]Device{}
	for _, dev := range devices {
		byID[dev.ID] = dev
		assert.True(t, dev.Remote)
	}

	local := byID[GlobalDeviceID(record.ID, "d-local")]
	require.NotZero(t, local.ID)
	assert.Equal(t, []string{slaveNode, masterNode}, local.Chain)

	nested := byID[GlobalDeviceID(record.ID, "r-x-d-nested")]
	require.NotZero(t, nested.ID)
	assert.Equal(t, []string{"node-x", slaveNode, masterNode}, nested.Chain)

	// A visited node id removes devices that already passed through it.
	filtered := master.RemoteDevicesFiltered("demo", map[string]bool{"node-x": true})
	require.Len(t, filtered, 1)
	assert.Equal(t, GlobalDeviceID(record.ID, "d-local"), filtered[0].ID)
}

func TestDirectSlaveSkipsNestedDevicesByDefault(t *testing.T) {
	const slaveNode = "node-c"
	srv := newNestedSlaveServer(t, slaveNode, []Device{
		{ID: "d-local", Platform: "demo", Chain: []string{slaveNode}},
		{ID: "r-x-d-nested", Platform: "demo", Remote: true, Chain: []string{"node-x", slaveNode}},
	}, nil)

	record := SlaveRecord{
		ID:        "c",
		Name:      "C",
		Mode:      SlaveModeDirect,
		Direct:    &DirectConfig{URL: srv.URL},
		CreatedAt: time.Now(),
	}

	master := NewMaster(newMemoryStore(), MasterConfig{NodeID: "node-a"})
	defer master.Close()
	master.ensureState(record)
	master.refreshSnapshot(record.ID)

	devices := master.RemoteDevices("demo")
	require.Len(t, devices, 1)
	assert.Equal(t, GlobalDeviceID(record.ID, "d-local"), devices[0].ID)
}

func TestDirectSlaveWithoutNodeIDNeverIncludesNested(t *testing.T) {
	// Older slaves do not expose /api/v1/cluster/info, so their node identity is
	// unknown and nested aggregation cannot detect cycles.
	srv := newNestedSlaveServer(t, "", []Device{
		{ID: "d-local", Platform: "demo"},
		{ID: "r-x-d-nested", Platform: "demo", Remote: true},
	}, nil)

	record := SlaveRecord{
		ID:            "c",
		Name:          "C",
		Mode:          SlaveModeDirect,
		Direct:        &DirectConfig{URL: srv.URL},
		IncludeNested: true,
		CreatedAt:     time.Now(),
	}

	master := NewMaster(newMemoryStore(), MasterConfig{NodeID: "node-a"})
	defer master.Close()
	master.ensureState(record)
	master.refreshSnapshot(record.ID)

	devices := master.RemoteDevices("demo")
	require.Len(t, devices, 1)
	assert.Equal(t, GlobalDeviceID(record.ID, "d-local"), devices[0].ID)
}

func TestSlaveIncludeNestedRoundTrip(t *testing.T) {
	store := newMemoryStore()
	master := NewMaster(store, MasterConfig{Version: "test", Relay: ADBRelayConfig{Bind: "127.0.0.1"}})
	defer master.Close()

	e := echo.New()
	master.RegisterRoutes(e, nil)
	server := httptest.NewServer(e)
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/cluster/slaves", "application/json", strings.NewReader(`{"name":"nested","include_nested":true}`))
	require.NoError(t, err)
	var created struct {
		Data struct {
			Slave SlaveView `json:"slave"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	assert.True(t, created.Data.Slave.IncludeNested)

	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/cluster/slaves/"+created.Data.Slave.ID, strings.NewReader(`{"include_nested":false}`))
	require.NoError(t, err)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	var updated struct {
		Data SlaveView `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
	resp.Body.Close()
	assert.False(t, updated.Data.IncludeNested)

	record, ok, err := store.GetSlave(created.Data.Slave.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, record.IncludeNested)
}

func TestDirectSlaveUsesClusterInventoryWhenLocalEnumerationFails(t *testing.T) {
	// The slave's own /api/v1/devices fails (e.g. no working adb), but its
	// aggregated cluster inventory still reports nested devices.
	var usedCluster bool
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}
	mux.HandleFunc("/api/v1/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"node_id": "node-c", "version": "9.9.9", "is_master": true})
	})
	mux.HandleFunc("/api/v1/cluster/devices", func(w http.ResponseWriter, r *http.Request) {
		usedCluster = true
		write(w, Snapshot{Devices: []Device{
			{ID: "r-x-d-nested", Platform: "demo", Remote: true, Chain: []string{"node-x", "node-c"}},
		}})
	})
	mux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":500,"message":"adb unavailable"}`, http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	record := SlaveRecord{
		ID:            "c",
		Name:          "C",
		Mode:          SlaveModeDirect,
		Direct:        &DirectConfig{URL: srv.URL},
		IncludeNested: true,
		CreatedAt:     time.Now(),
	}

	master := NewMaster(newMemoryStore(), MasterConfig{NodeID: "node-a"})
	defer master.Close()
	master.ensureState(record)
	master.refreshSnapshot(record.ID)

	assert.True(t, usedCluster)
	devices := master.RemoteDevices("demo")
	require.Len(t, devices, 1)
	assert.Equal(t, GlobalDeviceID(record.ID, "r-x-d-nested"), devices[0].ID)
}

func TestDisablingNestedHidesCachedDevices(t *testing.T) {
	srv := newNestedSlaveServer(t, "node-c", []Device{
		{ID: "d-local", Platform: "demo", Chain: []string{"node-c"}},
		{ID: "r-x-d-nested", Platform: "demo", Remote: true, Chain: []string{"node-x", "node-c"}},
	}, nil)

	record := SlaveRecord{
		ID:            "c",
		Name:          "C",
		Mode:          SlaveModeDirect,
		Direct:        &DirectConfig{URL: srv.URL},
		IncludeNested: true,
		CreatedAt:     time.Now(),
	}

	master := NewMaster(newMemoryStore(), MasterConfig{NodeID: "node-a"})
	defer master.Close()
	master.ensureState(record)
	master.refreshSnapshot(record.ID)
	require.Len(t, master.RemoteDevices("demo"), 2)

	// Turn nesting off while the slave is unreachable: the cached nested entry
	// must disappear without a successful refresh.
	srv.Close()
	record.IncludeNested = false
	master.ensureState(record)

	devices := master.RemoteDevices("demo")
	require.Len(t, devices, 1)
	assert.Equal(t, GlobalDeviceID(record.ID, "d-local"), devices[0].ID)
	assert.Equal(t, 1, master.view(record).DeviceCount)
}

func TestLegacyDirectSlaveWithSPAFallbackStaysOnline(t *testing.T) {
	// Older servers answer unknown routes with the SPA fallback (HTTP 200 HTML),
	// which must not mark the node offline.
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}
	mux.HandleFunc("/api/v1/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>app</body></html>"))
	})
	mux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		platform := r.URL.Query().Get("platform")
		if platform != "demo" {
			write(w, []Device{})
			return
		}
		write(w, []Device{{ID: "d-local", Platform: "demo"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	record := SlaveRecord{
		ID:        "c",
		Name:      "C",
		Mode:      SlaveModeDirect,
		Direct:    &DirectConfig{URL: srv.URL},
		CreatedAt: time.Now(),
	}

	master := NewMaster(newMemoryStore(), MasterConfig{NodeID: "node-a"})
	defer master.Close()
	master.ensureState(record)
	master.refreshSnapshot(record.ID)

	devices := master.RemoteDevices("demo")
	require.Len(t, devices, 1)
	assert.Equal(t, GlobalDeviceID(record.ID, "d-local"), devices[0].ID)
}

func TestRelayAllowsLoopbackForTunnelLeases(t *testing.T) {
	manager := newRelayManager(&Master{}, DefaultADBRelayConfig())
	defer manager.Close()

	r := &relay{manager: manager, allowedIP: net.ParseIP("10.1.2.3")}
	assert.True(t, r.allowed(&net.TCPAddr{IP: net.ParseIP("10.1.2.3")}))
	assert.False(t, r.allowed(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")}))

	// Leases requested by an upstream master over the tunnel accept the raw
	// stream dialed from the local loopback, while other peers stay blocked.
	r.allowLoopback = true
	assert.True(t, r.allowed(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")}))
	assert.False(t, r.allowed(&net.TCPAddr{IP: net.ParseIP("8.8.8.8")}))
}
