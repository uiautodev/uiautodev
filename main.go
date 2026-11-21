package main

import (
	"embed"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/adrg/xdg"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/uiautodev/uiautodev/webserver"

	slogmulti "github.com/samber/slog-multi"
)

var logFile *os.File

func getLogDir() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(xdg.Home, "Library", "Logs", "Uiautodev")
	}
	return filepath.Join(xdg.Home, ".uiautodev", "logs")
}

func getLogFile(name string) string {
	logDir := getLogDir()
	os.MkdirAll(logDir, 0755)
	return filepath.Join(logDir, name)
}

func openDir(dir string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("Failed to create directory", "error", err, "dir", dir)
	}

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default: // linux
		cmd = exec.Command("xdg-open", dir)
	}

	if err := cmd.Start(); err != nil {
		slog.Error("Failed to open directory", "error", err, "dir", dir)
		return
	}
	go cmd.Wait()
}

func openLogDir() {
	openDir(getLogDir())
}

func createMenu(app *App) *application.Menu {
	labels := currentNativeLabels()
	appMenu := application.NewMenu()

	if runtime.GOOS == "darwin" {
		appMenu.AddRole(application.AppMenu)
	}

	fileMenu := appMenu.AddSubmenu(labels.File)
	fileMenu.Add(labels.OpenLog).SetAccelerator("cmdorctrl+l").OnClick(func(_ *application.Context) {
		openLogDir()
	})
	fileMenu.AddSeparator()
	fileMenu.Add(labels.OpenPlugin).OnClick(func(_ *application.Context) {
		openDir(webserver.GetUserPluginDir())
	})
	fileMenu.Add(labels.OpenConfig).OnClick(func(_ *application.Context) {
		openDir(webserver.GetUserConfigDir())
	})
	fileMenu.AddSeparator()
	fileMenu.Add(labels.Settings).SetAccelerator("cmdorctrl+,").OnClick(func(_ *application.Context) {
		app.OpenSettings()
	})
	fileMenu.AddSeparator()
	fileMenu.Add(labels.CheckUpdates).OnClick(func(_ *application.Context) {
		go app.CheckForUpdates()
	})

	if runtime.GOOS == "darwin" {
		appMenu.AddRole(application.EditMenu)
	}

	return appMenu
}

func shortPath(path string, depth int) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) <= depth {
		return path
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}

func initLogger(name string) {
	opts := &slog.HandlerOptions{
		AddSource: true, // <-- include file:line
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey { // "source"
				if src, ok := a.Value.Any().(*slog.Source); ok {
					file := src.File
					a.Value = slog.StringValue(shortPath(file, 2))
				}
			}
			return a
		},
		Level: slog.LevelDebug,
	}
	logFilePath := getLogFile(name)
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	logFile = f
	h1 := slog.NewTextHandler(os.Stdout, opts)
	h2 := slog.NewTextHandler(f, opts)

	logger := slog.New(slogmulti.Fanout(h1, h2))
	slog.SetDefault(logger)
	slog.Info("Logger initialized", "name", name, "pid", os.Getpid())
}

//go:embed all:frontend/build
var assets embed.FS

func logPanic() {
	if r := recover(); r != nil {
		slog.Error("caught panic",
			"panic", r,
			"stack", string(debug.Stack()),
		)
	}
}

func loadWindowTitle() string {
	return "UIAutoDev Desktop"
}

func main() {
	app := NewApp()

	wailsApp := application.New(application.Options{
		Name:        "Uiautodev Desktop",
		Description: "UIAutoDev desktop application",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		OnShutdown: app.shutdown,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	app.app = wailsApp
	wailsApp.RegisterService(application.NewService(app))
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            loadWindowTitle(),
		Width:            1024,
		Height:           768,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	wailsApp.Menu.Set(createMenu(app))

	if err := wailsApp.Run(); err != nil {
		println("Error:", err.Error())
	}
}
