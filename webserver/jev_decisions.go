package webserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// jevRequestTimeout bounds a non-streaming Jev decision call so a stalled
// upstream cannot occupy the request, goroutine and socket indefinitely.
const jevRequestTimeout = 120 * time.Second

// jevDecisionsHandler handles POST /api/v1/jev/decisions. It forwards a Jev
// System One decision request to the configured endpoint, using the persisted
// api_key and injecting the configured default model when the request omits
// one. The provider's response is relayed back verbatim so a plugin can consume
// the structured decision directly.
func jevDecisionsHandler(c echo.Context) error {
	cfg, err := getJevConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load jev config: " + err.Error(),
		}, "  ")
	}
	endpoint := strings.TrimSpace(cfg.EndpointURL)
	if endpoint == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "endpoint_url is not configured",
		}, "  ")
	}
	if cfg.APIKey == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "api_key is not configured",
		}, "  ")
	}

	raw, err := readJSONRequestBody(c)
	if err != nil {
		status, message := bodyReadErrorResponse(err)
		return c.JSONPretty(status, APIResponse{Code: status, Message: message}, "  ")
	}

	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{
				Code:    400,
				Message: "invalid request body: " + err.Error(),
			}, "  ")
		}
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	// Fill in the configured default model when the caller omitted one.
	if model, _ := payload["model"].(string); model == "" && cfg.Model != "" {
		payload["model"] = cfg.Model
	}

	forwardBody, err := json.Marshal(payload)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "failed to marshal request body: " + err.Error(),
		}, "  ")
	}

	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodPost, endpoint, bytes.NewReader(forwardBody))
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to build request: " + err.Error(),
		}, "  ")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey) // pragma: allowlist secret
	req.Header.Set("Accept", "application/json")

	client, err := newOpenAIHTTPClient(cfg.Proxy, jevRequestTimeout)
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "invalid proxy config: " + err.Error(),
		}, "  ")
	}
	resp, err := client.Do(req)
	if err != nil {
		return c.JSONPretty(http.StatusBadGateway, APIResponse{
			Code:    502,
			Message: "jev request failed: " + err.Error(),
		}, "  ")
	}
	defer resp.Body.Close()

	// Relay provider headers (minus framing headers) and stream the body back
	// verbatim so the frontend can return the raw decision response.
	for k, values := range resp.Header {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Length", "Transfer-Encoding", "Connection":
			continue
		}
		for _, v := range values {
			c.Response().Header().Add(k, v)
		}
	}
	c.Response().WriteHeader(resp.StatusCode)
	_, _ = io.Copy(c.Response(), resp.Body)
	return nil
}
