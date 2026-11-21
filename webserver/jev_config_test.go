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

func TestJevConfigGetSetHandlers(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	// GET returns an empty config when nothing is set.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jev/config", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	assert.NoError(t, jevConfigGetHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var empty struct {
		Data struct {
			EndpointURL string `json:"endpoint_url"`
			APIKey      string `json:"api_key"`
			APIKeySet   bool   `json:"api_key_set"`
			Model       string `json:"model"`
			Proxy       string `json:"proxy"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &empty))
	assert.Equal(t, "", empty.Data.EndpointURL)
	assert.Equal(t, "", empty.Data.APIKey)
	assert.False(t, empty.Data.APIKeySet)
	assert.Equal(t, "", empty.Data.Model)
	assert.Equal(t, "", empty.Data.Proxy)

	// PUT persists the config.
	body, _ := json.Marshal(JevConfig{
		EndpointURL: "https://api.typesafe.ai/v1/systemone",
		APIKey:      "sk-jev", // pragma: allowlist secret
		Model:       "jev-latest",
		Proxy:       "socks5://127.0.0.1:1080",
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/jev/config", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, jevConfigPutHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	// GET now returns the persisted config.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jev/config", nil)
	rec = httptest.NewRecorder()
	c = echo.New().NewContext(req, rec)
	assert.NoError(t, jevConfigGetHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data struct {
			EndpointURL string `json:"endpoint_url"`
			APIKey      string `json:"api_key"`
			APIKeySet   bool   `json:"api_key_set"`
			Model       string `json:"model"`
			Proxy       string `json:"proxy"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "https://api.typesafe.ai/v1/systemone", got.Data.EndpointURL)
	assert.Equal(t, "sk-********ev", got.Data.APIKey) // pragma: allowlist secret
	assert.True(t, got.Data.APIKeySet)
	assert.Equal(t, "jev-latest", got.Data.Model)
	assert.Equal(t, "socks5://127.0.0.1:1080", got.Data.Proxy)
}

func TestJevConfigPutKeepsMaskedKey(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	put := func(cfg JevConfig) {
		t.Helper()
		body, _ := json.Marshal(cfg)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/jev/config", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		require.NoError(t, jevConfigPutHandler(c))
		require.Equal(t, http.StatusOK, rec.Code)
	}

	put(JevConfig{EndpointURL: "https://api.typesafe.ai/v1/systemone", APIKey: "sk-jev"}) // pragma: allowlist secret

	// Re-submitting the masked key must not overwrite the stored key.
	put(JevConfig{EndpointURL: "https://api.typesafe.ai/v1/systemone", APIKey: maskAPIKey("sk-jev")}) // pragma: allowlist secret

	stored, err := getJevConfig()
	require.NoError(t, err)
	assert.Equal(t, "sk-jev", stored.APIKey) // pragma: allowlist secret
}

func TestJevConfigPutHandler_InvalidProxy(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	body, _ := json.Marshal(JevConfig{
		EndpointURL: "https://api.typesafe.ai/v1/systemone",
		APIKey:      "sk-jev", // pragma: allowlist secret
		Proxy:       "ftp://127.0.0.1:21",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/jev/config", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	assert.NoError(t, jevConfigPutHandler(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var res APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Contains(t, res.Message, "invalid proxy")
}
