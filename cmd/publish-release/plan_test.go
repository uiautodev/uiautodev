package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssetKey(t *testing.T) {
	cases := []struct {
		prefix  string
		version string
		name    string
		want    string
	}{
		{"", "1.2.3", "a.dmg", "1.2.3/a.dmg"},
		{"uiautodev", "1.2.3", "a.dmg", "uiautodev/1.2.3/a.dmg"},
		{"uiautodev/", "1.2.3", "a.dmg", "uiautodev/1.2.3/a.dmg"},
	}
	for _, tc := range cases {
		if got := assetKey(tc.prefix, tc.version, tc.name); got != tc.want {
			t.Errorf("assetKey(%q, %q, %q) = %q, want %q",
				tc.prefix, tc.version, tc.name, got, tc.want)
		}
	}
}

func TestSkipAsset(t *testing.T) {
	skipped := []string{"", ".DS_Store", ".env", "build.log"}
	kept := []string{"release.json", "a.dmg", "uiautodev-server-linux-arm64-1.2.3"}

	for _, name := range skipped {
		if !skipAsset(name) {
			t.Errorf("skipAsset(%q) = false, want true", name)
		}
	}
	for _, name := range kept {
		if skipAsset(name) {
			t.Errorf("skipAsset(%q) = true, want false", name)
		}
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"release.json": "application/json",
		"a.dmg":        "application/x-apple-diskimage",
		"a.zip":        "application/zip",
		"a.exe":        "application/vnd.microsoft.portable-executable",
		"server":       "application/octet-stream",
	}
	for name, want := range cases {
		if got := contentTypeFor(name); got != want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReadManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "release.json", `{"version":"1.2.3","assets":[{"name":"a.dmg"}]}`)

	manifest, err := readManifest(filepath.Join(dir, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.2.3" || len(manifest.Assets) != 1 || manifest.Assets[0].Name != "a.dmg" {
		t.Fatalf("readManifest = %+v", manifest)
	}

	if _, err := readManifest(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("readManifest should fail for a missing file")
	}
}

func TestPlanUploads(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"release.json", "a.dmg", "b.zip", "stray.bin", ".DS_Store", "build.log"} {
		writeFile(t, dir, name, "x")
	}

	manifest := releaseManifest{
		Version: "1.2.3",
		Assets:  []manifestAsset{{Name: "a.dmg"}, {Name: "b.zip"}},
	}

	items, err := planUploads(manifest, dir, "", "1.2.3", "release.json")
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]string, len(items))
	for _, item := range items {
		got[filepath.Base(item.Path)] = item.Key
	}

	want := map[string]string{
		"a.dmg":        "1.2.3/a.dmg",
		"b.zip":        "1.2.3/b.zip",
		"release.json": "1.2.3/release.json",
	}
	if len(got) != len(want) {
		t.Fatalf("planUploads returned %v, want %v", got, want)
	}
	for name, key := range want {
		if got[name] != key {
			t.Errorf("key for %q = %q, want %q", name, got[name], key)
		}
	}
	if _, ok := got["stray.bin"]; ok {
		t.Error("planUploads should not include files missing from the manifest")
	}
}

func TestPlanUploadsMissingAsset(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "release.json", "{}")

	manifest := releaseManifest{Version: "1.2.3", Assets: []manifestAsset{{Name: "missing.dmg"}}}
	if _, err := planUploads(manifest, dir, "", "1.2.3", "release.json"); err == nil {
		t.Error("planUploads should fail when a manifest asset is missing on disk")
	}
}

func TestPlanUploadsWithPrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "release.json", "{}")
	writeFile(t, dir, "a.dmg", "x")

	manifest := releaseManifest{Version: "1.2.3", Assets: []manifestAsset{{Name: "a.dmg"}}}
	items, err := planUploads(manifest, dir, "uiautodev", "1.2.3", "release.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if filepath.Base(item.Path) == "a.dmg" && item.Key != "uiautodev/1.2.3/a.dmg" {
			t.Errorf("key = %q", item.Key)
		}
	}
}
