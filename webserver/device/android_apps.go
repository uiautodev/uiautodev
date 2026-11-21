package device

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	androidAppIDPattern     = regexp.MustCompile(`^(?:android|[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+)$`)
	androidActivityPattern  = regexp.MustCompile(`^[A-Za-z0-9_.$]+$`)
	androidComponentPattern = regexp.MustCompile(`([A-Za-z0-9_.$]+)/([A-Za-z0-9_.$]+)`)
	androidCurrentLines     = []string{"mCurrentFocus", "mFocusedApp", "mResumedActivity", "ResumedActivity"}
)

// ListApps lists installed app IDs for the requested scope.
func (d *AndroidDevice) ListApps(scope AppScope) ([]string, error) {
	command := "pm list packages"
	switch scope {
	case AppScopeUser:
		command += " -3"
	case AppScopeSystem:
		command += " -s"
	case AppScopeAll:
	default:
		return nil, fmt.Errorf("invalid app scope: %s", scope)
	}
	output, err := d.RunShell(command, 10000)
	if err != nil {
		return nil, err
	}
	apps := parseAndroidPackageList(output)
	sort.Strings(apps)
	return apps, nil
}

func parseAndroidPackageList(output string) []string {
	seen := make(map[string]bool)
	apps := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "package:")
		if line == "" || strings.ContainsAny(line, " \t") || !androidAppIDPattern.MatchString(line) {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		apps = append(apps, line)
	}
	return apps
}

// StartApp starts appID, resolving its launcher activity when activity is empty.
func (d *AndroidDevice) StartApp(appID, activity string) (CurrentApp, error) {
	appID = strings.TrimSpace(appID)
	if !androidAppIDPattern.MatchString(appID) {
		return CurrentApp{}, fmt.Errorf("invalid app_id %q", appID)
	}
	activity = strings.TrimSpace(activity)
	if activity == "" {
		resolved, err := d.resolveAndroidActivity(appID)
		if err != nil {
			return CurrentApp{}, err
		}
		activity = resolved
	}
	component, activityName, err := androidComponent(appID, activity)
	if err != nil {
		return CurrentApp{}, err
	}
	output, err := d.RunShell("am start -n '"+component+"'", 10000)
	if err != nil {
		return CurrentApp{}, err
	}
	// am start exits successfully even when the activity cannot be launched,
	// so the command output is the only failure signal.
	if failure := androidLaunchFailure(output); failure != "" {
		return CurrentApp{}, fmt.Errorf("failed to start %s: %s", appID, failure)
	}
	return CurrentApp{AppID: appID, Activity: activityName}, nil
}

// androidLaunchFailure returns the trimmed output when `am start` reported a
// command-level failure such as a missing activity or a security exception.
func androidLaunchFailure(output string) string {
	trimmed := strings.TrimSpace(output)
	switch {
	case strings.Contains(trimmed, "Error:"),
		strings.Contains(trimmed, "Error type"),
		strings.Contains(trimmed, "Exception occurred while executing"):
		return trimmed
	default:
		return ""
	}
}

func (d *AndroidDevice) resolveAndroidActivity(appID string) (string, error) {
	command := fmt.Sprintf(
		"cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.LAUNCHER %s",
		appID,
	)
	output, err := d.RunShell(command, 10000)
	if err != nil {
		return "", err
	}
	if resolved := parseAndroidResolvedActivity(output, appID); resolved != "" {
		return resolved, nil
	}
	return "", fmt.Errorf("failed to resolve main activity for %s; provide activity explicitly", appID)
}

func parseAndroidResolvedActivity(output, appID string) string {
	for _, match := range androidComponentPattern.FindAllStringSubmatch(output, -1) {
		if match[1] == appID {
			return match[2]
		}
	}
	return ""
}

func androidComponent(appID, activity string) (string, string, error) {
	if strings.Contains(activity, "/") {
		parts := strings.SplitN(activity, "/", 2)
		if parts[0] != appID || !androidActivityPattern.MatchString(parts[1]) {
			return "", "", fmt.Errorf("invalid activity %q for %s", activity, appID)
		}
		return parts[0] + "/" + parts[1], parts[1], nil
	}
	if !androidActivityPattern.MatchString(activity) {
		return "", "", fmt.Errorf("invalid activity %q", activity)
	}
	return appID + "/" + activity, activity, nil
}

// StopApp force-stops an app.
func (d *AndroidDevice) StopApp(appID string) error {
	appID = strings.TrimSpace(appID)
	if !androidAppIDPattern.MatchString(appID) {
		return fmt.Errorf("invalid app_id %q", appID)
	}
	_, err := d.RunShell("am force-stop "+appID, 10000)
	return err
}

// CurrentApp returns the foreground app and activity.
func (d *AndroidDevice) CurrentApp() (CurrentApp, error) {
	commands := []string{
		"dumpsys window windows | grep -E 'mCurrentFocus|mFocusedApp'",
		"dumpsys activity activities | grep -E 'mResumedActivity|ResumedActivity'",
	}
	for _, command := range commands {
		output, err := d.RunShell(command, 5000)
		if err != nil {
			continue
		}
		if app, ok := parseAndroidCurrentApp(output); ok {
			return app, nil
		}
	}
	return CurrentApp{}, fmt.Errorf("failed to determine current app")
}

func parseAndroidCurrentApp(output string) (CurrentApp, bool) {
	for _, line := range strings.Split(output, "\n") {
		if !androidCurrentMarker(line) {
			continue
		}
		match := androidComponentPattern.FindStringSubmatch(line)
		if len(match) == 3 {
			return CurrentApp{AppID: match[1], Activity: match[2]}, true
		}
	}
	return CurrentApp{}, false
}

func androidCurrentMarker(line string) bool {
	for _, marker := range androidCurrentLines {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}
