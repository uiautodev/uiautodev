package device

import (
	"archive/zip"
	"context"
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

// devicekitAssetName returns the release asset for a target and host
// architecture. Real devices are recognised as a target but their runner is not
// wired up yet.
func devicekitAssetName(target devicekitTarget, goarch string) (string, error) {
	if !target.Valid() {
		return "", fmt.Errorf("invalid devicekit target %q", target)
	}
	if target == devicekitTargetDevice {
		return "", fmt.Errorf("devicekit runner for real devices is not supported yet")
	}
	switch goarch {
	case "arm64":
		return "devicekit-ios-Sim-arm64.zip", nil
	case "amd64":
		return "devicekit-ios-Sim-x86_64.zip", nil
	}
	return "", fmt.Errorf("no devicekit runner for architecture %s", goarch)
}

// devicekitDownloadURL returns the GitHub asset URL for a target and GOARCH.
func devicekitDownloadURL(target devicekitTarget, goarch string) (string, error) {
	asset, err := devicekitAssetName(target, goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s", devicekitDownloadBaseURL, devicekitVersion, asset), nil
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
// URL that serves a valid archive and returns its path inside dir. requiredEntry
// is an archive entry that must be present, so an error page served by a mirror
// is rejected instead of being installed.
func downloadDevicekitRunner(ctx context.Context, urls []string, dir, requiredEntry string) (string, error) {
	failures := make([]string, 0, len(urls))
	for _, sourceURL := range urls {
		slog.Info("downloading devicekit runner", "url", sourceURL)
		zipPath, err := downloadDevicekitZip(ctx, sourceURL, dir, requiredEntry)
		if err == nil {
			return zipPath, nil
		}
		slog.Warn("devicekit runner download failed, trying next source", "url", sourceURL, "error", err)
		failures = append(failures, fmt.Sprintf("%s: %v", sourceURL, err))
	}
	return "", fmt.Errorf("download devicekit runner: %s", strings.Join(failures, "; "))
}

// downloadDevicekitZip fetches one URL into a temporary file and verifies it
// contains requiredEntry.
func downloadDevicekitZip(ctx context.Context, sourceURL, dir, requiredEntry string) (string, error) {
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

	file, err := os.CreateTemp(dir, ".devicekit-*")
	if err != nil {
		return "", fmt.Errorf("create download: %w", err)
	}
	path := file.Name()
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("write download: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("write download: %w", err)
	}
	if err := checkZipEntry(path, requiredEntry); err != nil {
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
