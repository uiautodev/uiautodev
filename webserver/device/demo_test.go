package device

import (
	"bytes"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDemoDevice(t *testing.T) {
	d := NewDemoDevice("")

	info := d.Info()
	assert.Equal(t, "demo", info.Platform)
	assert.Equal(t, "d-demo", info.ID)
	assert.Equal(t, "demo", info.Props["serial"])

	w, h, err := d.GetScreenSize()
	require.NoError(t, err)
	assert.Equal(t, 1080, w)
	assert.Equal(t, 1920, h)

	img, err := d.Screenshot()
	require.NoError(t, err)
	assert.Equal(t, 1080, img.Bounds().Dx())
	assert.Equal(t, 1920, img.Bounds().Dy())

	tree, err := d.GetUITree()
	require.NoError(t, err)
	require.NotNil(t, tree)
	xmlStr, err := tree.ToXML()
	require.NoError(t, err)
	assert.Contains(t, xmlStr, "Settings")
	assert.Contains(t, xmlStr, "Wi-Fi")

	// Interactions are no-ops.
	assert.NoError(t, d.Tap(1, 2))
	assert.NoError(t, d.Swipe(0, 0, 1, 1, 100))
	assert.NoError(t, d.InputText("x"))
	assert.NoError(t, d.ClearText())
	assert.NoError(t, d.PressKey(KeyHome, 0))

	// No live streaming.
	_, err = d.AcquireDeviceConnect()
	assert.Error(t, err)

	screens, err := d.GetScreens()
	require.NoError(t, err)
	assert.Len(t, screens, 1)
}

func TestDemoDeviceWebsocketStream(t *testing.T) {
	demoDeviceState.Lock()
	demoDeviceState.wifiOn = true
	demoDeviceState.bluetoothOn = false
	demoDeviceState.Unlock()

	dev := NewDemoDevice("")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = dev.StartWebsocketStream(r.Context(), conn)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)
	defer conn.Close()

	// The first frame is pushed immediately on connect.
	_, first, err := conn.ReadMessage()
	require.NoError(t, err)
	img, _, err := image.Decode(bytes.NewReader(first))
	require.NoError(t, err)
	assert.Equal(t, 1080, img.Bounds().Dx())

	// Toggling Wi-Fi pushes an updated frame.
	require.NoError(t, dev.Tap(500, 470))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, second, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

func TestDemoDeviceManager(t *testing.T) {
	serials, err := (&DemoDeviceManager{}).ListSerials()
	require.NoError(t, err)
	assert.Equal(t, []string{"demo"}, serials)
}

func TestParseDemoDeviceID(t *testing.T) {
	info, err := ParseDeviceID("d-demo")
	require.NoError(t, err)
	assert.Equal(t, Demo, info.Type)
	assert.Equal(t, "demo", info.Serial)

	dev, err := NewDevice("d-demo")
	require.NoError(t, err)
	_, ok := dev.Device.(*DemoDevice)
	assert.True(t, ok)
}

// demoProp returns the value of key on the node with the given resource-id.
func demoProp(root *Node, resourceID, key string) string {
	var found string
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Props["resource-id"] == resourceID {
			found = n.Props[key]
			return
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(root)
	return found
}

func TestDemoDeviceTapToggles(t *testing.T) {
	demoDeviceState.Lock()
	demoDeviceState.wifiOn = true
	demoDeviceState.bluetoothOn = false
	demoDeviceState.Unlock()

	d := NewDemoDevice("")
	tree, err := d.GetUITree()
	require.NoError(t, err)
	assert.Equal(t, "true", demoProp(tree, "com.android.settings:id/switch_wifi", "checked"))
	assert.Equal(t, "Connected to Demo-WiFi", demoProp(tree, "com.android.settings:id/summary_wifi", "text"))

	// Tapping the Wi-Fi row turns it off.
	require.NoError(t, d.Tap(500, 470))
	tree, err = d.GetUITree()
	require.NoError(t, err)
	assert.Equal(t, "false", demoProp(tree, "com.android.settings:id/switch_wifi", "checked"))
	assert.Equal(t, "Off", demoProp(tree, "com.android.settings:id/summary_wifi", "text"))

	// Tapping the Bluetooth row turns it on.
	require.NoError(t, d.Tap(500, 650))
	tree, err = d.GetUITree()
	require.NoError(t, err)
	assert.Equal(t, "true", demoProp(tree, "com.android.settings:id/switch_bluetooth", "checked"))
	assert.Equal(t, "On", demoProp(tree, "com.android.settings:id/summary_bluetooth", "text"))

	// Tapping outside the rows does nothing.
	require.NoError(t, d.Tap(10, 10))
	tree, err = d.GetUITree()
	require.NoError(t, err)
	assert.Equal(t, "false", demoProp(tree, "com.android.settings:id/switch_wifi", "checked"))
}
