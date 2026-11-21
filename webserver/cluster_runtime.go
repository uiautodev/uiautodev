package webserver

import (
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/uiautodev/uiautodev/webserver/cluster"
	"github.com/uiautodev/uiautodev/webserver/device"
)

var activeClusterMaster atomic.Pointer[cluster.Master]

func getActiveClusterMaster() *cluster.Master {
	return activeClusterMaster.Load()
}

func setActiveClusterMaster(master *cluster.Master) {
	activeClusterMaster.Store(master)
}

var snapshotCache struct {
	mu   sync.Mutex
	at   time.Time
	data cluster.Snapshot
}

func clusterDevicesHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		snapshotCache.mu.Lock()
		if time.Since(snapshotCache.at) > 30*time.Second || snapshotCache.data.Devices == nil {
			snapshotCache.data = buildClusterSnapshot()
			snapshotCache.at = time.Now()
		}
		snapshot := snapshotCache.data
		snapshotCache.mu.Unlock()

		master := clusterMasterFrom(c)
		visited := parseVisitedNodeIDs(c)
		nodeID := ""
		if master != nil {
			nodeID = master.NodeID()
		}

		devices := make([]cluster.Device, 0, len(snapshot.Devices))
		// A requester whose chain already contains this node is this node
		// itself (or a cycle): local devices would be duplicates.
		if nodeID == "" || !visited[nodeID] {
			for _, dev := range snapshot.Devices {
				if nodeID != "" {
					dev.Chain = []string{nodeID}
				}
				devices = append(devices, dev)
			}
		}
		// scope=all also returns devices aggregated from this node's own
		// slaves; upstream masters request it when nested devices are enabled.
		if c.QueryParam("scope") == "all" && master != nil {
			devices = append(devices, master.RemoteDevicesFiltered("", visited)...)
		}
		return c.JSON(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    cluster.Snapshot{Devices: devices, UpdatedAt: snapshot.UpdatedAt},
		})
	}
}

func buildClusterSnapshot() cluster.Snapshot {
	devices := make([]cluster.Device, 0)
	for _, deviceType := range allDeviceTypes() {
		manager := device.NewDeviceManager(deviceType)
		if manager == nil {
			continue
		}
		serials, err := manager.ListSerials()
		if err != nil {
			slog.Warn("cluster: list devices failed", "platform", deviceType.String(), "error", err)
			continue
		}
		for _, serial := range serials {
			id := serialID(deviceType, serial)
			dev, err := device.NewDevice(id)
			if err != nil {
				continue
			}
			info := dev.Info()
			props := info.Props
			if props == nil {
				props = map[string]any{}
			}
			if _, ok := props["serial"]; !ok {
				props["serial"] = serial
			}
			devices = append(devices, cluster.Device{
				ID:       id,
				Platform: deviceType.String(),
				Props:    props,
				Disabled: info.Disabled,
			})
		}
	}
	return cluster.Snapshot{Devices: devices, UpdatedAt: time.Now()}
}
