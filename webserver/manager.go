package webserver

import (
	"log/slog"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// deviceCacheItem is one cached device plus its release deadline. The mutex
// guards every field so the cleanup goroutine and request handlers do not race.
type deviceCacheItem struct {
	mu       sync.Mutex
	device   *device.DeviceWrapper
	deadline time.Time
	timeout  time.Duration
}

func (item *deviceCacheItem) touch(now time.Time) {
	item.mu.Lock()
	item.deadline = now.Add(item.timeout)
	item.mu.Unlock()
}

func (item *deviceCacheItem) expired(now time.Time) bool {
	item.mu.Lock()
	defer item.mu.Unlock()
	return now.After(item.deadline)
}

type deviceCacheManager struct {
	cache sync.Map
}

func NewDeviceCacheManager() *deviceCacheManager {
	dm := &deviceCacheManager{}
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			dm.cleanupExpiredDevices()
		}
	}()
	return dm
}

func (dm *deviceCacheManager) GetDevice(deviceId string, timeout time.Duration) (*device.DeviceWrapper, error) {
	if actual, ok := dm.cache.Load(deviceId); ok {
		item := actual.(*deviceCacheItem)
		item.touch(time.Now())
		return item.device, nil
	}

	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	newItem := &deviceCacheItem{
		device:   dev,
		deadline: now.Add(timeout),
		timeout:  timeout,
	}
	actual, loaded := dm.cache.LoadOrStore(deviceId, newItem)
	if loaded {
		item := actual.(*deviceCacheItem)
		item.touch(now)
		return item.device, nil
	}
	return dev, nil
}

func (dm *deviceCacheManager) ReleaseDevice(deviceId string) {
	actual, ok := dm.cache.LoadAndDelete(deviceId)
	if ok {
		item := actual.(*deviceCacheItem)
		item.device.ReleaseDeviceConnect()
	}
}

func (dm *deviceCacheManager) cleanupExpiredDevices() {
	now := time.Now()
	dm.cache.Range(func(key, value any) bool {
		deviceId := key.(string)
		item := value.(*deviceCacheItem)
		if item.expired(now) {
			slog.Info("releasing expired device", "device_id", deviceId)
			if dm.cache.CompareAndDelete(deviceId, item) {
				item.device.ReleaseDeviceConnect()
			}
		}
		return true
	})
}

// deviceCacheTimeout returns the fallback device cache lifetime. With presence
// enabled actual release is driven by occupancy, so the cache only needs a long
// safety net; otherwise the legacy one hour applies.
func deviceCacheTimeout() time.Duration {
	if getAccessConfig().Presence.Enabled {
		return 8 * time.Hour
	}
	return time.Hour
}

var deviceManagerInstance = NewDeviceCacheManager()
