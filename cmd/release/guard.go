package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Masterminds/semver"
)

// shouldPublishLatest decides whether this manifest should be published to
// updates/latest.json. It refuses when the online version is higher, so an old
// release is never pushed by mistake and clients never get a downgrade update.
// Failures to read the online manifest do not block publishing (it returns
// true) and only report the reason.
func shouldPublishLatest(ctx context.Context, client *http.Client, url, newVersion string) (bool, string) {
	if url == "" {
		return true, "no online manifest check configured, publishing directly"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return true, fmt.Sprintf("failed to build online manifest request, publishing anyway: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return true, fmt.Sprintf("failed to read online manifest, publishing anyway: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true, fmt.Sprintf("online manifest returned %s, publishing anyway", resp.Status)
	}

	var current struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&current); err != nil {
		return true, fmt.Sprintf("failed to parse online manifest, publishing anyway: %v", err)
	}

	currentVersion := strings.TrimPrefix(strings.TrimSpace(current.Version), "v")
	if currentVersion == "" {
		return true, "online manifest has no version, publishing anyway"
	}

	online, err := semver.NewVersion(currentVersion)
	if err != nil {
		return true, fmt.Sprintf("cannot parse online version %q, publishing anyway", currentVersion)
	}
	release, err := semver.NewVersion(newVersion)
	if err != nil {
		return true, fmt.Sprintf("cannot parse new version %q, publishing anyway", newVersion)
	}

	if online.GreaterThan(release) {
		return false, fmt.Sprintf("online version %s is higher than %s, skipping latest.json update", currentVersion, newVersion)
	}
	return true, fmt.Sprintf("online version %s, new version %s, updating latest.json", currentVersion, newVersion)
}
