package device

import (
	"testing"
)

func TestIOSDeviceUsesConfiguredConfig(t *testing.T) {
	SetIOSConfigResolver(func(udid string) IOSConfig {
		if udid == "custom" {
			return IOSConfig{WDA: 8200, MJPEG: 9200, BundleID: "com.example.WDA.xctrunner"}
		}
		return IOSConfig{}
	})
	t.Cleanup(func() { SetIOSConfigResolver(nil) })

	custom := NewIOSDevice("custom")
	if custom.wdaPort != 8200 || custom.mjpegPort != 9200 {
		t.Errorf("configured ports not applied: wda=%d mjpeg=%d", custom.wdaPort, custom.mjpegPort)
	}
	if custom.wdaBundle != "com.example.WDA.xctrunner" {
		t.Errorf("configured bundle id not applied: %q", custom.wdaBundle)
	}
	if got := custom.wdaURL(); got != "http://localhost:8200" {
		t.Errorf("wdaURL() = %q, want http://localhost:8200", got)
	}

	fallback := NewIOSDevice("other")
	if fallback.wdaPort != DefaultWDAPort || fallback.mjpegPort != DefaultWDAMJPEGPort {
		t.Errorf("default ports not applied: wda=%d mjpeg=%d", fallback.wdaPort, fallback.mjpegPort)
	}
	if fallback.wdaBundle != DefaultWDABundleID {
		t.Errorf("default bundle id not applied: %q", fallback.wdaBundle)
	}
}

func TestIOSInfoIsOnline(t *testing.T) {
	device := NewIOSDevice("udid-test")
	info := device.Info()
	if info.Disabled {
		t.Error("iOS devices listed by the local API should not be disabled")
	}
	if info.ID != "i-udid-test" {
		t.Errorf("Info().ID = %q, want i-udid-test", info.ID)
	}
	if info.Platform != "ios" {
		t.Errorf("Info().Platform = %q, want ios", info.Platform)
	}
}

func TestIOSEnsureWDALockSharedPerSerial(t *testing.T) {
	a := NewIOSDevice("udid-1")
	b := NewIOSDevice("udid-1")
	c := NewIOSDevice("udid-2")

	if a.ensureWDALock() != b.ensureWDALock() {
		t.Error("instances of the same serial should share one lock")
	}
	if a.ensureWDALock() == c.ensureWDALock() {
		t.Error("different serials should use different locks")
	}
}

// getTestIOSDevice returns the first available iOS device, or skips the test if none available
func getTestIOSDevice(t *testing.T) *IOSDevice {
	t.Helper()

	manager := &IOSDeviceManager{}
	serials, err := manager.ListSerials()
	if err != nil {
		t.Skipf("No iOS devices available (or failed to list devices): %v", err)
		return nil
	}

	if len(serials) == 0 {
		t.Skip("No iOS devices connected")
		return nil
	}

	udid := serials[0]
	t.Logf("Using device: %s", udid)
	return NewIOSDevice(udid)
}

func TestListIosDeviceSerials(t *testing.T) {
	serials, err := (&IOSDeviceManager{}).ListSerials()
	if err != nil {
		t.Skipf("Skipping TestListIosDeviceSerials: failed to list iOS devices: %v", err)
		return
	}
	t.Logf("iOS device serials: %v", serials)

	// Basic sanity check: we should get a list (even if it's empty)
	if serials == nil {
		t.Errorf("ListIosDeviceSerials returned nil slice")
	}
}

func TestIOSScreenshot(t *testing.T) {
	device := getTestIOSDevice(t)
	if device == nil {
		return
	}

	img, err := device.Screenshot()
	if err != nil {
		t.Errorf("Screenshot failed: %v", err)
		return
	}

	if img == nil {
		t.Error("Screenshot returned nil image")
		return
	}

	bounds := img.Bounds()
	t.Logf("Screenshot successful: size=%dx%d", bounds.Dx(), bounds.Dy())

	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Error("Screenshot has invalid dimensions")
	}
}
