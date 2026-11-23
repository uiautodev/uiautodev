package device

import (
	"strings"
	"testing"
)

func TestParseAndroidPackageList(t *testing.T) {
	output := strings.Join([]string{
		"package:com.example.one",
		"package:com.example.two",
		"",
		"package:com.example.one",
		"WARNING: linker: unused",
		"package:com.example..bad",
	}, "\n")
	apps := parseAndroidPackageList(output)
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

func TestParseAndroidResolvedActivity(t *testing.T) {
	output := "priority=0 preferredOrder=0 match=0x108000 specificIndex=-1\ncom.example.app/.MainActivity\n"
	if got := parseAndroidResolvedActivity(output, "com.example.app"); got != ".MainActivity" {
		t.Fatalf("got %q, want .MainActivity", got)
	}
	if got := parseAndroidResolvedActivity("com.other.app/.Main", "com.example.app"); got != "" {
		t.Fatalf("got %q, want empty for mismatched package", got)
	}
	if got := parseAndroidResolvedActivity("No activity found", "com.example.app"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestAndroidComponent(t *testing.T) {
	component, activity, err := androidComponent("com.example.app", "com.example.app.Main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if component != "com.example.app/com.example.app.Main" || activity != "com.example.app.Main" {
		t.Fatalf("got %q %q", component, activity)
	}

	component, _, err = androidComponent("com.example.app", "com.example.app.Main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if component != "com.example.app/com.example.app.Main" {
		t.Fatalf("got %q", component)
	}

	component, activity, err = androidComponent("com.example.app", "com.example.app/.Main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if component != "com.example.app/.Main" || activity != ".Main" {
		t.Fatalf("got %q %q", component, activity)
	}

	if _, _, err := androidComponent("com.example.app", "bad;rm -rf"); err == nil {
		t.Fatal("expected invalid activity error")
	}
}

func TestAndroidLaunchFailure(t *testing.T) {
	cases := []struct {
		name   string
		output string
		failed bool
	}{
		{
			name:   "success",
			output: "Starting: Intent { cmp=com.ugreen.pro/.ui.Splash }\n",
		},
		{
			name:   "already running",
			output: "Warning: Activity not started, its current task has been brought to the front",
		},
		{
			name: "missing activity",
			output: "Starting: Intent { cmp=com.ugreen.pro/.MissingActivity }\n" +
				"Error type 3\n" +
				"Error: Activity class {com.ugreen.pro/com.ugreen.pro.MissingActivity} does not exist.\n",
			failed: true,
		},
		{
			name: "security exception",
			output: "Starting: Intent { cmp=com.example.app/.Secret }\n" +
				"Exception occurred while executing 'start':\n" +
				"java.lang.SecurityException: Permission Denial",
			failed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failure := androidLaunchFailure(tc.output)
			if tc.failed && failure == "" {
				t.Fatalf("expected failure for %q", tc.output)
			}
			if !tc.failed && failure != "" {
				t.Fatalf("unexpected failure %q", failure)
			}
		})
	}
}

func TestParseAndroidCurrentApp(t *testing.T) {
	output := strings.Join([]string{
		"  mCurrentFocus=Window{1 u0 com.example.app/com.example.app.MainActivity}",
		"  mFocusedApp=ActivityRecord{1 u0 com.example.app/.MainActivity t1}",
	}, "\n")
	app, ok := parseAndroidCurrentApp(output)
	if !ok {
		t.Fatal("expected current app")
	}
	if app.AppID != "com.example.app" || app.Activity != "com.example.app.MainActivity" {
		t.Fatalf("got %+v", app)
	}

	if _, ok := parseAndroidCurrentApp("mCurrentFocus=null"); ok {
		t.Fatal("expected no current app")
	}
	if _, ok := parseAndroidCurrentApp("unrelated output"); ok {
		t.Fatal("expected no current app")
	}
}
