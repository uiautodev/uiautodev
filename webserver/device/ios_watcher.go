package device

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const defaultIOSWDAWatchInterval = 60 * time.Second

var iosWDAWatcherOnce sync.Once

// StartIOSWDAWatcher keeps WebDriverAgent running for connected iOS devices.
// It is safe to call more than once; only the first call starts a watcher.
func StartIOSWDAWatcher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultIOSWDAWatchInterval
	}
	iosWDAWatcherOnce.Do(func() {
		go runIOSWDAWatcher(ctx, interval)
	})
}

func runIOSWDAWatcher(ctx context.Context, interval time.Duration) {
	var retryAfter sync.Map // serial -> time.Time
	ensure := func(serial string) {
		if until, ok := retryAfter.Load(serial); ok {
			if time.Now().Before(until.(time.Time)) {
				return
			}
		}
		device := NewIOSDevice(serial)
		if err := device.EnsureWDA(); err != nil {
			retryAfter.Store(serial, time.Now().Add(5*time.Minute))
			slog.Warn("iOS WebDriverAgent auto-start failed", "device", serial, "error", err)
			return
		}
		retryAfter.Delete(serial)
		slog.Debug("iOS WebDriverAgent is ready", "device", serial)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		manager := &IOSDeviceManager{}
		serials, err := manager.ListSerials()
		if err != nil {
			slog.Debug("iOS device scan for WDA failed", "error", err)
		}
		for _, serial := range serials {
			go ensure(serial)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
