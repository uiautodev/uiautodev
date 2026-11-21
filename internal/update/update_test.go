package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetName(t *testing.T) {
	name, err := AssetName("darwin", "arm64", "1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "UiautodevDesktop-mac-1.2.3.dmg", name)

	name, err = AssetName("windows", "amd64", "v1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "UiautodevDesktop-windows-1.2.3.zip", name)

	_, err = AssetName("linux", "amd64", "1.2.3")
	require.Error(t, err)
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"1.0.4", "1.0.3", true},
		{"1.1.0", "1.0.9", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.3", "1.0.3", false},
		{"v1.0.3", "1.0.3", false},
		{"1.0.3", "1.0.3-2-gabc", false},
		{"1.0.4", "1.0.4-2-gabc", false},
		{"1.0.4", "dev", false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, isNewer(tc.latest, tc.current), "%s > %s", tc.latest, tc.current)
	}
}

func manifestServer(t *testing.T, manifest Manifest, assets map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/updates/latest.json" {
			data, err := json.Marshal(manifest)
			require.NoError(t, err)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
		name := filepath.Base(r.URL.Path)
		body, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestCheckResolvesDesktopAsset(t *testing.T) {
	manifest := Manifest{
		Version:    "1.1.0",
		ReleaseURL: "https://example.com/releases/1.1.0",
		Notes:      "changes",
		Assets: []Asset{{
			Name:   "UiautodevDesktop-mac-1.1.0.dmg",
			Size:   5,
			SHA256: "abc",
		}},
	}
	server := manifestServer(t, manifest, nil)
	defer server.Close()

	client := New(Options{
		ManifestURL: server.URL + "/updates/latest.json",
		BaseURL:     server.URL,
		Desktop:     true,
		GOOS:        "darwin",
		GOARCH:      "arm64",
	})
	info, err := client.Check(context.Background(), "1.0.0")
	require.NoError(t, err)
	assert.True(t, info.Available)
	assert.True(t, info.Desktop)
	assert.Equal(t, "1.1.0", info.Latest)
	assert.Equal(t, "changes", info.Notes)
	assert.Equal(t, "UiautodevDesktop-mac-1.1.0.dmg", info.Asset)
	assert.Equal(t, server.URL+"/1.1.0/UiautodevDesktop-mac-1.1.0.dmg", info.DownloadURL)
}

func TestCheckMissingSHA(t *testing.T) {
	manifest := Manifest{
		Version: "1.1.0",
		Assets:  []Asset{{Name: "UiautodevDesktop-mac-1.1.0.dmg", Size: 5}},
	}
	server := manifestServer(t, manifest, nil)
	defer server.Close()

	client := New(Options{
		ManifestURL: server.URL + "/updates/latest.json",
		BaseURL:     server.URL,
		Desktop:     true,
		GOOS:        "darwin",
		GOARCH:      "arm64",
	})
	_, err := client.Check(context.Background(), "1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha256")
}

func TestCheckUpToDate(t *testing.T) {
	manifest := Manifest{Version: "1.0.0"}
	server := manifestServer(t, manifest, nil)
	defer server.Close()

	client := New(Options{
		ManifestURL: server.URL + "/updates/latest.json",
		BaseURL:     server.URL,
		Desktop:     true,
		GOOS:        "darwin",
		GOARCH:      "arm64",
	})
	info, err := client.Check(context.Background(), "1.0.0")
	require.NoError(t, err)
	assert.False(t, info.Available)
	assert.Empty(t, info.DownloadURL)
}

func TestApplyDownloadsVerifiesAndInstalls(t *testing.T) {
	body := "fake update payload"
	sum := sha256.Sum256([]byte(body))
	name := "UiautodevDesktop-mac-1.1.0.dmg"
	manifest := Manifest{
		Version: "1.1.0",
		Assets:  []Asset{{Name: name, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}},
	}
	server := manifestServer(t, manifest, map[string]string{name: body})
	defer server.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "Uiautodev Desktop.app", "Contents", "MacOS", "uiautodev-desktop")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o755))
	require.NoError(t, os.WriteFile(exe, []byte("old"), 0o755))

	var installed string
	client := New(Options{
		ManifestURL: server.URL + "/updates/latest.json",
		BaseURL:     server.URL,
		DataDir:     filepath.Join(dir, "data"),
		Executable:  exe,
		Desktop:     true,
		GOOS:        "darwin",
		GOARCH:      "arm64",
		Install: func(assetPath, path string) error {
			installed = path
			return nil
		},
	})
	info, err := client.Check(context.Background(), "1.0.0")
	require.NoError(t, err)
	result, err := client.Apply(context.Background(), info)
	require.NoError(t, err)
	assert.True(t, result.Installed)
	assert.Equal(t, exe, installed)
	assert.Equal(t, Progress{Active: false, Percent: 100}, client.Progress())
}

func TestApplyRejectsChecksumMismatch(t *testing.T) {
	body := "fake update payload"
	name := "UiautodevDesktop-mac-1.1.0.dmg"
	manifest := Manifest{
		Version: "1.1.0",
		Assets:  []Asset{{Name: name, Size: int64(len(body)), SHA256: "deadbeef"}},
	}
	server := manifestServer(t, manifest, map[string]string{name: body})
	defer server.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "uiautodev-desktop")
	client := New(Options{
		ManifestURL: server.URL + "/updates/latest.json",
		BaseURL:     server.URL,
		DataDir:     filepath.Join(dir, "data"),
		Executable:  exe,
		Desktop:     true,
		GOOS:        "darwin",
		GOARCH:      "arm64",
		Install:     func(string, string) error { return nil },
	})
	info, err := client.Check(context.Background(), "1.0.0")
	require.NoError(t, err)
	_, err = client.Apply(context.Background(), info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	progress := client.Progress()
	assert.False(t, progress.Active)
	assert.NotEmpty(t, progress.Error)
}
