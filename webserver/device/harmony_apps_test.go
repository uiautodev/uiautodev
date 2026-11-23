package device

import "testing"

func TestParseHarmonyAppList(t *testing.T) {
	output := "com.example.one\ncom.example.two\n\ncom.example.one\nbad bundle!\n"
	apps := parseHarmonyAppList(output)
	want := []string{"com.example.one", "com.example.two"}
	if len(apps) != len(want) {
		t.Fatalf("got %v, want %v", apps, want)
	}
	for i := range want {
		if apps[i] != want[i] {
			t.Fatalf("got %v, want %v", apps, want)
		}
	}
}

func TestHarmonyAbilityResolution(t *testing.T) {
	outputs := []string{
		`{"name":"com.example.app","mainAbility":"EntryAbility"}`,
		`{"name":"com.example.app","mainAbilityName":"EntryAbility"}`,
		`{"name":"com.example.app","abilityName":"EntryAbility"}`,
	}
	for _, output := range outputs {
		var resolved string
		for _, pattern := range harmonyAbilityPatterns {
			if match := pattern.FindStringSubmatch(output); len(match) == 2 {
				resolved = match[1]
				break
			}
		}
		if resolved != "EntryAbility" {
			t.Fatalf("output %q resolved %q", output, resolved)
		}
	}

	missing := `{"name":"com.example.app"}`
	for _, pattern := range harmonyAbilityPatterns {
		if pattern.MatchString(missing) {
			t.Fatalf("unexpected match for %q with %v", missing, pattern)
		}
	}
}

func TestHarmonySystemAppDetection(t *testing.T) {
	system := []string{`{"isSystemApp":true}`, `isSystemApp: true`, `"systemApp":"true"`}
	for _, output := range system {
		if !harmonySystemAppPattern.MatchString(output) {
			t.Fatalf("expected system app for %q", output)
		}
	}
	if harmonySystemAppPattern.MatchString(`{"isSystemApp":false}`) {
		t.Fatal("did not expect system app")
	}
}

func TestParseHarmonyCurrentApp(t *testing.T) {
	output := `User ID #100
  current mission lists:{
    Mission ID #124  mission name #[#com.didi.it.dday:entry:com.didi.it.dday.SplashActivity]  lockedState #0  mission affinity #[12]
      AbilityRecord ID #2528
        app name [com.huawei.shell_assistant]
        main name [MainAbility]
        bundle name [com.huawei.shell_assistant]
        ability type [PAGE]
        state #BACKGROUND  start time [25487371]
        app state #FOREGROUND
    Mission ID #388  mission name #[#com.huawei.hmsapp.compass:entry:EntryAbility]  lockedState #0  mission affinity #[]
      AbilityRecord ID #8633
        app name [com.huawei.hmsapp.compass]
        main name [EntryAbility]
        bundle name [com.huawei.hmsapp.compass]
        ability type [PAGE]
        state #FOREGROUND  start time [89311279]
        app state #FOREGROUND
        ready #1  window attached #1  launcher #0
 }
  ExtensionRecords:
    uri [/com.xiaomi.mico/entry/ServiceAbility]
      AbilityRecord ID #24   state #FOREGROUND   start time [25576]
      main name [ServiceAbility]
      bundle name [com.xiaomi.mico]
      app state #BEGIN
`

	current, ok := parseHarmonyCurrentApp(output)
	if !ok {
		t.Fatal("expected a current app")
	}
	if current.AppID != "com.huawei.hmsapp.compass" || current.Activity != "EntryAbility" {
		t.Fatalf("got %+v", current)
	}

	if _, ok := parseHarmonyCurrentApp("no missions here"); ok {
		t.Fatal("expected no current app")
	}

	backgroundOnly := `Mission ID #1  mission name #[#com.example.app:entry:EntryAbility]  lockedState #0
      AbilityRecord ID #2
        state #BACKGROUND  start time [1]
        app state #FOREGROUND`
	if _, ok := parseHarmonyCurrentApp(backgroundOnly); ok {
		t.Fatal("did not expect a current app for background-only missions")
	}
}

func TestParseHarmonyCurrentAppUsesForegroundRecord(t *testing.T) {
	output := `User ID #100
  current mission lists:{
    Mission ID #500  mission name #[#com.launcher.app:entry:EntryAbility]  lockedState #0
      AbilityRecord ID #10
        app name [com.gallery.app]
        main name [MainAbility]
        bundle name [com.gallery.app]
        ability type [PAGE]
        state #FOREGROUND  start time [1]
        app state #FOREGROUND
        ready #1  window attached #1  launcher #0
 }
  ExtensionRecords:
`

	current, ok := parseHarmonyCurrentApp(output)
	if !ok {
		t.Fatal("expected a current app")
	}
	if current.AppID != "com.gallery.app" || current.Activity != "MainAbility" {
		t.Fatalf("got %+v", current)
	}
}

func TestParseHarmonyCurrentAppKeepsMissionForCompatHost(t *testing.T) {
	output := `User ID #100
  current mission lists:{
    Mission ID #132  mission name #[#com.ugreen.pro:entry:com.ugreen.pro.ui.Splash]  lockedState #0
      AbilityRecord ID #2624
        app name [com.huawei.shell_assistant]
        main name [MainAbility]
        bundle name [com.huawei.shell_assistant]
        ability type [PAGE]
        state #FOREGROUND  start time [27324920]
        app state #FOREGROUND
        ready #1  window attached #1  launcher #0
 }
  ExtensionRecords:
`

	current, ok := parseHarmonyCurrentApp(output)
	if !ok {
		t.Fatal("expected a current app")
	}
	if current.AppID != "com.ugreen.pro" || current.Activity != "com.ugreen.pro.ui.Splash" {
		t.Fatalf("got %+v", current)
	}
}

func TestHarmonyCommandFailure(t *testing.T) {
	cases := []struct {
		name   string
		output string
		failed bool
	}{
		{name: "start ok", output: "start ability successfully.\n"},
		{name: "force stop ok", output: "force stop process successfully.\n"},
		{name: "empty", output: ""},
		{name: "start failed", output: "error: failed to start ability.\n", failed: true},
		{name: "force stop failed", output: "error: failed to force stop the process.\n", failed: true},
		{name: "failure keyword", output: "start ability failure\n", failed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failure := harmonyCommandFailure(tc.output)
			if tc.failed && failure == "" {
				t.Fatalf("expected failure for %q", tc.output)
			}
			if !tc.failed && failure != "" {
				t.Fatalf("unexpected failure %q", failure)
			}
		})
	}
}
