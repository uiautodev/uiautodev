package webserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/version"

	"github.com/labstack/echo/v4"
)

// defaultVersionCheckURL is the release manifest returning the latest published
// version. It is a variable so tests can point it at a stub server.
var defaultVersionCheckURL = "https://dl.uiauto.dev/updates/latest.json"

// versionCheckURL is the effective endpoint, overridable in tests.
var versionCheckURL = defaultVersionCheckURL

// versionCheckTTL is how long a cached result is trusted before querying the
// release service again.
const versionCheckTTL = 24 * time.Hour

// versionCheckMu serializes the recheck/fetch/persist sequence so concurrent
// requests cannot all observe an expired cache and stampede the release
// service. Callers that arrive while a refresh is in flight wait on the lock
// and then serve the freshly persisted result.
var versionCheckMu sync.Mutex

// versionCheckHTTPTimeout caps a single release service request.
const versionCheckHTTPTimeout = 10 * time.Second

// remoteVersionInfo is the release service payload; only the version is used.
type remoteVersionInfo struct {
	Version string `json:"version"`
}

// VersionCheckResult is the response payload for GET /api/v1/version/latest.
type VersionCheckResult struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// fetchLatestVersion queries the release service for the latest version.
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: versionCheckHTTPTimeout}
	req, err := http.NewRequest(http.MethodGet, versionCheckURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uiautodev/"+version.Version)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version check failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var info remoteVersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("invalid version response: %w", err)
	}
	if info.Version == "" {
		return "", fmt.Errorf("invalid version response: empty version")
	}
	return info.Version, nil
}

// errVersionCheckRecentlyFailed is returned when a previous refresh failed
// within TTL and no cached version is available to fall back to.
var errVersionCheckRecentlyFailed = errors.New("version check failed recently and no cached version is available")

// latestVersion returns the latest version, querying the release service at
// most once per versionCheckTTL. The attempt timestamp is persisted even when
// the refresh fails, so an outage does not cause a retry (and its timeout) on
// every request. A failed refresh falls back to the last cached value when one
// exists.
func latestVersion() (string, error) {
	versionCheckMu.Lock()
	defer versionCheckMu.Unlock()

	rec, err := getVersionCheck()
	if err != nil {
		return "", err
	}
	if rec.LastCheckAt != nil && time.Since(*rec.LastCheckAt) < versionCheckTTL {
		if rec.LatestVersion != "" {
			return rec.LatestVersion, nil
		}
		return "", errVersionCheckRecentlyFailed
	}

	latest, err := fetchLatestVersion()
	now := time.Now()
	if err != nil {
		if setErr := setVersionCheck(VersionCheckRecord{LastCheckAt: &now, LatestVersion: rec.LatestVersion}); setErr != nil {
			slog.Warn("failed to persist version check attempt", "error", setErr)
		}
		if rec.LatestVersion != "" {
			slog.Warn("version check refresh failed, using cached value", "error", err)
			return rec.LatestVersion, nil
		}
		return "", err
	}

	if err := setVersionCheck(VersionCheckRecord{LastCheckAt: &now, LatestVersion: latest}); err != nil {
		slog.Warn("failed to persist version check result", "error", err)
	}
	return latest, nil
}

// versionLatestHandler handles GET /api/v1/version/latest.
func versionLatestHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		latest, err := latestVersion()
		if err != nil {
			return c.JSONPretty(http.StatusBadGateway, APIResponse{
				Code:    502,
				Message: "version check failed: " + err.Error(),
			}, "  ")
		}
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data: VersionCheckResult{
				CurrentVersion:  version.Version,
				LatestVersion:   latest,
				UpdateAvailable: isNewerVersion(latest, version.Version),
			},
		}, "  ")
	}
}
