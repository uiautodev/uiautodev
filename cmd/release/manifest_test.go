package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uiautodev/uiautodev/internal/update"
)

func TestRunManifestWritesManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "UiautodevDesktop-mac-1.2.3.dmg", "mac")

	out := filepath.Join(dir, "release.json")
	if err := runManifest([]string{"-dir", dir, "-version", "1.2.3", "-out", out}); err != nil {
		t.Fatal(err)
	}

	manifest, err := update.ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", manifest.Version)
	}
	if len(manifest.Assets) != 1 || manifest.Assets[0].Name != "UiautodevDesktop-mac-1.2.3.dmg" {
		t.Fatalf("assets = %+v", manifest.Assets)
	}
}

func TestRunManifestReleaseJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.dmg", "x")
	releaseJSON := filepath.Join(t.TempDir(), "gh-release.json")
	if err := os.WriteFile(releaseJSON, []byte(`{"body":"notes","publishedAt":"2026-01-01T00:00:00Z","url":"https://example.com/release"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "release.json")
	err := runManifest([]string{"-dir", dir, "-version", "1.2.3", "-release-json", releaseJSON, "-out", out})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := update.ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Notes != "notes" || manifest.PublishedAt != "2026-01-01T00:00:00Z" || manifest.ReleaseURL != "https://example.com/release" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.Assets) != 1 || manifest.Assets[0].Name != "a.dmg" {
		t.Fatalf("assets = %+v", manifest.Assets)
	}
}
