package device

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/webserver/utils"

	"github.com/codeskyblue/go-adbkit/adb"
	"github.com/codeskyblue/go-adbkit/tcpusb"
	"github.com/pkg/errors"
)

var adbOnce sync.Once
var adbPath string

func AdbPath() string {
	adbOnce.Do(func() {
		adbPath = utils.FindBinary("adb")
	})
	return adbPath
}

var adbClientOnce sync.Once
var adbClient *adb.Client

func getAdbClient() *adb.Client {
	adbClientOnce.Do(func() {
		adbClient = adb.NewClientWithOptions(adb.ClientOptions{
			Bin: AdbPath(),
		})
	})
	return adbClient
}

// AndroidDeviceManager implements DeviceEnumerator for Android devices
type AndroidDeviceManager struct{}

// ListSerials returns a list of Android device serial numbers
func (e *AndroidDeviceManager) ListSerials() ([]string, error) {
	client := getAdbClient()
	infos, err := client.ListDevices()
	if err != nil {
		return nil, err
	}
	var serials []string
	for _, info := range infos {
		if info.State != "device" {
			continue
		}
		serials = append(serials, info.Serial)
	}
	return serials, nil
}

// AndroidDevice implements Android-specific device operations.
type AndroidDevice struct {
	serial string

	adbkitServer *tcpusb.Server
	adbkitPort   int // Track the port used for adbkit
}

func NewAndroidDevice(serial string) *AndroidDevice {
	// If the serial contains ":", it may be a network device, ensure connected
	if strings.Contains(serial, ":") {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		getAdbClient().ConnectContext(ctx, serial)
	}
	return &AndroidDevice{
		serial: serial,
	}
}

func (d *AndroidDevice) getAdbkitDevice() *adb.Device {
	client := getAdbClient()
	return client.Device(adb.DeviceWithSerial(d.serial))
}

func (d *AndroidDevice) Info() DeviceInfo {
	device := d.getAdbkitDevice()
	props, err := device.Properties()
	if err != nil {
		return DeviceInfo{}
	}

	model := props["ro.product.model"]
	product := props["ro.product.name"]
	state, _ := device.State()

	propsMap := map[string]interface{}{
		"serial":  d.serial,
		"product": product,
		"model":   model,
		"state":   state,
	}
	disabled := state != "device" && state != "online"
	return DeviceInfo{
		ID:       "a-" + d.serial,
		Props:    propsMap,
		Disabled: disabled,
		Platform: "android",
	}
}

// RunShell runs adb shell <cmd> with optional timeout in milliseconds.
// Returns combined stdout/stderr and error.
func (d *AndroidDevice) RunShell(cmdStr string, timeoutMs int64) (string, error) {
	slog.Debug("Running adb shell command", "serial", d.serial, "cmd", cmdStr, "timeoutMs", timeoutMs)
	device := d.getAdbkitDevice()

	var ctx context.Context
	var cancel context.CancelFunc
	if timeoutMs > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	output, err := device.RunCommandContext(ctx, cmdStr)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", ctx.Err()
		}
		return "", err
	}

	return output, nil
}

// GetOrientation returns the device orientation using `dumpsys input`.
// Returns 0 for portrait, 1 for landscape.
func (d *AndroidDevice) GetOrientation() (int, error) {
	out, err := d.RunShell("dumpsys input", 5000)
	if err != nil {
		return 0, err
	}
	// Parse output for surfaceOrientation
	// Example lines: "SurfaceOrientation: 1" or "mSurfaceOrientation=1"
	outStr := string(out)
	re := regexp.MustCompile(`(?:SurfaceOrientation|mSurfaceOrientation)[\s:=]+(\d+)`)
	matches := re.FindStringSubmatch(outStr)
	if len(matches) >= 2 {
		orientation, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, err
		}
		return orientation, nil
	}
	return 0, nil
}

// GetScreenSize returns device physical width and height using `wm size`.
// Automatically adjusts width and height based on device orientation.
func (d *AndroidDevice) GetScreenSize() (int, int, error) {
	out, err := d.RunShell("wm size", 5000)
	if err != nil {
		return 0, 0, err
	}
	outStr := strings.TrimSpace(string(out))
	if strings.Contains(outStr, ":") {
		parts := strings.SplitN(outStr, ":", 2)
		outStr = strings.TrimSpace(parts[1])
	}
	dims := strings.SplitN(outStr, "x", 2)
	if len(dims) != 2 {
		return 0, 0, fmt.Errorf("unexpected wm size output: %s", outStr)
	}
	w, err := strconv.Atoi(strings.TrimSpace(dims[0]))
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(strings.TrimSpace(dims[1]))
	if err != nil {
		return 0, 0, err
	}

	// Check orientation and swap if landscape (rotation 90 or 270 degrees)
	orientation, err := d.GetOrientation()
	if err == nil && (orientation == 1 || orientation == 3) {
		// Landscape: swap width and height
		return h, w, nil
	}
	return w, h, nil
}

// Tap executes input tap x y on device.
func (d *AndroidDevice) Tap(x, y int) error {
	cmdStr := fmt.Sprintf("input tap %d %d", x, y)
	_, err := d.RunShell(cmdStr, 3000)
	return err
}

func (d *AndroidDevice) Screenshot() (image.Image, error) {
	client := getAdbClient()
	device := client.Device(adb.DeviceWithSerial(d.serial))
	img, err := device.Screencap()
	if err == nil {
		return img, nil
	}
	slog.Warn("screencap failed, falling back to yadb", "serial", d.serial, "error", err)
	img, yadbErr := d.screenshotByYadb()
	if yadbErr != nil {
		return nil, fmt.Errorf("screencap failed: %v; yadb fallback failed: %w", err, yadbErr)
	}
	return img, nil
}

// GetScreens retrieves all display/screen information from the device
func (d *AndroidDevice) GetScreens() ([]Screen, error) {
	// Use SurfaceFlinger to list display IDs
	output, err := d.RunShell("dumpsys SurfaceFlinger --display-id", 5000)
	if err != nil {
		// Fallback to a default screen
		screens := []Screen{{ID: "0"}}
		return screens, nil
	}

	// Parse output for display id patterns. Examples of lines we expect:
	//   Display 0
	//   displayId=0
	// Use regexes to cover common variants.
	reDisplay := regexp.MustCompile(`Display\s*#?\s*(\d+)`)
	reId := regexp.MustCompile(`displayId[:=]?\s*(\d+)`)

	lines := strings.Split(output, "\n")
	screens := make([]Screen, 0)
	seen := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if m := reDisplay.FindStringSubmatch(line); len(m) > 1 {
			id := m[1]
			if !seen[id] {
				screens = append(screens, Screen{ID: id})
				seen[id] = true
			}
			continue
		}
		if m := reId.FindStringSubmatch(line); len(m) > 1 {
			id := m[1]
			if !seen[id] {
				screens = append(screens, Screen{ID: id})
				seen[id] = true
			}
			continue
		}
	}

	// If none found, try to extract any solitary small integers on lines containing 'Display' or 'display'
	if len(screens) == 0 {
		reAnyNum := regexp.MustCompile(`(\d+)`)
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "display") {
				if m := reAnyNum.FindStringSubmatch(line); len(m) > 1 {
					id := m[1]
					if !seen[id] {
						screens = append(screens, Screen{ID: id})
						seen[id] = true
					}
				}
			}
		}
	}

	// Final fallback to single display id 0
	if len(screens) == 0 {
		screens = []Screen{{ID: "0"}}
	}

	return screens, nil
}

func (d *AndroidDevice) GetUITree() (*Node, error) {
	return d.GetUITreeWithBackend(UITreeBackendYadb)
}

// GetUITreeWithBackend dumps the UI hierarchy using the requested backend.
// yadb is the default and falls back to uiautomator dump on failure; the
// uiautomator backend uses `uiautomator dump` directly. Both fall back to
// uiautomator2 as a last resort.
func (d *AndroidDevice) GetUITreeWithBackend(backend UITreeBackend) (*Node, error) {
	var xmlContent []byte
	var err error

	switch backend {
	case UITreeBackendUiautomator:
		xmlContent, err = d.getPageXMLByCmd()
		if err != nil {
			slog.Warn("uiautomator dump failed, falling back to uiautomator2", "error", err)
		}
	default: // UITreeBackendYadb
		// First try yadb's efficient layout dump
		xmlContent, err = d.getPageXMLByYadb()
		if err != nil {
			slog.Warn("yadb layout dump failed, falling back to uiautomator dump", "error", err)
		}
		// Then fallback to uiautomator dump
		if len(xmlContent) == 0 {
			xmlContent, err = d.getPageXMLByCmd()
			if err != nil {
				slog.Warn("uiautomator dump failed, falling back to uiautomator2", "error", err)
			}
		}
	}
	// Finally fallback to uiautomator2
	if len(xmlContent) == 0 {
		xmlContent, err = d.getPageXMLByU2()
		if err != nil {
			return nil, err
		}
	}
	return d.buildUITree(xmlContent)
}

// buildUITree parses raw uiautomator XML and fills in the screen size when the
// XML does not carry it.
func (d *AndroidDevice) buildUITree(xmlContent []byte) (*Node, error) {
	root, err := ParseUIXML(string(xmlContent))
	if err != nil {
		return nil, err
	}
	// If width/height not present in XML, populate from wm size
	if root.Rect.Width == 0 || root.Rect.Height == 0 {
		w, h, err := d.GetScreenSize()
		slog.Debug("fetched screen size for ui tree", "width", w, "height", h)
		if err == nil {
			root.Rect.Width = w
			root.Rect.Height = h
		}
	}
	return root, nil
}

func (d *AndroidDevice) getPageXMLByU2() ([]byte, error) {
	// Use uiautomator2 to get the UI hierarchy XML
	device := d.getAdbkitDevice()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := device.OpenSocketContext(ctx, "tcp:9008")
	if err != nil {
		return nil, errors.Wrap(err, "failed to open uiautomator2 socket")
	}
	defer conn.Close()

	// Build JSON-RPC request body
	reqBody := `{"jsonrpc": "2.0", "id": 1, "method": "dumpWindowHierarchy", "params": [false, 50]}`
	req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost/jsonrpc/0", strings.NewReader(reqBody))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create HTTP request")
	}
	req.Header.Set("Content-Type", "application/json")

	// Write the request to the connection
	if err := req.Write(conn); err != nil {
		return nil, errors.Wrap(err, "failed to write HTTP request")
	}

	// Read the response from the connection
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read HTTP response")
	}
	defer resp.Body.Close()

	// Parse JSON-RPC response
	var respBody struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  string `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, errors.Wrap(err, "failed to parse uiautomator2 JSON response")
	}

	if respBody.Error != nil {
		return nil, fmt.Errorf("uiautomator2 error: %s", respBody.Error.Message)
	}

	return []byte(respBody.Result), nil
}

// getPageXMLByCmd retrieves the UI hierarchy for the main screen using uiautomator dump
func (d *AndroidDevice) getPageXMLByCmd() ([]byte, error) {
	// Run uiautomator to dump the UI hierarchy to /sdcard/window_dump.xml then cat it
	cmd := "uiautomator dump /sdcard/window_dump.xml"
	output, err := d.RunShell(cmd, 15000)
	if err != nil {
		return nil, errors.Wrap(err, "failed to run uiautomator dump")
	}
	if !strings.Contains(output, "UI hierchary dumped to:") {
		return nil, fmt.Errorf("uiautomator dump failed: %s", output)
	}
	device := d.getAdbkitDevice()
	dumpContent, err := device.ReadFile("/sdcard/window_dump.xml")
	if err != nil {
		return nil, errors.Wrap(err, "failed to open UI dump file")
	}
	return []byte(dumpContent), nil
}

func (d *AndroidDevice) Swipe(x1, y1, x2, y2, durationMs int) error {
	cmdStr := fmt.Sprintf("input swipe %d %d %d %d %d", x1, y1, x2, y2, durationMs)
	_, err := d.RunShell(cmdStr, 5000)
	return err
}

func (d *AndroidDevice) InputText(text string) error {
	// yadb pastes via clipboard, supporting non-ASCII text such as Chinese.
	if err := d.inputTextByYadb(text); err == nil {
		return nil
	} else {
		slog.Warn("yadb input text failed, falling back to input text", "serial", d.serial, "error", err)
	}
	// Fallback to native input text, escape spaces.
	escapedText := strings.ReplaceAll(text, " ", "%s")
	cmdStr := fmt.Sprintf("input text %s", escapedText)
	_, err := d.RunShell(cmdStr, 5000)
	return err
}

// ClearText selects all text in the currently focused field and deletes it.
// Uses yadb first, and falls back to the native method which requires
// Android 12+ (SDK 31) for the Shift+Move_Home combination.
func (d *AndroidDevice) ClearText() error {
	if err := d.clearTextByYadb(); err == nil {
		return nil
	} else {
		slog.Warn("yadb clear text failed, falling back to native method", "serial", d.serial, "error", err)
	}
	sdk, err := d.sdkVersion()
	if err != nil {
		return errors.Wrap(err, "ClearText: failed to read sdk version")
	}
	if sdk < 31 {
		return fmt.Errorf("ClearText is not supported on Android %d (SDK %d), requires SDK >= 31", sdk-1, sdk)
	}
	commands := []string{
		"input keyevent KEYCODE_MOVE_END",
		"input keycombination KEYCODE_SHIFT_LEFT KEYCODE_MOVE_HOME",
		"input keyevent KEYCODE_DEL",
	}
	for _, cmd := range commands {
		if _, err := d.RunShell(cmd, 5000); err != nil {
			return errors.Wrap(err, "ClearText failed")
		}
	}
	return nil
}

// sdkVersion returns the Android SDK level (e.g. 31 for Android 12) via
// `getprop ro.build.version.sdk`.
func (d *AndroidDevice) sdkVersion() (int, error) {
	out, err := d.RunShell("getprop ro.build.version.sdk", 5000)
	if err != nil {
		return 0, err
	}
	sdk, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("invalid sdk version %q: %w", strings.TrimSpace(out), err)
	}
	return sdk, nil
}

// PressKey simulates a key event on the device. metaState is currently
// ignored and only the single key is pressed.
func (d *AndroidDevice) PressKey(keyName KeyName, metaState MetaState) error {
	androidKeyCode, ok := androidKeyName(keyName)
	if !ok {
		return fmt.Errorf("unsupported key name: %s", keyName)
	}

	cmdStr := fmt.Sprintf("input keyevent %s", androidKeyCode)
	_, err := d.RunShell(cmdStr, 3000)
	return err
}

// androidKeyName maps a logical key name to the Android keycode name.
func androidKeyName(keyName KeyName) (string, bool) {
	switch keyName {
	case KeyHome:
		return "KEYCODE_HOME", true
	case KeyBack:
		return "KEYCODE_BACK", true
	case KeyMenu:
		return "KEYCODE_MENU", true
	case KeyRecents:
		return "KEYCODE_APP_SWITCH", true
	case KeyPower:
		return "KEYCODE_POWER", true
	case KeyVolumeUp:
		return "KEYCODE_VOLUME_UP", true
	case KeyVolumeDown:
		return "KEYCODE_VOLUME_DOWN", true
	case KeyTab:
		return "KEYCODE_TAB", true
	case KeySpace:
		return "KEYCODE_SPACE", true
	case KeyEnter:
		return "KEYCODE_ENTER", true
	case KeyEscape:
		return "KEYCODE_ESCAPE", true
	case KeyBackspace:
		return "KEYCODE_DEL", true
	case KeyDelete:
		return "KEYCODE_FORWARD_DEL", true
	case KeyCapsLock:
		return "KEYCODE_CAPS_LOCK", true
	case KeyPageUp:
		return "KEYCODE_PAGE_UP", true
	case KeyPageDown:
		return "KEYCODE_PAGE_DOWN", true
	case KeyInsert:
		return "KEYCODE_INSERT", true
	case KeyDpadUp:
		return "KEYCODE_DPAD_UP", true
	case KeyDpadDown:
		return "KEYCODE_DPAD_DOWN", true
	case KeyDpadLeft:
		return "KEYCODE_DPAD_LEFT", true
	case KeyDpadRight:
		return "KEYCODE_DPAD_RIGHT", true
	case KeyDpadCenter:
		return "KEYCODE_DPAD_CENTER", true
	case KeyComma:
		return "KEYCODE_COMMA", true
	case KeyPeriod:
		return "KEYCODE_PERIOD", true
	case KeyMinus:
		return "KEYCODE_MINUS", true
	case KeyEquals:
		return "KEYCODE_EQUALS", true
	case KeySemicolon:
		return "KEYCODE_SEMICOLON", true
	case KeyQuote:
		return "KEYCODE_APOSTROPHE", true
	case KeySlash:
		return "KEYCODE_SLASH", true
	case KeyBackslash:
		return "KEYCODE_BACKSLASH", true
	case KeyLeftBracket:
		return "KEYCODE_LEFT_BRACKET", true
	case KeyRightBracket:
		return "KEYCODE_RIGHT_BRACKET", true
	case KeyBackquote:
		return "KEYCODE_GRAVE", true
	case KeyPlus:
		return "KEYCODE_PLUS", true
	case KeyAt:
		return "KEYCODE_AT", true
	}
	// Single character keys: a-z and 0-9
	if len(keyName) == 1 {
		c := keyName[0]
		if c >= 'a' && c <= 'z' {
			return "KEYCODE_" + strings.ToUpper(string(c)), true
		}
		if c >= '0' && c <= '9' {
			return "KEYCODE_" + string(c), true
		}
	}
	return "", false
}

func (d *AndroidDevice) AcquireDeviceConnect() (*DeviceConnectInfo, error) {
	if d.adbkitServer != nil {
		slog.Debug("adbkit server already running", "serial", d.serial)
		return d.genDeviceConnectInfo(), nil
	}

	// Get a free port for the adbkit server
	port, err := utils.GetFreePort()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get free port")
	}
	slog.Debug("free port for adbkit", "serial", d.serial, "port", port)

	bridge := tcpusb.NewBridge(d.serial)
	bridge.Config.Port = port
	server, err := bridge.StartWithServer()
	if err != nil {
		return nil, errors.Wrap(err, "failed to start adbkit tcpusb bridge")
	}
	d.adbkitServer = server
	d.adbkitPort = port
	return d.genDeviceConnectInfo(), nil
}

func (d *AndroidDevice) genDeviceConnectInfo() *DeviceConnectInfo {
	return &DeviceConnectInfo{
		Type:      "tcp",
		Transport: "adb",
		Endpoint:  fmt.Sprintf("localhost:%d", d.adbkitPort),
	}
}

func (d *AndroidDevice) ReleaseDeviceConnect() error {
	if d.adbkitServer == nil {
		slog.Debug("No adbkit server to close", "serial", d.serial)
		return nil
	}
	slog.Debug("Disconnect device", "serial", d.serial)
	if err := d.adbkitServer.Close(); err != nil {
		return errors.Wrap(err, "failed to close adbkit server")
	}
	d.adbkitServer = nil
	d.adbkitPort = 0
	return nil
}

func (d *AndroidDevice) Connect(socketAddr string) (net.Conn, error) {
	device := d.getAdbkitDevice()
	return device.OpenSocket(socketAddr)
}
