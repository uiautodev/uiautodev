package webserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJevDecisionTestContext(t *testing.T, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jev/decisions", bytes.NewReader([]byte(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	return c, rec
}

func configureJev(t *testing.T, endpoint, apiKey, model string) {
	t.Helper()
	require.NoError(t, updateJevConfig(JevConfig{
		EndpointURL: endpoint,
		APIKey:      apiKey, // pragma: allowlist secret
		Model:       model,
	}))
}

func TestJevDecisionsHandler_InjectModelAndAuth(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	var gotModel, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"dec-1","model":"jev-latest","answers":{"urgent":{"type":"noul","noul":0.9}}}`))
	}))
	defer upstream.Close()

	configureJev(t, upstream.URL, "sk-jev", "jev-latest")

	c, rec := newJevDecisionTestContext(t, `{"state":"prod is down","questions":{"urgent":{"type":"noul","instructions":"Urgent?"}}}`)
	require.NoError(t, jevDecisionsHandler(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Bearer sk-jev", gotAuth)
	assert.Equal(t, "jev-latest", gotModel)

	var res map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, "dec-1", res["id"])
}

func TestJevDecisionsHandler_PassthroughModel(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"dec-2","answers":{}}`))
	}))
	defer upstream.Close()

	configureJev(t, upstream.URL, "sk-jev", "jev-latest")

	// Caller-supplied model wins over the configured default.
	c, rec := newJevDecisionTestContext(t, `{"model":"jev-1.13.0","state":"x","questions":{}}`)
	require.NoError(t, jevDecisionsHandler(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "jev-1.13.0", gotModel)
}

func TestJevDecisionsHandler_UpstreamError(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer upstream.Close()

	configureJev(t, upstream.URL, "sk-bad", "jev-latest")

	c, rec := newJevDecisionTestContext(t, `{"state":"x","questions":{}}`)
	require.NoError(t, jevDecisionsHandler(c))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "bad key")
}

func TestJevDecisionsHandler_MissingConfig(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	c, rec := newJevDecisionTestContext(t, `{"state":"x","questions":{}}`)
	require.NoError(t, jevDecisionsHandler(c))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var res APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Contains(t, res.Message, "endpoint_url")
}

func TestJevDecisionsHandler_RejectsOversizedBody(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	configureJev(t, "http://127.0.0.1:1", "sk-jev", "jev-latest")

	body := `{"state":"` + strings.Repeat("a", maxJSONRequestBody) + `"}`
	c, rec := newJevDecisionTestContext(t, body)
	require.NoError(t, jevDecisionsHandler(c))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}
