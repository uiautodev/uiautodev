package main

import "testing"

func TestRelativeKey(t *testing.T) {
	cases := []struct {
		prefix string
		key    string
		want   string
		ok     bool
	}{
		{"binaries/", "binaries/darwin/adb", "darwin/adb", true},
		{"binaries/", "binaries/windows/adb.exe", "windows/adb.exe", true},
		{"binaries", "binaries/darwin/adb", "darwin/adb", true},
		{"binaries/", "binaries/", "", false},
		{"binaries/", "other/darwin/adb", "other/darwin/adb", true},
		{"binaries/", "binaries/../secret", "", false},
		{"binaries/", "binaries//absolute", "absolute", true},
		{"", "/etc/passwd", "etc/passwd", true},
	}
	for _, tc := range cases {
		got, ok := relativeKey(tc.prefix, tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("relativeKey(%q, %q) = %q, %v; want %q, %v",
				tc.prefix, tc.key, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPlanDownloads(t *testing.T) {
	objects := []remoteObject{
		{Key: "binaries/darwin/adb", Size: 100},
		{Key: "binaries/darwin/ios", Size: 200},
		{Key: "binaries/", Size: 0},
	}
	sizes := map[string]int64{
		"binaries/darwin/ios": 200,
		"binaries/darwin/adb": 90,
	}
	sizeOf := func(path string) (int64, bool) {
		size, ok := sizes[path]
		return size, ok
	}

	items, skipped := planDownloads(objects, "binaries", "binaries/", sizeOf)
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(items) != 1 || items[0].Key != "binaries/darwin/adb" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Path != "binaries/darwin/adb" {
		t.Errorf("path = %q", items[0].Path)
	}
}

func TestPlanDownloadsNewFiles(t *testing.T) {
	objects := []remoteObject{{Key: "binaries/windows/adb.exe", Size: 10}}
	items, skipped := planDownloads(objects, "binaries", "binaries/", func(string) (int64, bool) { return 0, false })
	if skipped != 0 || len(items) != 1 {
		t.Fatalf("items = %+v, skipped = %d", items, skipped)
	}
	if items[0].Path != "binaries/windows/adb.exe" {
		t.Errorf("path = %q", items[0].Path)
	}
}

func TestIsExecutableName(t *testing.T) {
	for _, name := range []string{"adb", "ios"} {
		if !isExecutableName(name) {
			t.Errorf("isExecutableName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"adb.exe", "AdbWinApi.dll", "hdc.zip"} {
		if isExecutableName(name) {
			t.Errorf("isExecutableName(%q) = true, want false", name)
		}
	}
}
