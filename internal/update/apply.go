package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Apply downloads, verifies and installs the update described by info.
func (c *Client) Apply(ctx context.Context, info Info) (Result, error) {
	if !c.desktop {
		return Result{}, errors.New("in-app updates are only available in the desktop app")
	}
	if !info.Available {
		return Result{}, errors.New("no update available")
	}
	if info.DownloadURL == "" || info.Asset == "" || info.SHA256 == "" {
		return Result{}, errors.New("update asset is incomplete")
	}
	if c.dataDir == "" {
		return Result{}, errors.New("update data directory is not configured")
	}
	if c.exe == "" {
		return Result{}, errors.New("executable path is unknown")
	}
	dir := filepath.Join(c.dataDir, "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.reportError(err)
		return Result{}, err
	}
	dest := filepath.Join(dir, filepath.Base(info.Asset))
	c.setProgress(Progress{Active: true})
	if err := c.download(ctx, info.DownloadURL, info.SHA256, info.AssetSize, dest); err != nil {
		_ = os.Remove(dest)
		c.reportError(err)
		return Result{}, err
	}
	defer os.Remove(dest)
	c.setProgress(Progress{Active: true, Percent: 100})
	if err := c.install(dest, c.exe); err != nil {
		c.reportError(err)
		return Result{}, err
	}
	c.setProgress(Progress{Active: false, Percent: 100})
	return Result{Path: c.exe, Installed: true}, nil
}

// Restart launches the updated app and exits the current process. The helper
// waits briefly so the old process releases its HTTP port first.
func (c *Client) Restart() error {
	if c.exe == "" {
		return errors.New("executable path is unknown")
	}
	if err := c.relaunch(c.exe); err != nil {
		c.reportError(err)
		return err
	}
	if c.quit != nil {
		c.quit()
		return nil
	}
	os.Exit(0)
	return nil
}
