package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxManifestSize = 1 << 20

// Check reads the latest release manifest and resolves the asset for this
// platform when an update is available and the client is a desktop app.
func (c *Client) Check(ctx context.Context, current string) (Info, error) {
	manifest, err := c.latest(ctx)
	if err != nil {
		return Info{}, err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(manifest.Version), "v")
	if latest == "" {
		return Info{}, errors.New("release manifest has no version")
	}
	current = strings.TrimSpace(current)
	info := Info{
		Current:     current,
		Latest:      latest,
		Dev:         isDevVersion(current),
		ReleaseURL:  strings.TrimSpace(manifest.ReleaseURL),
		Notes:       strings.TrimSpace(manifest.Notes),
		PublishedAt: strings.TrimSpace(manifest.PublishedAt),
		Desktop:     c.desktop,
	}
	info.Available = isNewer(latest, current)
	if !info.Available || !c.desktop {
		return info, nil
	}
	assetName, err := AssetName(c.goos, c.goarch, latest)
	if err != nil {
		return info, err
	}
	for _, asset := range manifest.Assets {
		if asset.Name != assetName {
			continue
		}
		if strings.TrimSpace(asset.SHA256) == "" {
			return info, fmt.Errorf("release %s has no sha256 for %s", latest, assetName)
		}
		info.Asset = asset.Name
		info.AssetSize = asset.Size
		info.SHA256 = strings.ToLower(strings.TrimSpace(asset.SHA256))
		info.DownloadURL = c.assetURL(latest, asset.Name)
		return info, nil
	}
	return info, fmt.Errorf("release %s does not include %s", latest, assetName)
}

func (c *Client) latest(ctx context.Context) (Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.manifestURL, nil)
	if err != nil {
		return Manifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return Manifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize))
	if err != nil {
		return Manifest{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Manifest{}, fmt.Errorf("read release manifest: HTTP %d", resp.StatusCode)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}
