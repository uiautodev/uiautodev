//go:build !devtools

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// addDevToolsMenu is a no-op when the build does not enable Wails' devtools
// tag, since the developer tools are not available in that build.
func addDevToolsMenu(_ *application.Menu, _ nativeLabels, _ *App) {}
