package device

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	// AndroidPackageExtension is the installable Android package format.
	AndroidPackageExtension = ".apk"
	// IOSPackageExtension is the installable iOS package format.
	IOSPackageExtension = ".ipa"
	// HarmonyPackageExtension is the installable HarmonyOS package format.
	HarmonyPackageExtension = ".hap"
)

// appInstallTimeout bounds a single install on the device. Installs stream the
// whole package, so a game-sized bundle can legitimately take minutes.
const appInstallTimeout = 10 * time.Minute

// AppScope selects which installed apps a listing should include.
type AppScope string

const (
	AppScopeUser   AppScope = "user"
	AppScopeSystem AppScope = "system"
	AppScopeAll    AppScope = "all"
)

func (s AppScope) Valid() bool {
	switch s {
	case AppScopeUser, AppScopeSystem, AppScopeAll:
		return true
	default:
		return false
	}
}

// ParseAppScope parses an optional scope value, defaulting to user.
func ParseAppScope(value string) (AppScope, error) {
	if value == "" {
		return AppScopeUser, nil
	}
	scope := AppScope(value)
	if !scope.Valid() {
		return "", fmt.Errorf("invalid app scope %q: expected user, system or all", value)
	}
	return scope, nil
}

// CurrentApp describes the foreground app.
type CurrentApp struct {
	AppID    string `json:"app_id"`
	Activity string `json:"activity,omitempty"`
}

// AppController is implemented by platforms that support app management.
type AppController interface {
	ListApps(scope AppScope) ([]string, error)
	StartApp(appID, activity string) (CurrentApp, error)
	StopApp(appID string) error
	CurrentApp() (CurrentApp, error)
	// InstallApp installs the local package at localPath. Device-side failures
	// such as a rejected package are reported as errors.
	InstallApp(localPath string) error
	// PackageExtension returns the local package extension InstallApp accepts.
	PackageExtension() string
}

// ErrAppManagementUnsupported is returned for platforms without app APIs.
var ErrAppManagementUnsupported = errors.New("app management is not supported on this device")

// validatePackageExtension rejects packages that are not of the platform's
// installable format. The extension check mirrors what the platform installers
// enforce, so the HTTP layer can answer before streaming a large upload.
func validatePackageExtension(localPath, ext string) error {
	if strings.TrimSpace(localPath) == "" {
		return errors.New("package path is required")
	}
	if !strings.EqualFold(filepath.Ext(localPath), ext) {
		return fmt.Errorf("expected a %s package, got %q", ext, filepath.Base(localPath))
	}
	return nil
}

// GetAppController returns the app controller for a device, when supported.
func GetAppController(deviceID string) (*DeviceWrapper, AppController, error) {
	d, err := NewDevice(deviceID)
	if err != nil {
		return nil, nil, err
	}
	controller, ok := d.Device.(AppController)
	if !ok {
		return d, nil, ErrAppManagementUnsupported
	}
	return d, controller, nil
}
