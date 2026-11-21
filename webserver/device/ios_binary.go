package device

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/adrg/xdg"

	"github.com/uiautodev/uiautodev/webserver/utils"
)

// iosDownloadBaseURL is the base URL for lazily downloaded helper binaries.
var iosDownloadBaseURL = "https://dl.uiauto.dev/binaries"

var (
	iosBinaryMu   sync.Mutex
	iosBinaryPath string
)

func iosBinaryName() string {
	if runtime.GOOS == "windows" {
		return "ios.exe"
	}
	return "ios"
}

func iosDownloadURL(goos string) (string, error) {
	switch goos {
	case "darwin":
		return iosDownloadBaseURL + "/darwin/ios", nil
	case "windows":
		return iosDownloadBaseURL + "/windows/ios.exe", nil
	default:
		return "", fmt.Errorf("no prebuilt ios helper for %s", goos)
	}
}

// IOSPath returns the path to the ios helper. It prefers a binary bundled with
// the app (older installs), then a cached download, and finally downloads it
// from iosDownloadBaseURL. The download is done lazily on first use because the
// binary is too large to ship in every installer.
func IOSPath() (string, error) {
	iosBinaryMu.Lock()
	defer iosBinaryMu.Unlock()

	if iosBinaryPath != "" && utils.IsExists(iosBinaryPath) {
		return iosBinaryPath, nil
	}

	if bundled := utils.FindBinary(iosBinaryName()); filepath.IsAbs(bundled) && utils.IsExists(bundled) {
		iosBinaryPath = bundled
		return bundled, nil
	}

	cachePath, err := iosCachePath()
	if err != nil {
		return "", err
	}
	if utils.IsExists(cachePath) {
		iosBinaryPath = cachePath
		return cachePath, nil
	}

	if err := downloadIOSBinary(cachePath); err != nil {
		return "", err
	}
	iosBinaryPath = cachePath
	return cachePath, nil
}

func iosCachePath() (string, error) {
	dir := filepath.Join(xdg.DataHome, "uiautodev", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create ios cache dir: %w", err)
	}
	return filepath.Join(dir, iosBinaryName()), nil
}

func downloadIOSBinary(dst string) error {
	url, err := iosDownloadURL(runtime.GOOS)
	if err != nil {
		return err
	}

	slog.Info("downloading ios helper", "url", url, "path", dst)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download ios helper: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download ios helper: %s returned %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".ios-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write ios helper: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close ios helper: %w", err)
	}
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod ios helper: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("install ios helper: %w", err)
	}
	slog.Info("ios helper downloaded", "path", dst)
	return nil
}
