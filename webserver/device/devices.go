package device

import (
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"net"
	"strings"
)

// Screen represents a display/screen on the device
type Screen struct {
	ID string `json:"id"`
}

// DeviceInfo represents the device information returned by the API
type DeviceInfo struct {
	Platform string                 `json:"platform"`
	ID       string                 `json:"id"`
	Props    map[string]interface{} `json:"props"`
	Disabled bool                   `json:"disabled"`
}

type DeviceConnectInfo struct {
	Type      string `json:"type"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
}

// ScreenSize is the width/height response shape used by the device API.
type ScreenSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ShellRequest is the request body for running a shell command on a device.
type ShellRequest struct {
	Command string `json:"command"`
	Timeout int64  `json:"timeout,omitempty"` // milliseconds
}

// ShellResult is the response shape for shell execution.
type ShellResult struct {
	Output string `json:"output"`
}

// TapRequest is the request body for the tap action.
type TapRequest struct {
	X            float64  `json:"x"`
	Y            float64  `json:"y"`
	ScreenWidth  *float64 `json:"screenWidth,omitempty"`
	ScreenHeight *float64 `json:"screenHeight,omitempty"`
}

// SwipeRequest is the request body for the swipe action.
type SwipeRequest struct {
	X1           float64  `json:"x1"`
	Y1           float64  `json:"y1"`
	X2           float64  `json:"x2"`
	Y2           float64  `json:"y2"`
	DurationMs   int      `json:"durationMs"`
	ScreenWidth  *float64 `json:"screenWidth,omitempty"`
	ScreenHeight *float64 `json:"screenHeight,omitempty"`
}

// PressKeyRequest is the request body for the press_key action.
type PressKeyRequest struct {
	Key       KeyName   `json:"key"`
	MetaState MetaState `json:"metaState,omitempty"`
}

// InputTextRequest is the request body for the input_text action.
type InputTextRequest struct {
	Text string `json:"text"`
}

// ClipboardKind identifies the type of content held by the device clipboard.
type ClipboardKind string

const (
	// ClipboardKindText is a text entry.
	ClipboardKindText ClipboardKind = "text"
	// ClipboardKindImage is an image entry.
	ClipboardKindImage ClipboardKind = "image"
)

// Valid reports whether the kind is one of the supported clipboard kinds.
func (k ClipboardKind) Valid() bool {
	return k == ClipboardKindText || k == ClipboardKindImage
}

// MaxClipboardImageBytes bounds the decoded image size accepted by the API.
const MaxClipboardImageBytes = 10 << 20

// ClipboardImage is an image entry of the device clipboard. Data is standard
// base64 so the API stays JSON.
type ClipboardImage struct {
	Mime string `json:"mime"`
	Data string `json:"data"`
}

// ClipboardContent is a clipboard read or write payload. Text and image share
// one shape with a kind discriminator so new kinds can be added without
// breaking the API.
type ClipboardContent struct {
	Kind  ClipboardKind   `json:"kind"`
	Text  string          `json:"text,omitempty"`
	Image *ClipboardImage `json:"image,omitempty"`
}

// NormalizeClipboardContent fills in the kind when a client omits it, so
// {"text": "..."} keeps working while {"kind": "image", ...} is explicit.
func NormalizeClipboardContent(content ClipboardContent) ClipboardContent {
	if content.Kind == "" {
		if content.Image != nil {
			content.Kind = ClipboardKindImage
		} else {
			content.Kind = ClipboardKindText
		}
	}
	return content
}

// Validate reports whether the content can be sent to a device. The decoded
// image size is bounded because the payload travels as base64 JSON.
func (c ClipboardContent) Validate() error {
	switch c.Kind {
	case ClipboardKindText:
		if c.Image != nil {
			return errors.New("text clipboard content must not carry an image")
		}
		return nil
	case ClipboardKindImage:
		if c.Image == nil {
			return errors.New("image clipboard content is missing the image field")
		}
		if !strings.HasPrefix(c.Image.Mime, "image/") {
			return fmt.Errorf("invalid image mime type %q", c.Image.Mime)
		}
		decoded, err := base64.StdEncoding.DecodeString(c.Image.Data)
		if err != nil {
			return errors.New("image data is not valid base64")
		}
		if len(decoded) == 0 {
			return errors.New("image data is empty")
		}
		if len(decoded) > MaxClipboardImageBytes {
			return fmt.Errorf("image is larger than %d MB", MaxClipboardImageBytes>>20)
		}
		return nil
	default:
		return fmt.Errorf("invalid clipboard kind %q", c.Kind)
	}
}

// ClipboardDevice is implemented by devices that expose the system clipboard.
// An empty clipboard is reported as empty text, not an error. Image support is
// optional: devices without it return ErrClipboardImageUnsupported.
type ClipboardDevice interface {
	ReadClipboard() (ClipboardContent, error)
	WriteClipboard(content ClipboardContent) error
}

// ErrClipboardImageUnsupported is returned when a device can only read and
// write clipboard text, so the API can answer 400 instead of 500.
var ErrClipboardImageUnsupported = errors.New("this device does not support clipboard images")

// clipboardTimeoutMs covers the JVM/testhelper startup and, for writes, the
// clipboard service confirmation. Both take several seconds on a device that
// is busy streaming.
const clipboardTimeoutMs = 30000

// UITreeBackend selects how the UI hierarchy is dumped on Android. Devices
// that only expose a single dump method ignore this value.
type UITreeBackend string

const (
	// UITreeBackendYadb uses yadb's fast layout dump (default).
	UITreeBackendYadb UITreeBackend = "yadb"
	// UITreeBackendUiautomator uses `uiautomator dump` over adb.
	UITreeBackendUiautomator UITreeBackend = "uiautomator"
)

// UITreeBackendDumper is implemented by devices that support choosing the UI
// hierarchy dump backend. Devices without it always use their default method.
type UITreeBackendDumper interface {
	GetUITreeWithBackend(backend UITreeBackend) (*Node, error)
}

// Device is an abstraction over platform-specific device operations.
type Device interface {
	Info() DeviceInfo
	GetScreenSize() (w int, h int, err error)
	Tap(x, y int) error
	Swipe(x1, y1, x2, y2, durationMs int) error
	InputText(text string) error
	ClearText() error
	PressKey(keyName KeyName, metaState MetaState) error
	Screenshot() (image.Image, error)
	GetScreens() ([]Screen, error)
	GetUITree() (*Node, error)
	AcquireDeviceConnect() (*DeviceConnectInfo, error)
	ReleaseDeviceConnect() error
	Connect(socketAddr string) (net.Conn, error)
}

type DeviceWrapper struct {
	Device
}

func (ed *DeviceWrapper) Serial() string {
	info := ed.Info()
	return info.ID
}

// GetUITreeWithBackend dumps the UI hierarchy using the requested backend when
// the underlying device supports it, otherwise it falls back to the default.
func (ed *DeviceWrapper) GetUITreeWithBackend(backend UITreeBackend) (*Node, error) {
	if d, ok := ed.Device.(UITreeBackendDumper); ok {
		return d.GetUITreeWithBackend(backend)
	}
	return ed.GetUITree()
}

func (ed *DeviceWrapper) DumpXML() (string, error) {
	node, err := ed.GetUITree()
	if err != nil {
		return "", err
	}
	return node.ToXML()
}

func (ed *DeviceWrapper) FindElementsByXpath(xpath string) ([]string, error) {
	node, err := ed.GetUITree()
	if err != nil {
		return nil, err
	}
	xmlContent, err := node.ToXML()
	if err != nil {
		return nil, err
	}
	return FindElementsByXpath(xmlContent, xpath)
}

// DeviceManager is an interface for enumerating device serial numbers
type DeviceManager interface {
	ListSerials() ([]string, error)
}

func NewDeviceManager(deviceType DeviceType) DeviceManager {
	switch deviceType {
	case Android:
		return &AndroidDeviceManager{}
	case IOS:
		return &IOSDeviceManager{}
	case HarmonyOS:
		return &HarmonyOSDeviceManager{}
	case Demo:
		return &DemoDeviceManager{}
	}
	return nil
}
