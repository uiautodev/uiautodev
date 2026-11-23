package device

import (
	"errors"
	"fmt"
)

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
}

// ErrAppManagementUnsupported is returned for platforms without app APIs.
var ErrAppManagementUnsupported = errors.New("app management is not supported on this device")

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
