package device

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"log/slog"
	"math"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "image/png"

	"github.com/uiautodev/uiautodev/webserver/utils"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/uiautodev/go-hdckit/hdc"
)

var hdcClientOnce sync.Once
var hdcClient *hdc.Client

func getHdcClient() *hdc.Client {
	hdcClientOnce.Do(func() {
		hdcClient = hdc.NewClient(hdc.Options{
			Bin: utils.FindBinary("hdc"),
		})
	})
	return hdcClient
}

type HarmonyOSDeviceManager struct{}

func (m *HarmonyOSDeviceManager) ListSerials() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return getHdcClient().ListTargets(ctx)
}

type HarmonyOSDevice struct {
	connectKey string
	uiDriver   *hdc.UiDriver
	uiStarted  bool
	displayW   int
	displayH   int
	frameW     float64
	frameH     float64
	mu         sync.Mutex
}

func NewHarmonyOSDevice(connectKey string) *HarmonyOSDevice {
	return &HarmonyOSDevice{connectKey: connectKey}
}

func (d *HarmonyOSDevice) target() (*hdc.Target, error) {
	return getHdcClient().Target(d.connectKey)
}

func (d *HarmonyOSDevice) ensureUiDriver() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.uiStarted {
		return nil
	}
	target, err := d.target()
	if err != nil {
		return errors.Wrap(err, "failed to get target")
	}
	drv := target.CreateUiDriver()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := drv.Start(ctx); err != nil {
		drv.Stop(ctx)
		return errors.Wrap(err, "failed to start UiDriver")
	}
	d.uiDriver = drv
	d.uiStarted = true
	return nil
}

// displaySize returns the screen size used by the UiDriver for touch
// coordinates, fetching and caching it. The cache is keyed by the frame
// dimensions because a screen rotation changes both, so a stale display size
// is never applied to the newly rotated frames.
func (d *HarmonyOSDevice) displaySize(ctx context.Context, frameW, frameH float64) (int, int, error) {
	d.mu.Lock()
	if d.displayW > 0 && d.displayH > 0 && d.frameW == frameW && d.frameH == frameH {
		w, h := d.displayW, d.displayH
		d.mu.Unlock()
		return w, h, nil
	}
	driver := d.uiDriver
	d.mu.Unlock()
	if driver == nil {
		return 0, 0, fmt.Errorf("ui driver not started")
	}
	size, err := driver.DisplaySize(ctx)
	if err != nil {
		return 0, 0, errors.Wrap(err, "get display size")
	}
	d.mu.Lock()
	d.displayW, d.displayH = size.Width, size.Height
	d.frameW, d.frameH = frameW, frameH
	d.mu.Unlock()
	return size.Width, size.Height, nil
}

// touchPoint converts streamed frame coordinates (which may be downscaled
// relative to the display, e.g. the capture is half the display size) into the
// display coordinate space expected by the UiDriver touch APIs. When frameW or
// frameH is not provided the coordinates are returned unchanged.
func (d *HarmonyOSDevice) touchPoint(ctx context.Context, x, y, frameW, frameH float64) (int, int, error) {
	if frameW <= 0 || frameH <= 0 {
		return int(x), int(y), nil
	}
	dispW, dispH, err := d.displaySize(ctx, frameW, frameH)
	if err != nil {
		return 0, 0, err
	}
	px, py := scaleFrameToDisplay(x, y, frameW, frameH, dispW, dispH)
	return px, py, nil
}

// scaleFrameToDisplay maps a point in frame space to display space. Missing
// frame dimensions (frameW/frameH <= 0) leave the point unchanged.
func scaleFrameToDisplay(x, y, frameW, frameH float64, dispW, dispH int) (int, int) {
	if frameW <= 0 || frameH <= 0 {
		return int(x), int(y)
	}
	return int(x * float64(dispW) / frameW), int(y * float64(dispH) / frameH)
}

// harmonyControlMessage is the JSON payload sent by the frontend over the
// screen websocket to control the device.
type harmonyControlMessage struct {
	Type         string  `json:"type"`
	Action       string  `json:"action,omitempty"`
	X            float64 `json:"x,omitempty"`
	Y            float64 `json:"y,omitempty"`
	ScreenWidth  float64 `json:"screenWidth,omitempty"`
	ScreenHeight float64 `json:"screenHeight,omitempty"`
	Text         string  `json:"text,omitempty"`
	Key          string  `json:"key,omitempty"`
	Meta         int     `json:"metaState,omitempty"`
}

// HandleControlMessage applies a control command received over the websocket.
// Touch events and text input go through the uitest UiDriver; key events fall
// back to `uitest uiInput keyEvent` because the driver has no key API.
func (d *HarmonyOSDevice) HandleControlMessage(ctx context.Context, data []byte) error {
	var msg harmonyControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return errors.Wrap(err, "parse control message")
	}

	switch msg.Type {
	case "touch":
		if err := d.ensureUiDriver(); err != nil {
			return err
		}
		x, y, err := d.touchPoint(ctx, msg.X, msg.Y, msg.ScreenWidth, msg.ScreenHeight)
		if err != nil {
			return err
		}
		switch msg.Action {
		case "down":
			return d.uiDriver.TouchDown(ctx, x, y)
		case "move":
			return d.uiDriver.TouchMove(ctx, x, y)
		case "up":
			return d.uiDriver.TouchUp(ctx, x, y)
		default:
			return fmt.Errorf("unknown touch action: %q", msg.Action)
		}
	case "text":
		if msg.Text == "" {
			return nil
		}
		// The `uitest uiInput text` CLI is noticeably faster than the UiDriver
		// hypium RPC for text input, so use it instead.
		return d.InputText(msg.Text)
	case "key":
		return d.PressKey(KeyName(msg.Key), MetaState(msg.Meta))
	default:
		return fmt.Errorf("unknown control message type: %q", msg.Type)
	}
}

func (d *HarmonyOSDevice) Info() DeviceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	props := map[string]interface{}{
		"serial":  d.connectKey,
		"product": "HUAWEI",
	}
	if target, err := d.target(); err == nil {
		if params, err := target.Parameters(ctx); err == nil {
			for k, v := range params {
				props[k] = v
			}
		}
	}
	if model, err := d.RunShell("param get const.product.model", 5000); err == nil {
		props["model"] = strings.TrimSpace(model)
	}
	return DeviceInfo{
		Platform: "harmony",
		ID:       "h-" + d.connectKey,
		Props:    props,
		Disabled: false,
	}
}

func (d *HarmonyOSDevice) GetScreenSize() (int, int, error) {
	// DisplayManagerService reports the rotation-aware logical size, unlike the
	// RenderService panel resolution which stays fixed when the screen rotates.
	if out, err := d.RunShell("hidumper -s DisplayManagerService -a -a", 10000); err == nil {
		if w, h, err := parseHarmonyDisplaySize(out); err == nil {
			return w, h, nil
		}
	}
	out, err := d.RunShell("hidumper -s RenderService -a screen", 10000)
	if err != nil {
		return 0, 0, err
	}
	slog.Debug("hidumper output", "output", out)
	w, h, err := parseHarmonyScreenSize(out)
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func (d *HarmonyOSDevice) Tap(x, y int) error {
	cmd := fmt.Sprintf("uitest uiInput click %d %d", x, y)
	slog.Debug("Running Tap command", "cmd", cmd)
	out, err := d.RunShell(cmd, 5000)
	if err != nil {
		return errors.Wrap(err, "Tap failed: "+out)
	}
	return nil
}

func (d *HarmonyOSDevice) Swipe(x1, y1, x2, y2, durationMs int) error {
	dx := x2 - x1
	dy := y2 - y1
	distance := math.Sqrt(float64(dx*dx + dy*dy))
	velocity := 600 // default velocity (px/s)
	if durationMs > 0 && distance > 0 {
		velocity = int(distance / (float64(durationMs) / 1000.0))
	}
	if velocity < 200 {
		velocity = 200
	}
	if velocity > 40000 {
		velocity = 40000
	}
	out, err := d.RunShell(fmt.Sprintf("uitest uiInput swipe %d %d %d %d %d", x1, y1, x2, y2, velocity), 5000)
	if err != nil {
		return errors.Wrap(err, "Swipe failed: "+out)
	}
	return nil
}

func (d *HarmonyOSDevice) InputText(text string) error {
	if text == "" {
		return nil
	}
	// uitest uiInput text inputs into the currently focused field.
	cmd := fmt.Sprintf("uitest uiInput text %s", utils.ShellJoin([]string{text}))
	slog.Debug("Running InputText command", "cmd", cmd)
	out, err := d.RunShell(cmd, 5000)
	if err != nil {
		return errors.Wrap(err, "InputText failed: "+out)
	}
	return nil
}

// ClearText selects all text in the currently focused field (Ctrl+A) and
// deletes it.
func (d *HarmonyOSDevice) ClearText() error {
	if err := d.PressKey(KeyName("a"), MetaCtrl); err != nil {
		return errors.Wrap(err, "ClearText select all failed")
	}
	if err := d.PressKey(KeyDelete, MetaNone); err != nil {
		return errors.Wrap(err, "ClearText delete failed")
	}
	return nil
}

// PressKey simulates a key event on the device via uitest.
// A non-zero metaState emits a combination key, e.g. Ctrl+V presses ctrl_left + v.
func (d *HarmonyOSDevice) PressKey(keyName KeyName, metaState MetaState) error {
	codes, err := harmonyKeyCodes(keyName, metaState)
	if err != nil {
		return err
	}
	keyStrs := make([]string, 0, len(codes))
	for _, c := range codes {
		keyStrs = append(keyStrs, c.String())
	}
	out, err := d.RunShell(fmt.Sprintf("uitest uiInput keyEvent %s", strings.Join(keyStrs, " ")), 5000)
	if err != nil {
		return errors.Wrap(err, "PressKey failed: "+out)
	}
	return nil
}

// harmonyKeyCodes returns the HarmonyOS keycode sequence for a key press,
// with modifier keycodes from metaState emitted before the main key.
func harmonyKeyCodes(keyName KeyName, metaState MetaState) ([]harmonyKeyCode, error) {
	var codes []harmonyKeyCode
	// Order: ctrl, alt, shift, meta
	if metaState&(MetaCtrl|MetaCtrlLeft) != 0 {
		codes = append(codes, harmonyKeyCodeCtrlLeft)
	}
	if metaState&MetaCtrlRight != 0 {
		codes = append(codes, harmonyKeyCodeCtrlRight)
	}
	if metaState&(MetaAlt|MetaAltLeft) != 0 {
		codes = append(codes, harmonyKeyCodeAltLeft)
	}
	if metaState&MetaAltRight != 0 {
		codes = append(codes, harmonyKeyCodeAltRight)
	}
	if metaState&(MetaShift|MetaShiftLeft) != 0 {
		codes = append(codes, harmonyKeyCodeShiftLeft)
	}
	if metaState&MetaShiftRight != 0 {
		codes = append(codes, harmonyKeyCodeShiftRight)
	}
	if metaState&(MetaMeta|MetaMetaLeft) != 0 {
		codes = append(codes, harmonyKeyCodeMetaLeft)
	}
	if metaState&MetaMetaRight != 0 {
		codes = append(codes, harmonyKeyCodeMetaRight)
	}

	main, err := harmonyKeyName(keyName)
	if err != nil {
		return nil, err
	}
	codes = append(codes, main)
	return codes, nil
}

// harmonyKeyCode is a keycode value defined in @ohos.multimodalInput.keyCode.
type harmonyKeyCode int

const (
	harmonyKeyCodeHome       harmonyKeyCode = 1    // KEYCODE_HOME
	harmonyKeyCodeBack       harmonyKeyCode = 2    // KEYCODE_BACK
	harmonyKeyCodeVolumeUp   harmonyKeyCode = 16   // KEYCODE_VOLUME_UP
	harmonyKeyCodeVolumeDown harmonyKeyCode = 17   // KEYCODE_VOLUME_DOWN
	harmonyKeyCodePower      harmonyKeyCode = 18   // KEYCODE_POWER
	harmonyKeyCodeAppSelect  harmonyKeyCode = 2720 // KEYCODE_APPSELECT (opens recent tasks)

	harmonyKeyCodeAltLeft    harmonyKeyCode = 2045 // KEYCODE_ALT_LEFT
	harmonyKeyCodeAltRight   harmonyKeyCode = 2046 // KEYCODE_ALT_RIGHT
	harmonyKeyCodeShiftLeft  harmonyKeyCode = 2047 // KEYCODE_SHIFT_LEFT
	harmonyKeyCodeShiftRight harmonyKeyCode = 2048 // KEYCODE_SHIFT_RIGHT
	harmonyKeyCodeCtrlLeft   harmonyKeyCode = 2072 // KEYCODE_CTRL_LEFT
	harmonyKeyCodeCtrlRight  harmonyKeyCode = 2073 // KEYCODE_CTRL_RIGHT
	harmonyKeyCodeMetaLeft   harmonyKeyCode = 2076 // KEYCODE_META_LEFT
	harmonyKeyCodeMetaRight  harmonyKeyCode = 2077 // KEYCODE_META_RIGHT

	harmonyKeyCodeZero       harmonyKeyCode = 2000 // KEYCODE_0
	harmonyKeyCodeA          harmonyKeyCode = 2017 // KEYCODE_A
	harmonyKeyCodeDpadUp     harmonyKeyCode = 2012 // KEYCODE_DPAD_UP
	harmonyKeyCodeDpadDown   harmonyKeyCode = 2013 // KEYCODE_DPAD_DOWN
	harmonyKeyCodeDpadLeft   harmonyKeyCode = 2014 // KEYCODE_DPAD_LEFT
	harmonyKeyCodeDpadRight  harmonyKeyCode = 2015 // KEYCODE_DPAD_RIGHT
	harmonyKeyCodeDpadCenter harmonyKeyCode = 2016 // KEYCODE_DPAD_CENTER
	harmonyKeyCodeComma      harmonyKeyCode = 2043 // KEYCODE_COMMA
	harmonyKeyCodePeriod     harmonyKeyCode = 2044 // KEYCODE_PERIOD
	harmonyKeyCodeTab        harmonyKeyCode = 2049 // KEYCODE_TAB
	harmonyKeyCodeSpace      harmonyKeyCode = 2050 // KEYCODE_SPACE
	harmonyKeyCodeEnter      harmonyKeyCode = 2054 // KEYCODE_ENTER
	harmonyKeyCodeDel        harmonyKeyCode = 2055 // KEYCODE_DEL (backspace)
	harmonyKeyCodeGrave      harmonyKeyCode = 2056 // KEYCODE_GRAVE
	harmonyKeyCodeMinus      harmonyKeyCode = 2057 // KEYCODE_MINUS
	harmonyKeyCodeEquals     harmonyKeyCode = 2058 // KEYCODE_EQUALS
	harmonyKeyCodeLBracket   harmonyKeyCode = 2059 // KEYCODE_LEFT_BRACKET
	harmonyKeyCodeRBracket   harmonyKeyCode = 2060 // KEYCODE_RIGHT_BRACKET
	harmonyKeyCodeBackslash  harmonyKeyCode = 2061 // KEYCODE_BACKSLASH
	harmonyKeyCodeSemicolon  harmonyKeyCode = 2062 // KEYCODE_SEMICOLON
	harmonyKeyCodeApostrophe harmonyKeyCode = 2063 // KEYCODE_APOSTROPHE
	harmonyKeyCodeSlash      harmonyKeyCode = 2064 // KEYCODE_SLASH
	harmonyKeyCodeAt         harmonyKeyCode = 2065 // KEYCODE_AT
	harmonyKeyCodePlus       harmonyKeyCode = 2066 // KEYCODE_PLUS
	harmonyKeyCodePageUp     harmonyKeyCode = 2068 // KEYCODE_PAGE_UP
	harmonyKeyCodePageDown   harmonyKeyCode = 2069 // KEYCODE_PAGE_DOWN
	harmonyKeyCodeEscape     harmonyKeyCode = 2070 // KEYCODE_ESCAPE
	harmonyKeyCodeForwardDel harmonyKeyCode = 2071 // KEYCODE_FORWARD_DEL (delete)
	harmonyKeyCodeCapsLock   harmonyKeyCode = 2074 // KEYCODE_CAPS_LOCK
	harmonyKeyCodeInsert     harmonyKeyCode = 2083 // KEYCODE_INSERT
)

func (k harmonyKeyCode) String() string {
	return strconv.Itoa(int(k))
}

// harmonyKeyName maps a logical key to the HarmonyOS keycode value
// defined in @ohos.multimodalInput.keyCode.
func harmonyKeyName(keyName KeyName) (harmonyKeyCode, error) {
	switch keyName {
	case KeyHome:
		return harmonyKeyCodeHome, nil
	case KeyBack:
		return harmonyKeyCodeBack, nil
	case KeyVolumeUp:
		return harmonyKeyCodeVolumeUp, nil
	case KeyVolumeDown:
		return harmonyKeyCodeVolumeDown, nil
	case KeyPower:
		return harmonyKeyCodePower, nil
	case KeyRecents:
		return harmonyKeyCodeAppSelect, nil
	case KeyTab:
		return harmonyKeyCodeTab, nil
	case KeySpace:
		return harmonyKeyCodeSpace, nil
	case KeyEnter:
		return harmonyKeyCodeEnter, nil
	case KeyEscape:
		return harmonyKeyCodeEscape, nil
	case KeyBackspace:
		return harmonyKeyCodeDel, nil
	case KeyDelete:
		return harmonyKeyCodeForwardDel, nil
	case KeyCapsLock:
		return harmonyKeyCodeCapsLock, nil
	case KeyPageUp:
		return harmonyKeyCodePageUp, nil
	case KeyPageDown:
		return harmonyKeyCodePageDown, nil
	case KeyInsert:
		return harmonyKeyCodeInsert, nil
	case KeyDpadUp:
		return harmonyKeyCodeDpadUp, nil
	case KeyDpadDown:
		return harmonyKeyCodeDpadDown, nil
	case KeyDpadLeft:
		return harmonyKeyCodeDpadLeft, nil
	case KeyDpadRight:
		return harmonyKeyCodeDpadRight, nil
	case KeyDpadCenter:
		return harmonyKeyCodeDpadCenter, nil
	case KeyComma:
		return harmonyKeyCodeComma, nil
	case KeyPeriod:
		return harmonyKeyCodePeriod, nil
	case KeyMinus:
		return harmonyKeyCodeMinus, nil
	case KeyEquals:
		return harmonyKeyCodeEquals, nil
	case KeySemicolon:
		return harmonyKeyCodeSemicolon, nil
	case KeyQuote:
		return harmonyKeyCodeApostrophe, nil
	case KeySlash:
		return harmonyKeyCodeSlash, nil
	case KeyBackslash:
		return harmonyKeyCodeBackslash, nil
	case KeyLeftBracket:
		return harmonyKeyCodeLBracket, nil
	case KeyRightBracket:
		return harmonyKeyCodeRBracket, nil
	case KeyBackquote:
		return harmonyKeyCodeGrave, nil
	case KeyPlus:
		return harmonyKeyCodePlus, nil
	case KeyAt:
		return harmonyKeyCodeAt, nil
	}
	// Single character keys: a-z and 0-9 (consecutive in @ohos.multimodalInput.keyCode)
	if len(keyName) == 1 {
		c := keyName[0]
		if c >= 'a' && c <= 'z' {
			return harmonyKeyCodeA + harmonyKeyCode(c-'a'), nil
		}
		if c >= '0' && c <= '9' {
			return harmonyKeyCodeZero + harmonyKeyCode(c-'0'), nil
		}
	}
	return 0, fmt.Errorf("unsupported key name: %s", keyName)
}

func (d *HarmonyOSDevice) Screenshot() (image.Image, error) {
	remotePath := fmt.Sprintf("/data/local/tmp/screenshot_%d.jpeg", time.Now().UnixNano())
	defer d.RunShell("rm -f "+remotePath, 3000)

	out, err := d.RunShell("snapshot_display -f "+remotePath, 10000)
	if err != nil {
		return nil, errors.Wrap(err, "snapshot_display failed: "+out)
	}

	localFile, err := os.CreateTemp("", "harmony_screenshot_*.jpeg")
	if err != nil {
		return nil, errors.Wrap(err, "create temp file")
	}
	localPath := localFile.Name()
	localFile.Close()
	defer os.Remove(localPath)

	target, err := d.target()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get target")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := target.RecvFile(ctx, remotePath, localPath); err != nil {
		return nil, errors.Wrap(err, "RecvFile screenshot")
	}

	f, err := os.Open(localPath)
	if err != nil {
		return nil, errors.Wrap(err, "open screenshot file")
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, errors.Wrap(err, "decode screenshot")
	}
	return img, nil
}

func (d *HarmonyOSDevice) GetScreens() ([]Screen, error) {
	return []Screen{{ID: "0"}}, nil
}

func (d *HarmonyOSDevice) GetUITree() (*Node, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := d.RunShell("uitest dumpLayout -p /data/local/tmp/layout.json", 10000)
	if err != nil {
		return nil, errors.Wrap(err, "dumpLayout failed: "+out)
	}

	tmpFile, err := os.CreateTemp("", "harmony_layout_*.json")
	if err != nil {
		return nil, errors.Wrap(err, "create temp file")
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	target, err := d.target()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get target")
	}
	if err := target.RecvFile(ctx, "/data/local/tmp/layout.json", tmpPath); err != nil {
		return nil, errors.Wrap(err, "RecvFile layout.json")
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, errors.Wrap(err, "read layout file")
	}

	return parseHarmonyLayoutToNode(data)
}

func (d *HarmonyOSDevice) RunShell(cmdStr string, timeoutMs int64) (string, error) {
	slog.Debug("Running hdc shell command", "key", d.connectKey, "cmd", cmdStr, "timeoutMs", timeoutMs)

	var ctx context.Context
	var cancel context.CancelFunc
	if timeoutMs > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	target, err := d.target()
	if err != nil {
		return "", errors.Wrap(err, "failed to get target")
	}
	output, err := target.RunShell(ctx, cmdStr)
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func (d *HarmonyOSDevice) AcquireDeviceConnect() (*DeviceConnectInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (d *HarmonyOSDevice) ReleaseDeviceConnect() error {
	return nil
}

func (d *HarmonyOSDevice) Connect(socketAddr string) (net.Conn, error) {
	return nil, fmt.Errorf("not implemented")
}

// StartMJPEGStream starts screen capture and writes frames as MJPEG to w.
func (d *HarmonyOSDevice) startHealthCheck(ctx context.Context, streamCancel context.CancelFunc) {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 2*time.Second)
				targets, err := getHdcClient().ListTargets(checkCtx)
				checkCancel()
				if err != nil {
					slog.Warn("device health check failed, stopping screen stream", "key", d.connectKey, "err", err)
					streamCancel()
					return
				}
				found := false
				for _, t := range targets {
					if t == d.connectKey {
						found = true
						break
					}
				}
				if !found {
					slog.Warn("device not found in target list, stopping screen stream", "key", d.connectKey)
					streamCancel()
					return
				}
			}
		}
	}()
}

func (d *HarmonyOSDevice) startCapture(ctx context.Context, frameCh chan<- []byte, onStop func()) {
	driver := d.uiDriver
	go func() {
		defer close(frameCh)
		defer slog.Debug("stream capture goroutine exiting", "device", d.connectKey)

		stream, err := driver.CaptureScreen(ctx, 0)
		if err != nil {
			slog.Error("CaptureScreen failed", "err", err)
			onStop()
			return
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer stopCancel()
			stream.Close(stopCtx)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-stream.Frames():
				if !ok {
					slog.Warn("stream capture channel closed, stopping", "device", d.connectKey)
					onStop()
					return
				}
				select {
				case frameCh <- data:
				default:
				}
			}
		}
	}()
}

// StartMJPEGStream starts screen capture and writes frames as MJPEG to w.

// StartWebsocketStream starts screen capture and writes raw frames as
// WebSocket binary messages. Each message is one complete JPEG frame.
func (d *HarmonyOSDevice) StartWebsocketStream(ctx context.Context, conn *websocket.Conn) error {
	if err := d.ensureUiDriver(); err != nil {
		return err
	}

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	frameCh := make(chan []byte, 5)

	d.startHealthCheck(streamCtx, streamCancel)
	d.startCapture(streamCtx, frameCh, streamCancel)

	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-frameCh:
			if !ok {
				return fmt.Errorf("screen capture ended unexpectedly")
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return err
			}
		}
	}
}

func parseHarmonyScreenSize(output string) (int, int, error) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)render\s+(?:size|resolution)\s*[=:]\s*(\d+)\s*x\s*(\d+)`),
		regexp.MustCompile(`(?i)physical\s+(?:screen\s+)?resolution\s*[=:]\s*(\d+)\s*x\s*(\d+)`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(output)
		if len(matches) != 3 {
			continue
		}
		w, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, 0, err
		}
		h, err := strconv.Atoi(matches[2])
		if err != nil {
			return 0, 0, err
		}
		if w > 0 && h > 0 {
			return w, h, nil
		}
	}
	return 0, 0, fmt.Errorf("unexpected hidumper screen output: %s", strings.TrimSpace(output))
}

var harmonyDisplaySizeRe = regexp.MustCompile(`(?m)^Width:\s*(\d+)\s*\r?\nHeight:\s*(\d+)`)

// parseHarmonyDisplaySize extracts the current display size from the
// DisplayManagerService dump. Unlike the RenderService render resolution, this
// value swaps on screen rotation.
func parseHarmonyDisplaySize(output string) (int, int, error) {
	matches := harmonyDisplaySizeRe.FindStringSubmatch(output)
	if len(matches) != 3 {
		return 0, 0, fmt.Errorf("unexpected hidumper display output")
	}
	w, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, err
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("invalid display size %dx%d", w, h)
	}
	return w, h, nil
}

var boundsRe = regexp.MustCompile(`\[(\d+),\s*(\d+)\]\[(\d+),\s*(\d+)\]`)

func parseBoundsString(s string) (Rect, error) {
	matches := boundsRe.FindStringSubmatch(s)
	if len(matches) != 5 {
		return Rect{}, fmt.Errorf("invalid bounds format: %q", s)
	}
	x1, _ := strconv.Atoi(matches[1])
	y1, _ := strconv.Atoi(matches[2])
	x2, _ := strconv.Atoi(matches[3])
	y2, _ := strconv.Atoi(matches[4])
	return Rect{
		X:      x1,
		Y:      y1,
		Width:  x2 - x1,
		Height: y2 - y1,
	}, nil
}

func parseHarmonyLayoutToNode(layout any) (*Node, error) {
	if layout == nil {
		return nil, fmt.Errorf("empty layout")
	}

	var data map[string]any
	switch v := layout.(type) {
	case map[string]any:
		data = v
	case string:
		if err := json.Unmarshal([]byte(v), &data); err != nil {
			return nil, errors.Wrap(err, "parse layout json")
		}
	case []byte:
		if err := json.Unmarshal(v, &data); err != nil {
			return nil, errors.Wrap(err, "parse layout json")
		}
	default:
		return nil, fmt.Errorf("unexpected layout type: %T", layout)
	}

	// New format from uitest dumpLayout: attributes wraps node data
	// The top-level node is the hierarchy root, tag it accordingly so that
	// NodeToXML can serialize it back to uiautomator-style XML.
	root := parseHarmonyLayoutNode(data)
	root.TagName = defaultRootTagName
	return root, nil
}

var harmonyNodeID int

func parseHarmonyLayoutNode(data map[string]any) *Node {
	harmonyNodeID++

	// New format: attributes wraps all fields
	attrs, hasAttrs := data["attributes"].(map[string]any)
	if !hasAttrs {
		attrs = data
	}

	node := &Node{
		ID:       fmt.Sprintf("n-%d", harmonyNodeID),
		TagName:  getString(attrs, "type"),
		Children: []Node{},
		Props:    map[string]string{},
	}

	if s := getString(attrs, "bounds"); s != "" {
		if rect, err := parseBoundsString(s); err == nil {
			node.Rect = rect
		}
	}

	for k, v := range attrs {
		if k == "type" || k == "bounds" || k == "children" {
			continue
		}
		if s, ok := v.(string); ok {
			node.Props[k] = s
		} else {
			node.Props[k] = fmt.Sprintf("%v", v)
		}
	}

	if node.TagName == "" {
		node.TagName = "Unknown"
	}
	node.Props["class"] = node.TagName

	if children, ok := data["children"].([]any); ok {
		for _, child := range children {
			if childMap, ok := child.(map[string]any); ok {
				childNode := parseHarmonyLayoutNode(childMap)
				if childNode != nil {
					node.Children = append(node.Children, *childNode)
				}
			}
		}
	}

	return node
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ensure that lines with key=value are parsed correctly
var _ = strings.Index
