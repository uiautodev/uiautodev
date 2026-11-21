package webserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/internal/update"
	"github.com/uiautodev/uiautodev/version"

	"github.com/labstack/echo/v4"
)

// updateManifestURL is the stable entry point for release metadata. Tests
// replace it with a stub server.
var updateManifestURL = "https://dl.uiauto.dev/updates/latest.json"

// updateBaseURL is where versioned release directories live.
var updateBaseURL = "https://dl.uiauto.dev"

var (
	updateClientMu sync.Mutex
	updateClient   *update.Client
	updateApplyMu  sync.Mutex
)

func configureUpdateClient(config ServerConfig) {
	updateClientMu.Lock()
	defer updateClientMu.Unlock()
	updateClient = update.New(update.Options{
		ManifestURL: updateManifestURL,
		BaseURL:     updateBaseURL,
		DataDir:     filepath.Join(GetUserConfigDir(), "updates"),
		Desktop:     config.Audience == "desktop",
		Quit:        config.Quit,
	})
}

func currentUpdateClient() (*update.Client, error) {
	updateClientMu.Lock()
	defer updateClientMu.Unlock()
	if updateClient == nil {
		return nil, errors.New("updater is not configured")
	}
	return updateClient, nil
}

// CheckForUpdate exposes the configured updater to the native menu.
func CheckForUpdate(ctx context.Context) (update.Info, error) {
	client, err := currentUpdateClient()
	if err != nil {
		return update.Info{}, err
	}
	return client.Check(ctx, version.Version)
}

// setUpdateClient is used by tests to inject a client with a stub manifest URL.
func setUpdateClient(client *update.Client) {
	updateClientMu.Lock()
	defer updateClientMu.Unlock()
	updateClient = client
}

// StartUpdate starts a background install for the given update info. It is
// shared by the HTTP handler and the native menu dialog.
func StartUpdate(info update.Info) error {
	client, err := currentUpdateClient()
	if err != nil {
		return err
	}
	if !info.Desktop {
		return errors.New("in-app updates are only available in the desktop app")
	}
	if !info.Available {
		return errors.New("already up to date")
	}
	if !updateApplyMu.TryLock() {
		return errors.New("an update is already in progress")
	}
	client.ResetProgress()
	go func() {
		defer updateApplyMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		result, err := client.Apply(ctx, info)
		if err != nil {
			slog.Error("apply update failed", "error", err)
			return
		}
		slog.Info("update installed", "version", info.Latest, "path", result.Path)
		time.Sleep(1200 * time.Millisecond)
		if err := client.Restart(); err != nil {
			slog.Error("restart after update failed", "error", err)
		}
	}()
	return nil
}

func updateInfoHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := currentUpdateClient()
		if err != nil {
			return updateError(c, http.StatusInternalServerError, err)
		}
		info, err := client.Check(c.Request().Context(), version.Version)
		if err != nil {
			return updateError(c, http.StatusBadGateway, err)
		}
		return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: info}, "  ")
	}
}

func updateApplyHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := currentUpdateClient()
		if err != nil {
			return updateError(c, http.StatusInternalServerError, err)
		}
		info, err := client.Check(c.Request().Context(), version.Version)
		if err != nil {
			return updateError(c, http.StatusBadGateway, err)
		}
		if err := StartUpdate(info); err != nil {
			return updateError(c, http.StatusConflict, err)
		}
		return c.JSONPretty(http.StatusAccepted, APIResponse{
			Code:    0,
			Message: "update started",
			Data:    map[string]any{"version": info.Latest},
		}, "  ")
	}
}

func updateStatusHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := currentUpdateClient()
		if err != nil {
			return updateError(c, http.StatusInternalServerError, err)
		}
		return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: client.Progress()}, "  ")
	}
}

func updateError(c echo.Context, status int, err error) error {
	return c.JSONPretty(status, APIResponse{Code: status, Message: err.Error()}, "  ")
}
