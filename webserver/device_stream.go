package webserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// deviceListFallbackInterval is the low-frequency poll used for devices that
// have no native change events (remote cluster devices) and as a safety net
// when a watcher fails. Local devices are pushed as soon as they change.
var deviceListFallbackInterval = 5 * time.Second

// buildDeviceListForStream is the list source used by the stream handler. It
// is a variable so tests can inject device list changes.
var buildDeviceListForStream = buildDeviceList

var deviceListUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool {
		return true
	},
}

// deviceListMessage is one push on /stream/devices.
type deviceListMessage struct {
	Devices []APIDevice `json:"devices,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// deviceListStreamHandler streams device list changes over a WebSocket so the
// frontend no longer polls the HTTP endpoint. The list is re-read on an
// interval and only pushed when it changed.
func deviceListStreamHandler(c echo.Context) error {
	platform := c.QueryParam("platform")
	deviceType, ok := deviceTypeForPlatform(platform)
	if !ok {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid platform filter",
		}, "  ")
	}

	ws, err := deviceListUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	// Drain incoming frames so pings and close frames are handled; the client
	// does not need to send any data.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	changes, unsubscribeChanges := subscribeDeviceChanges(platform, deviceType)
	defer unsubscribeChanges()

	ticker := time.NewTicker(deviceListFallbackInterval)
	defer ticker.Stop()

	var last []byte
	send := func() bool {
		devices, err := buildDeviceListForStream(c, platform, deviceType)
		message := deviceListMessage{Devices: devices}
		if err != nil {
			message = deviceListMessage{Error: err.Error()}
		}
		data, marshalErr := json.Marshal(message)
		if marshalErr != nil {
			slog.Warn("failed to marshal device list", "error", marshalErr)
			return true
		}
		if bytes.Equal(data, last) {
			return true
		}
		ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if writeErr := ws.WriteMessage(websocket.TextMessage, data); writeErr != nil {
			return false
		}
		last = data
		return true
	}

	if !send() {
		return nil
	}
	for {
		select {
		case <-done:
			return nil
		case <-changes:
			if !send() {
				return nil
			}
		case <-ticker.C:
			if !send() {
				return nil
			}
		}
	}
}
