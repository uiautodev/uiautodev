package device

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// fakeTerminalSession records what the bridge forwards and lets the test push
// output and an exit code back, without touching a real device.
type fakeTerminalSession struct {
	outputReader *io.PipeReader
	outputWriter *io.PipeWriter
	inputCh      chan []byte
	resizeCh     chan [2]uint16
	exitCh       chan int
	closeCh      chan struct{}
	closeOnce    sync.Once
}

func newFakeTerminalSession() *fakeTerminalSession {
	reader, writer := io.Pipe()
	return &fakeTerminalSession{
		outputReader: reader,
		outputWriter: writer,
		inputCh:      make(chan []byte, 4),
		resizeCh:     make(chan [2]uint16, 4),
		exitCh:       make(chan int, 1),
		closeCh:      make(chan struct{}),
	}
}

func (f *fakeTerminalSession) Read(p []byte) (int, error) {
	return f.outputReader.Read(p)
}

func (f *fakeTerminalSession) Write(p []byte) (int, error) {
	select {
	case f.inputCh <- append([]byte(nil), p...):
	default:
	}
	return len(p), nil
}

func (f *fakeTerminalSession) Resize(rows, cols uint16) error {
	select {
	case f.resizeCh <- [2]uint16{rows, cols}:
	default:
	}
	return nil
}

func (f *fakeTerminalSession) Wait() (int, error) {
	code := <-f.exitCh
	return code, nil
}

func (f *fakeTerminalSession) Close() error {
	f.closeOnce.Do(func() {
		close(f.closeCh)
		_ = f.outputWriter.CloseWithError(io.ErrClosedPipe)
	})
	return nil
}

func awaitValue[T any](t *testing.T, ch <-chan T, message string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
	var zero T
	return zero
}

func TestTerminalHandlerRejectsNonAndroid(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/stream/devices/i-phone/terminal", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("device_id")
	c.SetParamValues("i-phone")

	err := WSTerminalHandler(c)

	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestTerminalWebSocketBridgesFrames(t *testing.T) {
	session := newFakeTerminalSession()
	opened := make(chan [2]uint16, 1)

	previousOpener := terminalSessionOpener
	terminalSessionOpener = func(deviceID string, rows, cols uint16) (TerminalSession, error) {
		require.Equal(t, "a-serial", deviceID)
		opened <- [2]uint16{rows, cols}
		return session, nil
	}
	defer func() {
		terminalSessionOpener = previousOpener
		// Let Wait return so the handler goroutine can finish.
		select {
		case session.exitCh <- 0:
		default:
		}
	}()

	e := echo.New()
	e.GET("/stream/devices/:device_id/terminal", WSTerminalHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices/a-serial/terminal?rows=30&cols=90"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)
	defer conn.Close()

	require.Equal(t, [2]uint16{30, 90}, awaitValue(t, opened, "terminal session was not opened with the requested size"))

	// Binary frames are keyboard input forwarded to the remote stdin.
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("echo hi\n")))
	require.Equal(t, []byte("echo hi\n"), awaitValue(t, session.inputCh, "input was not forwarded to the session"))

	// Text frames carry control messages, here a resize.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","rows":40,"cols":100}`)))
	require.Equal(t, [2]uint16{40, 100}, awaitValue(t, session.resizeCh, "resize was not forwarded to the session"))

	// When the remote process exits the bridge reports its exit code.
	require.NoError(t, session.outputWriter.Close())
	session.exitCh <- 7

	messageType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"exit","code":7}`, string(data))
}

func TestTerminalWebSocketSendsOutputAndExit(t *testing.T) {
	session := newFakeTerminalSession()

	previousOpener := terminalSessionOpener
	terminalSessionOpener = func(string, uint16, uint16) (TerminalSession, error) {
		return session, nil
	}
	defer func() { terminalSessionOpener = previousOpener }()

	e := echo.New()
	e.GET("/stream/devices/:device_id/terminal", WSTerminalHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices/a-serial/terminal"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)

	output := make(chan error, 1)
	go func() {
		_, err := session.outputWriter.Write([]byte("hello\n"))
		output <- err
	}()

	messageType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, messageType)
	require.Equal(t, "hello\n", string(data))
	require.NoError(t, awaitValue(t, output, "output was not consumed"))

	require.NoError(t, session.outputWriter.Close())
	session.exitCh <- 3

	messageType, data, err = conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"exit","code":3}`, string(data))

	require.NoError(t, conn.Close())
	awaitValue(t, session.closeCh, "session was not closed after the browser disconnected")
}

// The bridge must stay silent until the remote shell writes something, so the
// terminal starts at the device prompt instead of an application banner.
func TestTerminalWebSocketDoesNotSendBanner(t *testing.T) {
	session := newFakeTerminalSession()

	previousOpener := terminalSessionOpener
	terminalSessionOpener = func(string, uint16, uint16) (TerminalSession, error) {
		return session, nil
	}
	defer func() { terminalSessionOpener = previousOpener }()

	e := echo.New()
	e.GET("/stream/devices/:device_id/terminal", WSTerminalHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices/a-serial/terminal"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err, "server sent a frame before the shell produced output")

	session.exitCh <- 0
	require.NoError(t, conn.Close())
	awaitValue(t, session.closeCh, "session was not closed after the browser disconnected")
}

func TestTerminalWebSocketClosesSessionOnDisconnect(t *testing.T) {
	session := newFakeTerminalSession()

	previousOpener := terminalSessionOpener
	terminalSessionOpener = func(string, uint16, uint16) (TerminalSession, error) {
		return session, nil
	}
	defer func() { terminalSessionOpener = previousOpener }()

	e := echo.New()
	e.GET("/stream/devices/:device_id/terminal", WSTerminalHandler)
	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream/devices/a-serial/terminal"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)

	// Wait blocks until the process exits; buffering the code lets the handler
	// finish once the disconnect closes the output stream.
	session.exitCh <- 0
	require.NoError(t, conn.Close())

	awaitValue(t, session.closeCh, "session was not closed after the browser disconnected")
}
