package device

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/webserver/utils"

	"github.com/adrg/xdg"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/tunnel"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// Default WebDriverAgent settings. They can be overridden per device through
// SetIOSConfigResolver (backed by the app config).
const (
	DefaultWDAPort      = 8100
	DefaultWDAMJPEGPort = 9100
	DefaultWDABundleID  = "com.facebook.WebDriverAgentRunner.xctrunner"
)

// IOSConfig holds the overridable WebDriverAgent settings for a device.
type IOSConfig struct {
	WDA      int
	MJPEG    int
	BundleID string
	// WDAConfigured reports whether WDA was explicitly set. Simulators use it
	// to tell a real port override from the shared WebDriverAgent default.
	WDAConfigured bool
}

var (
	iosConfigMu       sync.RWMutex
	iosConfigResolver func(udid string) IOSConfig
)

// SetIOSConfigResolver registers the callback used to look up per-device WDA
// settings. Passing nil restores the defaults.
func SetIOSConfigResolver(fn func(udid string) IOSConfig) {
	iosConfigMu.Lock()
	defer iosConfigMu.Unlock()
	iosConfigResolver = fn
}

// resolveIOSConfig returns the configured WDA settings for a device, falling
// back to the defaults for missing or out-of-range values.
func resolveIOSConfig(udid string) IOSConfig {
	cfg := IOSConfig{
		WDA:      DefaultWDAPort,
		MJPEG:    DefaultWDAMJPEGPort,
		BundleID: DefaultWDABundleID,
	}
	iosConfigMu.RLock()
	fn := iosConfigResolver
	iosConfigMu.RUnlock()
	if fn == nil {
		return cfg
	}
	configured := fn(udid)
	if configured.WDA >= 1 && configured.WDA <= 65535 {
		cfg.WDA = configured.WDA
		cfg.WDAConfigured = true
	}
	if configured.MJPEG >= 1 && configured.MJPEG <= 65535 {
		cfg.MJPEG = configured.MJPEG
	}
	if configured.BundleID != "" {
		cfg.BundleID = configured.BundleID
	}
	return cfg
}

var iosTunnelCmd *exec.Cmd

// iosTunnelMu serializes starting the userspace tunnel. The tunnel is machine
// wide (a single port/pair-record path shared by every device), so concurrent
// EnsureWDA calls for different devices must not start it more than once.
var iosTunnelMu sync.Mutex

// EnsureIOSUserspaceTunnelForDevice ensures the userspace tunnel is running and
// exposes an RSD entry for the given device. A listening HTTP API is not enough:
// a stale tunnel process can keep the port open while no tunnel exists.
func EnsureIOSUserspaceTunnelForDevice(udid string) error {
	if CheckIOSUserspaceTunnelForDevice(udid) {
		return nil
	}

	iosTunnelMu.Lock()
	defer iosTunnelMu.Unlock()

	if CheckIOSUserspaceTunnelForDevice(udid) {
		return nil
	}

	// The tunnel API is up but does not know this device: restart the stale
	// agent before starting a fresh tunnel.
	if CheckIOSUserspaceTunnel() {
		slog.Info("iOS userspace tunnel is stale, restarting...", "device", udid)
		stopIOSUserspaceTunnel()
	}

	if err := startIOSUserspaceTunnel(); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if CheckIOSUserspaceTunnelForDevice(udid) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("iOS userspace tunnel did not expose device %s within timeout", udid)
}

// EnsureIOSUserspaceTunnel checks if the iOS userspace tunnel is running by testing
// if port 127.0.0.1:60105 is listening. If not running, it will start the tunnel in background.
// Returns an error if the tunnel cannot be started.
func EnsureIOSUserspaceTunnel() error {
	if CheckIOSUserspaceTunnel() {
		return nil
	}

	iosTunnelMu.Lock()
	defer iosTunnelMu.Unlock()

	if CheckIOSUserspaceTunnel() {
		return nil
	}
	return startIOSUserspaceTunnel()
}

func startIOSUserspaceTunnel() error {
	slog.Info("iOS userspace tunnel not running, starting...")
	pairRecordPath := filepath.Join(xdg.DataHome, "uiautodev", "ios-pair-records")
	if err := os.MkdirAll(pairRecordPath, 0700); err != nil {
		return fmt.Errorf("failed to create pair-record directory: %w", err)
	}
	if err := os.Chmod(pairRecordPath, 0700); err != nil {
		return fmt.Errorf("failed to chmod pair-record directory: %w", err)
	}
	iosBin, err := IOSPath()
	if err != nil {
		return fmt.Errorf("failed to get ios binary: %w", err)
	}
	iosTunnelCmd = NewCommand(iosBin, "tunnel", "start", "--userspace", "--pair-record-path="+pairRecordPath)
	if err := utils.StartBackgroundProcess(iosTunnelCmd); err != nil {
		return fmt.Errorf("failed to start iOS userspace tunnel: %w", err)
	}
	if err := utils.WaitForPort("127.0.0.1", ios.HttpApiPort(), 15*time.Second); err != nil {
		return fmt.Errorf("iOS userspace tunnel did not start within timeout: %w", err)
	}
	time.Sleep(2 * time.Second)
	slog.Info("iOS userspace tunnel started successfully")
	return nil
}

func stopIOSUserspaceTunnel() {
	if iosTunnelCmd != nil && iosTunnelCmd.Process != nil {
		_ = iosTunnelCmd.Process.Kill()
	}
	iosBin, err := IOSPath()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = NewCommandContext(ctx, iosBin, "tunnel", "stopagent").Run()
		cancel()
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !CheckIOSUserspaceTunnel() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// CheckIOSUserspaceTunnelForDevice reports whether the tunnel API has a tunnel
// entry for the device.
func CheckIOSUserspaceTunnelForDevice(udid string) bool {
	info, err := tunnel.TunnelInfoForDevice(udid, ios.HttpApiHost(), ios.HttpApiPort())
	return err == nil && strings.TrimSpace(info.Address) != ""
}

// CheckIOSUserspaceTunnel checks if the iOS userspace tunnel is running by testing
// if port 127.0.0.1:60105 is listening.
func CheckIOSUserspaceTunnel() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ios.HttpApiPort()), 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// getDeviceWithRsdProvider attempts to get a device with RSD provider for iOS 17+
// Note: make sure 'ios tunnel start --userspace' is running before calling this method
func getDeviceWithRsdProvider(udid string) (ios.DeviceEntry, error) {
	device, err := ios.GetDevice(udid)
	if err != nil {
		return device, err
	}

	// If device already has RSD provider, return it
	if device.Rsd != nil {
		return device, nil
	}

	// Try to get tunnel info for RSD
	tunnelInfoHost := ios.HttpApiHost()
	tunnelInfoPort := ios.HttpApiPort()

	info, err := tunnel.TunnelInfoForDevice(device.Properties.SerialNumber, tunnelInfoHost, tunnelInfoPort)
	if err != nil {
		slog.Debug("Failed to get tunnel info", "device", udid, "error", err, "note", "device may not need RSD")
		return device, nil
	}

	slog.Debug("Got tunnel info", "address", info.Address, "rsdPort", info.RsdPort)

	// Create RSD service connection
	rsdService, err := ios.NewWithAddrPortDevice(info.Address, info.RsdPort, device)
	if err != nil {
		return device, fmt.Errorf("could not connect to RSD: %w", err)
	}
	defer rsdService.Close()

	// Handshake to get RSD provider
	rsdProvider, err := rsdService.Handshake()
	if err != nil {
		return device, fmt.Errorf("RSD handshake failed: %w", err)
	}

	// Get device with RSD provider
	device1, err := ios.GetDeviceWithAddress(udid, info.Address, rsdProvider)
	if err != nil {
		return device, fmt.Errorf("failed to get device with RSD provider: %w", err)
	}

	// Copy UserspaceTUN info
	device1.UserspaceTUN = info.UserspaceTUN
	device1.UserspaceTUNPort = info.UserspaceTUNPort

	slog.Debug("Device with RSD provider created successfully")
	return device1, nil
}

// IOSDeviceManager implements DeviceEnumerator for iOS devices
type IOSDeviceManager struct{}

// ListSerials returns a list of iOS device serial numbers, including booted
// simulators. Simulators are not on usbmuxd, so a failure there is not fatal
// when the host still has simulators to offer.
func (m *IOSDeviceManager) ListSerials() ([]string, error) {
	physical := make([]string, 0)
	result, err := ios.ListDevices()
	if err != nil {
		slog.Debug("list usbmuxd iOS devices failed", "error", err)
	} else {
		for _, device := range result.DeviceList {
			physical = append(physical, device.Properties.SerialNumber)
		}
	}

	simulators := make([]string, 0)
	booted, simErr := listBootedSimulators()
	if simErr != nil {
		slog.Debug("list booted iOS simulators failed", "error", simErr)
	} else {
		for _, simulator := range booted {
			simulators = append(simulators, simulator.UDID)
		}
	}

	if err != nil && len(simulators) == 0 {
		return nil, err
	}
	return mergeIOSSerials(physical, simulators), nil
}

type IOSDevice struct {
	udid          string
	simulator     bool
	wdaConfigured bool
	wdaPort       int
	mjpegPort     int
	wdaBundle     string
	wdaClient     *WDAClient
	devicekit     *devicekitClient
}

func NewIOSDevice(udid string) *IOSDevice {
	cfg := resolveIOSConfig(udid)
	device := &IOSDevice{udid: udid, wdaPort: cfg.WDA, mjpegPort: cfg.MJPEG, wdaBundle: cfg.BundleID}
	if isSimulatorUDID(udid) {
		device.simulator = true
		device.wdaConfigured = cfg.WDAConfigured
		switch {
		case device.wdaConfigured:
			device.wdaPort = cfg.WDA
		default:
			// NewIOSDevice runs for every tap, screenshot and UI-tree request.
			// Use the port already assigned in this process; only the first
			// request needs the simctl device list.
			port, ok := cachedSimulatorPort(udid)
			if !ok {
				port = simulatorPort(udid, 0, bootedSimulatorUDIDs())
			}
			device.wdaPort = port
		}
		// The runner serves JSON-RPC and MJPEG on the same listener.
		device.mjpegPort = device.wdaPort
	}
	return device
}

func (d *IOSDevice) wdaURL() string {
	return fmt.Sprintf("http://localhost:%d", d.wdaPort)
}

// ios devicename这个命令跑起来没有idevicename速度快
func (d *IOSDevice) deviceName() (string, error) {
	if d.simulator {
		devices, err := listBootedSimulators()
		if err != nil {
			return "", err
		}
		for _, simulator := range devices {
			if simulator.UDID == d.udid {
				return simulator.Name, nil
			}
		}
		return "", fmt.Errorf("simulator %s is not booted", d.udid)
	}
	// output example: {"devicename":"iPhone"}
	var result struct {
		DeviceName string `json:"devicename"`
	}
	iosBin, err := IOSPath()
	if err != nil {
		return "", err
	}
	output, err := NewCommand(iosBin, "devicename", "--udid", d.udid).Output()
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(output, &result)
	if err != nil {
		return "", err
	}
	return result.DeviceName, nil
}

func (d *IOSDevice) Info() DeviceInfo {
	// deviceName, _ := d.deviceName()
	props := map[string]interface{}{
		"udid":   d.udid,
		"serial": d.udid,
		// "name": deviceName,
	}
	if d.simulator {
		props["simulator"] = true
	}
	return DeviceInfo{
		Platform: "ios",
		ID:       "i-" + d.udid,
		Props:    props,
		Disabled: false,
	}
}

// CheckWDA checks if the iOS automation server is running: WebDriverAgent for
// physical devices, the devicekit health endpoint for simulators.
func (d *IOSDevice) CheckWDA() bool {
	if d.simulator {
		return devicekitHealthy(d.wdaPort)
	}
	conn, err := d.newConnection(uint16(d.wdaPort))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// iosEnsureLocks serializes EnsureWDA per device serial. Every request builds a
// fresh IOSDevice, so the lock is shared across instances keyed by serial
// instead of living on the struct.
var iosEnsureLocks sync.Map // serial -> *sync.Mutex

func (d *IOSDevice) ensureWDALock() *sync.Mutex {
	lock, _ := iosEnsureLocks.LoadOrStore(d.udid, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

type iosWDAStatus struct {
	launched bool
	ready    bool
}

var iosWDAStatuses sync.Map // serial -> *iosWDAStatus

func iosWDAStatusFor(udid string) *iosWDAStatus {
	status, _ := iosWDAStatuses.LoadOrStore(udid, &iosWDAStatus{})
	return status.(*iosWDAStatus)
}

// EnsureWDA ensures that the iOS WebDriverAgent is running on this device.
// If WDA is not running, it will start it and wait up to 15 seconds for it to
// be ready. Concurrent calls for the same device are serialized so the tunnel
// and WDA are only started once.
func (d *IOSDevice) EnsureWDA() error {
	mu := d.ensureWDALock()
	mu.Lock()
	defer mu.Unlock()

	return d.ensureWDA()
}

func (d *IOSDevice) ensureWDA() error {
	if d.simulator {
		return d.ensureSimulatorRunner()
	}

	if d.wdaClient == nil {
		roundTripper := &wdaRoundTripper{makeRequest: d.makeRequest}
		d.wdaClient = NewWDAClientWithTransport(d.wdaURL(), roundTripper)
	}

	status := iosWDAStatusFor(d.udid)
	if d.CheckWDA() {
		status.launched = true
		status.ready = true
		return nil
	}

	// A successful `ios launch` is enough to make WDA available once the user
	// unlocks the device. Do not launch it again while it is waiting for that
	// confirmation. Only relaunch if WDA had previously been reachable.
	if status.launched {
		if !status.ready {
			return fmt.Errorf("iOS WebDriverAgent launch already requested; unlock the device to finish startup")
		}
		status.launched = false
		status.ready = false
	}

	if err := EnsureIOSUserspaceTunnelForDevice(d.udid); err != nil {
		return errors.Wrap(err, "EnsureWDA")
	}

	slog.Info("iOS WebDriverAgent not running, starting...", "device", d.udid)
	if err := d.launchWDA(); err != nil {
		return errors.Wrap(err, "EnsureWDA")
	}
	status.launched = true

	// Wait for WDA to be ready
	const timeout = 15 * time.Second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if d.CheckWDA() {
			status.ready = true
			slog.Info("iOS WebDriverAgent started successfully", "device", d.udid)
			return nil
		}
		<-ticker.C
	}
	return fmt.Errorf("iOS WebDriverAgent launched but is not reachable; unlock the device and retry")
}

// ensureSimulatorRunner starts the devicekit runner on a booted simulator.
// The runner is installed from a pinned release on first use, so the first
// interaction needs network access.
func (d *IOSDevice) ensureSimulatorRunner() error {
	if d.devicekit == nil {
		d.devicekit = newDevicekitClient(d.wdaPort)
	}
	if confirmed, ok := simulatorRunnerConfirmations.Load(d.udid); ok && confirmed.(int) == d.wdaPort && d.CheckWDA() {
		return nil
	}
	if until, ok := simulatorLaunchFailures.Load(d.udid); ok && time.Now().Before(until.(time.Time)) {
		return fmt.Errorf("devicekit runner for simulator %s failed to start recently; retrying in %s", d.udid, time.Until(until.(time.Time)).Round(time.Second))
	}

	// A runner left behind by an earlier process can hold the port assigned to
	// another simulator. Stop this simulator's runner; if it was healthy, give
	// its listener a moment to close so it can keep the same port. A port that
	// is still occupied afterwards belongs to a foreign process and is skipped
	// before launching, instead of waiting for a two-minute health timeout.
	wasHealthy := d.CheckWDA()
	terminateSimulatorRunner(d.udid)
	if wasHealthy {
		waitSimulatorPortFree(d.wdaPort, 3*time.Second)
	}
	for attempts := 0; simulatorPortOccupied(d.wdaPort); attempts++ {
		if d.wdaConfigured {
			return fmt.Errorf("simulator %s: port %d is already in use; set another WDA port in the device settings", d.udid, d.wdaPort)
		}
		if attempts > 100 {
			return fmt.Errorf("no free port for simulator %s", d.udid)
		}
		d.wdaPort = nextSimulatorPort(d.udid)
		d.mjpegPort = d.wdaPort
		d.devicekit = newDevicekitClient(d.wdaPort)
	}

	slog.Info("starting devicekit runner for iOS simulator", "device", d.udid, "port", d.wdaPort)
	if err := launchSimulatorRunner(d.udid, d.wdaPort); err != nil {
		simulatorLaunchFailures.Store(d.udid, time.Now().Add(simulatorLaunchFailureBackoff))
		return errors.Wrap(err, "EnsureWDA")
	}
	simulatorLaunchFailures.Delete(d.udid)
	simulatorRunnerConfirmations.Store(d.udid, d.wdaPort)
	return nil
}

func (d *IOSDevice) launchWDA() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	iosBin, err := IOSPath()
	if err != nil {
		return err
	}
	cmd := NewCommandContext(ctx, iosBin, "launch", "--udid", d.udid,
		"--env", fmt.Sprintf("USE_PORT=%d", d.wdaPort),
		"--env", fmt.Sprintf("MJPEG_SERVER_PORT=%d", d.mjpegPort),
		d.wdaBundle)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "launchWDA: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (d *IOSDevice) GetScreenSize() (int, int, error) {
	if err := d.EnsureWDA(); err != nil {
		return 0, 0, err
	}
	if d.simulator {
		return d.devicekit.ScreenSize()
	}
	return d.wdaClient.GetScreenSize()
}

func (d *IOSDevice) Tap(x, y int) error {
	if err := d.EnsureWDA(); err != nil {
		return err
	}
	if d.simulator {
		return d.devicekit.Tap(x, y)
	}
	return d.wdaClient.Tap(x, y)
}

func (d *IOSDevice) Swipe(x1, y1, x2, y2, durationMs int) error {
	if err := d.EnsureWDA(); err != nil {
		return err
	}
	if d.simulator {
		return d.devicekit.Swipe(x1, y1, x2, y2, durationMs)
	}
	return d.wdaClient.Swipe(x1, y1, x2, y2, durationMs)
}

func (d *IOSDevice) InputText(text string) error {
	if err := d.EnsureWDA(); err != nil {
		return err
	}
	if d.simulator {
		return d.devicekit.InputText(text)
	}
	return d.wdaClient.InputText(text)
}

func (d *IOSDevice) ClearText() error {
	if err := d.EnsureWDA(); err != nil {
		return err
	}
	if d.simulator {
		return d.devicekit.ClearText()
	}
	return d.wdaClient.ClearText()
}

func (d *IOSDevice) PressKey(keyName KeyName, metaState MetaState) error {
	if err := d.EnsureWDA(); err != nil {
		return err
	}
	if d.simulator {
		return d.devicekit.PressKey(keyName, metaState)
	}
	return d.wdaClient.PressKey(keyName, metaState)
}

func (d *IOSDevice) Screenshot() (image.Image, error) {
	if d.simulator {
		if err := d.EnsureWDA(); err != nil {
			return nil, err
		}
		return d.devicekit.Screenshot()
	}
	slog.Debug("Taking screenshot using CLI for iOS device", "device", d.udid)
	return d.screenshotWithCli()
}

func (d *IOSDevice) screenshotWithCli() (image.Image, error) {
	if err := EnsureIOSUserspaceTunnelForDevice(d.udid); err != nil {
		return nil, errors.Wrap(err, "Screenshot")
	}
	// Create temp file for screenshot
	tmpFile, err := os.CreateTemp("", "ios_screenshot_*.png")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Run ios screenshot command with 3 seconds timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	iosBin, err := IOSPath()
	if err != nil {
		return nil, err
	}
	cmd := NewCommandContext(ctx, iosBin, "screenshot", "--udid="+d.udid, "--output="+tmpPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ios screenshot failed: %w, output: %s", err, string(output))
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read screenshot file: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}
	return img, nil
}

func (d *IOSDevice) GetScreens() ([]Screen, error) {
	return []Screen{{ID: "0"}}, nil
}

// connectMJPEG dials the WDA MJPEG server, retrying briefly because the MJPEG
// listener may come up a moment after the WDA HTTP server that EnsureWDA waits on.
func (d *IOSDevice) connectMJPEG(ctx context.Context) (net.Conn, error) {
	const attempts = 6
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		conn, err := d.newConnection(uint16(d.mjpegPort))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// StartWebsocketStream connects to the WebDriverAgent MJPEG server and forwards
// each JPEG frame as a binary websocket message, matching the harmony/demo
// screen frames protocol.
func (d *IOSDevice) StartWebsocketStream(ctx context.Context, conn *websocket.Conn) error {
	if err := d.EnsureWDA(); err != nil {
		return err
	}

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if d.simulator {
		return d.devicekit.StreamMJPEG(streamCtx, func(frame []byte) error {
			return conn.WriteMessage(websocket.BinaryMessage, frame)
		})
	}

	rawConn, err := d.connectMJPEG(streamCtx)
	if err != nil {
		return errors.Wrap(err, "connect to WDA MJPEG server")
	}
	defer rawConn.Close()

	// Close the device connection when the stream context is canceled so the
	// blocking body read below unblocks.
	go func() {
		<-streamCtx.Done()
		rawConn.Close()
	}()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, fmt.Sprintf("http://localhost:%d/", d.mjpegPort), nil)
	if err != nil {
		return errors.Wrap(err, "build MJPEG request")
	}
	req.Header.Set("Accept", "multipart/x-mixed-replace")
	if err := req.Write(rawConn); err != nil {
		return errors.Wrap(err, "write MJPEG request")
	}

	resp, err := http.ReadResponse(bufio.NewReader(rawConn), req)
	if err != nil {
		return errors.Wrap(err, "read MJPEG response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("WDA MJPEG server returned HTTP %d", resp.StatusCode)
	}

	return extractJPEGFrames(streamCtx, resp.Body, func(frame []byte) error {
		return conn.WriteMessage(websocket.BinaryMessage, frame)
	})
}

func (d *IOSDevice) GetUITree() (*Node, error) {
	if err := d.EnsureWDA(); err != nil {
		return nil, err
	}
	if d.simulator {
		return d.devicekit.DumpUI()
	}
	req, err := http.NewRequest("GET", d.wdaURL()+"/source?format=json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := d.makeRequest(req)
	if err != nil {
		return nil, errors.Wrap(err, "getUITree")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "getUITree")
	}
	return parseWDASourceToNode(body)
}

func (d *IOSDevice) makeRequest(req *http.Request) (*http.Response, error) {
	port := uint16(80)
	if portStr := req.URL.Port(); portStr != "" {
		if p, err := strconv.ParseUint(portStr, 10, 16); err == nil {
			port = uint16(p)
		}
	}
	conn, err := d.newConnection(port)
	if err != nil {
		return nil, errors.Wrap(err, "makeRequest")
	}
	// Create custom HTTP transport using device connection
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return conn, nil
		},
		DisableKeepAlives: true, // Close conn when resp.Body.Close() is called
	}
	httpClient := http.Client{
		Transport: transport,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		conn.Close() // Close connection on error since resp.Body.Close() won't be called
		return nil, errors.Wrap(err, "makeRequest")
	}
	return resp, nil
}

func (d *IOSDevice) newConnection(port uint16) (conn net.Conn, err error) {
	if d.simulator {
		// Simulators run on this host, so the runner listens on loopback and
		// there is no usbmuxd hop.
		return net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	}

	deviceConn, err := ios.NewDeviceConnection(ios.GetUsbmuxdSocket())
	if err != nil {
		return nil, err
	}

	// Close deviceConn on error, but not on success
	defer func() {
		if err != nil {
			deviceConn.Close()
		}
	}()

	usbmux := ios.NewUsbMuxConnection(deviceConn)
	listResult, err := usbmux.ListDevices()
	if err != nil {
		return nil, err
	}

	var deviceEntry *ios.DeviceEntry
	for _, dev := range listResult.DeviceList {
		if dev.Properties.SerialNumber == d.udid {
			deviceEntry = &dev
			break
		}
	}
	if deviceEntry == nil || deviceEntry.DeviceID == 0 {
		return nil, fmt.Errorf("device with udid %s not found", d.udid)
	}
	if err := usbmux.Connect(deviceEntry.DeviceID, port); err != nil {
		return nil, errors.Wrap(err, "usbmux.Connect")
	}
	return deviceConn.Conn(), nil
}

func (d *IOSDevice) AcquireDeviceConnect() (*DeviceConnectInfo, error) {
	return nil, fmt.Errorf("AcquireDeviceConnect is not implemented for iOS devices")
}

func (d *IOSDevice) ReleaseDeviceConnect() error {
	return nil
}

func (d *IOSDevice) Connect(socketAddr string) (net.Conn, error) {
	// Parse socketAddr to extract port number
	// Support formats: "port", "localhost:port", or "host:port"
	if socketAddr == "wda" {
		if err := d.EnsureWDA(); err != nil {
			return nil, err
		}
		socketAddr = strconv.Itoa(d.wdaPort)
	}
	var portStr string
	if strings.Contains(socketAddr, ":") {
		_, portStr, _ = net.SplitHostPort(socketAddr)
	} else {
		portStr = socketAddr
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid port number '%s': %w", portStr, err)
	}

	// Use the existing newConnection method to connect via usbmux
	return d.newConnection(uint16(port))
}
