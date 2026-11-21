package webserver

import (
	"sync"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// startDeviceWatcher is a variable so tests can avoid talking to real
// platform tools.
var startDeviceWatcher = device.StartDeviceWatcher

var (
	deviceChangeMu       sync.Mutex
	deviceChangeSubs     = map[string]map[chan struct{}]struct{}{}
	deviceWatcherStarted = map[string]bool{}
)

// notifyDeviceChange wakes the device list streams of one platform so they
// re-read and push the new list. Streams for other platforms are not touched.
func notifyDeviceChange(platform string) {
	deviceChangeMu.Lock()
	defer deviceChangeMu.Unlock()
	for subscriber := range deviceChangeSubs[platform] {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}

// subscribeDeviceChanges registers one stream for local device change events
// of its platform. The platform watcher is started lazily on the first
// subscription.
func subscribeDeviceChanges(platform string, deviceType device.DeviceType) (<-chan struct{}, func()) {
	deviceChangeMu.Lock()
	if !deviceWatcherStarted[platform] && deviceType != device.Demo {
		deviceWatcherStarted[platform] = true
		startDeviceWatcher(deviceType, func() {
			notifyDeviceChange(platform)
		})
	}
	changes := make(chan struct{}, 1)
	if deviceChangeSubs[platform] == nil {
		deviceChangeSubs[platform] = map[chan struct{}]struct{}{}
	}
	deviceChangeSubs[platform][changes] = struct{}{}
	deviceChangeMu.Unlock()

	return changes, func() {
		deviceChangeMu.Lock()
		delete(deviceChangeSubs[platform], changes)
		if len(deviceChangeSubs[platform]) == 0 {
			delete(deviceChangeSubs, platform)
		}
		deviceChangeMu.Unlock()
	}
}
