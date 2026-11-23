package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestDeviceUITreeHandlerBackend(t *testing.T) {
	e := echo.New()
	e.GET("/api/v1/devices/:device_id/uitree", deviceUITreeHandler)

	base := "/api/v1/devices/d-demo/uitree"

	get := func(query string) int {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base+query, nil))
		return rec.Code
	}

	// No backend and known backends are accepted; demo falls back to its
	// bundled hierarchy.
	assert.Equal(t, http.StatusOK, get(""))
	assert.Equal(t, http.StatusOK, get("?backend=yadb"))
	assert.Equal(t, http.StatusOK, get("?backend=uiautomator"))

	// Unknown backend is rejected.
	assert.Equal(t, http.StatusBadRequest, get("?backend=bogus"))
}

func TestUIXMLHandler(t *testing.T) {
	e := echo.New()
	e.POST("/api/v1/ui/xml", uiXMLHandler)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ui/xml", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	treeBody := `{"uitree":{"id":"n-1","tagName":"hierarchy","rect":{"x":0,"y":0,"width":1080,"height":1920},"props":{},"children":[{"id":"n-2","tagName":"node","rect":{"x":10,"y":10,"width":100,"height":50},"props":{"class":"android.widget.Button","text":"OK","bounds":"[10,10][110,60]"},"children":[]}]}}`

	rec := post(treeBody)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Contains(t, resp.Data, "<hierarchy")
	assert.Contains(t, resp.Data, "android.widget.Button")
	assert.Contains(t, resp.Data, `text="OK"`)

	// Missing tree and malformed JSON are rejected.
	assert.Equal(t, http.StatusBadRequest, post(`{}`).Code)
	assert.Equal(t, http.StatusBadRequest, post(`{`).Code)
}
