//go:build darwin

package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppBundle(t *testing.T) {
	assert.Equal(t,
		"/Applications/Uiautodev Desktop.app",
		appBundle("/Applications/Uiautodev Desktop.app/Contents/MacOS/uiautodev-desktop"))
	assert.Empty(t, appBundle("/usr/local/bin/uiautodev-server"))
}

func TestFindAppBundle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Uiautodev Desktop.app", "Contents"), 0o755))

	found, err := findAppBundle(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Uiautodev Desktop.app"), found)
}

func TestFindAppBundleMissing(t *testing.T) {
	_, err := findAppBundle(t.TempDir())
	require.Error(t, err)
}
