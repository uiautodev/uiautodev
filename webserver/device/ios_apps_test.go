package device

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseIOSAppList(t *testing.T) {
	output := "com.example.one 1.0\ncom.example.two 2.0\n\ncom.example.one 1.0\n"
	apps := parseIOSAppList(output)
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

func TestWDALaunchAndTerminateApp(t *testing.T) {
	var launches []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		switch r.URL.Path {
		case "/wda/apps/launch":
			launches = append(launches, "launch:"+payload["bundleId"])
		case "/wda/apps/terminate":
			launches = append(launches, "terminate:"+payload["bundleId"])
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"value":null}`))
	}))
	defer srv.Close()

	client := NewWDAClient(srv.URL)
	if err := client.LaunchApp("com.example.app"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if err := client.TerminateApp("com.example.app"); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if len(launches) != 2 || launches[0] != "launch:com.example.app" || launches[1] != "terminate:com.example.app" {
		t.Fatalf("unexpected calls: %v", launches)
	}
}

func TestWDALaunchAppFallsBackToSession(t *testing.T) {
	var sessionLaunched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wda/apps/launch":
			w.WriteHeader(http.StatusNotFound)
		case "/status":
			_, _ = w.Write([]byte(`{"value":{"sessionId":"abc"},"sessionId":"abc"}`))
		case "/session/abc/wda/apps/launch":
			sessionLaunched = true
			_, _ = w.Write([]byte(`{"value":null}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewWDAClient(srv.URL)
	if err := client.LaunchApp("com.example.app"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !sessionLaunched {
		t.Fatal("expected session fallback")
	}
}

func TestWDAActiveAppInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wda/activeAppInfo" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"value":{"bundleId":"com.example.app","name":"Example"}}`))
	}))
	defer srv.Close()

	client := NewWDAClient(srv.URL)
	info, err := client.ActiveAppInfo()
	if err != nil {
		t.Fatalf("active app info: %v", err)
	}
	if info.BundleID != "com.example.app" || info.Name != "Example" {
		t.Fatalf("got %+v", info)
	}
}

func TestWDAActiveAppInfoFallsBackToSession(t *testing.T) {
	var sessionUsed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wda/activeAppInfo":
			w.WriteHeader(http.StatusNotFound)
		case "/status":
			_, _ = w.Write([]byte(`{"value":{"sessionId":"abc"},"sessionId":"abc"}`))
		case "/session/abc/wda/activeAppInfo":
			sessionUsed = true
			_, _ = w.Write([]byte(`{"value":{"bundleId":"com.example.app"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewWDAClient(srv.URL)
	info, err := client.ActiveAppInfo()
	if err != nil {
		t.Fatalf("active app info: %v", err)
	}
	if !sessionUsed || info.BundleID != "com.example.app" {
		t.Fatalf("sessionUsed=%v info=%+v", sessionUsed, info)
	}
}
