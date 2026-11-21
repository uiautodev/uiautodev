package cluster

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/uiautodev/uiautodev/webserver/device"
)

func yamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.MaxStreamWindowSize = 4 * 1024 * 1024
	cfg.LogOutput = io.Discard
	return cfg
}

// serveTunnel accepts streams from a yamux session and routes them to either
// the HTTP handler or a raw TCP connection.
func serveTunnel(ctx context.Context, session *yamux.Session, handler http.Handler) {
	listener := newStreamListener()
	server := &http.Server{
		Handler:           withTunnelRequest(handler),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			slog.Warn("cluster: http server over tunnel stopped", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = session.Close()
	}()

	for {
		stream, err := session.AcceptStream()
		if err != nil {
			listener.Close()
			_ = server.Close()
			return
		}
		go func(stream net.Conn) {
			frame := []byte{0}
			if _, err := io.ReadFull(stream, frame); err != nil {
				_ = stream.Close()
				return
			}
			switch frame[0] {
			case FrameHTTP:
				if !listener.enqueue(stream) {
					_ = stream.Close()
				}
			case FrameRaw:
				handleRawStream(stream)
			default:
				slog.Warn("cluster: unknown tunnel frame", "frame", frame[0])
				_ = stream.Close()
			}
		}(stream)
	}
}

func handleRawStream(stream net.Conn) {
	defer stream.Close()

	target, err := readRawTarget(stream)
	if err != nil {
		slog.Warn("cluster: invalid raw stream header", "error", err)
		return
	}

	var conn net.Conn
	switch {
	case target.DeviceID != "":
		dev, devErr := device.NewDevice(target.DeviceID)
		if devErr != nil {
			err = devErr
			break
		}
		conn, err = dev.Connect(target.SocketAddr)
	case target.Host != "":
		conn, err = net.DialTimeout("tcp", net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), 10*time.Second)
	default:
		err = fmt.Errorf("raw target is empty")
	}
	if err != nil {
		slog.Warn("cluster: failed to open raw target", "target", target, "error", err)
		return
	}
	bridgeConns(stream, conn)
}

// tunnelRequestContextKey marks HTTP requests that arrived over the
// authenticated cluster tunnel.
type tunnelRequestContextKey struct{}

// withTunnelRequest marks requests served through the cluster tunnel so
// intermediate adb relays can accept their loopback connections without
// weakening public same-IP restrictions.
func withTunnelRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tunnelRequestContextKey{}, true)))
	})
}

func isTunnelRequest(ctx context.Context) bool {
	marked, _ := ctx.Value(tunnelRequestContextKey{}).(bool)
	return marked
}

func bridgeConns(a, b net.Conn) {
	done := make(chan struct{}, 2)
	copyOne := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyOne(a, b)
	go copyOne(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

// streamListener adapts accepted yamux streams to an http.Server listener.
type streamListener struct {
	ch     chan net.Conn
	done   chan struct{}
	once   sync.Once
	closed chan struct{}
}

func newStreamListener() *streamListener {
	return &streamListener{
		ch:     make(chan net.Conn),
		done:   make(chan struct{}),
		closed: make(chan struct{}),
	}
}

func (l *streamListener) enqueue(conn net.Conn) bool {
	select {
	case l.ch <- conn:
		return true
	case <-l.done:
		return false
	}
}

func (l *streamListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ch:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *streamListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		close(l.closed)
	})
	return nil
}

func (l *streamListener) Addr() net.Addr { return tunnelAddr("tunnel") }

type tunnelAddr string

func (a tunnelAddr) Network() string { return "tunnel" }
func (a tunnelAddr) String() string  { return string(a) }

// openHTTPStream opens a tunnel stream and marks it as carrying HTTP.
func openHTTPStream(session *yamux.Session) (net.Conn, error) {
	stream, err := session.OpenStream()
	if err != nil {
		return nil, err
	}
	if _, err := stream.Write([]byte{FrameHTTP}); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
}

// openRawStream opens a tunnel stream and writes the raw connection target.
func openRawStream(session *yamux.Session, target RawTarget) (net.Conn, error) {
	stream, err := session.OpenStream()
	if err != nil {
		return nil, err
	}
	if _, err := stream.Write([]byte{FrameRaw}); err != nil {
		_ = stream.Close()
		return nil, err
	}
	header, err := json.Marshal(target)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(header)))
	if _, err := stream.Write(length[:]); err != nil {
		_ = stream.Close()
		return nil, err
	}
	if _, err := stream.Write(header); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
}

func readRawTarget(r io.Reader) (RawTarget, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return RawTarget{}, err
	}
	size := binary.BigEndian.Uint32(length[:])
	if size == 0 || size > 64*1024 {
		return RawTarget{}, fmt.Errorf("invalid raw header size %d", size)
	}
	header := make([]byte, size)
	if _, err := io.ReadFull(r, header); err != nil {
		return RawTarget{}, err
	}
	var target RawTarget
	if err := json.Unmarshal(header, &target); err != nil {
		return RawTarget{}, err
	}
	return target, nil
}
