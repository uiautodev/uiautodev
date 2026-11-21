package device

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pkg/errors"
)

// WDARect represents the rect structure from WebDriverAgent response
type WDARect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// WDAElement represents an element from WebDriverAgent source response
// Using map for unknown fields to handle arbitrary properties
type WDAElement struct {
	Rect     *WDARect               `json:"rect,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Children []WDAElement           `json:"children,omitempty"`
	Rest     map[string]interface{} `json:"-"` // All other fields
}

// UnmarshalJSON implements custom JSON unmarshaling for WDAElement
// This allows us to capture all fields while handling known ones explicitly
func (e *WDAElement) UnmarshalJSON(data []byte) error {
	// First, create a map to hold all fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	e.Rest = make(map[string]interface{})

	// Extract known fields
	for key, value := range raw {
		switch key {
		case "rect":
			if v, ok := value.(map[string]interface{}); ok {
				rectData, _ := json.Marshal(v)
				var rect WDARect
				if err := json.Unmarshal(rectData, &rect); err == nil {
					e.Rect = &rect
				}
			}
		case "type":
			if v, ok := value.(string); ok {
				e.Type = v
			}
		case "children":
			if v, ok := value.([]interface{}); ok {
				childrenData, _ := json.Marshal(v)
				_ = json.Unmarshal(childrenData, &e.Children)
			}
		default:
			// Store all other fields in Rest
			if valueStr, ok := value.(string); ok && valueStr == "<nil>" {
				e.Rest[key] = ""
			} else {
				e.Rest[key] = value
			}
		}
	}

	return nil
}

// WDAResponse represents the root response from WebDriverAgent
type WDAResponse struct {
	Value WDAElement `json:"value"`
}

// parseWDASourceToNode parses WebDriverAgent JSON source into Node structure
func parseWDASourceToNode(data []byte) (*Node, error) {
	var wdaResp WDAResponse
	if err := json.Unmarshal(data, &wdaResp); err != nil {
		return nil, errors.Wrap(err, "failed to parse WDA response")
	}

	idCounter := 0
	makeID := func() string {
		idCounter++
		return fmt.Sprintf("n-%d", idCounter)
	}

	var convert func(element WDAElement) Node
	convert = func(element WDAElement) Node {
		node := Node{
			ID:       makeID(),
			TagName:  element.Type,
			Props:    map[string]string{},
			Children: []Node{},
		}

		// Handle rect
		if element.Rect != nil {
			node.Rect = Rect{
				X:      int(element.Rect.X),
				Y:      int(element.Rect.Y),
				Width:  int(element.Rect.Width),
				Height: int(element.Rect.Height),
			}
		}

		// Set tagName from type, default to "Unknown" if empty
		if element.Type != "" {
			node.TagName = element.Type
		} else {
			node.TagName = "Unknown"
		}

		// Mirror the WDA type into props so the serialized XML uses it as the tag
		// name ("class") and keeps it visible as a "type" attribute, matching the
		// original WDA JSON source.
		node.Props["class"] = node.TagName
		if element.Type != "" {
			node.Props["type"] = node.TagName
		}

		// Convert all other properties from Rest map to props
		for key, value := range element.Rest {
			switch v := value.(type) {
			case string:
				node.Props[key] = v
			case nil:
				node.Props[key] = ""
			default:
				node.Props[key] = fmt.Sprintf("%v", value)
			}
		}

		// Process children
		for _, child := range element.Children {
			node.Children = append(node.Children, convert(child))
		}

		return node
	}

	// Convert root element
	rootNode := convert(wdaResp.Value)

	return &rootNode, nil
}

// WDAAction represents a single pointer action in WDA
type WDAAction struct {
	Type     string `json:"type"`
	Duration int    `json:"duration,omitempty"`
	X        *int   `json:"x,omitempty"`
	Y        *int   `json:"y,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Button   int    `json:"button,omitempty"`
}

func intPtr(v int) *int {
	return &v
}

// WDAPointerAction represents a pointer action with parameters
type WDAPointerAction struct {
	Type       string      `json:"type"`
	ID         string      `json:"id"`
	Parameters WDAParams   `json:"parameters"`
	Actions    []WDAAction `json:"actions"`
}

// WDAParams represents pointer parameters
type WDAParams struct {
	PointerType string `json:"pointerType"`
}

// WDAActionsRequest represents the full actions request payload
type WDAActionsRequest struct {
	Actions []WDAPointerAction `json:"actions"`
}

// buildTapPayload builds WDA actions payload for a tap at (x, y)
func buildTapPayload(x, y int) ([]byte, error) {
	payload := WDAActionsRequest{
		Actions: []WDAPointerAction{
			{
				Type: "pointer",
				ID:   "finger1",
				Parameters: WDAParams{
					PointerType: "touch",
				},
				Actions: []WDAAction{
					{
						Type:     "pointerMove",
						Duration: 100,
						X:        intPtr(x),
						Y:        intPtr(y),
						Origin:   "viewport",
					},
					{
						Type:   "pointerDown",
						Button: 0,
					},
					{
						Type:   "pointerUp",
						Button: 0,
					},
				},
			},
		},
	}

	return json.Marshal(payload)
}

// buildSwipePayload builds a W3C actions payload that drags from (x1, y1) to
// (x2, y2) over durationMs milliseconds.
func buildSwipePayload(x1, y1, x2, y2, durationMs int) ([]byte, error) {
	if durationMs <= 0 {
		durationMs = 300
	}
	payload := WDAActionsRequest{
		Actions: []WDAPointerAction{
			{
				Type: "pointer",
				ID:   "finger1",
				Parameters: WDAParams{
					PointerType: "touch",
				},
				Actions: []WDAAction{
					{
						Type:   "pointerMove",
						X:      intPtr(x1),
						Y:      intPtr(y1),
						Origin: "viewport",
					},
					{
						Type:   "pointerDown",
						Button: 0,
					},
					{
						Type:     "pointerMove",
						Duration: durationMs,
						X:        intPtr(x2),
						Y:        intPtr(y2),
						Origin:   "viewport",
					},
					{
						Type:   "pointerUp",
						Button: 0,
					},
				},
			},
		},
	}
	return json.Marshal(payload)
}

// WDAKeysRequest is the payload for the keyboard-level typing endpoint
// POST /session/:id/wda/keys. It types into the currently focused element.
type WDAKeysRequest struct {
	Value     []string `json:"value"`
	Frequency int      `json:"frequency,omitempty"`
}

// buildKeysPayload builds the payload that types text via /wda/keys.
func buildKeysPayload(text string) ([]byte, error) {
	return json.Marshal(WDAKeysRequest{Value: []string{text}})
}

// WDAKeyAction is a single action item in a W3C "key" input source.
type WDAKeyAction struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}

// WDAKeySource is a W3C input source of type "key".
type WDAKeySource struct {
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	Actions []WDAKeyAction `json:"actions"`
}

// WDAKeyActionsRequest is the payload for POST /session/:id/actions with a key
// input source.
type WDAKeyActionsRequest struct {
	Actions []WDAKeySource `json:"actions"`
}

// WebDriver key values for modifier keys. WDA maps these to the matching
// XCUIKeyboardKey constants when handling W3C key actions (iOS 15+).
const (
	wdaKeyShift   = "\uE008"
	wdaKeyControl = "\uE009"
	wdaKeyAlt     = "\uE00A"
	wdaKeyMeta    = "\uE03D" // Command on iOS/macOS
)

// wdaHardwareButton maps a logical key to a WDA pressButton name.
func wdaHardwareButton(keyName KeyName) (string, bool) {
	switch keyName {
	case KeyHome:
		return "home", true
	case KeyVolumeUp:
		return "volumeup", true
	case KeyVolumeDown:
		return "volumedown", true
	}
	return "", false
}

// wdaKeyValue maps a logical key to a WebDriver key value understood by WDA's
// W3C key actions.
func wdaKeyValue(keyName KeyName) (string, error) {
	switch keyName {
	case KeyEnter:
		return "\uE007", nil
	case KeyTab:
		return "\uE004", nil
	case KeySpace:
		return " ", nil
	case KeyEscape:
		return "\uE00C", nil
	case KeyBackspace:
		return "\uE003", nil
	case KeyDelete:
		return "\uE017", nil
	case KeyPageUp:
		return "\uE00E", nil
	case KeyPageDown:
		return "\uE00F", nil
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
	case KeyDpadLeft:
		return "\uE012", nil
	case KeyDpadUp:
		return "\uE013", nil
	case KeyDpadRight:
		return "\uE014", nil
	case KeyDpadDown:
		return "\uE015", nil
	case KeyDpadCenter:
		return "\uE007", nil
	}
	if len(keyName) == 1 {
		c := keyName[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return string(c), nil
		}
	}
	return "", fmt.Errorf("unsupported key for iOS: %s", keyName)
}

// wdaModifierValues returns the modifier key values that must be held while the
// main key is pressed.
func wdaModifierValues(metaState MetaState) []string {
	var mods []string
	if metaState&(MetaCtrl|MetaCtrlLeft|MetaCtrlRight) != 0 {
		mods = append(mods, wdaKeyControl)
	}
	if metaState&(MetaAlt|MetaAltLeft|MetaAltRight) != 0 {
		mods = append(mods, wdaKeyAlt)
	}
	if metaState&(MetaShift|MetaShiftLeft|MetaShiftRight) != 0 {
		mods = append(mods, wdaKeyShift)
	}
	if metaState&(MetaMeta|MetaMetaLeft|MetaMetaRight) != 0 {
		mods = append(mods, wdaKeyMeta)
	}
	return mods
}

// buildPressKeyActions builds a W3C key actions payload that presses keyName
// while holding the modifiers in metaState.
func buildPressKeyActions(keyName KeyName, metaState MetaState) ([]byte, error) {
	value, err := wdaKeyValue(keyName)
	if err != nil {
		return nil, err
	}
	mods := wdaModifierValues(metaState)

	actions := make([]WDAKeyAction, 0, len(mods)*2+2)
	for _, m := range mods {
		actions = append(actions, WDAKeyAction{Type: "keyDown", Value: m})
	}
	actions = append(actions,
		WDAKeyAction{Type: "keyDown", Value: value},
		WDAKeyAction{Type: "keyUp", Value: value},
	)
	for i := len(mods) - 1; i >= 0; i-- {
		actions = append(actions, WDAKeyAction{Type: "keyUp", Value: mods[i]})
	}

	payload := WDAKeyActionsRequest{
		Actions: []WDAKeySource{
			{Type: "key", ID: "keyboard", Actions: actions},
		},
	}
	return json.Marshal(payload)
}

// wdaRoundTripper implements http.RoundTripper interface
// It wraps a custom makeRequest function to allow WDAClient to use custom transport
type wdaRoundTripper struct {
	makeRequest func(*http.Request) (*http.Response, error)
}

// RoundTrip implements http.RoundTripper interface
func (t *wdaRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.makeRequest(req)
}

// Close closes the underlying connection if any
func (t *wdaRoundTripper) Close() error {
	// No-op for RoundTripper interface compliance
	return nil
}

// WDAStatusResponse represents the response from WDA /status endpoint
type WDAStatusResponse struct {
	Value struct {
		SessionID string `json:"sessionId"`
		Ready     bool   `json:"ready"`
		Message   string `json:"message"`
	} `json:"value"`
	SessionID string `json:"sessionId"`
}

// WDASessionResponse represents the response from WDA /session endpoint
type WDASessionResponse struct {
	Value struct {
		SessionID    string                 `json:"sessionId"`
		Capabilities map[string]interface{} `json:"capabilities"`
	} `json:"value"`
	SessionID string `json:"sessionId"`
}

// WDAClient represents a WebDriverAgent client for iOS device automation
type WDAClient struct {
	baseURL    string
	httpClient *http.Client
	sessionID  string
}

// NewWDAClient creates a new WDA client with standard HTTP client
func NewWDAClient(baseURL string) *WDAClient {
	return &WDAClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 0, // No timeout for long-running operations
		},
	}
}

// NewWDAClientWithTransport creates a new WDA client with custom RoundTripper
func NewWDAClientWithTransport(baseURL string, roundTripper http.RoundTripper) *WDAClient {
	return &WDAClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   0, // No timeout for long-running operations
			Transport: roundTripper,
		},
	}
}

// GetStatus retrieves the current status from WDA
func (c *WDAClient) GetStatus() (*WDAStatusResponse, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/status")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get WDA status")
	}
	defer resp.Body.Close()

	var statusResp WDAStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, errors.Wrap(err, "failed to decode status response")
	}

	// Update session ID from status response
	if statusResp.Value.SessionID != "" {
		c.sessionID = statusResp.Value.SessionID
	}

	return &statusResp, nil
}

// EnsureSession ensures that a WDA session exists, creating one if necessary
func (c *WDAClient) EnsureSession() error {
	status, err := c.GetStatus()
	if err != nil {
		return errors.Wrap(err, "failed to get status before ensuring session")
	}

	// If we already have a session ID, return
	if status.SessionID != "" {
		c.sessionID = status.SessionID
		return nil
	}

	// Create a new session
	return c.CreateSession()
}

// CreateSession creates a new WDA session
func (c *WDAClient) CreateSession() error {
	payload := map[string]interface{}{
		"capabilities":        map[string]interface{}{},
		"desiredCapabilities": map[string]interface{}{},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal session creation request")
	}

	resp, err := c.httpClient.Post(c.baseURL+"/session", "application/json", bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "failed to create WDA session")
	}
	defer resp.Body.Close()

	var sessionResp WDASessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return errors.Wrap(err, "failed to decode session response")
	}

	c.sessionID = sessionResp.SessionID
	return nil
}

// sessionEndpoint ensures a session exists and returns the full URL for a
// session-scoped endpoint (suffix starts with "/").
func (c *WDAClient) sessionEndpoint(suffix string) (string, error) {
	if err := c.EnsureSession(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/session/%s%s", c.baseURL, c.sessionID, suffix), nil
}

// executeAction POSTs a JSON payload to endpoint and reports WDA-level errors.
// WDA reports command failures with HTTP 200 and a value.error field.
func (c *WDAClient) executeAction(endpoint string, payload []byte, action string) error {
	resp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return errors.Wrap(err, action)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s failed: HTTP %d: %s", action, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var v struct {
		Value struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		} `json:"value"`
	}
	if json.Unmarshal(body, &v) == nil && v.Value.Error != "" {
		return fmt.Errorf("%s failed: %s: %s", action, v.Value.Error, v.Value.Message)
	}
	return nil
}

// Tap performs a tap action at specified coordinates
func (c *WDAClient) Tap(x, y int) error {
	body, err := buildTapPayload(x, y)
	if err != nil {
		return errors.Wrap(err, "failed to marshal tap action request")
	}
	endpoint, err := c.sessionEndpoint("/actions")
	if err != nil {
		return errors.Wrap(err, "failed to ensure session before tap")
	}
	return c.executeAction(endpoint, body, "tap")
}

// Swipe drags from (x1, y1) to (x2, y2) over durationMs milliseconds.
func (c *WDAClient) Swipe(x1, y1, x2, y2, durationMs int) error {
	body, err := buildSwipePayload(x1, y1, x2, y2, durationMs)
	if err != nil {
		return errors.Wrap(err, "failed to marshal swipe action request")
	}
	endpoint, err := c.sessionEndpoint("/actions")
	if err != nil {
		return errors.Wrap(err, "failed to ensure session before swipe")
	}
	return c.executeAction(endpoint, body, "swipe")
}

// InputText types text into the currently focused element.
func (c *WDAClient) InputText(text string) error {
	if text == "" {
		return nil
	}
	body, err := buildKeysPayload(text)
	if err != nil {
		return errors.Wrap(err, "failed to marshal keys request")
	}
	endpoint, err := c.sessionEndpoint("/wda/keys")
	if err != nil {
		return errors.Wrap(err, "failed to ensure session before input text")
	}
	return c.executeAction(endpoint, body, "input text")
}

// clearTextBackspaceCount is the number of backspace-delete sequences ClearText
// sends. WDA clears text internally by repeating the sequence once per character
// of the current value; since the length is unknown here a fixed upper bound is
// used.
const clearTextBackspaceCount = 50

// ClearText clears the currently focused text field.
func (c *WDAClient) ClearText() error {
	body, err := buildKeysPayload(strings.Repeat("\u0008\u007F", clearTextBackspaceCount))
	if err != nil {
		return errors.Wrap(err, "failed to marshal clear text request")
	}
	endpoint, err := c.sessionEndpoint("/wda/keys")
	if err != nil {
		return errors.Wrap(err, "failed to ensure session before clear text")
	}
	return c.executeAction(endpoint, body, "clear text")
}

// PressKey presses a key, holding the modifiers in metaState while doing so.
func (c *WDAClient) PressKey(keyName KeyName, metaState MetaState) error {
	if name, ok := wdaHardwareButton(keyName); ok {
		if metaState != MetaNone {
			return fmt.Errorf("hardware button %s does not support modifiers on iOS", keyName)
		}
		body, err := json.Marshal(map[string]string{"name": name})
		if err != nil {
			return errors.Wrap(err, "failed to marshal press button request")
		}
		endpoint, err := c.sessionEndpoint("/wda/pressButton")
		if err != nil {
			return errors.Wrap(err, "failed to ensure session before press key")
		}
		return c.executeAction(endpoint, body, "press key")
	}

	body, err := buildPressKeyActions(keyName, metaState)
	if err != nil {
		return err
	}
	endpoint, err := c.sessionEndpoint("/actions")
	if err != nil {
		return errors.Wrap(err, "failed to ensure session before press key")
	}
	return c.executeAction(endpoint, body, "press key")
}

// GetScreenSize retrieves screen size from WDA windowSize endpoint
// It creates a new session and gets the window size
func (c *WDAClient) GetScreenSize() (int, int, error) {
	// Ensure we have a valid session
	if err := c.EnsureSession(); err != nil {
		return 0, 0, errors.Wrap(err, "failed to ensure session before tap")
	}

	url := fmt.Sprintf("%s/session/%s/window/size", c.baseURL, c.sessionID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to get window size")
	}
	defer resp.Body.Close()

	var sizeResp struct {
		Value ScreenSize `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sizeResp); err != nil {
		return 0, 0, errors.Wrap(err, "failed to decode window size response")
	}
	// log.Printf("%+v\n", sizeResp)
	return sizeResp.Value.Width, sizeResp.Value.Height, nil
}
