package webserver

import (
	"net/http"
	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
)

func deviceEncodersHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	idInfo, err := device.ParseDeviceID(deviceID)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid device: " + err.Error(),
		}, "  ")
	}
	if idInfo.Type != device.Android {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "only android devices support encoders",
		}, "  ")
	}

	encoders, err := device.ListAndroidEncoders(deviceID)
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to list encoders: " + err.Error(),
		}, "  ")
	}

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    encoders,
	}, "  ")
}
