package webserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// openAIModelsRequest carries the provider endpoint to query. The UI passes the
// just-entered base URL and API key so model listing works before saving.
type openAIModelsRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Proxy   string `json:"proxy"`
}

// openaiModelsHandler handles POST /api/v1/openai/models. It queries the
// OpenAI-compatible provider's /models endpoint and returns the list of model
// ids. Empty base_url/api_key fall back to the persisted config.
func openaiModelsHandler(c echo.Context) error {
	var req openAIModelsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		}, "  ")
	}
	cfg, err := getOpenAIConfig()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to load openai config: " + err.Error(),
		}, "  ")
	}
	// A masked key sent back by the UI must resolve to the stored key.
	if req.APIKey != "" && req.APIKey == maskAPIKey(cfg.APIKey) {
		req.APIKey = ""
	}
	if req.BaseURL == "" || req.APIKey == "" {
		if req.BaseURL == "" {
			req.BaseURL = cfg.BaseURL
		}
		if req.APIKey == "" {
			req.APIKey = cfg.APIKey // pragma: allowlist secret
		}
		if req.Proxy == "" {
			req.Proxy = cfg.Proxy
		}
	}
	models, err := fetchOpenAIModels(req.BaseURL, req.APIKey, req.Proxy)
	if err != nil {
		return c.JSONPretty(http.StatusBadGateway, APIResponse{
			Code:    502,
			Message: err.Error(),
		}, "  ")
	}
	return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: models}, "  ")
}

// fetchOpenAIModels calls the provider's /models endpoint and returns a
// de-duplicated list of model ids. It handles both the standard
// `{"data":[{"id":...}]}` shape and a plain array of model name strings.
func fetchOpenAIModels(baseURL, apiKey, proxy string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is empty")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("api_key is empty")
	}
	endpoint := baseURL + "/models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client, err := newOpenAIHTTPClient(proxy, 20*time.Second)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("failed to fetch models (HTTP %d): %s", resp.StatusCode, msg)
	}
	type modelEntry struct {
		ID string `json:"id"`
	}
	var payload struct {
		Data []modelEntry `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Data) == 0 {
		var ids []string
		if err := json.Unmarshal(body, &ids); err != nil {
			return nil, fmt.Errorf("unrecognized models response: %w", err)
		}
		return ids, nil
	}
	models := make([]string, 0, len(payload.Data))
	seen := make(map[string]bool)
	for _, m := range payload.Data {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		models = append(models, m.ID)
	}
	return models, nil
}
