package cluster

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/uiautodev/uiautodev/webserver/device"
)

type remoteDevice struct {
	master   *Master
	slaveID  string
	localID  string
	globalID string
}

func newRemoteDevice(master *Master, slaveID, localID, globalID string) device.Device {
	return &remoteDevice{
		master:   master,
		slaveID:  slaveID,
		localID:  localID,
		globalID: globalID,
	}
}

func (d *remoteDevice) doJSON(ctx context.Context, method, path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := d.master.newSlaveRequest(ctx, method, d.slaveID, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.master.newClient(d.slaveID, SessionControl).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("slave returned HTTP %d", resp.StatusCode)
		}
		return err
	}
	if resp.StatusCode/100 != 2 || envelope.Code != 0 {
		message := envelope.Message
		if message == "" {
			message = fmt.Sprintf("slave returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("%s", message)
	}
	if out != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func (d *remoteDevice) Info() device.DeviceInfo {
	var data device.DeviceInfo
	if err := d.doJSON(context.Background(), http.MethodGet, "/api/v1/devices/"+d.localID, nil, &data); err != nil {
		return device.DeviceInfo{ID: d.globalID, Platform: platformFromID(d.localID)}
	}
	if data.Platform == "" {
		data.Platform = platformFromID(d.localID)
	}
	disabled := data.Disabled
	if !d.master.slaveOnline(d.slaveID) {
		disabled = true
	}
	return device.DeviceInfo{
		Platform: data.Platform,
		ID:       d.globalID,
		Props:    data.Props,
		Disabled: disabled,
	}
}

func platformFromID(localID string) string {
	if len(localID) < 2 {
		return ""
	}
	switch localID[:2] {
	case "a-":
		return "android"
	case "i-":
		return "ios"
	case "h-":
		return "harmony"
	case "d-":
		return "demo"
	default:
		return ""
	}
}

func (d *remoteDevice) GetScreenSize() (int, int, error) {
	var data device.ScreenSize
	if err := d.doJSON(context.Background(), http.MethodGet, "/api/v1/devices/"+d.localID+"/window/size", nil, &data); err != nil {
		return 0, 0, err
	}
	return data.Width, data.Height, nil
}

func (d *remoteDevice) Tap(x, y int) error {
	return d.doJSON(context.Background(), http.MethodPost, "/api/v1/devices/"+d.localID+"/actions/tap", device.TapRequest{
		X: float64(x),
		Y: float64(y),
	}, nil)
}

func (d *remoteDevice) Swipe(x1, y1, x2, y2, durationMs int) error {
	return d.doJSON(context.Background(), http.MethodPost, "/api/v1/devices/"+d.localID+"/actions/swipe", device.SwipeRequest{
		X1:         float64(x1),
		Y1:         float64(y1),
		X2:         float64(x2),
		Y2:         float64(y2),
		DurationMs: durationMs,
	}, nil)
}

func (d *remoteDevice) InputText(text string) error {
	return d.doJSON(context.Background(), http.MethodPost, "/api/v1/devices/"+d.localID+"/actions/input_text", device.InputTextRequest{Text: text}, nil)
}

func (d *remoteDevice) ClearText() error {
	return d.doJSON(context.Background(), http.MethodPost, "/api/v1/devices/"+d.localID+"/actions/clear_text", nil, nil)
}

func (d *remoteDevice) PressKey(keyName device.KeyName, metaState device.MetaState) error {
	return d.doJSON(context.Background(), http.MethodPost, "/api/v1/devices/"+d.localID+"/actions/press_key", device.PressKeyRequest{
		Key:       keyName,
		MetaState: metaState,
	}, nil)
}

func (d *remoteDevice) Screenshot() (image.Image, error) {
	var encoded string
	if err := d.doJSON(context.Background(), http.MethodGet, "/api/v1/devices/"+d.localID+"/screenshot", nil, &encoded); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(raw))
}

func (d *remoteDevice) GetScreens() ([]device.Screen, error) {
	var screens []device.Screen
	if err := d.doJSON(context.Background(), http.MethodGet, "/api/v1/devices/"+d.localID+"/screens", nil, &screens); err != nil {
		return nil, err
	}
	return screens, nil
}

func (d *remoteDevice) GetUITree() (*device.Node, error) {
	return d.GetUITreeWithBackend("")
}

func (d *remoteDevice) GetUITreeWithBackend(backend device.UITreeBackend) (*device.Node, error) {
	path := "/api/v1/devices/" + d.localID + "/uitree"
	if backend != "" {
		path += "?backend=" + url.QueryEscape(string(backend))
	}
	var node device.Node
	if err := d.doJSON(context.Background(), http.MethodGet, path, nil, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

func (d *remoteDevice) AcquireDeviceConnect() (*device.DeviceConnectInfo, error) {
	var data device.DeviceConnectInfo
	if err := d.doJSON(context.Background(), http.MethodGet, "/api/v1/devices/"+d.localID+"/connect", nil, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (d *remoteDevice) ReleaseDeviceConnect() error {
	return d.doJSON(context.Background(), http.MethodDelete, "/api/v1/devices/"+d.localID+"/connect", nil, nil)
}

func (d *remoteDevice) Connect(socketAddr string) (net.Conn, error) {
	session := d.master.currentSession(d.slaveID, SessionMedia)
	if session == nil {
		return nil, ErrSlaveOffline
	}
	return openRawStream(session, RawTarget{DeviceID: d.localID, SocketAddr: socketAddr})
}

// RunShell lets the master's MCP shell tool operate on remote devices.
func (d *remoteDevice) RunShell(command string, timeoutMs int64) (string, error) {
	var data device.ShellResult
	body := device.ShellRequest{Command: command, Timeout: timeoutMs}
	if err := d.doJSON(context.Background(), http.MethodPost, "/api/v1/devices/"+d.localID+"/shell", body, &data); err != nil {
		return "", err
	}
	return data.Output, nil
}

// ReadClipboard forwards the clipboard read to the slave that owns the device.
func (d *remoteDevice) ReadClipboard() (device.ClipboardContent, error) {
	var data device.ClipboardContent
	if err := d.doJSON(context.Background(), http.MethodGet, "/api/v1/devices/"+d.localID+"/clipboard", nil, &data); err != nil {
		return device.ClipboardContent{}, err
	}
	return data, nil
}

// WriteClipboard forwards the clipboard write to the slave that owns the device.
func (d *remoteDevice) WriteClipboard(content device.ClipboardContent) error {
	return d.doJSON(context.Background(), http.MethodPut, "/api/v1/devices/"+d.localID+"/clipboard", content, nil)
}
