package device

import (
	"os"
	"testing"
)

// TestAppControllerIntegration exercises the real app APIs against a device.
// It is skipped unless UIAUTODEV_TEST_DEVICE is set, e.g.
//
//	UIAUTODEV_TEST_DEVICE=h-SERIAL UIAUTODEV_TEST_APP=com.example.app go test ./webserver/device -run TestAppControllerIntegration -v
func TestAppControllerIntegration(t *testing.T) {
	deviceID := os.Getenv("UIAUTODEV_TEST_DEVICE")
	if deviceID == "" {
		t.Skip("set UIAUTODEV_TEST_DEVICE to run the integration test")
	}
	appID := os.Getenv("UIAUTODEV_TEST_APP")
	if appID == "" {
		t.Skip("set UIAUTODEV_TEST_APP to run the integration test")
	}

	_, controller, err := GetAppController(deviceID)
	if err != nil {
		t.Fatalf("get controller: %v", err)
	}

	apps, err := controller.ListApps(AppScopeUser)
	if err != nil {
		t.Fatalf("list apps: %v", err)
	}
	t.Logf("user apps: %d", len(apps))
	if len(apps) == 0 {
		t.Fatal("expected at least one user app")
	}
	found := false
	for _, app := range apps {
		if app == appID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s is not in the user app list", appID)
	}

	if _, err := controller.StartApp(appID, ""); err != nil {
		t.Fatalf("start app: %v", err)
	}

	systemApps, err := controller.ListApps(AppScopeSystem)
	if err != nil {
		t.Fatalf("list system apps: %v", err)
	}
	allApps, err := controller.ListApps(AppScopeAll)
	if err != nil {
		t.Fatalf("list all apps: %v", err)
	}
	t.Logf("system apps: %d, all apps: %d", len(systemApps), len(allApps))
	if len(systemApps) == 0 {
		t.Fatal("expected at least one system app")
	}
	if len(allApps) < len(apps) || len(allApps) < len(systemApps) {
		t.Fatalf("all apps (%d) should cover user (%d) and system (%d)", len(allApps), len(apps), len(systemApps))
	}
	current, err := controller.CurrentApp()
	if err != nil {
		t.Fatalf("current app after start: %v", err)
	}
	t.Logf("current after start: %+v", current)
	if current.AppID != appID {
		t.Fatalf("expected %s to be in the foreground, got %+v", appID, current)
	}

	if err := controller.StopApp(appID); err != nil {
		t.Fatalf("stop app: %v", err)
	}
	current, err = controller.CurrentApp()
	if err != nil {
		t.Fatalf("current app after stop: %v", err)
	}
	t.Logf("current after stop: %+v", current)
	if current.AppID == appID {
		t.Fatalf("%s is still in the foreground after stop", appID)
	}
}
