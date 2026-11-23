package webserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tinyPNGBase64 is a 1x1 transparent PNG used by the clipboard handler tests.
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

type stubClipboardDevice struct {
	device.Device
	content  device.ClipboardContent
	reads    int
	writes   []device.ClipboardContent
	readErr  error
	writeErr error
}

func (s *stubClipboardDevice) ReadClipboard() (device.ClipboardContent, error) {
	s.reads++
	if s.readErr != nil {
		return device.ClipboardContent{}, s.readErr
	}
	return s.content, nil
}

func (s *stubClipboardDevice) WriteClipboard(content device.ClipboardContent) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.writes = append(s.writes, content)
	s.content = content
	return nil
}

func newClipboardTestServer(t *testing.T, dev device.Device) *echo.Echo {
	t.Helper()
	device.RegisterRemoteDeviceResolver(func(deviceID string) (device.Device, bool) {
		return dev, deviceID == "x-test"
	})
	t.Cleanup(func() { device.RegisterRemoteDeviceResolver(nil) })

	e := echo.New()
	e.GET("/api/v1/devices/:device_id/clipboard", deviceClipboardGetHandler)
	e.PUT("/api/v1/devices/:device_id/clipboard", deviceClipboardPutHandler)
	return e
}

func clipboardRequest(e *echo.Echo, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/devices/x-test/clipboard", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func decodeClipboardResponse(t *testing.T, rec *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func decodeClipboardContent(t *testing.T, resp APIResponse) device.ClipboardContent {
	t.Helper()
	data, err := json.Marshal(resp.Data)
	require.NoError(t, err)
	var content device.ClipboardContent
	require.NoError(t, json.Unmarshal(data, &content))
	return content
}

func TestDeviceClipboardGetHandlerText(t *testing.T) {
	stub := &stubClipboardDevice{content: device.ClipboardContent{Kind: device.ClipboardKindText, Text: "hello 中文"}}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodGet, "")
	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodeClipboardResponse(t, rec)
	assert.Equal(t, 0, resp.Code)
	data, err := json.Marshal(resp.Data)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"text","text":"hello 中文"}`, string(data))
	assert.Equal(t, 1, stub.reads)
}

func TestDeviceClipboardGetHandlerImage(t *testing.T) {
	stub := &stubClipboardDevice{content: device.ClipboardContent{
		Kind:  device.ClipboardKindImage,
		Image: &device.ClipboardImage{Mime: "image/png", Data: tinyPNGBase64},
	}}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodGet, "")
	require.Equal(t, http.StatusOK, rec.Code)

	content := decodeClipboardContent(t, decodeClipboardResponse(t, rec))
	assert.Equal(t, device.ClipboardKindImage, content.Kind)
	require.NotNil(t, content.Image)
	assert.Equal(t, "image/png", content.Image.Mime)
	assert.Equal(t, tinyPNGBase64, content.Image.Data)
}

func TestDeviceClipboardGetHandlerReadError(t *testing.T) {
	stub := &stubClipboardDevice{readErr: errors.New("boom")}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodGet, "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, decodeClipboardResponse(t, rec).Message, "failed to read clipboard")
}

func TestDeviceClipboardPutHandlerText(t *testing.T) {
	stub := &stubClipboardDevice{}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodPut, `{"kind":"text","text":"写入设备 🚀"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []device.ClipboardContent{{Kind: device.ClipboardKindText, Text: "写入设备 🚀"}}, stub.writes)

	resp := decodeClipboardResponse(t, rec)
	assert.Equal(t, 0, resp.Code)
	data, err := json.Marshal(resp.Data)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"text","text":"写入设备 🚀"}`, string(data))
}

func TestDeviceClipboardPutHandlerImage(t *testing.T) {
	stub := &stubClipboardDevice{}
	e := newClipboardTestServer(t, stub)

	body := fmt.Sprintf(`{"kind":"image","image":{"mime":"image/png","data":%q}}`, tinyPNGBase64)
	rec := clipboardRequest(e, http.MethodPut, body)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, stub.writes, 1)
	assert.Equal(t, device.ClipboardKindImage, stub.writes[0].Kind)
	require.NotNil(t, stub.writes[0].Image)
	assert.Equal(t, "image/png", stub.writes[0].Image.Mime)
	assert.Equal(t, tinyPNGBase64, stub.writes[0].Image.Data)
}

// TestDeviceClipboardPutHandlerLegacyTextBody keeps {"text": "..."} working
// for clients written before the clipboard gained a kind discriminator.
func TestDeviceClipboardPutHandlerLegacyTextBody(t *testing.T) {
	stub := &stubClipboardDevice{}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodPut, `{"text":"hi"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []device.ClipboardContent{{Kind: device.ClipboardKindText, Text: "hi"}}, stub.writes)
}

func TestDeviceClipboardPutHandlerAcceptsEmptyText(t *testing.T) {
	stub := &stubClipboardDevice{}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodPut, `{"kind":"text","text":""}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []device.ClipboardContent{{Kind: device.ClipboardKindText}}, stub.writes)
}

func TestDeviceClipboardPutHandlerInvalidBody(t *testing.T) {
	e := newClipboardTestServer(t, &stubClipboardDevice{})

	// A JSON string instead of the expected object cannot be bound.
	rec := clipboardRequest(e, http.MethodPut, `"not an object"`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeviceClipboardPutHandlerInvalidContent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown kind", body: `{"kind":"video","text":"hi"}`},
		{name: "image missing", body: `{"kind":"image"}`},
		{name: "image mime", body: `{"kind":"image","image":{"mime":"text/plain","data":"aGk="}}`},
		{name: "image data", body: `{"kind":"image","image":{"mime":"image/png","data":"not base64!"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newClipboardTestServer(t, &stubClipboardDevice{})
			rec := clipboardRequest(e, http.MethodPut, tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestDeviceClipboardPutHandlerWriteError(t *testing.T) {
	stub := &stubClipboardDevice{writeErr: errors.New("boom")}
	e := newClipboardTestServer(t, stub)

	rec := clipboardRequest(e, http.MethodPut, `{"kind":"text","text":"hi"}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, decodeClipboardResponse(t, rec).Message, "failed to write clipboard")
}

func TestDeviceClipboardPutHandlerImageUnsupported(t *testing.T) {
	stub := &stubClipboardDevice{writeErr: fmt.Errorf("%w: testhelper only exposes clipboard text", device.ErrClipboardImageUnsupported)}
	e := newClipboardTestServer(t, stub)

	body := fmt.Sprintf(`{"kind":"image","image":{"mime":"image/png","data":%q}}`, tinyPNGBase64)
	rec := clipboardRequest(e, http.MethodPut, body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeClipboardResponse(t, rec).Message, "does not support clipboard images")
}

func TestDeviceClipboardUnsupportedPlatform(t *testing.T) {
	// A device that embeds the Device interface but has no clipboard API,
	// like the iOS and demo implementations.
	e := newClipboardTestServer(t, &stubTapDevice{})

	getRec := clipboardRequest(e, http.MethodGet, "")
	assert.Equal(t, http.StatusBadRequest, getRec.Code)
	assert.Contains(t, decodeClipboardResponse(t, getRec).Message, "only supported on Android and HarmonyOS")

	putRec := clipboardRequest(e, http.MethodPut, `{"text":"hi"}`)
	assert.Equal(t, http.StatusBadRequest, putRec.Code)
	assert.Contains(t, decodeClipboardResponse(t, putRec).Message, "only supported on Android and HarmonyOS")
}

func TestDeviceClipboardUnknownPlatform(t *testing.T) {
	e := newClipboardTestServer(t, &stubClipboardDevice{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/z-unknown/clipboard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeClipboardResponse(t, rec).Message, "invalid device")
}
