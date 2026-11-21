package webserver

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// maxDeviceRemarkLen bounds the length of a device remark in runes.
const maxDeviceRemarkLen = 100

// maxDeviceBundleIDLen bounds the length of a WDA bundle id in bytes.
const maxDeviceBundleIDLen = 255

// validDevicePort reports whether p is a valid port override. Zero means unset
// (use the default), otherwise the port must be in the TCP range.
func validDevicePort(p int) bool {
	return p == 0 || (p >= 1 && p <= 65535)
}

// deviceMetaGetHandler handles GET /api/v1/devices/:device_id/meta.
func deviceMetaGetHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	metas, err := getDeviceMetas()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load device meta: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    metas[deviceID],
	}, "  ")
}

// deviceMetaPutHandler handles PUT /api/v1/devices/:device_id/meta. It replaces
// the device meta; an empty meta removes the device entry.
func deviceMetaPutHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	var meta DeviceMeta
	if err := c.Bind(&meta); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	meta.Remark = strings.TrimSpace(meta.Remark)
	meta.WDABundleID = strings.TrimSpace(meta.WDABundleID)
	if utf8.RuneCountInString(meta.Remark) > maxDeviceRemarkLen {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "remark too long",
		}, "  ")
	}
	if len(meta.WDABundleID) > maxDeviceBundleIDLen {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "bundle id too long",
		}, "  ")
	}
	if !validDevicePort(meta.WDAPort) || !validDevicePort(meta.WDAMJPEGPort) {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid port, must be between 1 and 65535",
		}, "  ")
	}
	if err := setDeviceMeta(deviceID, meta); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to save device meta: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    meta,
	}, "  ")
}

// deviceMetaDeleteHandler handles DELETE /api/v1/devices/:device_id/meta.
func deviceMetaDeleteHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	if err := deleteDeviceMeta(deviceID); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to delete device meta: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
	}, "  ")
}
