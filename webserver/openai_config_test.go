package webserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIConfigGetSetHandlers(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	// GET returns an empty config when nothing is set.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openai/config", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	assert.NoError(t, openaiConfigGetHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var empty struct {
		Data struct {
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			APIKeySet bool   `json:"api_key_set"`
			Model     string `json:"model"`
			Proxy     string `json:"proxy"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &empty))
	assert.Equal(t, "", empty.Data.BaseURL)
	assert.Equal(t, "", empty.Data.APIKey)
	assert.False(t, empty.Data.APIKeySet)
	assert.Equal(t, "", empty.Data.Model)
	assert.Equal(t, "", empty.Data.Proxy)

	// PUT persists the config.
	body, _ := json.Marshal(OpenAIConfig{
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-test", // pragma: allowlist secret
		Model:   "gpt-4o",
		Proxy:   "socks5://127.0.0.1:1080",
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/openai/config", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, openaiConfigPutHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	// GET now returns the persisted config.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/openai/config", nil)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, openaiConfigGetHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data struct {
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			APIKeySet bool   `json:"api_key_set"`
			Model     string `json:"model"`
			Proxy     string `json:"proxy"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "https://api.example.com/v1", got.Data.BaseURL)
	assert.Equal(t, "sk-********st", got.Data.APIKey) // pragma: allowlist secret
	assert.True(t, got.Data.APIKeySet)
	assert.Equal(t, "gpt-4o", got.Data.Model)
	assert.Equal(t, "socks5://127.0.0.1:1080", got.Data.Proxy)
}

func TestMaskAPIKey(t *testing.T) {
	assert.Equal(t, "", maskAPIKey(""))
	assert.Equal(t, apiKeyMask, maskAPIKey("abcde"))
	assert.Equal(t, "abc********ef", maskAPIKey("abcdef"))
	assert.Equal(t, "sk-********st", maskAPIKey("sk-test")) // pragma: allowlist secret
}

func TestOpenAIConfigPutKeepsMaskedKey(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	put := func(cfg OpenAIConfig) {
		t.Helper()
		body, _ := json.Marshal(cfg)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/openai/config", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		require.NoError(t, openaiConfigPutHandler(c))
		require.Equal(t, http.StatusOK, rec.Code)
	}

	put(OpenAIConfig{BaseURL: "https://api.example.com/v1", APIKey: "sk-test"}) // pragma: allowlist secret

	// Re-submitting the masked key must not overwrite the stored key.
	put(OpenAIConfig{BaseURL: "https://api.example.com/v1", APIKey: maskAPIKey("sk-test")}) // pragma: allowlist secret

	stored, err := getOpenAIConfig()
	require.NoError(t, err)
	assert.Equal(t, "sk-test", stored.APIKey) // pragma: allowlist secret
}

func TestOpenAIConfigPutHandler_InvalidProxy(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	body, _ := json.Marshal(OpenAIConfig{
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-test", // pragma: allowlist secret
		Proxy:   "ftp://127.0.0.1:21",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/openai/config", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	assert.NoError(t, openaiConfigPutHandler(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var res APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Contains(t, res.Message, "invalid proxy")
}

func TestOpenAIModelsHandler(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	t.Run("returns model ids from provider", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/models", r.URL.Path)
			assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`))
		}))
		defer srv.Close()

		body, _ := json.Marshal(openAIModelsRequest{BaseURL: srv.URL, APIKey: "sk-test"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/openai/models", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		require.NoError(t, openaiModelsHandler(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var res struct {
			Data []string `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, []string{"gpt-4o", "gpt-4o-mini"}, res.Data)
	})

	t.Run("returns 502 when provider fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		body, _ := json.Marshal(openAIModelsRequest{BaseURL: srv.URL, APIKey: "sk-bad"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/openai/models", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		require.NoError(t, openaiModelsHandler(c))
		assert.Equal(t, http.StatusBadGateway, rec.Code)
		var res APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, 502, res.Code)
		assert.Contains(t, res.Message, "HTTP 401")
	})
}

func TestOpenAIModelsHandlerMaskedKeyFallback(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	require.NoError(t, updateOpenAIConfig(OpenAIConfig{APIKey: "sk-stored"})) // pragma: allowlist secret

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-stored", r.Header.Get("Authorization")) // pragma: allowlist secret
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	// The UI sends the masked key back; the handler must use the stored key.
	body, _ := json.Marshal(openAIModelsRequest{BaseURL: srv.URL, APIKey: maskAPIKey("sk-stored")}) // pragma: allowlist secret
	req := httptest.NewRequest(http.MethodPost, "/api/v1/openai/models", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	require.NoError(t, openaiModelsHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var res struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, []string{"gpt-4o"}, res.Data)
}
