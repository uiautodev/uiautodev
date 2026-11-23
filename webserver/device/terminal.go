package device

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// TerminalMessageType identifies a terminal control frame. WebSocket binary
// frames always carry raw terminal data; JSON control frames ride on text
// frames so the PTY stream stays byte-exact.
type TerminalMessageType string

const (
	// TerminalMessageResize asks the PTY to adopt a new window size.
	TerminalMessageResize TerminalMessageType = "resize"
	// TerminalMessageExit reports the remote process exit code.
	TerminalMessageExit TerminalMessageType = "exit"
	// TerminalMessageError reports that the shell could not be started.
	TerminalMessageError TerminalMessageType = "error"
)

// TerminalMessage is the JSON envelope of a terminal control frame.
type TerminalMessage struct {
	Type    TerminalMessageType `json:"type"`
	Rows    uint16              `json:"rows,omitempty"`
	Cols    uint16              `json:"cols,omitempty"`
	Code    *int                `json:"code,omitempty"`
	Message string              `json:"message,omitempty"`
}

// TerminalSession is an interactive shell bridged to a websocket. Read returns
// the process output, Write forwards stdin, Resize updates the window size and
// Wait blocks until the remote process exits.
type TerminalSession interface {
	io.Reader
	io.Writer
	Resize(rows, cols uint16) error
	Wait() (int, error)
	Close() error
}

// TerminalDevice is implemented by devices that can open an interactive shell.
type TerminalDevice interface {
	OpenTerminal(rows, cols uint16) (TerminalSession, error)
}

// ErrTerminalUnsupported is returned when a device cannot host a terminal.
var ErrTerminalUnsupported = errors.New("this device does not support an interactive terminal")

// terminalSessionOpener is a variable so tests can inject a fake session.
var terminalSessionOpener = func(deviceID string, rows, cols uint16) (TerminalSession, error) {
	dev, err := NewDevice(deviceID)
	if err != nil {
		return nil, err
	}
	terminalDevice, ok := dev.Device.(TerminalDevice)
	if !ok {
		return nil, ErrTerminalUnsupported
	}
	return terminalDevice.OpenTerminal(rows, cols)
}

const terminalReadBufferSize = 32 * 1024

// WSTerminalHandler bridges a websocket to an interactive Android shell.
func WSTerminalHandler(c echo.Context) error {
	deviceID := c.Param("device_id")
	// Only local Android devices implement shell v2; remote cluster devices
	// would need a tunnel that does not exist yet.
	if !strings.HasPrefix(deviceID, "a-") {
		return echo.NewHTTPError(http.StatusBadRequest, "terminal is only supported on local Android devices")
	}

	rows := terminalDimension(c, "rows")
	cols := terminalDimension(c, "cols")

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	if unregister := registerStreamConn(c.Request().Context(), ws); unregister != nil {
		defer unregister()
	}

	session, err := terminalSessionOpener(deviceID, rows, cols)
	if err != nil {
		writeTerminalMessage(ws, TerminalMessage{Type: TerminalMessageError, Message: err.Error()})
		return nil
	}
	serveTerminal(ws, session, isReadOnlyStream(c.Request().Context()))
	return nil
}

// terminalDimension reads an optional positive terminal size from the query.
// Invalid or missing values are reported as zero so the device keeps its
// default size.
func terminalDimension(c echo.Context, name string) uint16 {
	value, err := strconv.ParseUint(c.QueryParam(name), 10, 16)
	if err != nil || value == 0 {
		return 0
	}
	return uint16(value)
}

// serveTerminal pumps frames in both directions until the remote shell exits
// or the browser goes away. View-only sessions keep reading output but cannot
// inject input.
func serveTerminal(ws *websocket.Conn, session TerminalSession, readOnly bool) {
	defer session.Close()

	go func() {
		for {
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				// A closed browser must not leave the device shell running.
				session.Close()
				return
			}
			if readOnly {
				continue
			}
			switch messageType {
			case websocket.BinaryMessage:
				if _, err := session.Write(data); err != nil {
					session.Close()
					return
				}
			case websocket.TextMessage:
				handleTerminalControl(session, data)
			}
		}
	}()

	buffer := make([]byte, terminalReadBufferSize)
	for {
		n, err := session.Read(buffer)
		if n > 0 {
			if writeErr := ws.WriteMessage(websocket.BinaryMessage, buffer[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			break
		}
	}

	code, _ := session.Wait()
	writeTerminalMessage(ws, TerminalMessage{Type: TerminalMessageExit, Code: &code})
}

func handleTerminalControl(session TerminalSession, data []byte) {
	var message TerminalMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return
	}
	if message.Type != TerminalMessageResize || message.Rows == 0 || message.Cols == 0 {
		return
	}
	_ = session.Resize(message.Rows, message.Cols)
}

func writeTerminalMessage(ws *websocket.Conn, message TerminalMessage) {
	data, err := json.Marshal(message)
	if err != nil {
		return
	}
	_ = ws.WriteMessage(websocket.TextMessage, data)
}
