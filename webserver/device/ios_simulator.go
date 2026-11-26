package device

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/pkg/errors"
)

// The prebuilt devicekit-ios XCUITest runner is pinned per release. It is
// downloaded on first use because the app bundle is too large to ship and is
// licensed under FSL-1.1 (see the README).
const (
	devicekitVersion         = "0.0.38"
	devicekitRunnerBundleID  = "com.mobilenext.devicekit-iosUITests.xctrunner"
	devicekitRunnerAppName   = "devicekit-iosUITests-Runner.app"
	devicekitHealthTimeout   = 120 * time.Second
	simulatorToolTimeout     = 30 * time.Second
	simulatorInstallTimeout  = 2 * time.Minute
	devicekitDownloadTimeout = 10 * time.Minute
)

// devicekitDownloadBaseURL is a variable so tests can point at a local server.
var devicekitDownloadBaseURL = "https://github.com/mobile-next/devicekit-ios/releases/download"

// devicekitMirrors rewrites a GitHub asset URL into an equivalent URL served by
// a public mirror. Each mirror builds its own address, because they do not all
// share one URL shape. They are tried in order before GitHub itself, since
// github.com is often unreachable from mainland China.
var devicekitMirrors = []func(githubURL string) string{
	func(u string) string { return "https://gh-proxy.com/" + u },
	func(u string) string { return "https://ghfast.top/" + u },
	func(u string) string { return "https://ghproxy.net/" + u },
}

var simulatorUUIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// simulatorDevice is one simulator reported by `simctl list devices`.
type simulatorDevice struct {
	UDID    string
	Name    string
	Runtime string
}

// isSimulatorUDID reports whether a serial is a simulator UUID. Real devices
// use 24 or 40 hex characters without hyphens, so the dashed 36-character UUID
// cannot collide with one.
func isSimulatorUDID(udid string) bool {
	return simulatorUUIDPattern.MatchString(udid)
}

// devicekitDownloadURL returns the simulator runner archive on GitHub for a GOARCH.
func devicekitDownloadURL(goarch string) (string, error) {
	var arch string
	switch goarch {
	case "arm64":
		arch = "arm64"
	case "amd64":
		arch = "x86_64"
	default:
		return "", fmt.Errorf("no devicekit runner for architecture %s", goarch)
	}
	return fmt.Sprintf("%s/%s/devicekit-ios-Sim-%s.zip", devicekitDownloadBaseURL, devicekitVersion, arch), nil
}

// devicekitDownloadURLs returns the candidate download URLs for a GOARCH: the
// public mirrors first, then the direct GitHub asset as the last resort.
func devicekitDownloadURLs(goarch string) ([]string, error) {
	githubURL, err := devicekitDownloadURL(goarch)
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(devicekitMirrors)+1)
	for _, mirror := range devicekitMirrors {
		urls = append(urls, mirror(githubURL))
	}
	return append(urls, githubURL), nil
}

// simctlDeviceList is the subset of `simctl list devices -j` we need.
type simctlDeviceList struct {
	Devices map[string][]simctlDevice `json:"devices"`
}

type simctlDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	State       string `json:"state"`
	IsAvailable *bool  `json:"isAvailable"`
}

// parseSimctlDeviceList keeps available, booted simulators. A missing
// isAvailable is treated as available, because older simctl versions omit it.
func parseSimctlDeviceList(data []byte) ([]simulatorDevice, error) {
	var list simctlDeviceList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, errors.Wrap(err, "parse simctl device list")
	}
	devices := make([]simulatorDevice, 0)
	for runtimeName, entries := range list.Devices {
		for _, entry := range entries {
			if entry.State != "Booted" {
				continue
			}
			if entry.IsAvailable != nil && !*entry.IsAvailable {
				continue
			}
			if !isSimulatorUDID(entry.UDID) {
				continue
			}
			devices = append(devices, simulatorDevice{
				UDID:    entry.UDID,
				Name:    entry.Name,
				Runtime: runtimeName,
			})
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].UDID < devices[j].UDID })
	return devices, nil
}

// simulatorsSupported reports whether the host can run iOS simulators.
func simulatorsSupported() bool {
	return runtime.GOOS == "darwin"
}

// runSimctl runs `xcrun simctl` and returns stdout. The combined error includes
// stderr so failures are actionable.
func runSimctl(ctx context.Context, args ...string) ([]byte, error) {
	if !simulatorsSupported() {
		return nil, errors.New("iOS simulators are only supported on macOS")
	}
	cmd := NewCommandContext(ctx, "xcrun", append([]string{"simctl"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("xcrun simctl %s: %w: %s", strings.Join(args, " "), err, detail)
	}
	return stdout.Bytes(), nil
}

// listBootedSimulators returns the booted, available simulators. It is empty on
// non-macOS hosts and when xcrun is not installed.
func listBootedSimulators() ([]simulatorDevice, error) {
	if !simulatorsSupported() {
		return []simulatorDevice{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), simulatorToolTimeout)
	defer cancel()
	output, err := runSimctl(ctx, "list", "devices", "-j")
	if err != nil {
		return nil, err
	}
	return parseSimctlDeviceList(output)
}

// bootedSimulatorUDIDs is the sorted UDID list used to hand out ports.
func bootedSimulatorUDIDs() []string {
	devices, err := listBootedSimulators()
	if err != nil {
		slog.Debug("list booted iOS simulators failed", "error", err)
		return nil
	}
	udids := make([]string, len(devices))
	for i, device := range devices {
		udids[i] = device.UDID
	}
	return udids
}

// assignSimulatorPort returns the JSON-RPC/MJPEG port for a simulator. An
// explicit per-device port wins; otherwise simulators take consecutive ports
// from DefaultWDAPort in UDID order, so several booted simulators never share
// one and the port stays stable across requests.
func assignSimulatorPort(udid string, configured int, booted []string) int {
	if configured >= 1 && configured <= 65535 {
		return configured
	}
	sorted := append([]string(nil), booted...)
	sort.Strings(sorted)
	for i, candidate := range sorted {
		if candidate == udid {
			return DefaultWDAPort + i
		}
	}
	return DefaultWDAPort
}

var (
	simulatorPortsMu sync.Mutex
	simulatorPorts   = map[string]int{}
	// simulatorRunnerConfirmations marks a simulator whose runner this process
	// started on the stored port, so later requests can trust /health without
	// terminating and relaunching it.
	simulatorRunnerConfirmations sync.Map // udid -> int
)

// simulatorPort assigns a simulator its port once and keeps it for the life of
// the process. Without the cache, booting another simulator whose UDID sorts
// earlier would shift every port and make two runners collide.
func simulatorPort(udid string, configured int, booted []string) int {
	if configured >= 1 && configured <= 65535 {
		return configured
	}
	simulatorPortsMu.Lock()
	defer simulatorPortsMu.Unlock()
	if port, ok := simulatorPorts[udid]; ok {
		return port
	}
	port := assignSimulatorPort(udid, 0, booted)
	port = firstUnassignedPortLocked(port)
	simulatorPorts[udid] = port
	return port
}

// nextSimulatorPort moves a simulator past a port that is already in use by
// another process, keeping the assignments of other simulators intact.
func nextSimulatorPort(udid string) int {
	simulatorPortsMu.Lock()
	defer simulatorPortsMu.Unlock()
	port := simulatorPorts[udid] + 1
	if port < DefaultWDAPort {
		port = DefaultWDAPort
	}
	port = firstUnassignedPortLocked(port)
	simulatorPorts[udid] = port
	return port
}

// firstUnassignedPortLocked walks forward to a port no simulator in this
// process uses. Callers must hold simulatorPortsMu.
func firstUnassignedPortLocked(port int) int {
	used := make(map[int]bool, len(simulatorPorts))
	for _, assigned := range simulatorPorts {
		used[assigned] = true
	}
	for used[port] {
		port++
	}
	return port
}

// mergeIOSSerials adds simulator UDIDs to the usbmuxd device list, preserving
// order and removing duplicates.
func mergeIOSSerials(physical, simulators []string) []string {
	serials := make([]string, 0, len(physical)+len(simulators))
	seen := make(map[string]bool, len(physical)+len(simulators))
	for _, serial := range append(append([]string(nil), physical...), simulators...) {
		if serial == "" || seen[serial] {
			continue
		}
		seen[serial] = true
		serials = append(serials, serial)
	}
	return serials
}

// devicekitCacheDir is the directory holding the downloaded runner of the
// pinned devicekit version.
func devicekitCacheDir() string {
	return filepath.Join(xdg.DataHome, "uiautodev", "devicekit-ios", devicekitVersion)
}

var devicekitDownloadMu sync.Mutex

// ensureDevicekitRunner returns the local path to the pinned simulator runner,
// downloading and extracting it once per version.
func ensureDevicekitRunner() (string, error) {
	dir := devicekitCacheDir()
	appPath := filepath.Join(dir, devicekitRunnerAppName)
	if info, err := os.Stat(filepath.Join(appPath, "Info.plist")); err == nil && !info.IsDir() {
		return appPath, nil
	}

	devicekitDownloadMu.Lock()
	defer devicekitDownloadMu.Unlock()
	// Another request may have finished the download while we waited.
	if info, err := os.Stat(filepath.Join(appPath, "Info.plist")); err == nil && !info.IsDir() {
		return appPath, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create devicekit cache dir: %w", err)
	}
	urls, err := devicekitDownloadURLs(runtime.GOARCH)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), devicekitDownloadTimeout)
	defer cancel()
	zipPath, err := downloadDevicekitRunner(ctx, urls, dir)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipPath)

	tmpDir, err := os.MkdirTemp(dir, ".extract-*")
	if err != nil {
		return "", fmt.Errorf("create devicekit extract dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := extractDevicekitRunner(zipPath, tmpDir); err != nil {
		return "", err
	}
	if err := os.RemoveAll(appPath); err != nil {
		return "", fmt.Errorf("replace devicekit runner: %w", err)
	}
	if err := os.Rename(filepath.Join(tmpDir, devicekitRunnerAppName), appPath); err != nil {
		return "", fmt.Errorf("install devicekit runner: %w", err)
	}
	slog.Info("devicekit simulator runner ready", "path", appPath)
	return appPath, nil
}

// devicekitDownloadClient fails fast on a dead mirror (connect, TLS or response
// headers) and leaves the body transfer to the overall download deadline.
var devicekitDownloadClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}()

// downloadDevicekitRunner downloads the runner zip from the first candidate URL
// that serves a valid archive and returns its path inside dir.
func downloadDevicekitRunner(ctx context.Context, urls []string, dir string) (string, error) {
	failures := make([]string, 0, len(urls))
	for _, sourceURL := range urls {
		slog.Info("downloading devicekit simulator runner", "url", sourceURL)
		zipPath, err := downloadDevicekitZip(ctx, sourceURL, dir)
		if err == nil {
			return zipPath, nil
		}
		slog.Warn("devicekit runner download failed, trying next source", "url", sourceURL, "error", err)
		failures = append(failures, fmt.Sprintf("%s: %v", sourceURL, err))
	}
	return "", fmt.Errorf("download devicekit runner: %s", strings.Join(failures, "; "))
}

// downloadDevicekitZip fetches one URL into a temporary zip file and verifies it
// contains the runner, so an error page served by a proxy is not accepted.
func downloadDevicekitZip(ctx context.Context, sourceURL, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := devicekitDownloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("returned %s", resp.Status)
	}

	zipFile, err := os.CreateTemp(dir, ".devicekit-*.zip")
	if err != nil {
		return "", fmt.Errorf("create download: %w", err)
	}
	zipPath := zipFile.Name()
	if _, err := io.Copy(zipFile, resp.Body); err != nil {
		zipFile.Close()
		os.Remove(zipPath)
		return "", fmt.Errorf("write download: %w", err)
	}
	if err := zipFile.Close(); err != nil {
		os.Remove(zipPath)
		return "", fmt.Errorf("write download: %w", err)
	}
	if err := checkDevicekitZip(zipPath); err != nil {
		os.Remove(zipPath)
		return "", err
	}
	return zipPath, nil
}

// checkDevicekitZip reports whether the archive contains the runner bundle.
func checkDevicekitZip(zipPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	want := devicekitRunnerAppName + "/Info.plist"
	for _, file := range reader.File {
		if file.Name == want {
			return nil
		}
	}
	return fmt.Errorf("zip does not contain %s", devicekitRunnerAppName)
}

// extractDevicekitRunner extracts only the runner app from the release zip,
// rejecting entries that would escape destDir.
func extractDevicekitRunner(zipPath, destDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open devicekit zip: %w", err)
	}
	defer reader.Close()

	root := devicekitRunnerAppName + "/"
	destRoot := filepath.Clean(destDir) + string(os.PathSeparator)
	foundInfo := false
	for _, file := range reader.File {
		if file.Name != root && !strings.HasPrefix(file.Name, root) {
			continue
		}
		if strings.Contains(file.Name, "..") {
			return fmt.Errorf("devicekit zip contains an unsafe path %q", file.Name)
		}
		target := filepath.Join(destDir, filepath.FromSlash(file.Name))
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), destRoot) {
			return fmt.Errorf("devicekit zip entry %q escapes the destination", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create devicekit dir: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create devicekit dir: %w", err)
		}
		src, err := file.Open()
		if err != nil {
			return fmt.Errorf("open devicekit zip entry: %w", err)
		}
		mode := file.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			src.Close()
			return fmt.Errorf("create devicekit file: %w", err)
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		if closeErr := dst.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return fmt.Errorf("extract devicekit file: %w", copyErr)
		}
		if filepath.Base(target) == "Info.plist" && filepath.Dir(target) == filepath.Join(destDir, devicekitRunnerAppName) {
			foundInfo = true
		}
	}
	if !foundInfo {
		return fmt.Errorf("devicekit zip does not contain %s", devicekitRunnerAppName)
	}
	return nil
}

// terminateSimulatorRunner stops the runner on one simulator. The error is
// ignored because it is often not running.
func terminateSimulatorRunner(udid string) {
	if !simulatorsSupported() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), simulatorToolTimeout)
	defer cancel()
	_, _ = runSimctl(ctx, "terminate", udid, devicekitRunnerBundleID)
}

// waitSimulatorPortReleased gives a terminated runner a moment to close its
// listener, so a restart keeps its assigned port instead of walking upward.
func waitSimulatorPortReleased(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && devicekitHealthy(port) {
		time.Sleep(100 * time.Millisecond)
	}
}

// devicekitHealthy reports whether the runner answers its health check.
func devicekitHealthy(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16))
	return strings.TrimSpace(string(body)) == "OK"
}

// simulatorRunnerInstalled reports whether the pinned runner version is
// already installed on the simulator, so a normal restart can skip the install.
func simulatorRunnerInstalled(udid string) bool {
	if !simulatorsSupported() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), simulatorToolTimeout)
	defer cancel()
	output, err := runSimctl(ctx, "get_app_container", udid, devicekitRunnerBundleID)
	if err != nil {
		return false
	}
	infoPlist := filepath.Join(strings.TrimSpace(string(output)), "Info.plist")
	cmd := NewCommandContext(ctx, "plutil", "-extract", "CFBundleShortVersionString", "raw", infoPlist)
	version, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(version)) == devicekitVersion
}

// launchSimulatorRunner installs the pinned runner on the simulator and starts
// it on port, then waits for the health endpoint. An already installed pinned
// version is reused.
func launchSimulatorRunner(udid string, port int) error {
	if !simulatorsSupported() {
		return errors.New("iOS simulators are only supported on macOS")
	}

	ctx, cancel := context.WithTimeout(context.Background(), simulatorInstallTimeout)
	defer cancel()

	// A running runner keeps its old port; stop it so the launch below takes
	// effect. The error is ignored because it may not be running.
	_, _ = runSimctl(ctx, "terminate", udid, devicekitRunnerBundleID)
	if !simulatorRunnerInstalled(udid) {
		appPath, err := ensureDevicekitRunner()
		if err != nil {
			return err
		}
		if _, err := runSimctl(ctx, "install", udid, appPath); err != nil {
			return errors.Wrap(err, "install devicekit runner")
		}
	}

	cmd := NewCommandContext(ctx, "xcrun", "simctl", "launch", udid, devicekitRunnerBundleID)
	cmd.Env = append(os.Environ(), fmt.Sprintf("SIMCTL_CHILD_DEVICEKIT_LISTEN_PORT=%d", port))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launch devicekit runner: %w: %s", err, strings.TrimSpace(string(output)))
	}

	deadline := time.Now().Add(devicekitHealthTimeout)
	for time.Now().Before(deadline) {
		if devicekitHealthy(port) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("devicekit runner did not become healthy on port %d within %s", port, devicekitHealthTimeout)
}

// simulatorApp is the subset of a `simctl listapps` entry we need.
type simulatorApp struct {
	ApplicationType string `json:"ApplicationType"`
}

// parseSimulatorAppList filters simctl listapps JSON by scope. The map key is
// the bundle identifier.
func parseSimulatorAppList(data []byte, scope AppScope) ([]string, error) {
	if !scope.Valid() {
		return nil, fmt.Errorf("invalid app scope: %s", scope)
	}
	var entries map[string]simulatorApp
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, errors.Wrap(err, "parse simctl listapps")
	}
	apps := make([]string, 0, len(entries))
	for bundleID, app := range entries {
		if !iosBundleIDPattern.MatchString(bundleID) {
			continue
		}
		switch scope {
		case AppScopeUser:
			if app.ApplicationType != "User" {
				continue
			}
		case AppScopeSystem:
			if app.ApplicationType != "System" {
				continue
			}
		}
		apps = append(apps, bundleID)
	}
	sort.Strings(apps)
	return apps, nil
}

// listSimulatorApps lists installed simulator apps via simctl. simctl prints an
// ASCII plist, which plutil converts to the JSON parsed below.
func (d *IOSDevice) listSimulatorApps(scope AppScope) ([]string, error) {
	if !scope.Valid() {
		return nil, fmt.Errorf("invalid app scope: %s", scope)
	}
	ctx, cancel := context.WithTimeout(context.Background(), simulatorToolTimeout)
	defer cancel()
	output, err := runSimctl(ctx, "listapps", d.udid)
	if err != nil {
		return nil, errors.Wrap(err, "list simulator apps")
	}

	cmd := NewCommandContext(ctx, "plutil", "-convert", "json", "-o", "-", "-")
	cmd.Stdin = bytes.NewReader(output)
	jsonOutput, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrap(err, "convert simulator app list")
	}
	return parseSimulatorAppList(jsonOutput, scope)
}
