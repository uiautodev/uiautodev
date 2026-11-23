package main

import "testing"

func TestRunUploadDryRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.dmg", "x")
	writeFile(t, dir, "release.json", `{"version":"1.2.3","assets":[{"name":"a.dmg"}]}`)

	if err := runUpload([]string{"-dir", dir, "-dry-run"}); err != nil {
		t.Fatalf("runUpload dry run failed: %v", err)
	}
}

func TestRunUploadMissingManifest(t *testing.T) {
	err := runUpload([]string{"-dir", t.TempDir(), "-dry-run"})
	if err == nil {
		t.Fatal("runUpload should fail when the manifest is missing")
	}
}

func TestRunUploadMissingAsset(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "release.json", `{"version":"1.2.3","assets":[{"name":"missing.dmg"}]}`)

	if err := runUpload([]string{"-dir", dir, "-dry-run"}); err == nil {
		t.Fatal("runUpload should fail when a manifest asset is missing")
	}
}

func TestRunUploadNoVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.dmg", "x")
	writeFile(t, dir, "release.json", `{"assets":[{"name":"a.dmg"}]}`)

	if err := runUpload([]string{"-dir", dir, "-dry-run"}); err == nil {
		t.Fatal("runUpload should fail when no version can be determined")
	}
}

func TestRunUploadVersionFlagOverridesManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.dmg", "x")
	writeFile(t, dir, "release.json", `{"version":"1.0.0","assets":[{"name":"a.dmg"}]}`)

	if err := runUpload([]string{"-dir", dir, "-version", "v2.0.0", "-dry-run"}); err != nil {
		t.Fatalf("runUpload dry run failed: %v", err)
	}
}
