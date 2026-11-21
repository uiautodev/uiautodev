package webserver

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// errClipboardUnsupported is returned for platforms without a clipboard API,
// such as iOS and the demo device.
var errClipboardUnsupported = errors.New("clipboard is only supported on Android and HarmonyOS devices")

// clipboardDevice returns the clipboard accessor for a device. Remote devices
// implement it by forwarding to the slave that owns the device.
func clipboardDevice(dev *device.DeviceWrapper) (device.ClipboardDevice, error) {
	if c, ok := dev.Device.(device.ClipboardDevice); ok {
		return c, nil
	}
	return nil, errClipboardUnsupported
}

// deviceClipboardGetHandler handles GET /api/v1/devices/:device_id/clipboard.
// The response data is {kind, text} or {kind, image: {mime, data}}.
func deviceClipboardGetHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	clipboard, err := clipboardDevice(dev)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
	}
	content, err := clipboard.ReadClipboard()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to read clipboard: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: content}, "  ")
}

// deviceClipboardPutHandler handles PUT /api/v1/devices/:device_id/clipboard.
// Request JSON: {"kind": "text", "text": string} or
// {"kind": "image", "image": {"mime": string, "data": base64}}.
func deviceClipboardPutHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if len(deviceId) < 2 {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device id"}, "  ")
	}
	var content device.ClipboardContent
	if err := c.Bind(&content); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request body: " + err.Error()}, "  ")
	}
	content = device.NormalizeClipboardContent(content)
	if err := content.Validate(); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	clipboard, err := clipboardDevice(dev)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
	}
	if err := clipboard.WriteClipboard(content); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, device.ErrClipboardImageUnsupported) {
			status = http.StatusBadRequest
		}
		return c.JSONPretty(status, APIResponse{Code: status, Message: "failed to write clipboard: " + err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: content}, "  ")
}
