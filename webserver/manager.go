package webserver

import (
	"log/slog"
	"sync"
	"time"
	"github.com/uiautodev/uiautodev/webserver/device"
)

type deviceCacheItem struct {
	device   *device.DeviceWrapper
	deadline time.Time
	timeout  time.Duration
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
	newItem := &deviceCacheItem{
		deadline: time.Now().Add(timeout),
		timeout:  timeout,
	}
	actual, loaded := dm.cache.LoadOrStore(deviceId, newItem)
	if !loaded {
		dev, err := device.NewDevice(deviceId)
		if err != nil {
			dm.cache.Delete(deviceId)
			return nil, err
		}
		newItem.device = dev
		return dev, nil
	}
	item := actual.(*deviceCacheItem)
	item.deadline = time.Now().Add(item.timeout)
	return item.device, nil
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
		if now.After(item.deadline) {
			slog.Info("releasing expired device", "device_id", deviceId)
			if dm.cache.CompareAndDelete(deviceId, item) {
				item.device.ReleaseDeviceConnect()
			}
		}
		return true
	})
}

var deviceManagerInstance = NewDeviceCacheManager()
