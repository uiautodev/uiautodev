package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOldExecutables(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "uiautodev-desktop.exe")
	require.NoError(t, os.WriteFile(exe, []byte("current"), 0o755))

	oldBackup := exe + ".old-100"
	otherBackup := exe + ".old-200"
	otherFile := filepath.Join(dir, "other.exe")
	require.NoError(t, os.WriteFile(oldBackup, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(otherBackup, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(otherFile, []byte("keep"), 0o755))

	cleanupOldExecutables(exe)

	_, err := os.Stat(oldBackup)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(otherBackup)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(exe)
	require.NoError(t, err)
	_, err = os.Stat(otherFile)
	require.NoError(t, err)
}
