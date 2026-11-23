package webserver

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// deviceScreenshotHandler handles GET /api/v1/devices/:device_id/screenshot
// Returns a base64-encoded screenshot in JSON format.
func deviceScreenshotHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}
	img, err := dev.Screenshot()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to take screenshot: " + err.Error()}, "  ")
	}

	// Encode image to PNG buffer
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to encode image: " + err.Error()}, "  ")
	}

	// Convert to base64
	base64Str := base64.StdEncoding.EncodeToString(buf.Bytes())

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "image/png in base64 format",
		Data:    base64Str,
	}, "  ")
}

// deviceScreensHandler handles GET /api/v1/devices/:device_id/screens
// Returns a list of displays/screens available on the device.
func deviceScreensHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}

	screens, err := dev.GetScreens()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: "failed to get screens: " + err.Error()}, "  ")
	}

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    screens,
	}, "  ")
}

// deviceUITreeHandler handles GET /api/v1/devices/:device_id/uitree
// Returns the UI hierarchy (main screen). The optional `backend` query
// parameter selects the dump method on Android: yadb (default) or uiautomator.
func deviceUITreeHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid device: " + err.Error()}, "  ")
	}

	backend := device.UITreeBackend(c.QueryParam("backend"))
	switch backend {
	case "", device.UITreeBackendYadb, device.UITreeBackendUiautomator:
	default:
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid backend: " + string(backend)}, "  ")
	}

	tree, err := dev.GetUITreeWithBackend(backend)
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: tree}, "  ")
}

// UIXMLRequest represents the request body for UI tree XML conversion.
type UIXMLRequest struct {
	UTree *device.Node `json:"uitree"`
}

// uiXMLHandler handles POST /api/v1/ui/xml
// Converts a UI tree into uiautomator-style XML.
func uiXMLHandler(c echo.Context) error {
	var req UIXMLRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request: " + err.Error()}, "  ")
	}

	if req.UTree == nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "uitree is required"}, "  ")
	}

	xmlContent, err := req.UTree.ToXML()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()}, "  ")
	}

	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: xmlContent}, "  ")
}

// XPathQueryRequest represents the request body for XPath query
type XPathQueryRequest struct {
	XPath string       `json:"xpath"`
	UTree *device.Node `json:"uitree"`
}

// uiXPathQueryHandler handles POST /api/v1/ui/xpath_query
// Queries the UI tree using XPath and returns matching nodes.
func uiXPathQueryHandler(c echo.Context) error {
	var req XPathQueryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid request: " + err.Error()}, "  ")
	}

	if req.XPath == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "xpath is required"}, "  ")
	}

	if req.UTree == nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "uitree is required"}, "  ")
	}

	matchedNodes, err := device.FindNodesByXPathInTree(req.UTree, req.XPath)
	if err != nil {
		if errors.Is(err, device.ErrInvalidXPath) {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
		}
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()}, "  ")
	}

	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: matchedNodes}, "  ")
}
