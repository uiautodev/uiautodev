package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildManifestHashesAssets(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	write("UiautodevDesktop-mac-1.2.3.dmg", "mac")
	write("UiautodevDesktop-windows-1.2.3.zip", "windows")
	write("release.json", "{}")
	write("SHA256SUMS", "ignored")
	write(".DS_Store", "ignored")
	write("1.2.3", "")

	manifest, err := BuildManifest(dir, "v1.2.3", "", "notes", "https://example.com/release", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", manifest.Version)
	assert.Equal(t, "1.2.3", manifest.Tag)
	assert.Equal(t, "notes", manifest.Notes)
	assert.Len(t, manifest.Assets, 2)

	byName := map[string]Asset{}
	for _, asset := range manifest.Assets {
		byName[asset.Name] = asset
	}
	mac := byName["UiautodevDesktop-mac-1.2.3.dmg"]
	sum := sha256.Sum256([]byte("mac"))
	assert.Equal(t, hex.EncodeToString(sum[:]), mac.SHA256)
	assert.Equal(t, int64(3), mac.Size)
	assert.Contains(t, byName, "UiautodevDesktop-windows-1.2.3.zip")
}

func TestBuildManifestRequiresAssets(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "release.json"), []byte("{}"), 0o644))
	_, err := BuildManifest(dir, "1.2.3", "", "", "", "")
	require.Error(t, err)
}

func TestReadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.json")
	manifest := Manifest{Version: "1.2.3", Assets: []Asset{{Name: "app.dmg", Size: 1, SHA256: "abc"}}}
	require.NoError(t, WriteManifest(path, manifest))

	read, err := ReadManifest(path)
	require.NoError(t, err)
	assert.Equal(t, manifest, read)

	_, err = ReadManifest(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)

	bad := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{"), 0o644))
	_, err = ReadManifest(bad)
	require.Error(t, err)
}

func TestWriteManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "release.json")
	manifest := Manifest{Version: "1.2.3", Assets: []Asset{{Name: "app.dmg", Size: 1, SHA256: "abc"}}}
	require.NoError(t, WriteManifest(path, manifest))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var decoded Manifest
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, manifest, decoded)
}
