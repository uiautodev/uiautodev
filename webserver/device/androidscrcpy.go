package device

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/codeskyblue/go-adbkit/adb"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

//go:embed static
var staticFiles embed.FS

type ScrcpyService struct {
	device    *adb.Device
	serial    string
	cancel    context.CancelFunc // used to stop the scrcpy server
	quitChan  chan error         // pipeVideo, pipeControl all send error to this chan
	version   string
	scid      int32    // https://github.com/Genymobile/scrcpy/blob/b08093d1c09d3fff1238abfa57e3efddeadcf41c/doc/develop.md?plain=1#L314
	extraArgs []string // additional arguments to pass to scrcpy server
}

func NewScrcpyService(serial string, extraArgs []string) (*ScrcpyService, error) {
	client := getAdbClient()
	device := client.Device(adb.DeviceWithSerial(serial))

	// Verify device is available
	_, err := device.State()
	if err != nil {
		return nil, fmt.Errorf("device is not ok: %s", serial)
	}

	scid := rand.New(rand.NewSource(time.Now().UnixNano())).Int31()
	return &ScrcpyService{
		device:    device,
		serial:    serial,
		quitChan:  make(chan error, 1),
		version:   "3.3.3",
		scid:      scid,
		extraArgs: extraArgs,
	}, nil
}

func (s *ScrcpyService) Wait() error {
	return <-s.quitChan
}

// Start starts the scrcpy server, pipes video to rwc and, when control is true,
// relays input events from rwc. With control=false the control socket is still
// opened (the server waits for it before streaming) but every input message from
// the client is dropped.
func (s *ScrcpyService) Start(rwc io.ReadWriter, control bool) (err error) {
	defer func() {
		if err != nil {
			slog.Error("scrcpy service start failed:", "serial", s.serial, "err", err)
			s.Stop()
		}
	}()
	if err = s.pushJar(); err != nil {
		return err
	}
	if err = s.startJarServer(); err != nil {
		return err
	}

	if err := s.pipeVideo(rwc); err != nil {
		return err
	}
	reader := io.Reader(rwc)
	if !control {
		reader = &discardingReader{r: rwc}
	}
	if err := s.pipeControl(reader); err != nil {
		return err
	}
	return nil
}

// discardingReader reads the websocket so a disconnect is still detected but
// throws the data away, which keeps a view-only session from injecting input.
type discardingReader struct {
	r   io.Reader
	buf []byte
}

func (d *discardingReader) Read(p []byte) (int, error) {
	if len(d.buf) < len(p) {
		d.buf = make([]byte, len(p))
	}
	n, err := d.r.Read(d.buf[:len(p)])
	return n, err
}

func (s *ScrcpyService) Stop() error {
	slog.Debug("stopping scrcpy service", "serial", s.serial)
	// Cancel the context to stop the scrcpy server
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *ScrcpyService) getJarFileName() string {
	return fmt.Sprintf("scrcpy-server-v%s.jar", s.version)
}

func (s *ScrcpyService) pushJar() error {
	// Open scrcpy server jar file
	jarFile, err := staticFiles.Open(fmt.Sprintf("static/%s", s.getJarFileName()))
	if err != nil {
		return errors.Wrap(err, "failed to open scrcpy server jar")
	}
	defer jarFile.Close()

	syncService, err := s.device.NewSyncService()
	if err != nil {
		return err
	}
	defer syncService.Close()

	opts := adb.SyncPushOptions{
		Mode:  0644,
		Mtime: time.Now().Unix(),
	}
	err = syncService.Push(jarFile, fmt.Sprintf("/data/local/tmp/%s", s.getJarFileName()), opts)
	if err != nil {
		return err
	}
	return nil
}

func (s *ScrcpyService) startJarServer() error {
	// Start scrcpy server on device
	shellArgs := []string{
		fmt.Sprintf("CLASSPATH=/data/local/tmp/%s", s.getJarFileName()),
		"app_process",
		"/",
		"com.genymobile.scrcpy.Server",
		s.version,
		fmt.Sprintf("scid=%x", s.scid),
		"log_level=verbose",
		"tunnel_forward=true",
		"audio=false",
	}
	// There are other args. for example:
	// max_size=1000
	// max_fps=30 video_bit_rate=2000000
	// send_frame_meta=true
	// control=true
	// show_touches=true
	// stay_awake=true
	// clipboard_autosync=true
	// display_id=0
	argKeys := make(map[string]bool)
	for _, arg := range shellArgs {
		if key, _, ok := strings.Cut(arg, "="); ok {
			argKeys[key] = true
		}
	}
	// should not have duplicate keys
	for _, arg := range s.extraArgs {
		if key, _, ok := strings.Cut(arg, "="); ok {
			if argKeys[key] {
				slog.Warn("duplicate argument key in extraArgs, skipping", "key", key)
				continue
			}
			shellArgs = append(shellArgs, arg)
		}
	}

	// Build shell command string
	shellCmd := strings.Join(shellArgs, " ")
	slog.Info("Starting scrcpy server", "serial", s.serial, "cmd", shellCmd)

	// Create context for controlling the server
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	go func() {
		// Use RunCommandContext to start and monitor the server
		output, err := s.device.RunCommandContext(ctx, shellCmd)
		if err != nil && ctx.Err() != context.Canceled {
			slog.Debug("scrcpy server error:", "serial", s.serial, "error", err)
		}
		slog.Debug("scrcpy stopped, output:", "serial", s.serial, "output", strings.TrimSpace(output))
		s.quitChan <- err
	}()

	// Wait a bit for server to start
	select {
	case err := <-s.quitChan:
		return err
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func (s *ScrcpyService) getRemoteAbstractName() string {
	if s.scid <= 0 {
		return "localabstract:scrcpy"
	}
	return fmt.Sprintf("localabstract:scrcpy_%08x", s.scid) // 16进制
}

func (s *ScrcpyService) connectToServerWithRetry() (net.Conn, error) {
	var conn net.Conn
	var err error
	// Use device.OpenSocket to directly connect to scrcpy server socket
	for i := 0; i < 30; i++ {
		conn, err = s.device.OpenSocket(s.getRemoteAbstractName())
		if err == nil {
			return conn, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, err
}

func (s *ScrcpyService) pipeVideo(writer io.Writer) error {
	conn, err := s.connectToServerWithRetry()
	if err != nil {
		return err
	}

	var dummyByte [1]byte
	_, err = conn.Read(dummyByte[:])
	if err != nil {
		conn.Close()
		return err
	}
	if dummyByte[0] != 0x00 {
		conn.Close()
		return errors.New("invalid scrcpy server response")
	}
	slog.Info("scrcpy video stream started", "serial", s.serial)
	go func() {
		_, err := io.Copy(writer, conn)
		conn.Close()
		slog.Debug("pipeVideo finished")
		s.quitChan <- errors.Wrap(err, "pipeVideo")
	}()
	return nil
}

func (s *ScrcpyService) pipeControl(reader io.Reader) error {
	conn, err := s.device.OpenSocket(s.getRemoteAbstractName())
	if err != nil {
		return err
	}
	go func() {
		_, err := io.Copy(conn, reader)
		conn.Close()
		slog.Debug("pipeControl finished")
		s.quitChan <- errors.Wrap(err, "pipeControl")
	}()
	return nil
}

type WebsocketRWWrapper struct {
	conn *websocket.Conn
}

func (w *WebsocketRWWrapper) Read(p []byte) (n int, err error) {
	_, reader, err := w.conn.NextReader()
	if err != nil {
		return 0, err
	}
	return reader.Read(p)
}

func (w *WebsocketRWWrapper) Write(p []byte) (n int, err error) {
	writer, err := w.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, err = writer.Write(p)
	if err != nil {
		return n, err
	}
	err = writer.Close()
	return n, err
}

func handleAndroidScrcpyWebSocket(conn *websocket.Conn, serial string, extraArgs []string, readOnly bool) error {
	// 1. push scrcpy.jar to device
	// 2. start scrcpy server on device
	// 3. forward port
	// 4. start scrcpy client and connect to forwarded port
	// 5. relay data between websocket and scrcpy client
	scrcpyService, err := NewScrcpyService(serial, extraArgs)
	if err != nil {
		return err
	}
	slog.Info("Starting scrcpy websocket service", "serial", serial)
	if err = scrcpyService.Start(&WebsocketRWWrapper{conn: conn}, !readOnly); err != nil {
		return err
	}
	defer scrcpyService.Stop()
	err2 := scrcpyService.Wait()
	slog.Info("scrcpy websocket service exited", "err", err2)
	conn.Close()
	return nil
}
