package webserver

import "uiautodev-wails/webserver/device"

func init() {
	device.SetIOSConfigResolver(func(udid string) device.IOSConfig {
		metas, err := getDeviceMetas()
		if err != nil {
			return device.IOSConfig{}
		}
		meta := metas["i-"+udid]
		return device.IOSConfig{
			WDA:      meta.WDAPort,
			MJPEG:    meta.WDAMJPEGPort,
			BundleID: meta.WDABundleID,
		}
	})
}
