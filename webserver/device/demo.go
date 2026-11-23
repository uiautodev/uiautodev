package device

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static/demo/demo-wifi-on-bt-off.png
var demoScreenshotWifiOnBtOff []byte

//go:embed static/demo/demo-wifi-off-bt-off.png
var demoScreenshotWifiOffBtOff []byte

//go:embed static/demo/demo-wifi-on-bt-on.png
var demoScreenshotWifiOnBtOn []byte

//go:embed static/demo/demo-wifi-off-bt-on.png
var demoScreenshotWifiOffBtOn []byte

//go:embed static/demo/uitree.xml
var demoUITreeXML []byte

const demoSerial = "demo"

// Demo settings row bounds in device pixels (see static/demo/uitree.xml).
const (
	demoWifiRowTop    = 384
	demoWifiRowBottom = 564
	demoBtRowTop      = 564
	demoBtRowBottom   = 744
)

// errDemoNoStreaming is returned by demo device operations that need a real
// device connection (the demo only supports screenshot, UI dump and taps).
var errDemoNoStreaming = errors.New("demo device does not support live streaming")

// demoDeviceState is shared by every DemoDevice instance so toggles survive the
// per-request device construction.
var demoDeviceState = &struct {
	sync.Mutex
	wifiOn      bool
	bluetoothOn bool
}{wifiOn: true}

// DemoDevice is a built-in fake device that serves a bundled screenshot and UI
// hierarchy so the UI can be explored without a real device connected. Tapping
// the Wi-Fi / Bluetooth rows toggles their state.
type DemoDevice struct {
	serial string
}

func NewDemoDevice(serial string) *DemoDevice {
	if serial == "" {
		serial = demoSerial
	}
	return &DemoDevice{serial: serial}
}

func (d *DemoDevice) Info() DeviceInfo {
	return DeviceInfo{
		Platform: "demo",
		ID:       "d-" + d.serial,
		Props: map[string]interface{}{
			"serial":  d.serial,
			"model":   "Demo Phone",
			"product": "demo",
			"state":   "device",
		},
		Disabled: false,
	}
}

var (
	demoImagesMu sync.Mutex
	demoImages   = map[string]image.Image{}
)

func decodeDemoImage(key string, data []byte) (image.Image, error) {
	demoImagesMu.Lock()
	defer demoImagesMu.Unlock()
	if img, ok := demoImages[key]; ok {
		return img, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	demoImages[key] = img
	return img, nil
}

// imageKey returns the bundle image matching the current Wi-Fi/Bluetooth state.
func (d *DemoDevice) imageKey() string {
	demoDeviceState.Lock()
	defer demoDeviceState.Unlock()
	switch {
	case demoDeviceState.wifiOn && demoDeviceState.bluetoothOn:
		return "wifi-on-bt-on"
	case demoDeviceState.wifiOn:
		return "wifi-on-bt-off"
	case demoDeviceState.bluetoothOn:
		return "wifi-off-bt-on"
	default:
		return "wifi-off-bt-off"
	}
}

func (d *DemoDevice) loadImage() (image.Image, error) {
	switch d.imageKey() {
	case "wifi-on-bt-on":
		return decodeDemoImage("wifi-on-bt-on", demoScreenshotWifiOnBtOn)
	case "wifi-on-bt-off":
		return decodeDemoImage("wifi-on-bt-off", demoScreenshotWifiOnBtOff)
	case "wifi-off-bt-on":
		return decodeDemoImage("wifi-off-bt-on", demoScreenshotWifiOffBtOn)
	default:
		return decodeDemoImage("wifi-off-bt-off", demoScreenshotWifiOffBtOff)
	}
}

var (
	demoFrameMu    sync.Mutex
	demoFrameCache = map[string][]byte{}
)

// encodeDemoFrame JPEG-encodes the given image once per state key and caches it.
func encodeDemoFrame(key string, img image.Image) ([]byte, error) {
	demoFrameMu.Lock()
	defer demoFrameMu.Unlock()
	if data, ok := demoFrameCache[key]; ok {
		return data, nil
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	demoFrameCache[key] = data
	return data, nil
}

func (d *DemoDevice) GetScreenSize() (int, int, error) {
	img, err := d.loadImage()
	if err != nil {
		return 0, 0, err
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), nil
}

func (d *DemoDevice) Screenshot() (image.Image, error) {
	return d.loadImage()
}

// StartWebsocketStream pushes a JPEG frame whenever the demo state changes.
// The demo renders from a small set of bundled images, so instead of a real
// capture pipeline it polls the current state and only writes on change.
func (d *DemoDevice) StartWebsocketStream(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	lastKey := ""
	sendFrame := func() error {
		key := d.imageKey()
		if key == lastKey {
			return nil
		}
		img, err := d.loadImage()
		if err != nil {
			return err
		}
		frame, err := encodeDemoFrame(key, img)
		if err != nil {
			return err
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return err
		}
		lastKey = key
		return nil
	}

	if err := sendFrame(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := sendFrame(); err != nil {
				return err
			}
		}
	}
}

func (d *DemoDevice) GetScreens() ([]Screen, error) {
	return []Screen{{ID: "0"}}, nil
}

func (d *DemoDevice) GetUITree() (*Node, error) {
	root, err := ParseUIXML(string(demoUITreeXML))
	if err != nil {
		return nil, err
	}
	demoDeviceState.Lock()
	wifi, bt := demoDeviceState.wifiOn, demoDeviceState.bluetoothOn
	demoDeviceState.Unlock()

	setDemoSwitch(root, "com.android.settings:id/switch_wifi", "com.android.settings:id/summary_wifi", wifi, "Connected to Demo-WiFi")
	setDemoSwitch(root, "com.android.settings:id/switch_bluetooth", "com.android.settings:id/summary_bluetooth", bt, "On")
	return root, nil
}

// setDemoSwitch updates a switch node's checked/text state and its row summary
// text based on the given on/off state.
func setDemoSwitch(root *Node, switchID, summaryID string, on bool, onText string) {
	onOff := "Off"
	if on {
		onOff = "On"
	}
	var walk func(n *Node)
	walk = func(n *Node) {
		switch n.Props["resource-id"] {
		case switchID:
			n.Props["checked"] = strconv.FormatBool(on)
			n.Props["text"] = onOff
		case summaryID:
			if on {
				n.Props["text"] = onText
			} else {
				n.Props["text"] = "Off"
			}
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(root)
}

func (d *DemoDevice) Tap(x, y int) error {
	demoDeviceState.Lock()
	defer demoDeviceState.Unlock()
	switch {
	case inDemoRect(x, y, demoWifiRowTop, demoWifiRowBottom):
		demoDeviceState.wifiOn = !demoDeviceState.wifiOn
	case inDemoRect(x, y, demoBtRowTop, demoBtRowBottom):
		demoDeviceState.bluetoothOn = !demoDeviceState.bluetoothOn
	}
	return nil
}

func inDemoRect(x, y, top, bottom int) bool {
	return x >= 48 && x < 1032 && y >= top && y < bottom
}

func (d *DemoDevice) Swipe(x1, y1, x2, y2, durationMs int) error          { return nil }
func (d *DemoDevice) InputText(text string) error                         { return nil }
func (d *DemoDevice) ClearText() error                                    { return nil }
func (d *DemoDevice) PressKey(keyName KeyName, metaState MetaState) error { return nil }

func (d *DemoDevice) AcquireDeviceConnect() (*DeviceConnectInfo, error) {
	return nil, errDemoNoStreaming
}

func (d *DemoDevice) ReleaseDeviceConnect() error { return nil }

func (d *DemoDevice) Connect(socketAddr string) (net.Conn, error) {
	return nil, errDemoNoStreaming
}

// DemoDeviceManager enumerates the built-in demo device.
type DemoDeviceManager struct{}

func (e *DemoDeviceManager) ListSerials() ([]string, error) {
	return []string{demoSerial}, nil
}
