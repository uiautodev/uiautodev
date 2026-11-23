package webserver

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// apiKeyMask is the fixed-length middle used when masking a persisted API key.
const apiKeyMask = "********"

// maskAPIKey returns a display-safe form of key that keeps the first three and
// last two characters, hiding the middle behind a fixed-length mask. An empty
// key stays empty; keys too short to mask are hidden entirely.
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	r := []rune(key)
	if len(r) <= 5 {
		return apiKeyMask
	}
	return string(r[:3]) + apiKeyMask + string(r[len(r)-2:])
}

// openaiConfigGetHandler handles GET /api/v1/openai/config. It returns OpenAI
// provider settings with the persisted API key masked (first 3 and last 2
// characters visible) so the UI can show which key is configured.
func openaiConfigGetHandler(c echo.Context) error {
	cfg, err := getOpenAIConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load openai config: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"base_url":    cfg.BaseURL,
			"api_key":     maskAPIKey(cfg.APIKey), // pragma: allowlist secret
			"api_key_set": cfg.APIKey != "",       // pragma: allowlist secret
			"model":       cfg.Model,
			"proxy":       cfg.Proxy,
		},
	}, "  ")
}

// openaiConfigPutHandler handles PUT /api/v1/openai/config. It persists the
// OpenAI provider settings and returns the saved config. Because the API key is
// never returned in clear text, an empty or masked api_key keeps the previously
// persisted key; it cannot be cleared through this endpoint.
func openaiConfigPutHandler(c echo.Context) error {
	var req OpenAIConfig
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	existing, err := getOpenAIConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load openai config: " + err.Error(),
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
	if err := updateOpenAIConfig(req); err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to save openai config: " + err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"base_url":    req.BaseURL,
			"api_key":     maskAPIKey(req.APIKey), // pragma: allowlist secret
			"api_key_set": req.APIKey != "",       // pragma: allowlist secret
			"model":       req.Model,
			"proxy":       req.Proxy,
		},
	}, "  ")
}
