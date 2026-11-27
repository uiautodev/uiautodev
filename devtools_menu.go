//go:build devtools

package main

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// addDevToolsMenu appends a View menu with the native webview developer tools.
// Only builds compiled with the Wails devtools tag get it: the menu item would
// do nothing otherwise because Wails strips the devtools support from plain
// production builds.
func addDevToolsMenu(menu *application.Menu, labels nativeLabels, app *App) {
	viewMenu := menu.AddSubmenu(labels.View)
	item := viewMenu.Add(labels.DevTools)
	if runtime.GOOS == "darwin" {
		item.SetAccelerator("Alt+Command+I")
	}
	item.OnClick(func(_ *application.Context) {
		if app.app == nil {
			return
		}
		window := app.app.Window.Current()
		if window == nil {
			return
		}
		window.OpenDevTools()
	})
}
