package device

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// devicekitClient talks to the devicekit-ios XCUITest runner that runs inside a
// booted simulator. The runner exposes JSON-RPC 2.0 over HTTP.
type devicekitClient struct {
	baseURL      string
	httpClient   *http.Client
	streamClient *http.Client
}

func newDevicekitClient(port int) *devicekitClient {
	return &devicekitClient{
		baseURL:      fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient:   &http.Client{Timeout: 2 * time.Minute},
		streamClient: &http.Client{},
	}
}

type devicekitRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	ID      int    `json:"id"`
}

type devicekitRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type devicekitRPCResponse struct {
	Result json.RawMessage    `json:"result"`
	Error  *devicekitRPCError `json:"error"`
}

// call performs one JSON-RPC request and decodes result into out when non-nil.
func (c *devicekitClient) call(method string, params, out any) error {
	payload, err := json.Marshal(devicekitRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})
	if err != nil {
		return errors.Wrapf(err, "encode %s request", method)
	}
	resp, err := c.httpClient.Post(c.baseURL+"/rpc", "application/json", bytes.NewReader(payload))
	if err != nil {
		return errors.Wrapf(err, "call %s", method)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrapf(err, "read %s response", method)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope devicekitRPCResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return errors.Wrapf(err, "decode %s response", method)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%s failed: %s (code %d)", method, envelope.Error.Message, envelope.Error.Code)
	}
	if out == nil || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return errors.Wrapf(err, "decode %s result", method)
	}
	return nil
}

// Tap taps at (x, y) in screen points.
func (c *devicekitClient) Tap(x, y int) error {
	return c.call("device.io.tap", map[string]int{"x": x, "y": y}, nil)
}

// Swipe drags from (x1, y1) to (x2, y2) over durationMs milliseconds.
func (c *devicekitClient) Swipe(x1, y1, x2, y2, durationMs int) error {
	if durationMs <= 0 {
		durationMs = 300
	}
	return c.call("device.io.swipe", map[string]any{
		"x1":       x1,
		"y1":       y1,
		"x2":       x2,
		"y2":       y2,
		"duration": float64(durationMs) / 1000,
	}, nil)
}

// InputText types text into the focused field.
func (c *devicekitClient) InputText(text string) error {
	if text == "" {
		return nil
	}
	return c.call("device.io.text", map[string]string{"text": text}, nil)
}

// devicekitKey is one entry of a device.io.keys request.
type devicekitKey struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers"`
}

// ClearText clears the focused text field with the HID clear key.
func (c *devicekitClient) ClearText() error {
	return c.pressKeys([]devicekitKey{{Key: "clear", Modifiers: []string{}}})
}

func (c *devicekitClient) pressKeys(keys []devicekitKey) error {
	return c.call("device.io.keys", map[string]any{"keys": keys}, nil)
}

// devicekitHardwareButton maps a logical key to a device.io.button name.
func devicekitHardwareButton(keyName KeyName) (string, bool) {
	switch keyName {
	case KeyHome:
		return "home", true
	case KeyPower:
		return "lock", true
	case KeyVolumeUp:
		return "volumeUp", true
	case KeyVolumeDown:
		return "volumeDown", true
	}
	return "", false
}

// devicekitKeyName maps a logical key to a name device.io.keys understands.
func devicekitKeyName(keyName KeyName) (string, error) {
	switch keyName {
	case KeyEnter:
		return "return", nil
	case KeyTab:
		return "tab", nil
	case KeySpace:
		return "space", nil
	case KeyEscape:
		return "escape", nil
	case KeyBackspace:
		return "backspace", nil
	case KeyDelete:
		return "delete", nil
	case KeyPageUp:
		return "pageup", nil
	case KeyPageDown:
		return "pagedown", nil
	case KeyDpadUp:
		return "up", nil
	case KeyDpadDown:
		return "down", nil
	case KeyDpadLeft:
		return "left", nil
	case KeyDpadRight:
		return "right", nil
	case KeyComma:
		return ",", nil
	case KeyPeriod:
		return ".", nil
	case KeyMinus:
		return "-", nil
	case KeyEquals:
		return "=", nil
	case KeySemicolon:
		return ";", nil
	case KeyQuote:
		return "'", nil
	case KeySlash:
		return "/", nil
	case KeyBackslash:
		return "\\", nil
	case KeyLeftBracket:
		return "[", nil
	case KeyRightBracket:
		return "]", nil
	case KeyBackquote:
		return "`", nil
	case KeyPlus:
		return "+", nil
	case KeyAt:
		return "@", nil
	}
	if len(keyName) == 1 {
		c := keyName[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return string(c), nil
		}
	}
	return "", fmt.Errorf("unsupported key for iOS simulator: %s", keyName)
}

// devicekitModifiers returns the device.io.keys modifiers for a meta state.
func devicekitModifiers(metaState MetaState) []string {
	modifiers := make([]string, 0, 4)
	if metaState&(MetaCtrl|MetaCtrlLeft|MetaCtrlRight) != 0 {
		modifiers = append(modifiers, "control")
	}
	if metaState&(MetaAlt|MetaAltLeft|MetaAltRight) != 0 {
		modifiers = append(modifiers, "option")
	}
	if metaState&(MetaShift|MetaShiftLeft|MetaShiftRight) != 0 {
		modifiers = append(modifiers, "shift")
	}
	if metaState&(MetaMeta|MetaMetaLeft|MetaMetaRight) != 0 {
		modifiers = append(modifiers, "command")
	}
	return modifiers
}

// PressKey presses a hardware button or a keyboard key.
func (c *devicekitClient) PressKey(keyName KeyName, metaState MetaState) error {
	if button, ok := devicekitHardwareButton(keyName); ok {
		if metaState != MetaNone {
			return fmt.Errorf("hardware button %s does not support modifiers on iOS", keyName)
		}
		return c.call("device.io.button", map[string]string{"button": button}, nil)
	}
	key, err := devicekitKeyName(keyName)
	if err != nil {
		return err
	}
	return c.pressKeys([]devicekitKey{{Key: key, Modifiers: devicekitModifiers(metaState)}})
}

// ScreenSize returns the screen size in points.
func (c *devicekitClient) ScreenSize() (int, int, error) {
	var info struct {
		ScreenSize struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"screenSize"`
	}
	if err := c.call("device.info", struct{}{}, &info); err != nil {
		return 0, 0, errors.Wrap(err, "get screen size")
	}
	return int(info.ScreenSize.Width), int(info.ScreenSize.Height), nil
}

// Screenshot captures a PNG screenshot from the runner.
func (c *devicekitClient) Screenshot() (image.Image, error) {
	var result struct {
		Data string `json:"data"`
	}
	if err := c.call("device.screenshot", map[string]string{"format": "png"}, &result); err != nil {
		return nil, errors.Wrap(err, "take screenshot")
	}
	data := result.Data
	if strings.HasPrefix(data, "data:") {
		if comma := strings.IndexByte(data, ','); comma >= 0 {
			data = data[comma+1:]
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, errors.Wrap(err, "decode screenshot")
	}
	img, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		return nil, errors.Wrap(err, "decode screenshot image")
	}
	return img, nil
}

// DumpUI returns the accessibility hierarchy of the foreground app.
func (c *devicekitClient) DumpUI() (*Node, error) {
	var raw json.RawMessage
	if err := c.call("device.dump.ui", map[string]string{"format": "json"}, &raw); err != nil {
		return nil, errors.Wrap(err, "dump ui")
	}
	return parseDevicekitUI(raw)
}

// LaunchApp launches an app by bundle ID.
func (c *devicekitClient) LaunchApp(bundleID string) error {
	return c.call("device.apps.launch", map[string]string{"bundleId": bundleID}, nil)
}

// TerminateApp terminates an app by bundle ID.
func (c *devicekitClient) TerminateApp(bundleID string) error {
	return c.call("device.apps.terminate", map[string]string{"bundleId": bundleID}, nil)
}

// ForegroundApp returns the foreground app reported by the runner.
func (c *devicekitClient) ForegroundApp() (WDAAppInfo, error) {
	var info WDAAppInfo
	if err := c.call("device.apps.foreground", struct{}{}, &info); err != nil {
		return WDAAppInfo{}, errors.Wrap(err, "get foreground app")
	}
	return info, nil
}

// openStream opens a streaming endpoint such as /mjpeg. The returned response
// body must be closed by the caller.
func (c *devicekitClient) openStream(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, errors.Wrap(err, "build devicekit stream request")
	}
	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "open devicekit %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("devicekit %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

// StreamMJPEG forwards every JPEG frame of the runner MJPEG stream.
func (c *devicekitClient) StreamMJPEG(ctx context.Context, onFrame func([]byte) error) error {
	resp, err := c.openStream(ctx, "/mjpeg")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return extractJPEGFrames(ctx, resp.Body, onFrame)
}

// devicekitUIRect is the frame of a devicekit accessibility element.
type devicekitUIRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// devicekitUIElement mirrors SourceTreeElement in devicekit-ios.
type devicekitUIElement struct {
	Type             string               `json:"type"`
	Label            string               `json:"label"`
	Name             string               `json:"name"`
	Value            string               `json:"value"`
	PlaceholderValue string               `json:"placeholderValue"`
	RawIdentifier    string               `json:"rawIdentifier"`
	Enabled          *bool                `json:"enabled"`
	Selected         *bool                `json:"selected"`
	HasFocus         *bool                `json:"hasFocus"`
	Rect             devicekitUIRect      `json:"rect"`
	Children         []devicekitUIElement `json:"children"`
}

// parseDevicekitUI converts a device.dump.ui result into the same Node tree the
// WebDriverAgent path produces.
func parseDevicekitUI(data []byte) (*Node, error) {
	var element devicekitUIElement
	if err := json.Unmarshal(data, &element); err != nil {
		return nil, errors.Wrap(err, "parse devicekit ui tree")
	}

	idCounter := 0
	makeID := func() string {
		idCounter++
		return fmt.Sprintf("n-%d", idCounter)
	}

	var convert func(element devicekitUIElement) Node
	convert = func(element devicekitUIElement) Node {
		node := Node{
			ID:      makeID(),
			TagName: element.Type,
			Rect: Rect{
				X:      int(element.Rect.X),
				Y:      int(element.Rect.Y),
				Width:  int(element.Rect.Width),
				Height: int(element.Rect.Height),
			},
			Props:    map[string]string{},
			Children: []Node{},
		}
		if node.TagName == "" {
			node.TagName = "Unknown"
		}
		// Mirror the WDA serialization: class drives the XML tag name and type
		// keeps the original element type visible.
		node.Props["class"] = node.TagName
		if element.Type != "" {
			node.Props["type"] = element.Type
		}
		addStringProp(node.Props, "label", element.Label)
		addStringProp(node.Props, "name", element.Name)
		addStringProp(node.Props, "value", element.Value)
		addStringProp(node.Props, "placeholderValue", element.PlaceholderValue)
		addStringProp(node.Props, "rawIdentifier", element.RawIdentifier)
		if element.Enabled != nil {
			node.Props["enabled"] = boolProp(*element.Enabled)
		}
		if element.Selected != nil {
			node.Props["selected"] = boolProp(*element.Selected)
		}
		if element.HasFocus != nil {
			node.Props["hasFocus"] = boolProp(*element.HasFocus)
		}
		for _, child := range element.Children {
			node.Children = append(node.Children, convert(child))
		}
		return node
	}

	// devicekit wraps the app and the status bar in a synthetic Any element
	// with a zero frame. Publish a hierarchy root sized to its children instead
	// so the XML _size and the tree view are meaningful.
	if element.Type == "XCUIElementTypeAny" && len(element.Children) > 0 {
		root := Node{
			ID:       makeID(),
			TagName:  defaultRootTagName,
			Rect:     unionDevicekitRect(element.Children),
			Props:    map[string]string{"class": defaultRootTagName},
			Children: []Node{},
		}
		for _, child := range element.Children {
			root.Children = append(root.Children, convert(child))
		}
		return &root, nil
	}

	root := convert(element)
	return &root, nil
}

func addStringProp(props map[string]string, key, value string) {
	if value != "" {
		props[key] = value
	}
}

func boolProp(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// unionDevicekitRect returns the bounding box of the direct children.
func unionDevicekitRect(children []devicekitUIElement) Rect {
	if len(children) == 0 {
		return Rect{}
	}
	left, top := children[0].Rect.X, children[0].Rect.Y
	right := left + children[0].Rect.Width
	bottom := top + children[0].Rect.Height
	for _, child := range children[1:] {
		left = minFloat(left, child.Rect.X)
		top = minFloat(top, child.Rect.Y)
		right = maxFloat(right, child.Rect.X+child.Rect.Width)
		bottom = maxFloat(bottom, child.Rect.Y+child.Rect.Height)
	}
	return Rect{
		X:      int(left),
		Y:      int(top),
		Width:  int(right - left),
		Height: int(bottom - top),
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
