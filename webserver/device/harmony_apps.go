package device

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	harmonyAppIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)
	harmonyAbilityPattern   = regexp.MustCompile(`^[A-Za-z0-9_.$]+$`)
	harmonySystemAppPattern = regexp.MustCompile(`(?i)"?(?:isSystemApp|systemApp|isSystem)"?\s*[:=]\s*"?true"?`)
	harmonyAbilityPatterns  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)"?(?:mainAbility|mainAbilityName)"?\s*[:=]\s*"?([A-Za-z0-9_.$]+)"?`),
		regexp.MustCompile(`(?i)"?abilityName"?\s*[:=]\s*"?([A-Za-z0-9_.$]+)"?`),
	}
	harmonyCurrentMissionPattern = regexp.MustCompile(`Mission ID #(?:\d+)\s+mission name #\[#([A-Za-z0-9_.]+):[^:\]]+:([A-Za-z0-9_.$]+)\]`)
)

// ListApps lists HarmonyOS bundle names.
func (d *HarmonyOSDevice) ListApps(scope AppScope) ([]string, error) {
	if scope != AppScopeAll && scope != AppScopeUser && scope != AppScopeSystem {
		return nil, fmt.Errorf("invalid app scope: %s", scope)
	}
	output, err := d.RunShell("bm dump -a", 15000)
	if err != nil {
		return nil, err
	}
	apps := parseHarmonyAppList(output)
	if scope == AppScopeAll {
		sort.Strings(apps)
		return apps, nil
	}
	// Classifying an app requires its own bundle dump; probe several in
	// parallel because a full scan is otherwise very slow over hdc.
	systemApps := d.detectHarmonySystemApps(apps)
	filtered := make([]string, 0, len(apps))
	for _, appID := range apps {
		system, known := systemApps[appID]
		if !known {
			// Keep unknown apps in the user list rather than dropping them.
			if scope == AppScopeUser {
				filtered = append(filtered, appID)
			}
			continue
		}
		if (scope == AppScopeSystem) == system {
			filtered = append(filtered, appID)
		}
	}
	sort.Strings(filtered)
	return filtered, nil
}

const harmonyAppProbeWorkers = 8

// detectHarmonySystemApps reports which bundles are system apps. Bundles whose
// dump fails are absent from the result (treated as unknown).
func (d *HarmonyOSDevice) detectHarmonySystemApps(apps []string) map[string]bool {
	flags := make(map[string]bool, len(apps))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, harmonyAppProbeWorkers)
	for _, appID := range apps {
		wg.Add(1)
		go func(appID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			detail, err := d.RunShell("bm dump -n "+appID, 5000)
			if err != nil {
				return
			}
			mu.Lock()
			flags[appID] = harmonySystemAppPattern.MatchString(detail)
			mu.Unlock()
		}(appID)
	}
	wg.Wait()
	return flags
}

func parseHarmonyAppList(output string) []string {
	seen := make(map[string]bool)
	apps := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !harmonyAppIDPattern.MatchString(line) {
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

// StartApp starts a HarmonyOS app, resolving its main ability when needed.
func (d *HarmonyOSDevice) StartApp(appID, activity string) (CurrentApp, error) {
	appID = strings.TrimSpace(appID)
	if !harmonyAppIDPattern.MatchString(appID) {
		return CurrentApp{}, fmt.Errorf("invalid app_id %q", appID)
	}
	activity = strings.TrimSpace(activity)
	if activity == "" {
		resolved, err := d.resolveHarmonyAbility(appID)
		if err != nil {
			return CurrentApp{}, err
		}
		activity = resolved
	}
	if !harmonyAbilityPattern.MatchString(activity) {
		return CurrentApp{}, fmt.Errorf("invalid activity %q", activity)
	}
	output, err := d.RunShell(fmt.Sprintf("aa start -b '%s' -a '%s'", appID, activity), 10000)
	if err != nil {
		return CurrentApp{}, err
	}
	// hdc reports a nil error even when aa itself failed, so inspect the output.
	if failure := harmonyCommandFailure(output); failure != "" {
		return CurrentApp{}, fmt.Errorf("failed to start %s: %s", appID, failure)
	}
	return CurrentApp{AppID: appID, Activity: activity}, nil
}

func (d *HarmonyOSDevice) resolveHarmonyAbility(appID string) (string, error) {
	output, err := d.RunShell("bm dump -n "+appID, 10000)
	if err != nil {
		return "", err
	}
	for _, pattern := range harmonyAbilityPatterns {
		if match := pattern.FindStringSubmatch(output); len(match) == 2 {
			return match[1], nil
		}
	}
	return "", fmt.Errorf("failed to resolve main ability for %s; provide activity explicitly", appID)
}

// StopApp force-stops a HarmonyOS app.
func (d *HarmonyOSDevice) StopApp(appID string) error {
	appID = strings.TrimSpace(appID)
	if !harmonyAppIDPattern.MatchString(appID) {
		return fmt.Errorf("invalid app_id %q", appID)
	}
	output, err := d.RunShell("aa force-stop "+appID, 10000)
	if err != nil {
		return err
	}
	if failure := harmonyCommandFailure(output); failure != "" {
		return fmt.Errorf("failed to stop %s: %s", appID, failure)
	}
	return nil
}

// harmonyCommandFailure returns the trimmed output when an `aa` command
// reported a failure without a transport error.
func harmonyCommandFailure(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	for _, line := range strings.Split(trimmed, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "failure") {
			return trimmed
		}
	}
	return ""
}

// CurrentApp returns the foreground HarmonyOS app by reading the current
// mission list from `aa dump -a`. The mission whose ability record is in
// state #FOREGROUND is the one shown on screen.
func (d *HarmonyOSDevice) CurrentApp() (CurrentApp, error) {
	output, err := d.RunShell("aa dump -a", 10000)
	if err != nil {
		return CurrentApp{}, err
	}
	if current, ok := parseHarmonyCurrentApp(output); ok {
		return current, nil
	}
	return CurrentApp{}, fmt.Errorf("failed to determine current app")
}

type harmonyMission struct {
	bundle     string // bundle from the mission name
	ability    string // ability from the mission name
	foreground bool
	record     *harmonyAbilityRecord // foreground ability record, when parsed
}

// harmonyCompatHosts are system processes that host Android apps on HarmonyOS.
// They are not the app the user interacts with, even when their ability record
// is the foreground one.
var harmonyCompatHosts = map[string]bool{
	"com.huawei.shell_assistant": true,
}

// appIdentity resolves which bundle/ability represents the mission on screen.
// The mission name can name a different app than its ability record (for
// example when another app hosts the ability), so the record wins when it owns
// the on-screen window.
func (m *harmonyMission) appIdentity() (string, string) {
	record := m.record
	if record != nil && record.window && record.bundle != "" && record.bundle != m.bundle && !harmonyCompatHosts[record.bundle] {
		return record.bundle, record.ability
	}
	return m.bundle, m.ability
}

// parseHarmonyCurrentApp finds the foreground mission in `aa dump -a` output.
// Only the leading "current mission lists" block is considered: later sections
// list extension/service records that also carry a foreground state but are not
// what the user sees.
func parseHarmonyCurrentApp(output string) (CurrentApp, bool) {
	var missions []*harmonyMission
	var current *harmonyMission
	var record *harmonyAbilityRecord
	inMissions := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inMissions {
			if strings.Contains(line, "current mission lists") {
				inMissions = true
			}
			continue
		}
		if trimmed == "}" || strings.HasPrefix(trimmed, "ExtensionRecords") {
			break
		}
		if match := harmonyCurrentMissionPattern.FindStringSubmatch(line); len(match) == 3 {
			current = &harmonyMission{bundle: match[1], ability: match[2]}
			missions = append(missions, current)
			record = nil
			continue
		}
		if strings.HasPrefix(trimmed, "AbilityRecord ID") {
			record = &harmonyAbilityRecord{}
			continue
		}
		if record == nil {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "main name ["):
			record.ability = harmonyBracketValue(trimmed)
		case strings.HasPrefix(trimmed, "bundle name ["):
			record.bundle = harmonyBracketValue(trimmed)
		case strings.Contains(trimmed, "window attached #"):
			record.window = strings.Contains(trimmed, "window attached #1")
		}
		// Mission records also carry an "app state #FOREGROUND" line; only the
		// ability's own state decides which mission is on screen.
		if current != nil && strings.Contains(line, "state #FOREGROUND") && !strings.Contains(line, "app state") {
			current.foreground = true
			current.record = record
		}
	}
	// Prefer the last focused mission when several report foreground state.
	for i := len(missions) - 1; i >= 0; i-- {
		mission := missions[i]
		if !mission.foreground {
			continue
		}
		appID, ability := mission.appIdentity()
		if !harmonyAppIDPattern.MatchString(appID) {
			continue
		}
		return CurrentApp{AppID: appID, Activity: ability}, true
	}
	return CurrentApp{}, false
}

// harmonyAbilityRecord mirrors the ability record fields needed to identify the
// app behind a mission.
type harmonyAbilityRecord struct {
	bundle  string
	ability string
	window  bool
}

func harmonyBracketValue(line string) string {
	start := strings.Index(line, "[")
	end := strings.LastIndex(line, "]")
	if start < 0 || end <= start {
		return ""
	}
	return line[start+1 : end]
}
