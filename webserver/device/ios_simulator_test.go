package device

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSimulatorUDID(t *testing.T) {
	tests := []struct {
		udid string
		want bool
	}{
		{"18D3CC49-93D4-404A-9213-2B3799F38DF0", true},
		{"18d3cc49-93d4-404a-9213-2b3799f38df0", true},
		{"00008110-000A1C2E0A88801E", false},
		{"18D3CC4993D4404A92132B3799F38DF0", false},
		{"18D3CC49-93D4-404A-9213-2B3799F38DF", false},
		{"not-a-udid", false},
		{"", false},
	}
	for _, test := range tests {
		if got := isSimulatorUDID(test.udid); got != test.want {
			t.Errorf("isSimulatorUDID(%q) = %v, want %v", test.udid, got, test.want)
		}
	}
}

func TestParseSimctlDeviceList(t *testing.T) {
	sample := `{
	  "devices": {
	    "com.apple.CoreSimulator.SimRuntime.iOS-18-3": [
	      {"udid": "BBBBBBBB-1111-2222-3333-444444444444", "name": "iPhone 15", "state": "Shutdown", "isAvailable": true},
	      {"udid": "AAAAAAAA-1111-2222-3333-444444444444", "name": "iPhone 16", "state": "Booted", "isAvailable": true},
	      {"udid": "CCCCCCCC-1111-2222-3333-444444444444", "name": "Old iPad", "state": "Booted", "isAvailable": false}
	    ],
	    "com.apple.CoreSimulator.SimRuntime.iOS-26-5": [
	      {"udid": "DDDDDDDD-1111-2222-3333-444444444444", "name": "iPhone 17", "state": "Booted"}
	    ]
	  }
	}`
	devices, err := parseSimctlDeviceList([]byte(sample))
	if err != nil {
		t.Fatalf("parseSimctlDeviceList() error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2: %+v", len(devices), devices)
	}
	if devices[0].UDID != "AAAAAAAA-1111-2222-3333-444444444444" || devices[0].Name != "iPhone 16" {
		t.Errorf("unexpected first device: %+v", devices[0])
	}
	// A missing isAvailable (older simctl) is treated as available.
	if devices[1].UDID != "DDDDDDDD-1111-2222-3333-444444444444" {
		t.Errorf("unexpected second device: %+v", devices[1])
	}
}

func TestParseSimctlDeviceListRejectsInvalidJSON(t *testing.T) {
	if _, err := parseSimctlDeviceList([]byte("{")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestMergeIOSSerials(t *testing.T) {
	physical := []string{"real-1", "real-2"}
	simulators := []string{"SIM-2", "SIM-1", "real-1"}
	got := mergeIOSSerials(physical, simulators)
	want := []string{"real-1", "real-2", "SIM-2", "SIM-1"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestAssignSimulatorPort(t *testing.T) {
	booted := []string{
		"BBBBBBBB-1111-2222-3333-444444444444",
		"AAAAAAAA-1111-2222-3333-444444444444",
		"CCCCCCCC-1111-2222-3333-444444444444",
	}
	tests := []struct {
		name       string
		udid       string
		configured int
		want       int
	}{
		{"first by udid", booted[1], 0, DefaultWDAPort},
		{"second by udid", booted[0], 0, DefaultWDAPort + 1},
		{"third by udid", booted[2], 0, DefaultWDAPort + 2},
		{"configured wins", booted[2], 9100, 9100},
		{"unknown simulator falls back", "DDDDDDDD-1111-2222-3333-444444444444", 0, DefaultWDAPort},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := assignSimulatorPort(test.udid, test.configured, booted); got != test.want {
				t.Errorf("assignSimulatorPort() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSimulatorPortStaysAssigned(t *testing.T) {
	first := "99999999-9999-9999-9999-999999999999"
	second := "11111111-1111-1111-1111-111111111111"
	t.Cleanup(func() {
		simulatorPortsMu.Lock()
		delete(simulatorPorts, first)
		delete(simulatorPorts, second)
		simulatorPortsMu.Unlock()
	})

	// The first simulator takes the base port.
	if got := simulatorPort(first, 0, []string{first}); got != DefaultWDAPort {
		t.Fatalf("first simulator port = %d, want %d", got, DefaultWDAPort)
	}
	// Booting an earlier-sorting simulator must not move the first one.
	booted := []string{second, first}
	if got := simulatorPort(first, 0, booted); got != DefaultWDAPort {
		t.Errorf("first simulator port moved to %d", got)
	}
	// The second simulator avoids the port already in use.
	if got := simulatorPort(second, 0, booted); got == DefaultWDAPort {
		t.Errorf("second simulator reused port %d", got)
	}
}

func TestExtractDevicekitRunner(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "runner.zip")
	writeTestZip(t, zipPath, map[string]string{
		devicekitRunnerAppName + "/Info.plist":                   "plist",
		devicekitRunnerAppName + "/PlugIns/runner.xctest/runner": "binary",
		"other.app/Info.plist":                                   "ignored",
	})
	dest := t.TempDir()
	if err := extractDevicekitRunner(zipPath, dest); err != nil {
		t.Fatalf("extractDevicekitRunner() error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, devicekitRunnerAppName, "PlugIns", "runner.xctest", "runner"))
	if err != nil || string(data) != "binary" {
		t.Fatalf("extracted runner = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "other.app", "Info.plist")); !os.IsNotExist(err) {
		t.Errorf("unrelated zip entry was extracted")
	}
}

func TestExtractDevicekitRunnerRejectsUnsafePath(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "runner.zip")
	writeTestZip(t, zipPath, map[string]string{
		devicekitRunnerAppName + "/../../evil": "bad",
	})
	if err := extractDevicekitRunner(zipPath, t.TempDir()); err == nil {
		t.Fatal("expected an error for a zip-slip entry")
	}
}

func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestNextSimulatorPortSkipsAssigned(t *testing.T) {
	leaf := "33333333-3333-3333-3333-333333333333"
	branch := "44444444-4444-4444-4444-444444444444"
	t.Cleanup(func() {
		simulatorPortsMu.Lock()
		delete(simulatorPorts, leaf)
		delete(simulatorPorts, branch)
		simulatorPortsMu.Unlock()
	})

	if got := simulatorPort(leaf, 0, []string{leaf}); got != DefaultWDAPort {
		t.Fatalf("leaf port = %d, want %d", got, DefaultWDAPort)
	}
	if got := simulatorPort(branch, 0, []string{branch, leaf}); got != DefaultWDAPort+1 {
		t.Fatalf("branch port = %d, want %d", got, DefaultWDAPort+1)
	}
	if got := nextSimulatorPort(branch); got != DefaultWDAPort+2 {
		t.Errorf("next port = %d, want %d", got, DefaultWDAPort+2)
	}
}

func TestNewIOSDeviceMarksSimulator(t *testing.T) {
	simulator := "55555555-5555-5555-5555-555555555555"
	SetIOSConfigResolver(func(udid string) IOSConfig {
		if udid == simulator {
			return IOSConfig{WDA: 9300, MJPEG: 9400, BundleID: "com.example.runner"}
		}
		return IOSConfig{}
	})
	t.Cleanup(func() { SetIOSConfigResolver(nil) })

	device := NewIOSDevice(simulator)
	if !device.simulator {
		t.Fatal("simulator UUID was not marked as a simulator")
	}
	if !device.wdaConfigured || device.wdaPort != 9300 {
		t.Errorf("configured simulator port = %d (configured=%v)", device.wdaPort, device.wdaConfigured)
	}
	// The runner serves JSON-RPC and MJPEG on one port.
	if device.mjpegPort != 9300 {
		t.Errorf("simulator MJPEG port = %d, want 9300", device.mjpegPort)
	}

	physical := NewIOSDevice("00008110-000A1C2E0A88801E")
	if physical.simulator {
		t.Error("physical device serial was marked as a simulator")
	}
}

func TestParseSimulatorAppList(t *testing.T) {
	sample := `{
	  "com.example.user": {"ApplicationType": "User"},
	  "com.apple.system": {"ApplicationType": "System"},
	  "com.apple.internal": {"ApplicationType": "Internal"},
	  "bad bundle id": {"ApplicationType": "User"}
	}`
	user, err := parseSimulatorAppList([]byte(sample), AppScopeUser)
	if err != nil {
		t.Fatalf("user scope: %v", err)
	}
	if len(user) != 1 || user[0] != "com.example.user" {
		t.Errorf("user scope = %v", user)
	}

	system, err := parseSimulatorAppList([]byte(sample), AppScopeSystem)
	if err != nil {
		t.Fatalf("system scope: %v", err)
	}
	if len(system) != 1 || system[0] != "com.apple.system" {
		t.Errorf("system scope = %v", system)
	}

	all, err := parseSimulatorAppList([]byte(sample), AppScopeAll)
	if err != nil {
		t.Fatalf("all scope: %v", err)
	}
	if len(all) != 3 || all[0] != "com.apple.internal" || all[1] != "com.apple.system" || all[2] != "com.example.user" {
		t.Errorf("all scope = %v", all)
	}

	if _, err := parseSimulatorAppList([]byte(sample), AppScope("bogus")); err == nil {
		t.Error("expected an error for an invalid scope")
	}
}

func TestParseDevicekitUI(t *testing.T) {
	sample := `{
	  "type": "XCUIElementTypeAny",
	  "rect": {"x": 0, "y": 0, "width": 0, "height": 0},
	  "children": [
	    {
	      "type": "XCUIElementTypeApplication",
	      "label": "Example",
	      "rect": {"x": 0, "y": 0, "width": 402, "height": 874},
	      "children": [
	        {
	          "type": "XCUIElementTypeButton",
	          "name": "submit",
	          "rawIdentifier": "submit",
	          "enabled": false,
	          "rect": {"x": 10, "y": 20, "width": 100, "height": 44}
	        }
	      ]
	    },
	    {
	      "type": "XCUIElementTypeStatusBar",
	      "rect": {"x": 0, "y": 0, "width": 402, "height": 54}
	    }
	  ]
	}`
	root, err := parseDevicekitUI([]byte(sample))
	if err != nil {
		t.Fatalf("parseDevicekitUI() error: %v", err)
	}
	if root.TagName != defaultRootTagName {
		t.Errorf("root tag = %q, want %q", root.TagName, defaultRootTagName)
	}
	if root.Rect.Width != 402 || root.Rect.Height != 874 {
		t.Errorf("root rect = %+v, want 402x874", root.Rect)
	}
	if len(root.Children) != 2 {
		t.Fatalf("root has %d children, want 2", len(root.Children))
	}
	app := root.Children[0]
	if app.TagName != "XCUIElementTypeApplication" || app.Props["class"] != "XCUIElementTypeApplication" {
		t.Errorf("unexpected application node: %+v", app)
	}
	if app.Props["label"] != "Example" {
		t.Errorf("application label = %q", app.Props["label"])
	}
	if len(app.Children) != 1 {
		t.Fatalf("application has %d children, want 1", len(app.Children))
	}
	button := app.Children[0]
	if button.TagName != "XCUIElementTypeButton" || button.Props["name"] != "submit" {
		t.Errorf("unexpected button node: %+v", button)
	}
	if button.Props["rawIdentifier"] != "submit" || button.Props["enabled"] != "0" {
		t.Errorf("unexpected button props: %+v", button.Props)
	}
	if button.Rect != (Rect{X: 10, Y: 20, Width: 100, Height: 44}) {
		t.Errorf("button rect = %+v", button.Rect)
	}
}

func TestParseDevicekitUIPlainRoot(t *testing.T) {
	root, err := parseDevicekitUI([]byte(`{"type":"XCUIElementTypeApplication","rect":{"x":0,"y":0,"width":10,"height":20}}`))
	if err != nil {
		t.Fatalf("parseDevicekitUI() error: %v", err)
	}
	if root.TagName != "XCUIElementTypeApplication" || root.Rect.Width != 10 || root.Rect.Height != 20 {
		t.Errorf("unexpected root: %+v", root)
	}
}

func TestDevicekitRPCRequests(t *testing.T) {
	type received struct {
		Method string
		Params map[string]any
	}
	var calls []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			JSONRPC string         `json:"jsonrpc"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.JSONRPC != "2.0" {
			t.Errorf("jsonrpc = %q", request.JSONRPC)
		}
		calls = append(calls, received{Method: request.Method, Params: request.Params})
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"success":true}}`))
	}))
	defer srv.Close()

	client := newDevicekitTestClient(srv.URL)
	if err := client.Tap(12, 34); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if err := client.Swipe(1, 2, 3, 4, 300); err != nil {
		t.Fatalf("swipe: %v", err)
	}
	if err := client.InputText("hello"); err != nil {
		t.Fatalf("input text: %v", err)
	}
	if err := client.ClearText(); err != nil {
		t.Fatalf("clear text: %v", err)
	}
	if err := client.PressKey(KeyEnter, MetaShift); err != nil {
		t.Fatalf("press key: %v", err)
	}
	if err := client.PressKey(KeyHome, MetaNone); err != nil {
		t.Fatalf("press home: %v", err)
	}
	if err := client.LaunchApp("com.example.app"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if err := client.TerminateApp("com.example.app"); err != nil {
		t.Fatalf("terminate: %v", err)
	}

	wantMethods := []string{
		"device.io.tap",
		"device.io.swipe",
		"device.io.text",
		"device.io.keys",
		"device.io.keys",
		"device.io.button",
		"device.apps.launch",
		"device.apps.terminate",
	}
	if len(calls) != len(wantMethods) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(wantMethods), calls)
	}
	for i, want := range wantMethods {
		if calls[i].Method != want {
			t.Errorf("call %d method = %q, want %q", i, calls[i].Method, want)
		}
	}
	if calls[0].Params["x"] != float64(12) || calls[0].Params["y"] != float64(34) {
		t.Errorf("tap params = %+v", calls[0].Params)
	}
	if calls[1].Params["duration"] != 0.3 {
		t.Errorf("swipe duration = %v, want 0.3", calls[1].Params["duration"])
	}
	if calls[2].Params["text"] != "hello" {
		t.Errorf("text params = %+v", calls[2].Params)
	}
	assertDevicekitKey(t, calls[3].Params, "clear", []any{})
	assertDevicekitKey(t, calls[4].Params, "return", []any{"shift"})
	if calls[5].Params["button"] != "home" {
		t.Errorf("button params = %+v", calls[5].Params)
	}
	if calls[6].Params["bundleId"] != "com.example.app" {
		t.Errorf("launch params = %+v", calls[6].Params)
	}
}

func assertDevicekitKey(t *testing.T, params map[string]any, wantKey string, wantModifiers []any) {
	t.Helper()
	keys, ok := params["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("keys params = %+v", params)
	}
	key, ok := keys[0].(map[string]any)
	if !ok {
		t.Fatalf("key entry = %+v", keys[0])
	}
	if key["key"] != wantKey {
		t.Errorf("key = %v, want %v", key["key"], wantKey)
	}
	modifiers, _ := key["modifiers"].([]any)
	if len(modifiers) != len(wantModifiers) {
		t.Fatalf("modifiers = %v, want %v", modifiers, wantModifiers)
	}
	for i := range wantModifiers {
		if modifiers[i] != wantModifiers[i] {
			t.Errorf("modifiers = %v, want %v", modifiers, wantModifiers)
		}
	}
}

func TestDevicekitRPCResults(t *testing.T) {
	pngData := testPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch request.Method {
		case "device.info":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"screenSize":{"width":402,"height":874},"scale":3}}`))
		case "device.screenshot":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"format":"png","data":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(pngData) + `"}}`))
		case "device.apps.foreground":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"bundleId":"com.example.app","name":"Example"}}`))
		case "device.dump.ui":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"type":"XCUIElementTypeApplication","rect":{"x":0,"y":0,"width":10,"height":20}}}`))
		default:
			t.Errorf("unexpected method %q", request.Method)
		}
	}))
	defer srv.Close()

	client := newDevicekitTestClient(srv.URL)

	width, height, err := client.ScreenSize()
	if err != nil || width != 402 || height != 874 {
		t.Fatalf("ScreenSize() = %d, %d, %v", width, height, err)
	}

	img, err := client.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot() error: %v", err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Errorf("screenshot size = %v", img.Bounds())
	}

	info, err := client.ForegroundApp()
	if err != nil || info.BundleID != "com.example.app" || info.Name != "Example" {
		t.Fatalf("ForegroundApp() = %+v, %v", info, err)
	}

	tree, err := client.DumpUI()
	if err != nil || tree.TagName != "XCUIElementTypeApplication" {
		t.Fatalf("DumpUI() = %+v, %v", tree, err)
	}
}

func TestDevicekitRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Unsupported key: nope"}}`))
	}))
	defer srv.Close()

	client := newDevicekitTestClient(srv.URL)
	if err := client.Tap(1, 2); err == nil || !strings.Contains(err.Error(), "Unsupported key") {
		t.Fatalf("expected the RPC error message, got %v", err)
	}
}

func newDevicekitTestClient(url string) *devicekitClient {
	client := newDevicekitClient(0)
	client.baseURL = url
	return client
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func bootedSimulatorDevice(t *testing.T) *IOSDevice {
	t.Helper()
	devices, err := listBootedSimulators()
	if err != nil {
		t.Skipf("no booted iOS simulator: %v", err)
	}
	if len(devices) == 0 {
		t.Skip("no booted iOS simulator")
	}
	t.Logf("using simulator: %s (%s)", devices[0].Name, devices[0].UDID)
	return NewIOSDevice(devices[0].UDID)
}

// TestIOSSimulatorDevice exercises a real booted simulator. It skips when no
// simulator is booted or the devicekit runner cannot be installed (the first
// run downloads it, so it needs network access).
func TestIOSSimulatorDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping simulator integration test in short mode")
	}
	device := bootedSimulatorDevice(t)
	if err := device.EnsureWDA(); err != nil {
		t.Skipf("devicekit runner unavailable: %v", err)
	}

	width, height, err := device.GetScreenSize()
	if err != nil {
		t.Fatalf("GetScreenSize() error: %v", err)
	}
	if width <= 0 || height <= 0 {
		t.Fatalf("invalid screen size %dx%d", width, height)
	}

	img, err := device.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot() error: %v", err)
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Error("screenshot has invalid dimensions")
	}

	tree, err := device.GetUITree()
	if err != nil {
		t.Fatalf("GetUITree() error: %v", err)
	}
	if tree == nil || tree.TagName == "" {
		t.Fatalf("unexpected UI tree: %+v", tree)
	}

	apps, err := device.ListApps(AppScopeUser)
	if err != nil {
		t.Fatalf("ListApps(user) error: %v", err)
	}
	foundRunner := false
	for _, app := range apps {
		if app == devicekitRunnerBundleID {
			foundRunner = true
		}
	}
	if !foundRunner {
		t.Errorf("user app list %v does not contain the devicekit runner", apps)
	}
}
