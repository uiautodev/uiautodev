package device

import (
	"fmt"
)

// DeviceType represents the type of device
type DeviceType int

const (
	Android DeviceType = iota
	IOS
	HarmonyOS
	Demo
)

// String returns the string representation of DeviceType
func (dt DeviceType) String() string {
	switch dt {
	case Android:
		return "android"
	case IOS:
		return "ios"
	case HarmonyOS:
		return "harmony"
	case Demo:
		return "demo"
	default:
		return "unknown"
	}
}

type DeviceIDInfo struct {
	Type   DeviceType
	Name   string
	Serial string
}

func ParseDeviceID(deviceId string) (*DeviceIDInfo, error) {
	if len(deviceId) < 2 {
		return nil, fmt.Errorf("invalid device id")
	}
	prefix := deviceId[:2]
	var deviceType DeviceType
	switch prefix {
	case "a-":
		deviceType = Android
	case "i-":
		deviceType = IOS
	case "h-":
		deviceType = HarmonyOS
	case "d-":
		deviceType = Demo
	default:
		return nil, fmt.Errorf("unknown device platform: %s", prefix)
	}

	serial := deviceId[2:]
	return &DeviceIDInfo{
		Type:   deviceType,
		Name:   "",
		Serial: serial,
	}, nil
}

// RemoteDeviceResolver resolves a device ID that is not handled by the local
// platform factories, e.g. a master-wide remote device ID.
type RemoteDeviceResolver func(deviceID string) (Device, bool)

var remoteDeviceResolver RemoteDeviceResolver

// RegisterRemoteDeviceResolver installs the resolver used by NewDevice for
// non-local device IDs. Passing nil clears it.
func RegisterRemoteDeviceResolver(resolver RemoteDeviceResolver) {
	remoteDeviceResolver = resolver
}

// NewDevice is the factory method that creates devices based on device ID
func NewDevice(deviceId string) (*DeviceWrapper, error) {
	if remoteDeviceResolver != nil {
		if remoteDevice, ok := remoteDeviceResolver(deviceId); ok {
			return &DeviceWrapper{Device: remoteDevice}, nil
		}
	}
	idInfo, err := ParseDeviceID(deviceId)
	if err != nil {
		return nil, err
	}
	var device Device
	switch idInfo.Type {
	case Android:
		device = NewAndroidDevice(idInfo.Serial)
	case IOS:
		device = NewIOSDevice(idInfo.Serial)
	case HarmonyOS:
		device = NewHarmonyOSDevice(idInfo.Serial)
	case Demo:
		device = NewDemoDevice(idInfo.Serial)
	default:
		return nil, fmt.Errorf("unknown device type")
	}
	return &DeviceWrapper{Device: device}, nil
}
