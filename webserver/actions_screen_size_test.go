package webserver

import (
	"errors"
	"testing"
	"time"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubScreenSizeDevice struct {
	device.Device
	calls         int
	width, height int
	err           error
}

func (s *stubScreenSizeDevice) GetScreenSize() (int, int, error) {
	s.calls++
	return s.width, s.height, s.err
}

func resetScreenSizeCache() {
	screenSizeCache.Lock()
	screenSizeCache.entries = make(map[string]cachedScreenSize)
	screenSizeCache.Unlock()
}

func TestScaledScreenSizeReusesRecentLookup(t *testing.T) {
	resetScreenSizeCache()
	stub := &stubScreenSizeDevice{width: 1080, height: 2400}
	dev := &device.DeviceWrapper{Device: stub}
	now := time.Now()

	width, height, err := scaledScreenSize("a-test", dev, 920, 2048, now)
	require.NoError(t, err)
	assert.Equal(t, 1080, width)
	assert.Equal(t, 2400, height)
	assert.Equal(t, 1, stub.calls)

	// Within the TTL the cached value is reused instead of asking the device.
	width, height, err = scaledScreenSize("a-test", dev, 920, 2048, now.Add(screenSizeCacheTTL-time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, 1080, width)
	assert.Equal(t, 2400, height)
	assert.Equal(t, 1, stub.calls)
}

func TestScaledScreenSizeRefreshesAfterTTL(t *testing.T) {
	resetScreenSizeCache()
	stub := &stubScreenSizeDevice{width: 1080, height: 2400}
	dev := &device.DeviceWrapper{Device: stub}
	now := time.Now()

	_, _, err := scaledScreenSize("a-test", dev, 920, 2048, now)
	require.NoError(t, err)

	// The device rotated while the entry was cached.
	stub.width, stub.height = 2400, 1080
	width, height, err := scaledScreenSize("a-test", dev, 2048, 920, now.Add(screenSizeCacheTTL))
	require.NoError(t, err)
	assert.Equal(t, 2400, width)
	assert.Equal(t, 1080, height)
	assert.Equal(t, 2, stub.calls)
}

func TestScaledScreenSizeRefreshesWhenOrientationChanges(t *testing.T) {
	resetScreenSizeCache()
	stub := &stubScreenSizeDevice{width: 1080, height: 2400}
	dev := &device.DeviceWrapper{Device: stub}
	now := time.Now()

	_, _, err := scaledScreenSize("a-test", dev, 920, 2048, now)
	require.NoError(t, err)
	assert.Equal(t, 1, stub.calls)

	// The device rotated inside the TTL: the requested coordinate space is now
	// landscape, so a cached portrait size (or the reverse) would scale taps to
	// the wrong place.
	stub.width, stub.height = 2400, 1080
	width, height, err := scaledScreenSize("a-test", dev, 2048, 920, now.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, 2400, width)
	assert.Equal(t, 1080, height)
	assert.Equal(t, 2, stub.calls)
}

func TestScaledScreenSizeDoesNotCacheErrors(t *testing.T) {
	resetScreenSizeCache()
	stub := &stubScreenSizeDevice{err: errors.New("device offline")}
	dev := &device.DeviceWrapper{Device: stub}
	now := time.Now()

	_, _, err := scaledScreenSize("a-test", dev, 920, 2048, now)
	require.Error(t, err)

	stub.err = nil
	stub.width, stub.height = 720, 1600
	width, height, err := scaledScreenSize("a-test", dev, 720, 1600, now)
	require.NoError(t, err)
	assert.Equal(t, 720, width)
	assert.Equal(t, 1600, height)
	assert.Equal(t, 2, stub.calls)
}

func TestScaledScreenSizeKeepsDevicesSeparate(t *testing.T) {
	resetScreenSizeCache()
	stubA := &stubScreenSizeDevice{width: 1080, height: 2400}
	stubB := &stubScreenSizeDevice{width: 720, height: 1600}
	now := time.Now()

	_, _, err := scaledScreenSize("a-one", &device.DeviceWrapper{Device: stubA}, 920, 2048, now)
	require.NoError(t, err)
	width, height, err := scaledScreenSize("a-two", &device.DeviceWrapper{Device: stubB}, 540, 1200, now)
	require.NoError(t, err)
	assert.Equal(t, 720, width)
	assert.Equal(t, 1600, height)
	assert.Equal(t, 1, stubA.calls)
	assert.Equal(t, 1, stubB.calls)
}
