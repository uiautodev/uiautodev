package webserver

import (
	"log/slog"

	"github.com/uiautodev/uiautodev/webserver/device"
)

// listDevicesByPlatform enumerates device IDs for the given platforms, grouped
// by platform name, without querying each device's details, so it stays fast
// even with many devices. Every requested platform is present in the result,
// with an empty slice if no devices are connected.
func listDevicesByPlatform(types []device.DeviceType) map[string][]string {
	out := make(map[string][]string, len(types))
	for _, deviceType := range types {
		out[deviceType.String()] = []string{}
		manager := device.NewDeviceManager(deviceType)
		if manager == nil {
			continue
		}
		serials, err := manager.ListSerials()
		if err != nil {
			slog.Warn("listDevicesByPlatform: failed to list devices", "platform", deviceType.String(), "error", err)
			continue
		}
		for _, serial := range serials {
			out[deviceType.String()] = append(out[deviceType.String()], serialID(deviceType, serial))
		}
	}
	if master := getActiveClusterMaster(); master != nil {
		allowed := make(map[string]bool, len(types))
		for _, deviceType := range types {
			allowed[deviceType.String()] = true
		}
		for _, dev := range master.RemoteDevices("") {
			if allowed[dev.Platform] {
				out[dev.Platform] = append(out[dev.Platform], dev.ID)
			}
		}
	}
	return out
}

// allDeviceTypes returns every supported platform type, used as the default
// when the list_devices tool is called without a platforms argument.
func allDeviceTypes() []device.DeviceType {
	return []device.DeviceType{device.Android, device.IOS, device.HarmonyOS, device.Demo}
}

// serialID prefixes a platform serial with its device ID prefix.
func serialID(deviceType device.DeviceType, serial string) string {
	switch deviceType {
	case device.Android:
		return "a-" + serial
	case device.IOS:
		return "i-" + serial
	case device.HarmonyOS:
		return "h-" + serial
	case device.Demo:
		return "d-" + serial
	default:
		return serial
	}
}
