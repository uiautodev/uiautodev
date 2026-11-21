package device

import (
	"bytes"
	"strings"
	"testing"
)

func TestYadbBinaryEmbedded(t *testing.T) {
	if yadbVersion == "" {
		t.Fatal("yadbVersion should not be empty")
	}
	if len(yadbBinary) < 2 {
		t.Fatalf("yadbBinary too small: %d bytes", len(yadbBinary))
	}
	// yadb is distributed as an APK/DEX archive (zip container).
	if !bytes.HasPrefix(yadbBinary, []byte("PK")) {
		t.Fatalf("yadbBinary does not look like a zip archive, got prefix %q", yadbBinary[:2])
	}
	// The remote path embeds the version so a version bump forces a re-push.
	if !strings.Contains(yadbRemotePath, yadbVersion) {
		t.Fatalf("yadbRemotePath %q does not embed version %q", yadbRemotePath, yadbVersion)
	}
}
