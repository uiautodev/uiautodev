package webserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenAIChatTestContext(t *testing.T, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/openai/chat/completions", bytes.NewReader([]byte(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	return c, rec
}

func configureOpenAI(t *testing.T, baseURL, apiKey, model string) {
	t.Helper()
	require.NoError(t, updateOpenAIConfig(OpenAIConfig{
		BaseURL: baseURL,
		APIKey:  apiKey, // pragma: allowlist secret
		Model:   model,
	}))
}

func TestOpenAIChatCompletionsHandler_InjectModelAndAuth(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	var gotModel, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-1","object":"chat.completion","choices":[]}`))
	}))
	defer upstream.Close()

	configureOpenAI(t, upstream.URL, "sk-test", "gpt-4o")

	c, rec := newOpenAIChatTestContext(t, `{"messages":[{"role":"user","content":"hi"}]}`)
	require.NoError(t, openAIChatCompletionsHandler(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Bearer sk-test", gotAuth)
	assert.Equal(t, "gpt-4o", gotModel)

	var res map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, "cmpl-1", res["id"])
}

func TestOpenAIChatCompletionsHandler_PassthroughModel(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	configureOpenAI(t, upstream.URL, "sk-test", "gpt-4o")

	// Caller-supplied model wins over the configured default.
	c, rec := newOpenAIChatTestContext(t, `{"model":"custom-model","messages":[]}`)
	require.NoError(t, openAIChatCompletionsHandler(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "custom-model", gotModel)
}

func TestOpenAIChatCompletionsHandler_Streaming(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	const sse = "data: {\"id\":\"cmpl-1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sse)
	}))
	defer upstream.Close()

	configureOpenAI(t, upstream.URL, "sk-test", "gpt-4o")

	c, rec := newOpenAIChatTestContext(t, `{"messages":[],"stream":true,"model":"gpt-4o"}`)
	require.NoError(t, openAIChatCompletionsHandler(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, sse, rec.Body.String())
}

func TestOpenAIChatCompletionsHandler_MissingConfig(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))

	c, rec := newOpenAIChatTestContext(t, `{"messages":[]}`)
	require.NoError(t, openAIChatCompletionsHandler(c))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var res APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Contains(t, res.Message, "base_url")
}

func TestOpenAIChatCompletionsHandler_RejectsOversizedBody(t *testing.T) {
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	configureOpenAI(t, "http://127.0.0.1:1", "sk-test", "gpt-4o")

	body := `{"messages":[{"role":"user","content":"` + strings.Repeat("a", maxJSONRequestBody) + `"}]}`
	c, rec := newOpenAIChatTestContext(t, body)
	require.NoError(t, openAIChatCompletionsHandler(c))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}
