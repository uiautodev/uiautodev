package device

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
)

// devicekitVersion is the pinned devicekit-ios release whose prebuilt runners
// are downloaded on first use, because the bundles are too large to ship and
// are licensed under FSL-1.1 (see the README).
const devicekitVersion = "0.0.38"

// devicekitDownloadTimeout bounds downloading a runner across all sources.
const devicekitDownloadTimeout = 10 * time.Minute

// devicekitTarget is the kind of Apple platform a prebuilt runner is built for.
type devicekitTarget string

const (
	devicekitTargetSimulator devicekitTarget = "sim"
	devicekitTargetDevice    devicekitTarget = "device"
)

// Valid reports whether the target is known.
func (t devicekitTarget) Valid() bool {
	return t == devicekitTargetSimulator || t == devicekitTargetDevice
}

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

// devicekitAsset pins one release archive: its name, exact size and SHA-256.
// The runner is installed and executed inside the simulator, so a mirror that
// serves different bytes must never be accepted, even though the mirrors are
// tried before github.com.
type devicekitAsset struct {
	Name   string
	Size   int64
	SHA256 string
}

// devicekitSimulatorAssets maps GOARCH to the pinned release assets of
// devicekitVersion. Update the hashes together with the version.
var devicekitSimulatorAssets = map[string]devicekitAsset{
	"arm64": {
		Name:   "devicekit-ios-Sim-arm64.zip",
		Size:   10270152,
		SHA256: "2eb7d66af4023f209e53a9305273198aa343249f35bbc601e8a927a290b4b076",
	},
	"amd64": {
		Name:   "devicekit-ios-Sim-x86_64.zip",
		Size:   10453967,
		SHA256: "063a2c7457638d12a493e79546a8825ec1914d6415398fbf941454ad814a8eb3",
	},
}

// devicekitAssetFor returns the pinned release asset for a target and host
// architecture. Real devices are recognised as a target but their runner is not
// wired up yet.
func devicekitAssetFor(target devicekitTarget, goarch string) (devicekitAsset, error) {
	if !target.Valid() {
		return devicekitAsset{}, fmt.Errorf("invalid devicekit target %q", target)
	}
	if target == devicekitTargetDevice {
		return devicekitAsset{}, fmt.Errorf("devicekit runner for real devices is not supported yet")
	}
	asset, ok := devicekitSimulatorAssets[goarch]
	if !ok {
		return devicekitAsset{}, fmt.Errorf("no devicekit runner for architecture %s", goarch)
	}
	return asset, nil
}

// devicekitAssetName returns the release asset name for a target and host
// architecture.
func devicekitAssetName(target devicekitTarget, goarch string) (string, error) {
	asset, err := devicekitAssetFor(target, goarch)
	if err != nil {
		return "", err
	}
	return asset.Name, nil
}

// devicekitDownloadURL returns the GitHub asset URL for a target and GOARCH.
func devicekitDownloadURL(target devicekitTarget, goarch string) (string, error) {
	asset, err := devicekitAssetFor(target, goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s", devicekitDownloadBaseURL, devicekitVersion, asset.Name), nil
}

// devicekitDownloadURLs returns the candidate URLs for a target and GOARCH: the
// public mirrors first, then the direct GitHub asset as the last resort.
func devicekitDownloadURLs(target devicekitTarget, goarch string) ([]string, error) {
	githubURL, err := devicekitDownloadURL(target, goarch)
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(devicekitMirrors)+1)
	for _, mirror := range devicekitMirrors {
		urls = append(urls, mirror(githubURL))
	}
	return append(urls, githubURL), nil
}

// devicekitCacheDir is the directory holding the downloaded runner of the pinned
// devicekit version for a target.
func devicekitCacheDir(target devicekitTarget) string {
	return filepath.Join(xdg.DataHome, "uiautodev", "devicekit-ios", string(target), devicekitVersion)
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

// downloadDevicekitRunner downloads the runner archive from the first candidate
// URL that serves the pinned archive and returns its path inside dir. A mirror
// error page, an oversized archive or modified bytes are rejected instead of
// being installed.
func downloadDevicekitRunner(ctx context.Context, urls []string, dir string, asset devicekitAsset) (string, error) {
	failures := make([]string, 0, len(urls))
	for _, sourceURL := range urls {
		slog.Info("downloading devicekit runner", "url", sourceURL)
		zipPath, err := downloadDevicekitZip(ctx, sourceURL, dir, asset)
		if err == nil {
			return zipPath, nil
		}
		slog.Warn("devicekit runner download failed, trying next source", "url", sourceURL, "error", err)
		failures = append(failures, fmt.Sprintf("%s: %v", sourceURL, err))
	}
	return "", fmt.Errorf("download devicekit runner: %s", strings.Join(failures, "; "))
}

// downloadDevicekitZip fetches one URL into a temporary file and verifies its
// size, SHA-256 and runner entry before returning it.
func downloadDevicekitZip(ctx context.Context, sourceURL, dir string, asset devicekitAsset) (string, error) {
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
	if resp.ContentLength > asset.Size {
		return "", fmt.Errorf("archive is %d bytes, want at most %d", resp.ContentLength, asset.Size)
	}

	file, err := os.CreateTemp(dir, ".devicekit-*")
	if err != nil {
		return "", fmt.Errorf("create download: %w", err)
	}
	path := file.Name()
	hasher := sha256.New()
	// Read one byte past the pinned size so an endless response cannot fill the
	// disk before the checksum is checked.
	written, err := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(resp.Body, asset.Size+1))
	if err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("write download: %w", err)
	}
	if written > asset.Size {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("archive exceeds the pinned size of %d bytes", asset.Size)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("write download: %w", err)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(digest, asset.SHA256) {
		os.Remove(path)
		return "", fmt.Errorf("archive SHA-256 is %s, want %s", digest, asset.SHA256)
	}
	if err := checkZipEntry(path, asset.Name+"/Info.plist"); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// checkZipEntry reports whether the archive contains the named entry.
func checkZipEntry(zipPath, entry string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name == entry {
			return nil
		}
	}
	return fmt.Errorf("zip does not contain %s", entry)
}
