package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"github.com/uiautodev/uiautodev/internal/update"
	"github.com/uiautodev/uiautodev/version"
	"github.com/uiautodev/uiautodev/webserver"

	"github.com/labstack/echo/v4"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// App struct
type App struct {
	app        *application.App
	echoServer *echo.Echo
	// apiToken is a per-process secret that guards cluster management routes.
	// It is only exposed through the Wails bridge, so web pages cannot mutate
	// cluster settings through the CORS-enabled local API.
	apiToken string
}

// NewApp creates a new App application struct
func NewApp() *App {
	apiToken, err := randomToken()
	if err != nil {
		slog.Warn("failed to generate api token", "error", err)
	}
	return &App{apiToken: apiToken}
}

func randomToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GetAPIToken returns the per-process desktop API token.
func (a *App) GetAPIToken() string {
	return a.apiToken
}

// ServiceStartup is called by Wails when the application starts.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	initLogger("app.log")
	defer logPanic()
	slog.Info("app startup", "pid", os.Getpid())
	update.CleanupOldExecutables()
	// The wails frontend is served by the wails AssetServer (a different origin),
	// so CORS must be enabled; the trusted desktop environment needs no password.
	a.echoServer = webserver.CreateEchoServerWithConfig(webserver.ServerConfig{
		CORS:               true,
		DisablePluginAuth:  true,
		Audience:           "desktop",
		APIToken:           a.apiToken,
		StartIOSWDAWatcher: true,
		Quit: func() {
			if a.app != nil {
				a.app.Quit()
			}
		},
		OnLocaleChange: func(string) {
			if a.app != nil {
				application.InvokeSync(func() {
					a.app.Menu.Set(createMenu(a))
				})
			}
		},
	})
	go func() {
		slog.Info("starting HTTP API server", "addr", "localhost:44944")
		err := a.echoServer.Start("localhost:44944")
		slog.Info("HTTP API server exited", "err", err)
	}()
	return nil
}

// shutdown is called when the app is closing
func (a *App) shutdown() {
	slog.Info("app shutdown")
	if logFile != nil {
		logFile.Close()
	}
	webserver.CloseProxyLogger()
	if a.echoServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		a.echoServer.Shutdown(ctx)
	}
	webserver.CloseCluster()
}

func (a *App) GetServerConfig() webserver.ServerConfig {
	config := webserver.ServerConfig{}
	config.DisableDeviceListing = false
	config.Audience = "desktop"
	config.Version = version.Version
	config.BuildTime = version.BuildTime
	config.GitCommit = version.GitCommit
	return config
}

func SafeStringFunc(f func() (string, error)) string {
	s, err := f()
	if err != nil {
		return ""
	}
	return s
}

func (a *App) RefreshWindowTitle() {
	if a.app == nil {
		return
	}
	window := a.app.Window.Current()
	if window == nil {
		return
	}
	window.SetTitle(loadWindowTitle())
}

// CheckForUpdates runs an update check from the native menu and reports the
// result in a native dialog. Viewing details opens the in-app update section.
func (a *App) CheckForUpdates() {
	if a.app == nil {
		return
	}
	labels := currentNativeLabels()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := webserver.CheckForUpdate(ctx)
	if err != nil {
		a.app.Dialog.Error().
			SetTitle(labels.CheckUpdates).
			SetMessage(labels.CheckFailed + err.Error()).
			Show()
		return
	}
	if !info.Available {
		a.app.Dialog.Info().
			SetTitle(labels.CheckUpdates).
			SetMessage(fmt.Sprintf(labels.UpToDateFmt, info.Current)).
			Show()
		return
	}

	dialog := a.app.Dialog.Question().
		SetTitle(labels.UpdateTitle).
		SetMessage(fmt.Sprintf(labels.AvailableFmt, info.Latest, info.Current))

	updateButton := dialog.AddButton(labels.UpdateButton)
	updateButton.OnClick(func() {
		if err := webserver.StartUpdate(info); err != nil {
			a.app.Dialog.Error().
				SetTitle(labels.UpdateFailed).
				SetMessage(err.Error()).
				Show()
			return
		}
		a.OpenUpdateSettings()
	})
	viewButton := dialog.AddButton(labels.ViewDetails)
	viewButton.OnClick(a.OpenUpdateSettings)
	laterButton := dialog.AddButton(labels.Later)

	dialog.SetDefaultButton(updateButton)
	dialog.SetCancelButton(laterButton)
	dialog.Show()
}

// OpenSettings asks the frontend to open the settings page.
func (a *App) OpenSettings() {
	if a.app == nil {
		return
	}
	a.app.Event.Emit("open-settings")
}

// OpenUpdateSettings asks the frontend to navigate to the update section.
func (a *App) OpenUpdateSettings() {
	if a.app == nil {
		return
	}
	a.app.Event.Emit("open-update-settings")
}

func (a *App) SaveFileDialog(defaultFilename string, exts []string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("application is not ready")
	}
	dialog := a.app.Dialog.SaveFile()
	dialog.SetOptions(&application.SaveFileDialogOptions{
		Title:    "Save File",
		Filename: defaultFilename,
	})
	if len(exts) > 0 {
		dialog.AddFilter(filterDisplayName(exts), filterPattern(exts))
	}
	return dialog.PromptForSingleSelection()
}

func filterPattern(exts []string) string {
	patterns := make([]string, 0, len(exts))
	for _, ext := range exts {
		patterns = append(patterns, "*."+strings.TrimPrefix(ext, "."))
	}
	return strings.Join(patterns, ";")
}

func filterDisplayName(exts []string) string {
	labels := make([]string, 0, len(exts))
	for _, ext := range exts {
		labels = append(labels, strings.ToUpper(strings.TrimPrefix(ext, ".")))
	}
	return strings.Join(labels, ", ") + " Files"
}

func (a *App) SaveFileWithData(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
