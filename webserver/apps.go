package webserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
)

func appsListHandler(c echo.Context) error {
	_, controller, err := device.GetAppController(c.Param("device_id"))
	if err != nil {
		return writeAppError(c, err)
	}
	scope, err := device.ParseAppScope(strings.TrimSpace(c.QueryParam("scope")))
	if err != nil {
		return writeAppError(c, err)
	}
	apps, err := controller.ListApps(scope)
	if err != nil {
		return writeAppError(c, err)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"apps": apps},
	}, "  ")
}

func appCurrentHandler(c echo.Context) error {
	_, controller, err := device.GetAppController(c.Param("device_id"))
	if err != nil {
		return writeAppError(c, err)
	}
	current, err := controller.CurrentApp()
	if err != nil {
		return writeAppError(c, err)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: current}, "  ")
}

func appStartHandler(c echo.Context) error {
	_, controller, err := device.GetAppController(c.Param("device_id"))
	if err != nil {
		return writeAppError(c, err)
	}
	appID := strings.TrimSpace(c.Param("app_id"))
	if appID == "" {
		return writeAppError(c, errors.New("app_id is required"))
	}
	var req struct {
		Activity string `json:"activity"`
	}
	if c.Request().Body != nil {
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			return writeAppError(c, errors.New("invalid request body"))
		}
	}
	current, err := controller.StartApp(appID, strings.TrimSpace(req.Activity))
	if err != nil {
		return writeAppError(c, err)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: current}, "  ")
}

func appStopHandler(c echo.Context) error {
	_, controller, err := device.GetAppController(c.Param("device_id"))
	if err != nil {
		return writeAppError(c, err)
	}
	appID := strings.TrimSpace(c.Param("app_id"))
	if appID == "" {
		return writeAppError(c, errors.New("app_id is required"))
	}
	if err := controller.StopApp(appID); err != nil {
		return writeAppError(c, err)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"app_id": appID},
	}, "  ")
}

func writeAppError(c echo.Context, err error) error {
	status := http.StatusBadRequest
	if errors.Is(err, device.ErrAppManagementUnsupported) {
		status = http.StatusNotImplemented
	}
	return c.JSONPretty(status, APIResponse{Code: status, Message: err.Error()}, "  ")
}
