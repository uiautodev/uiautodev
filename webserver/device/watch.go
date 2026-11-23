package device

import (
	"context"
	"log/slog"
	"time"

	"github.com/codeskyblue/go-adbkit/adb"
	"github.com/danielpaulus/go-ios/ios"
)

// deviceWatchRetryDelay is how long to wait before restarting a watcher after
// it stops (device tool missing, service restarted, ...).
const deviceWatchRetryDelay = 3 * time.Second

// StartDeviceWatcher starts the platform-native device watcher for one device
// type and calls onChange whenever a local device is attached or detached. The
// watcher runs for the lifetime of the process.
func StartDeviceWatcher(deviceType DeviceType, onChange func()) {
	switch deviceType {
	case Android:
		go watchAndroidDevices(onChange)
	case IOS:
		go watchIOSDevices(onChange)
	case HarmonyOS:
		go watchHarmonyDevices(onChange)
	}
}

// watchAndroidDevices follows adb's `host:track-devices` stream.
func watchAndroidDevices(onChange func()) {
	for {
		client := getAdbClient()
		err := client.TrackDevicesWithCallback(func([]adb.DeviceInfo) {
			onChange()
		})
		slog.Debug("android device watcher stopped", "error", err)
		time.Sleep(deviceWatchRetryDelay)
	}
}

// watchIOSDevices follows the usbmuxd attach/detach stream.
func watchIOSDevices(onChange func()) {
	for {
		readMessage, closeListener, err := ios.Listen()
		if err != nil {
			slog.Debug("ios device watcher failed to start", "error", err)
			time.Sleep(deviceWatchRetryDelay)
			continue
		}
		for {
			message, err := readMessage()
			if err != nil {
				break
			}
			if message.DeviceAttached() || message.DeviceDetached() {
				onChange()
			}
		}
		_ = closeListener()
		time.Sleep(deviceWatchRetryDelay)
	}
}

// watchHarmonyDevices follows the hdc target tracker, which reports additions
// and removals as they happen.
func watchHarmonyDevices(onChange func()) {
	for {
		tracker, err := getHdcClient().TrackTargets(context.Background())
		if err != nil {
			slog.Debug("harmony device watcher failed to start", "error", err)
			time.Sleep(deviceWatchRetryDelay)
			continue
		}

		added := tracker.Added()
		removed := tracker.Removed()
		for added != nil || removed != nil {
			select {
			case _, ok := <-added:
				if !ok {
					added = nil
					continue
				}
				onChange()
			case _, ok := <-removed:
				if !ok {
					removed = nil
					continue
				}
				onChange()
			}
		}
		tracker.Close()
		time.Sleep(deviceWatchRetryDelay)
	}
}
