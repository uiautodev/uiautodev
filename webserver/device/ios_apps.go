package device

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

var iosBundleIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ListApps lists installed app bundle IDs via the go-ios CLI.
func (d *IOSDevice) ListApps(scope AppScope) ([]string, error) {
	iosBin, err := IOSPath()
	if err != nil {
		return nil, err
	}
	args := []string{"apps", "--udid", d.udid}
	switch scope {
	case AppScopeUser:
		args = append(args, "--list")
	case AppScopeSystem:
		args = append(args, "--system", "--list")
	case AppScopeAll:
		args = append(args, "--all", "--list")
	default:
		return nil, fmt.Errorf("invalid app scope: %s", scope)
	}
	output, err := NewCommand(iosBin, args...).CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "list iOS apps: %s", strings.TrimSpace(string(output)))
	}
	apps := parseIOSAppList(string(output))
	sort.Strings(apps)
	return apps, nil
}

func parseIOSAppList(output string) []string {
	seen := make(map[string]bool)
	apps := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 || !iosBundleIDPattern.MatchString(fields[0]) {
			continue
		}
		if seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		apps = append(apps, fields[0])
	}
	return apps
}

// StartApp launches an iOS app through WDA.
func (d *IOSDevice) StartApp(appID, _ string) (CurrentApp, error) {
	appID = strings.TrimSpace(appID)
	if !iosBundleIDPattern.MatchString(appID) {
		return CurrentApp{}, fmt.Errorf("invalid app_id %q", appID)
	}
	if err := d.EnsureWDA(); err != nil {
		return CurrentApp{}, err
	}
	if err := d.wdaClient.LaunchApp(appID); err != nil {
		return CurrentApp{}, err
	}
	return CurrentApp{AppID: appID}, nil
}

// StopApp terminates an iOS app through WDA.
func (d *IOSDevice) StopApp(appID string) error {
	appID = strings.TrimSpace(appID)
	if !iosBundleIDPattern.MatchString(appID) {
		return fmt.Errorf("invalid app_id %q", appID)
	}
	if err := d.EnsureWDA(); err != nil {
		return err
	}
	return d.wdaClient.TerminateApp(appID)
}

// CurrentApp returns the foreground iOS app through WDA.
func (d *IOSDevice) CurrentApp() (CurrentApp, error) {
	if err := d.EnsureWDA(); err != nil {
		return CurrentApp{}, err
	}
	info, err := d.wdaClient.ActiveAppInfo()
	if err != nil {
		return CurrentApp{}, err
	}
	if strings.TrimSpace(info.BundleID) == "" {
		return CurrentApp{}, fmt.Errorf("WDA did not report an active app")
	}
	return CurrentApp{AppID: info.BundleID}, nil
}

// WDAAppInfo is the active app payload returned by WDA.
type WDAAppInfo struct {
	BundleID string `json:"bundleId"`
	Name     string `json:"name,omitempty"`
}

// LaunchApp launches a bundle through WDA.
func (c *WDAClient) LaunchApp(bundleID string) error {
	return c.postAppAction("/wda/apps/launch", map[string]string{"bundleId": bundleID}, "launch app")
}

// TerminateApp terminates a bundle through WDA.
func (c *WDAClient) TerminateApp(bundleID string) error {
	return c.postAppAction("/wda/apps/terminate", map[string]string{"bundleId": bundleID}, "terminate app")
}

// ActiveAppInfo returns the foreground app reported by WDA.
func (c *WDAClient) ActiveAppInfo() (WDAAppInfo, error) {
	info, err := c.getAppInfo("/wda/activeAppInfo")
	if err == nil {
		return info, nil
	}
	endpoint, sessionErr := c.sessionEndpoint("/wda/activeAppInfo")
	if sessionErr != nil {
		return WDAAppInfo{}, err
	}
	return c.getAppInfoURL(endpoint)
}

func (c *WDAClient) postAppAction(path string, payload any, action string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, action)
	}
	err = c.executeAction(c.baseURL+path, body, action)
	if err == nil {
		return nil
	}
	// Older WDA builds expose these actions under a session.
	if !strings.Contains(err.Error(), "HTTP 404") {
		return err
	}
	endpoint, sessionErr := c.sessionEndpoint(path)
	if sessionErr != nil {
		return err
	}
	return c.executeAction(endpoint, body, action)
}

func (c *WDAClient) getAppInfo(path string) (WDAAppInfo, error) {
	return c.getAppInfoURL(c.baseURL + path)
}

func (c *WDAClient) getAppInfoURL(endpoint string) (WDAAppInfo, error) {
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return WDAAppInfo{}, errors.Wrap(err, "get active app")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WDAAppInfo{}, fmt.Errorf("get active app failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var response struct {
		Value struct {
			WDAAppInfo
			Error   string `json:"error"`
			Message string `json:"message"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return WDAAppInfo{}, errors.Wrap(err, "decode active app")
	}
	if response.Value.Error != "" {
		return WDAAppInfo{}, fmt.Errorf("get active app failed: %s: %s", response.Value.Error, response.Value.Message)
	}
	return response.Value.WDAAppInfo, nil
}
