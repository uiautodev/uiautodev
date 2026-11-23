// Package update implements desktop self-updates for uiautodev.
//
// A release publishes a release.json manifest next to its assets. The client
// reads the latest manifest, downloads the matching desktop asset, verifies
// its SHA256 from the manifest, installs it and relaunches the app.
package update

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	defaultManifestURL = "https://dl.uiauto.dev/updates/latest.json"
	defaultBaseURL     = "https://dl.uiauto.dev"
	userAgent          = "uiautodev-updater/1.0"
)

// Options configures a Client. ManifestURL and BaseURL are only overridden by
// tests; production always uses the defaults above.
type Options struct {
	ManifestURL string
	BaseURL     string
	DataDir     string
	Executable  string
	Desktop     bool
	GOOS        string
	GOARCH      string
	HTTPClient  *http.Client
	// Quit exits the current app after the relaunch helper has started. When
	// nil the process exits directly.
	Quit func()
	// Install and Relaunch exist so tests can replace platform behavior.
	Install  func(assetPath, exe string) error
	Relaunch func(exe string) error
}

// Manifest is the release.json payload published with every release.
type Manifest struct {
	Version     string  `json:"version"`
	Tag         string  `json:"tag,omitempty"`
	PublishedAt string  `json:"published_at,omitempty"`
	ReleaseURL  string  `json:"release_url,omitempty"`
	Notes       string  `json:"notes,omitempty"`
	Assets      []Asset `json:"assets"`
}

// Asset describes one downloadable release artifact.
type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Info is the result of an update check.
type Info struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Dev         bool   `json:"dev"`
	ReleaseURL  string `json:"release_url,omitempty"`
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Asset       string `json:"asset,omitempty"`
	AssetSize   int64  `json:"asset_size,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	SHA256      string `json:"-"`
	Desktop     bool   `json:"desktop"`
}

// Progress is the in-memory state of the running update. Only the percentage
// is shown by the UI.
type Progress struct {
	Active  bool    `json:"active"`
	Percent float64 `json:"percent"`
	Error   string  `json:"error,omitempty"`
}

// Result reports an installed update.
type Result struct {
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

// Client checks for and installs desktop updates.
type Client struct {
	progressMu sync.Mutex
	progress   Progress

	manifestURL string
	baseURL     string
	dataDir     string
	exe         string
	desktop     bool
	goos        string
	goarch      string
	http        *http.Client
	quit        func()
	install     func(assetPath, exe string) error
	relaunch    func(exe string) error
}

// New builds a Client, filling platform defaults.
func New(opts Options) *Client {
	c := &Client{
		manifestURL: strings.TrimSpace(opts.ManifestURL),
		baseURL:     strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		dataDir:     opts.DataDir,
		exe:         opts.Executable,
		desktop:     opts.Desktop,
		goos:        opts.GOOS,
		goarch:      opts.GOARCH,
		http:        opts.HTTPClient,
		quit:        opts.Quit,
		install:     opts.Install,
		relaunch:    opts.Relaunch,
	}
	if c.manifestURL == "" {
		c.manifestURL = defaultManifestURL
	}
	if c.baseURL == "" {
		c.baseURL = defaultBaseURL
	}
	if c.goos == "" {
		c.goos = runtime.GOOS
	}
	if c.goarch == "" {
		c.goarch = runtime.GOARCH
	}
	if c.exe == "" {
		if exe, err := os.Executable(); err == nil {
			c.exe = exe
		}
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 15 * time.Minute}
	}
	if c.install == nil {
		c.install = installDesktopAsset
	}
	if c.relaunch == nil {
		c.relaunch = relaunchProcess
	}
	return c
}

// ResetProgress clears any previous result before a new update starts.
func (c *Client) ResetProgress() {
	c.setProgress(Progress{Active: true})
}

// Progress returns a snapshot of the current update progress.
func (c *Client) Progress() Progress {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	return c.progress
}

func (c *Client) setProgress(progress Progress) {
	c.progressMu.Lock()
	c.progress = progress
	c.progressMu.Unlock()
}

func (c *Client) reportError(err error) {
	c.setProgress(Progress{Active: false, Percent: c.Progress().Percent, Error: err.Error()})
}
