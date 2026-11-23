package device

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uiautodev/uiautodev/webserver/utils"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func WSScrcpyHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if !strings.HasPrefix(deviceId, "a-") {
		return errors.New("only android devices supported for scrcpy")
	}
	serial := deviceId[2:]

	// c.Request().URL.Query() returns url.Values with auto-decoded values
	var extraArgs []string
	for key, values := range c.Request().URL.Query() {
		for _, val := range values {
			extraArgs = append(extraArgs, key+"="+val)
		}
	}
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	if unregister := registerStreamConn(c.Request().Context(), ws); unregister != nil {
		defer unregister()
	}

	// The scrcpy websocket is bidirectional; a view-only session may watch but
	// must not inject input events.
	readOnly := isReadOnlyStream(c.Request().Context())
	err = handleAndroidScrcpyWebSocket(ws, serial, extraArgs, readOnly)
	if err != nil {
		slog.Info("scrcpy ws closed", "err", err)
	}
	return nil
}

func MJPEGHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	idInfo, err := ParseDeviceID(deviceId)
	if err != nil {
		return errors.Wrap(err, "invalid device id")
	}

	switch idInfo.Type {
	case IOS:
		return mjpegiOSHandler(c, idInfo)
	case HarmonyOS:
		return fmt.Errorf("use /screen/frames for harmony screen stream")
	default:
		return fmt.Errorf("mjpeg streaming not supported for %s devices", idInfo.Type)
	}
}

// screenFramesStreamer is implemented by devices that can push raw screen
// frames over a websocket.
type screenFramesStreamer interface {
	StartWebsocketStream(ctx context.Context, conn *websocket.Conn) error
}

// wsControlHandler is implemented by devices that accept input control messages
// over the screen websocket in addition to streaming frames.
type wsControlHandler interface {
	HandleControlMessage(ctx context.Context, data []byte) error
}

func WSScreenFramesHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if !strings.HasPrefix(deviceId, "h-") && !strings.HasPrefix(deviceId, "d-") && !strings.HasPrefix(deviceId, "i-") {
		return errors.New("only harmony, demo and ios devices support screen frames")
	}

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	if unregister := registerStreamConn(c.Request().Context(), ws); unregister != nil {
		defer unregister()
	}

	dev, err := NewDevice(deviceId)
	if err != nil {
		return errors.Wrap(err, "failed to create device")
	}

	streamer, ok := dev.Device.(screenFramesStreamer)
	if !ok {
		return errors.New("device does not support screen frames")
	}

	// A view-only session may read frames but must not send control messages.
	var onMessage func(context.Context, []byte)
	if !isReadOnlyStream(c.Request().Context()) {
		onMessage = controlMessageHandler(dev.Device)
	}
	ctx, cancel := startWebsocketReader(c.Request().Context(), ws, onMessage)
	defer cancel()

	err = streamer.StartWebsocketStream(ctx, ws)
	if err != nil {
		slog.Info("ws screen frames closed", "device", deviceId, "err", err)
	}
	return nil
}

// controlMessageHandler returns a websocket message callback for devices that
// accept control commands, or nil for devices that only stream frames.
func controlMessageHandler(dev Device) func(ctx context.Context, data []byte) {
	handler, ok := dev.(wsControlHandler)
	if !ok {
		return nil
	}
	return func(ctx context.Context, data []byte) {
		if err := handler.HandleControlMessage(ctx, data); err != nil {
			slog.Warn("failed to handle websocket control message", "err", err)
		}
	}
}

// startWebsocketReader reads messages from conn until the peer disconnects,
// invoking onMessage for each one when it is non-nil, and cancels the returned
// context when reading fails. Reading from the socket is what lets the server
// notice a peer disconnect: the request context is not canceled when the peer
// closes the connection after the websocket upgrade.
func startWebsocketReader(parent context.Context, conn *websocket.Conn, onMessage func(ctx context.Context, data []byte)) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		defer cancel()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if onMessage != nil {
				onMessage(ctx, data)
			}
		}
	}()
	return ctx, cancel
}

// cancelOnWebsocketClose derives a context from parent that gets canceled once
// conn is closed by the peer. The request context is not canceled when the peer
// closes the connection after the websocket upgrade, so a stream that only
// writes on change (e.g. the demo) would otherwise keep polling after the
// client is gone. Reading from the socket is what lets the server notice the
// disconnect.
func cancelOnWebsocketClose(parent context.Context, conn *websocket.Conn) (context.Context, context.CancelFunc) {
	return startWebsocketReader(parent, conn, nil)
}

func mjpegiOSHandler(c echo.Context, idInfo *DeviceIDInfo) error {
	port, err := utils.GetFreePort()
	if err != nil {
		return errors.Wrap(err, "failed to get free port")
	}
	iosBin, err := IOSPath()
	if err != nil {
		return errors.Wrap(err, "failed to get ios binary")
	}
	cmd := NewCommand(iosBin, "screenshot", "--udid", idInfo.Serial, "--stream", "--port", strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "failed to start MJPEG stream command")
	}
	defer func() {
		slog.Debug("kill ios mjpeg server", "port", port)
		cmd.Process.Kill()
	}()
	slog.Info("starting MJPEG stream", "device", "i-"+idInfo.Serial, "port", port)

	// Wait until the server is up
	if err := utils.WaitForPort("127.0.0.1", port, 5*time.Second); err != nil {
		return errors.Wrap(err, "failed to wait for MJPEG server to start")
	}

	// Proxy the MJPEG stream to the client
	mjpegURL := "http://127.0.0.1:" + strconv.Itoa(port)
	proxyResp, err := http.Get(mjpegURL)
	if err != nil {
		return errors.Wrap(err, "failed to connect to MJPEG stream")
	}
	defer proxyResp.Body.Close()

	// Set headers for MJPEG streaming
	c.Response().Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=--BoundaryString")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	// Copy the stream to the client
	_, err = io.Copy(c.Response(), proxyResp.Body)
	return err
}
