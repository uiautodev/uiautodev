package webserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uiautodev/uiautodev/version"

	"github.com/Masterminds/semver"
	"github.com/labstack/echo/v4"
)

const (
	// defaultPluginMarketURL is the official plugin registry base URL.
	defaultPluginMarketURL = "https://get.uiauto.dev"
	// marketHTTPTimeout caps a single registry request.
	marketHTTPTimeout = 30 * time.Second
)

// PluginSummary mirrors the Registry plugin summary payload.
type PluginSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Author        string   `json:"author"`
	Description   string   `json:"description"`
	IconURL       string   `json:"icon_url,omitempty"`
	Homepage      string   `json:"homepage,omitempty"`
	Repository    string   `json:"repository,omitempty"`
	License       string   `json:"license,omitempty"`
	Tags          []string `json:"tags"`
	Platforms     []string `json:"platforms"`
	LatestVersion string   `json:"latest_version"`
	Downloads     int      `json:"downloads"`
	UpdatedAt     string   `json:"updated_at"`
}

// PluginVersion mirrors the Registry plugin version payload.
type PluginVersion struct {
	Version       string `json:"version"`
	Sha256        string `json:"sha256"`
	Size          int64  `json:"size"`
	MinAppVersion string `json:"min_app_version,omitempty"`
	Changelog     string `json:"changelog,omitempty"`
	PublishedAt   string `json:"published_at"`
	DownloadURL   string `json:"download_url"`
}

// MarketItem is a PluginSummary enriched with local installation state.
type MarketItem struct {
	PluginSummary
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
	UpdateAvailable  bool   `json:"update_available"`
}

// MarketSourceInfo describes the effective plugin market source.
type MarketSourceInfo struct {
	URL        string `json:"url"`
	DefaultURL string `json:"default_url"`
	IsCustom   bool   `json:"is_custom"`
}

// marketListResponse is the Registry plugin list payload.
type marketListResponse struct {
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Items    []PluginSummary `json:"items"`
}

// marketEnvelope is the shared Registry response envelope.
type marketEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// marketClient is a minimal Registry API client.
type marketClient struct {
	baseURL string
	http    *http.Client
}

func newMarketClient(baseURL string) *marketClient {
	return &marketClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: marketHTTPTimeout},
	}
}

func (c *marketClient) endpoint(path string) string {
	return c.baseURL + "/api/v1/market" + path
}

// getJSON performs a GET request and decodes the envelope data into out.
func (c *marketClient) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uiautodev/"+version.Version)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("market request failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var env marketEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("invalid market response: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("market error %d: %s", env.Code, env.Message)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("invalid market data: %w", err)
		}
	}
	return nil
}

// listPlugins fetches the plugin list from the Registry.
func (c *marketClient) listPlugins(ctx context.Context, query url.Values, out *marketListResponse) error {
	target := c.endpoint("/plugins")
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return c.getJSON(ctx, target, out)
}

// getVersion fetches a single plugin version. An empty version resolves latest.
func (c *marketClient) getVersion(ctx context.Context, id, ver string) (*PluginVersion, error) {
	if ver == "" {
		ver = "latest"
	}
	var pv PluginVersion
	target := c.endpoint("/plugins/" + url.PathEscape(id) + "/versions/" + url.PathEscape(ver))
	if err := c.getJSON(ctx, target, &pv); err != nil {
		return nil, err
	}
	if pv.DownloadURL != "" {
		if abs, err := resolveURL(c.baseURL, pv.DownloadURL); err == nil {
			pv.DownloadURL = abs
		}
	}
	return &pv, nil
}

// downloadToTemp downloads a plugin archive to a temp file, enforcing maxSize and
// returning the file path, its size and its SHA-256 (lowercase hex).
func (c *marketClient) downloadToTemp(ctx context.Context, rawURL string, maxSize int64) (string, int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("User-Agent", "uiautodev/"+version.Version)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, "", fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "uiautodev-market-*.zip")
	if err != nil {
		return "", 0, "", err
	}
	tmpPath := tmp.Name()

	hasher := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, maxSize+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return "", 0, "", copyErr
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return "", 0, "", closeErr
	}
	if n > maxSize {
		os.Remove(tmpPath)
		return "", 0, "", fmt.Errorf("plugin archive too large")
	}
	return tmpPath, n, hex.EncodeToString(hasher.Sum(nil)), nil
}

// resolveURL resolves ref against base when ref is relative.
func resolveURL(base, ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(u).String(), nil
}

// --- source resolution ---

// isLoopbackHost reports whether host (without port) is localhost or a loopback IP.
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resolveMarketSource returns the effective market source.
func resolveMarketSource() (string, error) {
	source, err := getMarketSource()
	if err != nil {
		return "", err
	}
	source = strings.TrimRight(strings.TrimSpace(source), "/")
	if source == "" {
		return defaultPluginMarketURL, nil
	}
	return source, nil
}

// validateMarketSourceURL validates a user-supplied market source. An empty
// input restores the default source.
func validateMarketSourceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid url: missing host")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("custom plugin source must use http or https")
	}
	// Setting the source back to the official one is always allowed.
	normalized := strings.TrimRight(raw, "/")
	if normalized == defaultPluginMarketURL {
		return defaultPluginMarketURL, nil
	}
	// Only loopback sources may use plain http; everything else needs https.
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("custom plugin source must use https")
	}
	return normalized, nil
}

// marketSourceError maps a source resolution/validation error to an HTTP response.
func marketSourceError(c echo.Context, err error) error {
	return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
}

// --- handlers ---

// marketListHandler handles GET /api/v1/market/plugins.
func marketListHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		source, err := resolveMarketSource()
		if err != nil {
			return marketSourceError(c, err)
		}

		query := url.Values{}
		for _, key := range []string{"q", "tag", "platform", "author", "sort", "page", "page_size"} {
			if v := c.QueryParam(key); v != "" {
				query.Set(key, v)
			}
		}

		list := &marketListResponse{}
		client := newMarketClient(source)
		if err := client.listPlugins(c.Request().Context(), query, list); err != nil {
			return c.JSONPretty(http.StatusBadGateway, APIResponse{
				Code:    502,
				Message: "market request failed: " + err.Error(),
			}, "  ")
		}

		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data: map[string]any{
				"source":    source,
				"total":     list.Total,
				"page":      list.Page,
				"page_size": list.PageSize,
				"items":     mergeMarketItems(list.Items),
			},
		}, "  ")
	}
}

// mergeMarketItems enriches registry summaries with local installation state.
func mergeMarketItems(summaries []PluginSummary) []MarketItem {
	pm := GetPluginManager()
	records, _ := getPluginRecords()
	items := make([]MarketItem, 0, len(summaries))
	for _, s := range summaries {
		item := MarketItem{PluginSummary: s}
		if p, ok := pm.GetPlugin(s.ID); ok {
			item.Installed = true
			ver := ""
			if rec, ok := records[s.ID]; ok {
				ver = rec.Version
			}
			if ver == "" {
				ver = p.Manifest.Version
			}
			item.InstalledVersion = ver
			item.UpdateAvailable = isNewerVersion(s.LatestVersion, ver)
		}
		items = append(items, item)
	}
	return items
}

// isNewerVersion reports whether latest is a strictly newer semver than current.
func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}
	l, err1 := semver.NewVersion(latest)
	c, err2 := semver.NewVersion(current)
	if err1 != nil || err2 != nil {
		return false
	}
	return l.GreaterThan(c)
}

type marketInstallRequest struct {
	Version string `json:"version"`
}

// marketInstallHandler handles POST /api/v1/market/plugins/:id/install.
func marketInstallHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		if err := validatePluginID(id); err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
		}

		var req marketInstallRequest
		if c.Request().ContentLength != 0 {
			if err := c.Bind(&req); err != nil {
				return c.JSONPretty(http.StatusBadRequest, APIResponse{
					Code:    400,
					Message: "invalid request body: " + err.Error(),
				}, "  ")
			}
		}

		source, err := resolveMarketSource()
		if err != nil {
			return marketSourceError(c, err)
		}

		ctx := c.Request().Context()
		client := newMarketClient(source)
		pv, err := client.getVersion(ctx, id, req.Version)
		if err != nil {
			return c.JSONPretty(http.StatusBadGateway, APIResponse{
				Code:    502,
				Message: "failed to resolve version: " + err.Error(),
			}, "  ")
		}
		if err := checkMinAppVersion(pv.MinAppVersion); err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
		}

		// Already installed at the requested version: nothing to do.
		if records, _ := getPluginRecords(); records != nil {
			if rec, ok := records[id]; ok && rec.Version == pv.Version {
				if _, statErr := os.Stat(filepath.Join(GetUserPluginDir(), id, "plugin.json")); statErr == nil {
					c.Response().Header().Set("Location", "/api/v1/plugins/"+id)
					return c.JSONPretty(http.StatusOK, APIResponse{
						Code:    0,
						Message: "ok",
						Data:    map[string]string{"id": id, "version": pv.Version},
					}, "  ")
				}
			}
		}

		zipPath, _, sha, err := client.downloadToTemp(ctx, pv.DownloadURL, pluginZipMaxSize)
		if err != nil {
			return c.JSONPretty(http.StatusBadGateway, APIResponse{
				Code:    502,
				Message: "download failed: " + err.Error(),
			}, "  ")
		}
		defer os.Remove(zipPath)

		if pv.Sha256 == "" || !strings.EqualFold(sha, pv.Sha256) {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{
				Code:    400,
				Message: "checksum mismatch",
			}, "  ")
		}

		targetDir, err := installPluginFromMarketZip(zipPath, id, GetUserPluginDir())
		if err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{
				Code:    400,
				Message: "failed to install plugin: " + err.Error(),
			}, "  ")
		}

		pm := GetPluginManager()
		pm.LoadAll(getPluginDirs())
		p, _ := pm.GetPlugin(id)

		now := time.Now()
		if err := recordPluginInstall(id, PluginRecord{
			Version:     pv.Version,
			Source:      source,
			Sha256:      sha,
			InstalledAt: &now,
		}); err != nil {
			slog.Warn("failed to record plugin install", "id", id, "error", err)
		}

		name := id
		if p != nil && p.Manifest.Name != "" {
			name = p.Manifest.Name
		}
		slog.Info("installed plugin from market", "id", id, "version", pv.Version, "dir", targetDir)
		c.Response().Header().Set("Location", "/api/v1/plugins/"+id)
		return c.JSONPretty(http.StatusCreated, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    map[string]string{"id": id, "name": name, "version": pv.Version},
		}, "  ")
	}
}

// checkMinAppVersion rejects plugins that require a newer app version.
func checkMinAppVersion(minAppVersion string) error {
	if minAppVersion == "" {
		return nil
	}
	min, err := semver.NewVersion(minAppVersion)
	if err != nil {
		return nil
	}
	cur, err := semver.NewVersion(version.Version)
	if err != nil {
		return nil
	}
	if min.GreaterThan(cur) {
		return fmt.Errorf("plugin requires app version %s or newer", minAppVersion)
	}
	return nil
}

// buildMarketSourceInfo assembles the market source response payload.
func buildMarketSourceInfo() (MarketSourceInfo, error) {
	source, err := resolveMarketSource()
	if err != nil {
		return MarketSourceInfo{}, err
	}
	return MarketSourceInfo{
		URL:        source,
		DefaultURL: defaultPluginMarketURL,
		IsCustom:   source != defaultPluginMarketURL,
	}, nil
}

// marketSourceGetHandler handles GET /api/v1/market/source.
func marketSourceGetHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		info, err := buildMarketSourceInfo()
		if err != nil {
			return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()}, "  ")
		}
		return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: info}, "  ")
	}
}

type marketSourceRequest struct {
	URL string `json:"url"`
}

// marketSourcePutHandler handles PUT /api/v1/market/source.
func marketSourcePutHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req marketSourceRequest
		if err := c.Bind(&req); err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{
				Code:    400,
				Message: "invalid request body: " + err.Error(),
			}, "  ")
		}
		normalized, err := validateMarketSourceURL(req.URL)
		if err != nil {
			return marketSourceError(c, err)
		}
		if err := setMarketSource(normalized); err != nil {
			return c.JSONPretty(http.StatusInternalServerError, APIResponse{
				Code:    500,
				Message: "failed to save market source: " + err.Error(),
			}, "  ")
		}

		info, err := buildMarketSourceInfo()
		if err != nil {
			return c.JSONPretty(http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()}, "  ")
		}
		return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: info}, "  ")
	}
}
