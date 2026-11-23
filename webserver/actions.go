package webserver

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// errShellUnsupported is returned when a device type cannot run shell commands.
var errShellUnsupported = errors.New("shell command is only supported on Android and HarmonyOS devices")

// screenSizeCacheTTL bounds how long a device screen size is reused. A tap or
// swipe that carries video coordinates needs the device size to scale them,
// but looking it up costs two adb shell round-trips (`wm size` and a large
// `dumpsys input` dump). During a burst of taps (e.g. a plugin replaying a
// recorded sequence) that latency dominates. The TTL is short because the
// size changes when the device rotates.
const screenSizeCacheTTL = 3 * time.Second

const screenSizeCacheMaxEntries = 32

type cachedScreenSize struct {
	width     int
	height    int
	expiresAt time.Time
}

var screenSizeCache = struct {
	sync.Mutex
	entries map[string]cachedScreenSize
}{entries: make(map[string]cachedScreenSize)}

// scaledScreenSize returns the device screen size used to scale video
// coordinates, reusing a recent lookup when possible. videoWidth/videoHeight
// are the coordinate space of the incoming request; when their orientation
// doesn't match the cached size the device was rotated and the cache is
// refreshed instead of scaling against a stale size.
func scaledScreenSize(deviceId string, dev *device.DeviceWrapper, videoWidth, videoHeight float64, now time.Time) (int, int, error) {
	screenSizeCache.Lock()
	entry, ok := screenSizeCache.entries[deviceId]
	screenSizeCache.Unlock()
	if ok && now.Before(entry.expiresAt) && sameOrientation(entry, videoWidth, videoHeight) {
		return entry.width, entry.height, nil
	}

	width, height, err := dev.GetScreenSize()
	if err != nil {
		return 0, 0, err
	}

	screenSizeCache.Lock()
	if len(screenSizeCache.entries) >= screenSizeCacheMaxEntries {
		for id, cached := range screenSizeCache.entries {
			if !now.Before(cached.expiresAt) {
				delete(screenSizeCache.entries, id)
			}
		}
	}
	screenSizeCache.entries[deviceId] = cachedScreenSize{
		width:     width,
		height:    height,
		expiresAt: now.Add(screenSizeCacheTTL),
	}
	screenSizeCache.Unlock()
	return width, height, nil
}

// sameOrientation reports whether the cached screen size has the same
// orientation as the requested coordinate space. Unknown (non-positive)
// request sizes are not checked.
func sameOrientation(size cachedScreenSize, videoWidth, videoHeight float64) bool {
	if videoWidth <= 0 || videoHeight <= 0 {
		return true
	}
	return (size.width > size.height) == (videoWidth > videoHeight)
}

// shellRunner is implemented by local Android/Harmony devices and by remote
// devices proxied through the cluster tunnel.
type shellRunner interface {
	RunShell(command string, timeoutMs int64) (string, error)
}

// runDeviceShell executes a shell command on devices that support it.
func runDeviceShell(dev *device.DeviceWrapper, command string, timeoutMs int64) (string, error) {
	if runner, ok := dev.Device.(shellRunner); ok {
		return runner.RunShell(command, timeoutMs)
	}
	switch d := dev.Device.(type) {
	case *device.AndroidDevice:
		return d.RunShell(command, timeoutMs)
	case *device.HarmonyOSDevice:
		return d.RunShell(command, timeoutMs)
	default:
		return "", errShellUnsupported
	}
}

// deviceShellHandler executes shell commands on a device.
// Expects JSON body: { "command": "<command>", "timeout": 5 } // ms
func deviceShellHandler(c echo.Context) error {
	slog.Info("deviceShellHandler called")
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	var req device.ShellRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	if req.Command == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "command is required"}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	out, err := runDeviceShell(dev, req.Command, req.Timeout)
	if err != nil {
		if errors.Is(err, errShellUnsupported) {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
		}
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to run shell: " + err.Error()}, "  ")
	}
	data := device.ShellResult{Output: out}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: data}, "  ")
}

// deviceTapHandler handles POST /api/v1/devices/:device_id/actions/tap
// Request JSON: { x: number, y: number, screenWidth?: number, screenHeight?: number }
func deviceTapHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	var req device.TapRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	// x and y are required
	if req.X == 0 && req.Y == 0 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "x and y are required"}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	// If client provided screenWidth/screenHeight, scale coordinates to device size
	x := req.X
	y := req.Y
	if req.ScreenWidth != nil && req.ScreenHeight != nil {
		devW, devH, err := scaledScreenSize(deviceId, dev, *req.ScreenWidth, *req.ScreenHeight, time.Now())
		if err != nil {
			return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to get device screen size: " + err.Error()}, "  ")
		}
		if *req.ScreenWidth != 0 && *req.ScreenHeight != 0 {
			x = x * float64(devW) / *req.ScreenWidth
			y = y * float64(devH) / *req.ScreenHeight
		}
	}
	// Use device Tap
	intX, intY := int(x), int(y)
	if err := dev.Tap(intX, intY); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to perform tap: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: fmt.Sprintf("tap performed at (%d, %d)", intX, intY)}, "  ")
}

// deviceSwipeHandler handles POST /api/v1/devices/:device_id/actions/swipe
// Request JSON: { x1, y1, x2, y2, durationMs?, screenWidth?, screenHeight? }
func deviceSwipeHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	var req device.SwipeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	// Default duration
	durationMs := req.DurationMs
	if durationMs <= 0 {
		durationMs = 300
	}
	// Scale coordinates if screenWidth/screenHeight provided
	x1, y1, x2, y2 := req.X1, req.Y1, req.X2, req.Y2
	if req.ScreenWidth != nil && req.ScreenHeight != nil {
		devW, devH, err := scaledScreenSize(deviceId, dev, *req.ScreenWidth, *req.ScreenHeight, time.Now())
		if err != nil {
			return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to get device screen size: " + err.Error()}, "  ")
		}
		if *req.ScreenWidth != 0 && *req.ScreenHeight != 0 {
			x1 = x1 * float64(devW) / *req.ScreenWidth
			y1 = y1 * float64(devH) / *req.ScreenHeight
			x2 = x2 * float64(devW) / *req.ScreenWidth
			y2 = y2 * float64(devH) / *req.ScreenHeight
		}
	}
	if err := dev.Swipe(int(x1), int(y1), int(x2), int(y2), durationMs); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to perform swipe: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: fmt.Sprintf("swipe from (%d,%d) to (%d,%d)", int(x1), int(y1), int(x2), int(y2))}, "  ")
}

// devicePressKeyHandler handles POST /api/v1/devices/:device_id/actions/press_key
// Request JSON: { key: string } - key can be "home", "back", or "menu" (case-insensitive)
func devicePressKeyHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	var req device.PressKeyRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	if !req.Key.Valid() {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid or unsupported key code (supported: home, back, menu, recents, power, volume_up, volume_down, a-z, 0-9, tab, space, enter, escape, backspace, delete, dpad keys, dpad_center, common punctuation)"}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	if err := dev.PressKey(req.Key, req.MetaState); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to press key: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: fmt.Sprintf("key pressed: %s", req.Key)}, "  ")
}

// deviceInputTextHandler handles POST /api/v1/devices/:device_id/actions/input_text
// Request JSON: { text: string }
func deviceInputTextHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	var req device.InputTextRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	if req.Text == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "text is required"}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	if err := dev.InputText(req.Text); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to input text: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "text input successful"}, "  ")
}

// deviceClearTextHandler handles POST /api/v1/devices/:device_id/actions/clear_text
func deviceClearTextHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	if err := dev.ClearText(); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to clear text: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "text cleared"}, "  ")
}
