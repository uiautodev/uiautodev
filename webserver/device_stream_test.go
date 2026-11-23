package webserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uiautodev/uiautodev/webserver/device"
)

func stubDeviceWatchers(t *testing.T) {
	t.Helper()
	old := startDeviceWatcher
	startDeviceWatcher = func(device.DeviceType, func()) {}
	t.Cleanup(func() {
		startDeviceWatcher = old
	})
}

func TestDeviceListStreamInvalidPlatform(t *testing.T) {
	e := echo.New()
	e.GET("/stream/devices", deviceListStreamHandler)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/devices?platform=bogus", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeviceListStreamPushesInitialListAndSkipsUnchanged(t *testing.T) {
	stubDeviceWatchers(t)
	oldInterval := deviceListFallbackInterval
	deviceListFallbackInterval = 100 * time.Millisecond
	defer func() { deviceListFallbackInterval = oldInterval }()

	e := echo.New()
	e.GET("/stream/devices", deviceListStreamHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices?platform=demo"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	var first deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&first))
	require.Empty(t, first.Error)
	require.NotEmpty(t, first.Devices)
	assert.Equal(t, "demo", first.Devices[0].Platform)
	assert.Equal(t, "d-demo", first.Devices[0].ID)

	// The demo device list does not change, so no further message is sent.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(350*time.Millisecond)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err)
}

func TestDeviceListStreamPushesChanges(t *testing.T) {
	stubDeviceWatchers(t)
	oldInterval := deviceListFallbackInterval
	oldBuilder := buildDeviceListForStream
	deviceListFallbackInterval = 100 * time.Millisecond
	calls := 0
	buildDeviceListForStream = func(_ echo.Context, platform string, _ device.DeviceType) ([]APIDevice, error) {
		calls++
		if calls == 1 {
			return []APIDevice{{Platform: platform, ID: "a-one", Props: map[string]any{"serial": "one"}}}, nil
		}
		return []APIDevice{
			{Platform: platform, ID: "a-one", Props: map[string]any{"serial": "one"}},
			{Platform: platform, ID: "a-two", Props: map[string]any{"serial": "two"}},
		}, nil
	}
	defer func() {
		deviceListFallbackInterval = oldInterval
		buildDeviceListForStream = oldBuilder
	}()

	e := echo.New()
	e.GET("/stream/devices", deviceListStreamHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices?platform=android"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	var first deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&first))
	require.Len(t, first.Devices, 1)

	var second deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&second))
	require.Len(t, second.Devices, 2)
	assert.Equal(t, "a-two", second.Devices[1].ID)
}

func TestDeviceListStreamPushesOnChangeNotification(t *testing.T) {
	stubDeviceWatchers(t)
	oldInterval := deviceListFallbackInterval
	oldBuilder := buildDeviceListForStream
	// The ticker must not fire; only the watcher notification drives the push.
	deviceListFallbackInterval = time.Hour
	calls := 0
	buildDeviceListForStream = func(_ echo.Context, platform string, _ device.DeviceType) ([]APIDevice, error) {
		calls++
		if calls == 1 {
			return []APIDevice{{Platform: platform, ID: "a-one"}}, nil
		}
		return []APIDevice{{Platform: platform, ID: "a-one"}, {Platform: platform, ID: "a-two"}}, nil
	}
	defer func() {
		deviceListFallbackInterval = oldInterval
		buildDeviceListForStream = oldBuilder
	}()

	e := echo.New()
	e.GET("/stream/devices", deviceListStreamHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices?platform=android"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	var first deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&first))
	require.Len(t, first.Devices, 1)

	notifyDeviceChange("android")

	var second deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&second))
	require.Len(t, second.Devices, 2)
	assert.Equal(t, "a-two", second.Devices[1].ID)
}

func TestDeviceListStreamIgnoresOtherPlatformChanges(t *testing.T) {
	stubDeviceWatchers(t)
	oldInterval := deviceListFallbackInterval
	oldBuilder := buildDeviceListForStream
	// The ticker must not fire; only an android notification may push.
	deviceListFallbackInterval = time.Hour
	var calls atomic.Int32
	buildDeviceListForStream = func(_ echo.Context, platform string, _ device.DeviceType) ([]APIDevice, error) {
		if calls.Add(1) == 1 {
			return []APIDevice{{Platform: platform, ID: "a-one"}}, nil
		}
		return []APIDevice{{Platform: platform, ID: "a-one"}, {Platform: platform, ID: "a-two"}}, nil
	}
	defer func() {
		deviceListFallbackInterval = oldInterval
		buildDeviceListForStream = oldBuilder
	}()

	e := echo.New()
	e.GET("/stream/devices", deviceListStreamHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices?platform=android"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	var first deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&first))
	require.Len(t, first.Devices, 1)
	require.Equal(t, int32(1), calls.Load())

	// An iOS change must not wake the Android stream: no re-read happens.
	notifyDeviceChange("ios")
	time.Sleep(250 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load())

	notifyDeviceChange("android")
	var second deviceListMessage
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.ReadJSON(&second))
	require.Len(t, second.Devices, 2)
	require.Equal(t, int32(2), calls.Load())
}
