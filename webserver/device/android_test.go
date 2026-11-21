package device

import (
	"testing"

	"github.com/codeskyblue/go-adbkit/adb"
)

// getTestDevice returns a connected Android device for testing.
// Skips the test if no device is connected.
func getTestDevice(t *testing.T) *AndroidDevice {
	t.Helper()
	client := getAdbClient()
	d := client.Device(adb.AnyDevice())
	serial, err := d.Serial()
	if err != nil {
		t.Skipf("no device connected: %v", err)
	}
	return NewAndroidDevice(serial)
}

func TestAndroidDeviceInfo(t *testing.T) {
	device := getTestDevice(t)
	info := device.Info()

	// check info.ID is not empty
	if info.ID == "" {
		t.Errorf("info.ID should not be empty")
	}
	// check info.ID starts with "a-"
	if info.ID[0:2] != "a-" {
		t.Errorf("info.ID should start with 'a-', got %s", info.ID)
	}
	// check info.Platform is "android"
	if info.Platform != "android" {
		t.Errorf("info.Platform should be 'android', got %s", info.Platform)
	}
	// check info.Props is not nil
	if info.Props == nil {
		t.Errorf("info.Props should not be nil")
	}
	// check serial in props
	if serial, ok := info.Props["serial"]; !ok || serial != "" {
		t.Logf("info.Props['serial'] = %v", serial)
	}
}

func TestAndroidKeyName(t *testing.T) {
	tests := []struct {
		input     KeyName
		want      string
		wantError bool
	}{
		{KeyHome, "KEYCODE_HOME", false},
		{KeyBack, "KEYCODE_BACK", false},
		{KeyMenu, "KEYCODE_MENU", false},
		{KeyRecents, "KEYCODE_APP_SWITCH", false},
		{KeyVolumeUp, "KEYCODE_VOLUME_UP", false},
		{KeyTab, "KEYCODE_TAB", false},
		{KeySpace, "KEYCODE_SPACE", false},
		{KeyEnter, "KEYCODE_ENTER", false},
		{KeyEscape, "KEYCODE_ESCAPE", false},
		{KeyBackspace, "KEYCODE_DEL", false},
		{KeyDelete, "KEYCODE_FORWARD_DEL", false},
		{KeyDpadUp, "KEYCODE_DPAD_UP", false},
		{KeyDpadCenter, "KEYCODE_DPAD_CENTER", false},
		{KeyComma, "KEYCODE_COMMA", false},
		{KeyPlus, "KEYCODE_PLUS", false},
		{KeyAt, "KEYCODE_AT", false},
		{KeyName("a"), "KEYCODE_A", false},
		{KeyName("z"), "KEYCODE_Z", false},
		{KeyName("0"), "KEYCODE_0", false},
		{KeyName("9"), "KEYCODE_9", false},
		{KeyName(""), "", true},
		{KeyName("unknown"), "", true},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got, ok := androidKeyName(tt.input)
			if tt.wantError {
				if ok {
					t.Errorf("expected error for input %q", tt.input)
				}
				return
			}
			if !ok {
				t.Errorf("androidKeyName(%q) unexpected error", tt.input)
				return
			}
			if got != tt.want {
				t.Errorf("androidKeyName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAndroidDeviceGetOrientation(t *testing.T) {
	device := getTestDevice(t)
	orientation, err := device.GetOrientation()
	if err != nil {
		t.Errorf("GetOrientation failed: %v", err)
	}
	t.Logf("Device orientation: %d", orientation)
}

func TestAndroidDeviceGetScreenSize(t *testing.T) {
	device := getTestDevice(t)
	width, height, err := device.GetScreenSize()
	if err != nil {
		t.Errorf("GetScreenSize failed: %v", err)
	}
	t.Logf("Device screen size: %dx%d", width, height)

	// Basic sanity checks
	if width <= 0 {
		t.Errorf("width should be positive, got %d", width)
	}
	if height <= 0 {
		t.Errorf("height should be positive, got %d", height)
	}
	// Check for reasonable screen sizes (between 320x480 and 8K)
	if width < 320 || width > 7680 {
		t.Errorf("width %d seems unreasonable", width)
	}
	if height < 480 || height > 4320 {
		t.Errorf("height %d seems unreasonable", height)
	}
}

func TestAndroidDeviceAcquireReleaseConnect(t *testing.T) {
	device := getTestDevice(t)

	// Test AcquireDeviceConnect
	info, err := device.AcquireDeviceConnect()
	if err != nil {
		t.Errorf("AcquireDeviceConnect failed: %v", err)
	}
	t.Logf("Device connect info: type=%s, transport=%s, endpoint=%s", info.Type, info.Transport, info.Endpoint)

	// Verify info fields
	if info.Type != "tcp" {
		t.Errorf("expected Type 'tcp', got '%s'", info.Type)
	}
	if info.Transport != "adb" {
		t.Errorf("expected Transport 'adb', got '%s'", info.Transport)
	}
	if info.Endpoint == "" {
		t.Errorf("Endpoint should not be empty")
	}

	// Test that calling AcquireDeviceConnect again returns the same info
	info2, err := device.AcquireDeviceConnect()
	if err != nil {
		t.Errorf("second AcquireDeviceConnect failed: %v", err)
	}
	if info2.Endpoint != info.Endpoint {
		t.Errorf("second AcquireDeviceConnect should return same endpoint, got %s vs %s", info2.Endpoint, info.Endpoint)
	}

	// Test ReleaseDeviceConnect
	if err := device.ReleaseDeviceConnect(); err != nil {
		t.Errorf("ReleaseDeviceConnect failed: %v", err)
	}

	// Test that calling ReleaseDeviceConnect again is safe (should return nil)
	if err := device.ReleaseDeviceConnect(); err != nil {
		t.Errorf("second ReleaseDeviceConnect should return nil, got: %v", err)
	}

	// Test that we can acquire again after release
	info3, err := device.AcquireDeviceConnect()
	if err != nil {
		t.Errorf("AcquireDeviceConnect after release failed: %v", err)
	}
	if info3.Endpoint == "" {
		t.Errorf("Endpoint after re-acquire should not be empty")
	}

	// Clean up
	if err := device.ReleaseDeviceConnect(); err != nil {
		t.Errorf("final ReleaseDeviceConnect failed: %v", err)
	}
}
