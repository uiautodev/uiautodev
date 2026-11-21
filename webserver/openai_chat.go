package webserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// maxJSONRequestBody caps the body size read by the plugin-facing JSON proxy
// endpoints (OpenAI chat, Jev decisions) so a caller cannot exhaust memory with
// an unbounded request body.
const maxJSONRequestBody = 8 << 20

// readJSONRequestBody reads the request body, enforcing maxJSONRequestBody.
func readJSONRequestBody(c echo.Context) ([]byte, error) {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, maxJSONRequestBody)
	return io.ReadAll(body)
}

// bodyReadErrorResponse maps a body-read error to an HTTP status and
// client-facing message, returning 413 when the body exceeded the limit.
func bodyReadErrorResponse(err error) (int, string) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return http.StatusRequestEntityTooLarge, "request body too large"
	}
	return http.StatusBadRequest, "failed to read request body: " + err.Error()
}

// openAIChatCompletionsHandler handles POST /api/v1/openai/chat/completions.
// It forwards an OpenAI-compatible chat completion request to the configured
// provider, using the persisted base_url/api_key, and injects the configured
// default model when the request omits one. The provider's response (single
// JSON object or SSE stream when stream=true) is relayed back verbatim so a
// plugin can use it exactly like the OpenAI SDK.
func openAIChatCompletionsHandler(c echo.Context) error {
	cfg, err := getOpenAIConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load openai config: " + err.Error(),
		}, "  ")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "base_url is not configured",
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
	streaming, _ := payload["stream"].(bool)

	forwardBody, err := json.Marshal(payload)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "failed to marshal request body: " + err.Error(),
		}, "  ")
	}

	endpoint := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodPost, endpoint, bytes.NewReader(forwardBody))
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to build request: " + err.Error(),
		}, "  ")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey) // pragma: allowlist secret
	req.Header.Set("Accept", openAIAccept(streaming))

	client, err := newOpenAIHTTPClient(cfg.Proxy, 0)
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
			Message: "openai request failed: " + err.Error(),
		}, "  ")
	}
	defer resp.Body.Close()

	// Relay provider headers (without framing/body-length that would break
	// streaming) and stream the body back verbatim.
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

func openAIAccept(streaming bool) string {
	if streaming {
		return "text/event-stream"
	}
	return "application/json"
}
