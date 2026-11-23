package webserver

import (
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

func setupDeviceMetaTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetConfigFile(filepath.Join(dir, configFileName))
	t.Cleanup(func() { SetConfigFile("") })
}

func TestDeviceMetaHandlers(t *testing.T) {
	setupDeviceMetaTest(t)
	e := echo.New()
	e.GET("/api/v1/devices/:device_id/meta", deviceMetaGetHandler)
	e.PUT("/api/v1/devices/:device_id/meta", deviceMetaPutHandler)
	e.DELETE("/api/v1/devices/:device_id/meta", deviceMetaDeleteHandler)

	deviceID := "a-192.168.0.67:4733"
	base := "/api/v1/devices/" + deviceID + "/meta"

	get := func() DeviceMeta {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var resp struct {
			Data DeviceMeta `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Data
	}

	put := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, base, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	del := func() int {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, base, nil))
		return rec.Code
	}

	assert.Equal(t, DeviceMeta{}, get())

	assert.Equal(t, http.StatusOK, put(`{"remark":"测试机 A"}`))
	assert.Equal(t, DeviceMeta{Remark: "测试机 A"}, get())

	assert.Equal(t, http.StatusOK, put(`{"remark":"  spaced  "}`))
	assert.Equal(t, DeviceMeta{Remark: "spaced"}, get())

	assert.Equal(t, http.StatusBadRequest, put(`{"remark":"`+strings.Repeat("x", maxDeviceRemarkLen+1)+`"}`))

	// iOS WDA overrides round-trip and are validated.
	assert.Equal(t, http.StatusOK, put(`{"wda_port":8200,"wda_mjpeg_port":9200,"wda_bundle_id":"  com.example.WDA.xctrunner  "}`))
	assert.Equal(t, DeviceMeta{WDAPort: 8200, WDAMJPEGPort: 9200, WDABundleID: "com.example.WDA.xctrunner"}, get())
	assert.Equal(t, http.StatusBadRequest, put(`{"wda_port":70000}`))
	assert.Equal(t, http.StatusBadRequest, put(`{"wda_mjpeg_port":-1}`))
	assert.Equal(t, http.StatusBadRequest, put(`{"wda_bundle_id":"`+strings.Repeat("x", maxDeviceBundleIDLen+1)+`"}`))
	assert.Equal(t, DeviceMeta{WDAPort: 8200, WDAMJPEGPort: 9200, WDABundleID: "com.example.WDA.xctrunner"}, get())

	// An empty remark clears the entry.
	assert.Equal(t, http.StatusOK, put(`{"remark":""}`))
	assert.Equal(t, DeviceMeta{}, get())

	// DELETE clears it as well.
	require.NoError(t, setDeviceMeta(deviceID, DeviceMeta{Remark: "bye"}))
	assert.Equal(t, http.StatusOK, del())
	assert.Equal(t, DeviceMeta{}, get())

	// Persisted under device_meta keyed by device id.
	require.NoError(t, setDeviceMeta(deviceID, DeviceMeta{Remark: "keep"}))
	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, DeviceMeta{Remark: "keep"}, cfg.DeviceMeta[deviceID])
}
