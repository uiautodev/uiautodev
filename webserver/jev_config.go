package webserver

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// jevConfigGetHandler handles GET /api/v1/jev/config. It returns Jev provider
// settings with the persisted API key masked (first 3 and last 2 characters
// visible) so the UI can show which key is configured.
func jevConfigGetHandler(c echo.Context) error {
	cfg, err := getJevConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load jev config: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    jevConfigData(cfg),
	}, "  ")
}

// jevConfigPutHandler handles PUT /api/v1/jev/config. It persists the Jev
// provider settings and returns the saved config. Because the API key is never
// returned in clear text, an empty or masked api_key keeps the previously
// persisted key; it cannot be cleared through this endpoint.
func jevConfigPutHandler(c echo.Context) error {
	var req JevConfig
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	existing, err := getJevConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load jev config: " + err.Error(),
		}, "  ")
	}
	if req.APIKey == "" || req.APIKey == maskAPIKey(existing.APIKey) {
		req.APIKey = existing.APIKey // pragma: allowlist secret
	}
	proxyURL, err := parseProxyURL(req.Proxy)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid proxy: " + err.Error(),
		}, "  ")
	}
	if proxyURL != nil {
		req.Proxy = proxyURL.String()
	} else {
		req.Proxy = ""
	}
	if err := updateJevConfig(req); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to save jev config: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    jevConfigData(req),
	}, "  ")
}

// jevConfigData renders a JevConfig for the API response with the API key
// masked.
func jevConfigData(cfg JevConfig) map[string]any {
	return map[string]any{
		"endpoint_url": cfg.EndpointURL,
		"api_key":      maskAPIKey(cfg.APIKey), // pragma: allowlist secret
		"api_key_set":  cfg.APIKey != "",       // pragma: allowlist secret
		"model":        cfg.Model,
		"proxy":        cfg.Proxy,
	}
}
