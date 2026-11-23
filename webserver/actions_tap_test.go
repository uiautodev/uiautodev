package webserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTapDevice struct {
	device.Device
	sizeCalls     int
	width, height int
	tapped        [][2]int
}

func (s *stubTapDevice) GetScreenSize() (int, int, error) {
	s.sizeCalls++
	return s.width, s.height, nil
}

func (s *stubTapDevice) Tap(x, y int) error {
	s.tapped = append(s.tapped, [2]int{x, y})
	return nil
}

func TestDeviceTapHandlerScalesVideoCoordinates(t *testing.T) {
	resetScreenSizeCache()
	stub := &stubTapDevice{width: 1080, height: 2400}
	device.RegisterRemoteDeviceResolver(func(deviceID string) (device.Device, bool) {
		return stub, deviceID == "x-test"
	})
	defer device.RegisterRemoteDeviceResolver(nil)

	e := echo.New()
	e.POST("/api/v1/devices/:device_id/actions/tap", deviceTapHandler)
	body := `{"x":540,"y":1200,"screenWidth":920,"screenHeight":2048}`

	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/x-test/actions/tap", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, post())
	// 540 * 1080/920 = 633.9, 1200 * 2400/2048 = 1406.25
	assert.Equal(t, [][2]int{{633, 1406}}, stub.tapped)

	// The second tap reuses the cached screen size instead of running
	// `wm size` + `dumpsys input` again.
	require.Equal(t, http.StatusOK, post())
	assert.Len(t, stub.tapped, 2)
	assert.Equal(t, 1, stub.sizeCalls)
}

func TestDeviceTapHandlerDoesNotNeedScreenSizeWhenOmitted(t *testing.T) {
	resetScreenSizeCache()
	stub := &stubTapDevice{width: 1080, height: 2400}
	device.RegisterRemoteDeviceResolver(func(deviceID string) (device.Device, bool) {
		return stub, deviceID == "x-test"
	})
	defer device.RegisterRemoteDeviceResolver(nil)

	e := echo.New()
	e.POST("/api/v1/devices/:device_id/actions/tap", deviceTapHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/x-test/actions/tap", strings.NewReader(`{"x":10,"y":20}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, [][2]int{{10, 20}}, stub.tapped)
	assert.Equal(t, 0, stub.sizeCalls)
}
