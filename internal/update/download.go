package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const maxDownloadSize = 1 << 30

func (c *Client) assetURL(version, name string) string {
	return c.baseURL + "/" + url.PathEscape(version) + "/" + url.PathEscape(name)
}

func (c *Client) download(ctx context.Context, rawURL, expectedSHA string, knownTotal int64, dest string) error {
	expected := strings.ToLower(strings.TrimSpace(expectedSHA))
	if expected == "" {
		return errors.New("download update: sha256 is missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download update: HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".uiautodev-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hash := sha256.New()
	total := resp.ContentLength
	if total <= 0 {
		total = knownTotal
	}
	progress := &progressWriter{client: c, total: total}
	written, err := io.Copy(io.MultiWriter(tmp, hash, progress), io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		tmp.Close()
		return err
	}
	if written > maxDownloadSize {
		tmp.Close()
		return errors.New("download update: asset is too large")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(dest))
	}
	return os.Rename(tmpName, dest)
}

type progressWriter struct {
	client     *Client
	total      int64
	downloaded int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.downloaded += int64(len(p))
	percent := 0.0
	if w.total > 0 {
		percent = float64(w.downloaded) / float64(w.total) * 100
		if percent > 100 {
			percent = 100
		}
	}
	w.client.setProgress(Progress{Active: true, Percent: percent})
	return len(p), nil
}
