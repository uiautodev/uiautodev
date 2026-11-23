//go:build darwin

package update

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func installDesktopAsset(assetPath, exe string) error {
	bundle := appBundle(exe)
	if bundle == "" {
		return errors.New("cannot locate the app bundle")
	}
	if err := ensureWritable(filepath.Dir(bundle)); err != nil {
		return fmt.Errorf("应用所在目录不可写，请先把应用拖入 Applications 后再更新: %w", err)
	}
	mount, err := os.MkdirTemp("", "uiautodev-dmg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mount)
	attach := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-quiet", "-mountpoint", mount, assetPath)
	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("mount update image: %v: %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("hdiutil", "detach", "-quiet", mount).Run()

	sourceApp, err := findAppBundle(mount)
	if err != nil {
		return err
	}
	if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", sourceApp).CombinedOutput(); err != nil {
		return fmt.Errorf("verify update signature: %v: %s", err, strings.TrimSpace(string(out)))
	}

	dest := filepath.Join(filepath.Dir(bundle), "."+filepath.Base(bundle)+".new-"+stamp())
	defer os.RemoveAll(dest)
	if out, err := exec.Command("ditto", sourceApp, dest).CombinedOutput(); err != nil {
		return fmt.Errorf("copy update: %v: %s", err, strings.TrimSpace(string(out)))
	}

	backup := bundle + ".old-" + stamp()
	if err := os.Rename(bundle, backup); err != nil {
		return fmt.Errorf("replace app bundle: %w", err)
	}
	if err := os.Rename(dest, bundle); err != nil {
		_ = os.Rename(backup, bundle)
		return err
	}
	if err := os.RemoveAll(backup); err != nil {
		slog.Warn("failed to remove old app bundle", "path", backup, "error", err)
	}
	return nil
}

func findAppBundle(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", errors.New("update image does not contain an app bundle")
}

func ensureWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".uiautodev-write-test-*")
	if err != nil {
		return err
	}
	name := file.Name()
	file.Close()
	return os.Remove(name)
}

func stamp() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
