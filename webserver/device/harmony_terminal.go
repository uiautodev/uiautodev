package device

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	harmonyShellConnectTimeout = 15 * time.Second
	// maxHCDFrameSize bounds an incoming frame so a broken server cannot make
	// the bridge allocate an arbitrary amount of memory.
	maxHCDFrameSize = 4 << 20
)

var harmonyShellDial = func(ctx context.Context, addr string) (net.Conn, error) {
	dialer := net.Dialer{}
	return dialer.DialContext(ctx, "tcp", addr)
}

// harmonyTerminal speaks the interactive hdc shell channel. The go-hdckit
// Target.Shell API only streams a one-shot command and does not expose stdin,
// so the websocket bridge needs the raw channel: length-prefixed frames after
// the `shell` request are both the remote output and the keyboard input.
type harmonyTerminal struct {
	conn net.Conn

	// pending is only touched by the single reader in serveTerminal, while
	// writes may come from the websocket goroutine, so they need no shared lock.
	pending []byte

	writeMu sync.Mutex

	done      chan struct{}
	doneOnce  sync.Once
	closeOnce sync.Once
}

// OpenTerminal opens an interactive shell on the device through the hdc
// server. The hdc shell channel allocates the pseudo-terminal on the device,
// so this works on every host OS without a local pty.
func (d *HarmonyOSDevice) OpenTerminal(rows, cols uint16) (TerminalSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), harmonyShellConnectTimeout)
	defer cancel()

	// Use go-hdckit to start the server and wait until the target can serve a
	// shell; the interactive channel below uses the same wire protocol.
	target, err := d.target()
	if err != nil {
		return nil, errors.Wrap(err, "get hdc target")
	}
	if _, err := target.RunShell(ctx, "true"); err != nil {
		return nil, errors.Wrap(err, "hdc target is not ready")
	}

	opts := getHdcClient().Options()
	addr := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	conn, err := harmonyShellDial(ctx, addr)
	if err != nil {
		return nil, errors.Wrap(err, "connect hdc server")
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	terminal, err := startHarmonyShell(conn, d.connectKey)
	if err != nil {
		return nil, err
	}
	// The deadline only guards the handshake; an interactive shell may stay
	// idle for as long as the browser keeps it open.
	_ = conn.SetDeadline(time.Time{})

	// hdc does not carry a window-size control frame, so rows and cols are
	// ignored here; the remote shell keeps its default size.
	return terminal, nil
}

func startHarmonyShell(conn net.Conn, connectKey string) (*harmonyTerminal, error) {
	terminal := &harmonyTerminal{conn: conn, done: make(chan struct{})}
	if err := terminal.handshake(connectKey); err != nil {
		terminal.Close()
		return nil, err
	}
	if err := writeHCDFrame(conn, []byte("shell")); err != nil {
		terminal.Close()
		return nil, errors.Wrap(err, "start hdc shell")
	}
	return terminal, nil
}

func (t *harmonyTerminal) handshake(connectKey string) error {
	hello, err := readHCDFrame(t.conn)
	if err != nil {
		return errors.Wrap(err, "read hdc handshake")
	}
	if !bytes.HasPrefix(hello, []byte("OHOS HDC")) {
		return errors.New("unexpected hdc handshake")
	}
	banner := make([]byte, 12)
	copy(banner, hello)
	key := make([]byte, 32)
	copy(key, connectKey)
	if err := writeHCDFrame(t.conn, append(banner, key...)); err != nil {
		return errors.Wrap(err, "write hdc handshake")
	}
	return nil
}

func (t *harmonyTerminal) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(t.pending) == 0 {
		frame, err := readHCDFrame(t.conn)
		if err != nil {
			t.markDone()
			return 0, err
		}
		t.pending = frame
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

func (t *harmonyTerminal) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := writeHCDFrame(t.conn, p); err != nil {
		t.markDone()
		return 0, err
	}
	return len(p), nil
}

// Resize is a no-op because hdc has no window-size frame for interactive
// shells.
func (t *harmonyTerminal) Resize(rows, cols uint16) error {
	return nil
}

// Wait unblocks when the remote shell or the browser closes the channel. hdc
// does not report the remote exit code, so it is always zero.
func (t *harmonyTerminal) Wait() (int, error) {
	<-t.done
	return 0, nil
}

// Close hangs up the channel, which makes the hdc daemon terminate the remote
// shell. Closing the connection also unblocks a Read waiting for output.
func (t *harmonyTerminal) Close() error {
	var err error
	t.closeOnce.Do(func() {
		err = t.conn.Close()
		t.markDone()
	})
	return err
}

func (t *harmonyTerminal) markDone() {
	t.doneOnce.Do(func() {
		close(t.done)
	})
}

func readHCDFrame(conn net.Conn) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > maxHCDFrameSize {
		return nil, fmt.Errorf("hdc frame is too large: %d bytes", size)
	}
	if size == 0 {
		return nil, nil
	}
	frame := make([]byte, size)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

func writeHCDFrame(conn net.Conn, payload []byte) error {
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	for len(frame) > 0 {
		n, err := conn.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}
