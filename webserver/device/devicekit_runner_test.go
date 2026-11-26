package device

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevicekitDownloadURL(t *testing.T) {
	arm64, err := devicekitDownloadURL(devicekitTargetSimulator, "arm64")
	if err != nil {
		t.Fatalf("arm64: %v", err)
	}
	wantArm64 := devicekitDownloadBaseURL + "/" + devicekitVersion + "/devicekit-ios-Sim-arm64.zip"
	if arm64 != wantArm64 {
		t.Errorf("arm64 URL = %q, want %q", arm64, wantArm64)
	}

	amd64, err := devicekitDownloadURL(devicekitTargetSimulator, "amd64")
	if err != nil {
		t.Fatalf("amd64: %v", err)
	}
	if !strings.HasSuffix(amd64, "/devicekit-ios-Sim-x86_64.zip") {
		t.Errorf("amd64 URL = %q", amd64)
	}

	if _, err := devicekitDownloadURL(devicekitTargetSimulator, "riscv64"); err == nil {
		t.Error("expected an error for an unsupported architecture")
	}
	if _, err := devicekitDownloadURL(devicekitTargetDevice, "arm64"); err == nil {
		t.Error("expected an error for the not yet supported real device target")
	}
	if _, err := devicekitDownloadURL(devicekitTarget("bogus"), "arm64"); err == nil {
		t.Error("expected an error for an invalid target")
	}
}

func TestDevicekitCacheDirPerTarget(t *testing.T) {
	dirs := make(map[string]bool)
	for _, target := range []devicekitTarget{devicekitTargetSimulator, devicekitTargetDevice} {
		dir := devicekitCacheDir(target)
		if got := filepath.Base(filepath.Dir(dir)); got != string(target) {
			t.Errorf("cache dir %q target segment = %q, want %q", dir, got, target)
		}
		if filepath.Base(dir) != devicekitVersion {
			t.Errorf("cache dir %q is not versioned by %q", dir, devicekitVersion)
		}
		if dirs[dir] {
			t.Errorf("duplicate cache dir %q", dir)
		}
		dirs[dir] = true
	}
}

func TestDevicekitDownloadURLsMirrorFirst(t *testing.T) {
	direct := devicekitDownloadBaseURL + "/" + devicekitVersion + "/devicekit-ios-Sim-arm64.zip"
	urls, err := devicekitDownloadURLs(devicekitTargetSimulator, "arm64")
	if err != nil {
		t.Fatalf("arm64: %v", err)
	}
	if len(urls) != len(devicekitMirrors)+1 {
		t.Fatalf("got %d URLs, want %d: %v", len(urls), len(devicekitMirrors)+1, urls)
	}
	if urls[0] != "https://gh-proxy.com/"+direct {
		t.Errorf("first URL = %q, want the gh-proxy.com mirror", urls[0])
	}
	if last := urls[len(urls)-1]; last != direct {
		t.Errorf("last URL = %q, want the direct GitHub asset %q", last, direct)
	}
	seen := make(map[string]bool, len(urls))
	for _, u := range urls {
		if u == "" || seen[u] {
			t.Errorf("URLs contain an empty or duplicate entry: %v", urls)
		}
		seen[u] = true
	}

	if _, err := devicekitDownloadURLs(devicekitTargetSimulator, "riscv64"); err == nil {
		t.Error("expected an error for an unsupported architecture")
	}
}

func TestDownloadDevicekitRunnerFallsBackToMirror(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "runner.zip")
	writeTestZip(t, zipPath, map[string]string{
		devicekitRunnerAppName + "/Info.plist": "plist",
	})
	archive, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read test zip: %v", err)
	}

	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusServiceUnavailable)
	}))
	defer github.Close()
	// A proxy may answer 200 with an HTML page; that must not be accepted.
	mirrorErrorPage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>rate limited</html>"))
	}))
	defer mirrorErrorPage.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer mirror.Close()

	dir := t.TempDir()
	got, err := downloadDevicekitRunner(t.Context(), []string{github.URL, mirrorErrorPage.URL, mirror.URL}, dir, devicekitRunnerAppName+"/Info.plist")
	if err != nil {
		t.Fatalf("downloadDevicekitRunner() error: %v", err)
	}
	if filepath.Dir(got) != dir {
		t.Errorf("zip path = %q, want a file inside %q", got, dir)
	}
	if err := checkZipEntry(got, devicekitRunnerAppName+"/Info.plist"); err != nil {
		t.Errorf("downloaded zip is invalid: %v", err)
	}
}

func TestDownloadDevicekitRunnerReportsAllSources(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusServiceUnavailable)
	}))
	defer failing.Close()

	dir := t.TempDir()
	_, err := downloadDevicekitRunner(t.Context(), []string{failing.URL, failing.URL}, dir, devicekitRunnerAppName+"/Info.plist")
	if err == nil {
		t.Fatal("expected an error when every source fails")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error does not mention the HTTP status: %v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("failed downloads left files behind: %v", entries)
	}
}
