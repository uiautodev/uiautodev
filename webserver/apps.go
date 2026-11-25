package webserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
)

// maxAppPackageBytes bounds an uploaded package. Game bundles can be several
// gigabytes, so the limit is intentionally generous.
var maxAppPackageBytes int64 = 5 << 30

// appPackageTempDir overrides os.TempDir for uploaded packages. Tests point it
// at t.TempDir to assert that temporary files are cleaned up.
var appPackageTempDir = ""

// appControllerLookup resolves the app controller for a device. It is a
// variable so tests can exercise the upload handler without a real device.
var appControllerLookup = device.GetAppController

var errAppPackageTooLarge = errors.New("app package is too large")

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
	_, controller, err := appControllerLookup(c.Param("device_id"))
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

// appInstallHandler uploads a local package and installs it on the device. The
// upload is streamed to a server-named temporary file, and the client filename
// is only used for the extension check and the response.
func appInstallHandler(c echo.Context) error {
	_, controller, err := appControllerLookup(c.Param("device_id"))
	if err != nil {
		return writeAppError(c, err)
	}
	ext := controller.PackageExtension()
	localPath, name, err := receiveAppPackage(c, ext)
	if err != nil {
		return writeAppError(c, err)
	}
	defer os.Remove(localPath)

	if err := controller.InstallApp(localPath); err != nil {
		return writeAppError(c, err)
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"file_name": name, "installed": true},
	}, "  ")
}

// receiveAppPackage streams the "file" part of a multipart upload into a
// temporary file and returns its path and the client filename.
func receiveAppPackage(c echo.Context, ext string) (string, string, error) {
	reader, err := c.Request().MultipartReader()
	if err != nil {
		return "", "", errors.New(`expected a multipart/form-data upload with a "file" field`)
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return "", "", errors.New(`form field "file" is required`)
		}
		if err != nil {
			return "", "", errors.New("invalid multipart upload")
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}
		name := filepath.Base(part.FileName())
		if !strings.EqualFold(filepath.Ext(name), ext) {
			part.Close()
			return "", "", fmt.Errorf("expected a %s package, got %q", ext, name)
		}
		localPath, err := saveAppPackage(part, ext)
		part.Close()
		if err != nil {
			return "", "", err
		}
		return localPath, name, nil
	}
}

// saveAppPackage writes the upload to a temporary file with the platform
// extension, so the installer can validate a path it fully controls.
func saveAppPackage(src io.Reader, ext string) (string, error) {
	file, err := os.CreateTemp(appPackageTempDir, "uiautodev-app-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temporary package: %w", err)
	}
	path := file.Name()
	written, copyErr := io.Copy(file, io.LimitReader(src, maxAppPackageBytes+1))
	closeErr := file.Close()
	switch {
	case copyErr != nil:
		os.Remove(path)
		return "", fmt.Errorf("save uploaded package: %w", copyErr)
	case closeErr != nil:
		os.Remove(path)
		return "", fmt.Errorf("save uploaded package: %w", closeErr)
	case written > maxAppPackageBytes:
		os.Remove(path)
		return "", errAppPackageTooLarge
	case written == 0:
		os.Remove(path)
		return "", errors.New("uploaded package is empty")
	}
	return path, nil
}

func writeAppError(c echo.Context, err error) error {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, device.ErrAppManagementUnsupported):
		status = http.StatusNotImplemented
	case errors.Is(err, errAppPackageTooLarge):
		status = http.StatusRequestEntityTooLarge
	}
	return c.JSONPretty(status, APIResponse{Code: status, Message: err.Error()}, "  ")
}
