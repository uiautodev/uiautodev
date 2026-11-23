package update

import (
	"os"
	"path/filepath"
	"strings"
)

// CleanupOldExecutables removes leftover .old-* backups next to the running
// executable. Windows updates rename the previous executable before replacing
// it; the old process still needs that backup until it exits, so the new
// process cleans it up on startup.
func CleanupOldExecutables() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cleanupOldExecutables(exe)
}

func cleanupOldExecutables(exe string) {
	dir := filepath.Dir(exe)
	base := filepath.Base(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), base+".old-") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}
