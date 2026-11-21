package update

import (
	"fmt"
	"strings"
)

// AssetName returns the desktop asset for the current platform.
func AssetName(goos, goarch, version string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	switch goos {
	case "darwin":
		return "UiautodevDesktop-mac-" + version + ".dmg", nil
	case "windows":
		return "UiautodevDesktop-windows-" + version + ".zip", nil
	default:
		return "", fmt.Errorf("desktop updates are not available for %s/%s", goos, goarch)
	}
}

// appBundle returns the path to the .app directory containing exe.
func appBundle(exe string) string {
	marker := ".app/Contents/MacOS/"
	if i := strings.Index(exe, marker); i >= 0 {
		return exe[:i+len(".app")]
	}
	return ""
}
