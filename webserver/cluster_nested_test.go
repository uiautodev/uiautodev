package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/uiautodev/uiautodev/webserver/cluster"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClusterNestedDeviceAggregation covers the serving side of nested
// aggregation: /api/v1/cluster/devices (push masters) and /api/v1/devices
// (direct masters) must expose the node chain and honour the visited filter.
func TestClusterNestedDeviceAggregation(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	// A direct slave (node-c) that itself aggregates a device from node-x.
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"node_id": "node-c", "version": "9.9.9", "is_master": true})
	})
	mux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		devices := []cluster.Device{
			{ID: "d-local", Platform: "demo", Chain: []string{"node-c"}},
			{ID: "r-x-d-nested", Platform: "demo", Remote: true, Chain: []string{"node-x", "node-c"}},
		}
		if platform := r.URL.Query().Get("platform"); platform != "" {
			matched := devices[:0]
			for _, dev := range devices {
				if dev.Platform == platform {
					matched = append(matched, dev)
				}
			}
			devices = matched
		}
		write(w, devices)
	})
	slave := httptest.NewServer(mux)
	defer slave.Close()

	store := newClusterStore()
	require.NoError(t, store.SaveSlave(cluster.SlaveRecord{
		ID:            "c",
		Name:          "C",
		Mode:          cluster.SlaveModeDirect,
		Direct:        &cluster.DirectConfig{URL: slave.URL},
		IncludeNested: true,
		CreatedAt:     time.Now(),
	}))

	e := CreateEchoServerWithConfig(ServerConfig{
		NodeID:            "node-a",
		DisablePluginAuth: true,
		Audience:          "server",
	})
	t.Cleanup(func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	})

	nestedID := "r-c-r-x-d-nested"
	waitForCondition(t, 5*time.Second, func() bool {
		master := getActiveClusterMaster()
		if master == nil {
			return false
		}
		for _, dev := range master.RemoteDevices("demo") {
			if dev.ID == nestedID {
				return true
			}
		}
		return false
	})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	readClusterSnapshot := func(path string) cluster.Snapshot {
		rec := get(path)
		require.Equal(t, http.StatusOK, rec.Code)
		var resp struct {
			Data cluster.Snapshot `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Data
	}

	readDevices := func(path string) []APIDevice {
		rec := get(path)
		require.Equal(t, http.StatusOK, rec.Code)
		var resp struct {
			Data []APIDevice `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Data
	}

	// Default scope only reports this node's own devices.
	own := readClusterSnapshot("/api/v1/cluster/devices")
	for _, dev := range own.Devices {
		assert.False(t, dev.Remote, "default scope must not include nested devices")
		assert.Equal(t, []string{"node-a"}, dev.Chain)
	}

	// scope=all includes the slave's nested device with the full chain.
	nested := readClusterSnapshot("/api/v1/cluster/devices?scope=all")
	var found *cluster.Device
	for i := range nested.Devices {
		if nested.Devices[i].ID == nestedID {
			found = &nested.Devices[i]
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, []string{"node-x", "node-c", "node-a"}, found.Chain)

	// The visited filter drops this node's own devices (cycle prevention).
	visited := readClusterSnapshot("/api/v1/cluster/devices?scope=all&visited=node-a")
	for _, dev := range visited.Devices {
		assert.True(t, dev.Remote, "local devices must be dropped when visited contains this node")
	}

	// The standard device list used by direct masters exposes the same chain.
	devices := readDevices("/api/v1/devices?platform=demo")
	var nestedAPI *APIDevice
	for i := range devices {
		if devices[i].ID == nestedID {
			nestedAPI = &devices[i]
		}
	}
	require.NotNil(t, nestedAPI)
	assert.Equal(t, []string{"node-x", "node-c", "node-a"}, nestedAPI.Chain)

	filtered := readDevices("/api/v1/devices?platform=demo&visited=node-a")
	for _, dev := range filtered {
		assert.True(t, dev.Remote)
	}
}

// memClusterStore is an in-memory cluster.Store for tests that need isolated
// slave registries per server in the same process.
type memClusterStore struct {
	mu      sync.Mutex
	records map[string]cluster.SlaveRecord
}

func newMemClusterStore() *memClusterStore {
	return &memClusterStore{records: make(map[string]cluster.SlaveRecord)}
}

func (s *memClusterStore) ListSlaves() ([]cluster.SlaveRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]cluster.SlaveRecord, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, record)
	}
	return out, nil
}

func (s *memClusterStore) GetSlave(id string) (cluster.SlaveRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	return record, ok, nil
}

func (s *memClusterStore) SaveSlave(record cluster.SlaveRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = record
	return nil
}

func (s *memClusterStore) DeleteSlave(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

// TestCascadeAcrossTwoHops verifies A -> B -> C aggregation: B aggregates a
// direct slave C, and A aggregates B with nested devices enabled.
func TestCascadeAcrossTwoHops(t *testing.T) {
	SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	defer SetConfigFile("")
	defer func() { managementAuthDisabled = false }()

	// Node C: a direct slave with one demo device.
	cMux := http.NewServeMux()
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}
	cMux.HandleFunc("/api/v1/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"node_id": "node-c", "version": "9.9.9", "is_master": true})
	})
	cMux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		if platform := r.URL.Query().Get("platform"); platform != "" && platform != "demo" {
			write(w, []cluster.Device{})
			return
		}
		write(w, []cluster.Device{{ID: "d-c", Platform: "demo"}})
	})
	cServer := httptest.NewServer(cMux)
	defer cServer.Close()

	// Node B: a master that aggregates C (leaf only).
	bStore := newMemClusterStore()
	require.NoError(t, bStore.SaveSlave(cluster.SlaveRecord{
		ID:        "c",
		Name:      "C",
		Mode:      cluster.SlaveModeDirect,
		Direct:    &cluster.DirectConfig{URL: cServer.URL},
		CreatedAt: time.Now(),
	}))
	bMaster := cluster.NewMaster(bStore, cluster.MasterConfig{Version: "test", NodeID: "node-b"})
	defer bMaster.Close()
	bEcho := echo.New()
	bEcho.Use(clusterContextMiddleware(bMaster))
	bMaster.RegisterRoutes(bEcho, nil)
	bEcho.GET("/api/v1/cluster/devices", clusterDevicesHandler())
	bEcho.GET("/api/v1/cluster/info", clusterInfoHandler())
	bServer := httptest.NewServer(bEcho)
	defer bServer.Close()

	waitForCondition(t, 10*time.Second, func() bool {
		for _, dev := range bMaster.RemoteDevices("demo") {
			if dev.ID == "r-c-d-c" {
				return true
			}
		}
		return false
	})

	// Node A: a master that aggregates B with nested devices enabled.
	aStore := newMemClusterStore()
	require.NoError(t, aStore.SaveSlave(cluster.SlaveRecord{
		ID:            "b",
		Name:          "B",
		Mode:          cluster.SlaveModeDirect,
		Direct:        &cluster.DirectConfig{URL: bServer.URL},
		IncludeNested: true,
		CreatedAt:     time.Now(),
	}))
	_ = CreateEchoServerWithConfig(ServerConfig{
		NodeID:            "node-a",
		ClusterStore:      aStore,
		DisablePluginAuth: true,
		Audience:          "server",
	})
	t.Cleanup(func() {
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
	})

	waitForCondition(t, 10*time.Second, func() bool {
		master := getActiveClusterMaster()
		if master == nil {
			return false
		}
		for _, dev := range master.RemoteDevices("demo") {
			if dev.ID == "r-b-r-c-d-c" {
				assert.Equal(t, []string{"node-c", "node-b", "node-a"}, dev.Chain)
				return true
			}
		}
		return false
	})
}
