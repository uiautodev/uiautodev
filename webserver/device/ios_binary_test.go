package device

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
)

func TestIOSDownloadURL(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		url, err := iosDownloadURL(goos)
		if err != nil {
			t.Fatalf("iosDownloadURL(%q): %v", goos, err)
		}
		if url == "" {
			t.Errorf("iosDownloadURL(%q) returned empty URL", goos)
		}
	}
	if _, err := iosDownloadURL("linux"); err == nil {
		t.Error("iosDownloadURL(linux) should return an error")
	}
}

func TestDownloadIOSBinary(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("ios helper is only available for darwin and windows")
	}

	content := []byte("fake ios binary")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+runtime.GOOS+"/"+iosBinaryName() {
			http.NotFound(w, r)
			return
		}
		w.Write(content)
	}))
	defer ts.Close()

	oldBase := iosDownloadBaseURL
	iosDownloadBaseURL = ts.URL
	defer func() { iosDownloadBaseURL = oldBase }()

	dst := t.TempDir() + "/" + iosBinaryName()
	if err := downloadIOSBinary(dst); err != nil {
		t.Fatalf("downloadIOSBinary: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("downloaded content = %q, want %q", got, content)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat downloaded file: %v", err)
	}
	if info.Mode().Perm()&0755 != 0755 {
		t.Errorf("downloaded file mode = %v, want executable", info.Mode().Perm())
	}
}

func TestDownloadIOSBinaryHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	oldBase := iosDownloadBaseURL
	iosDownloadBaseURL = ts.URL
	defer func() { iosDownloadBaseURL = oldBase }()

	dst := t.TempDir() + "/" + iosBinaryName()
	if err := downloadIOSBinary(dst); err == nil {
		t.Fatal("downloadIOSBinary should fail on HTTP 404")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("failed download should not leave a file behind")
	}
}
