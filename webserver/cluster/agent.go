package cluster

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

// AgentConfig configures the slave side of a cluster tunnel.
type AgentConfig struct {
	MasterURL string
	Token     string
	Name      string
	// NodeID is this node's stable cluster identity, reported to the master.
	NodeID  string
	Version string
	// Sessions is the number of outbound sessions. The default is 2, one for
	// control traffic and one for media streams.
	Sessions int
	// InsecureTLS skips TLS verification. Useful when the master uses the
	// built-in self-signed certificate.
	InsecureTLS bool
}

// Agent runs the slave side of the cluster tunnel. It owns no project-specific
// state; the caller provides the HTTP handler to expose to the master, which
// keeps it reusable from the Wails desktop application later.
type Agent struct {
	cfg     AgentConfig
	wsURL   string
	slaveID string
	handler http.Handler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewAgent(cfg AgentConfig, handler http.Handler) (*Agent, error) {
	wsURL, err := NormalizeMasterURL(cfg.MasterURL)
	if err != nil {
		return nil, err
	}
	slaveID, _, err := ParseToken(cfg.Token)
	if err != nil {
		return nil, err
	}
	if cfg.Name == "" {
		cfg.Name = DefaultSlaveName(slaveID)
	}
	if cfg.Sessions <= 0 {
		cfg.Sessions = 2
	}
	return &Agent{
		cfg:     cfg,
		wsURL:   wsURL,
		slaveID: slaveID,
		handler: handler,
	}, nil
}

// Start launches the tunnel sessions and returns immediately. The sessions keep
// reconnecting until Stop is called or ctx is canceled.
func (a *Agent) Start(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)

	kinds := []string{SessionControl}
	if a.cfg.Sessions > 1 {
		kinds = append(kinds, SessionMedia)
	}
	for _, kind := range kinds {
		a.wg.Add(1)
		go func(kind string) {
			defer a.wg.Done()
			a.run(kind)
		}(kind)
	}
}

func (a *Agent) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
}

func (a *Agent) run(kind string) {
	backoff := time.Second
	for {
		if a.ctx.Err() != nil {
			return
		}
		err := a.connectOnce(kind)
		if a.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("cluster: tunnel session ended", "kind", kind, "error", err, "retry_in", backoff)
		} else {
			// The session was established; reconnect quickly after a normal
			// disconnect such as a master restart.
			backoff = time.Second
		}
		wait := backoff + time.Duration(rand.Int63n(int64(backoff/2+1)))
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(wait):
		}
		if backoff < time.Minute {
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
		}
	}
}

func (a *Agent) connectOnce(kind string) error {
	dialer := websocket.Dialer{
		HandshakeTimeout:  15 * time.Second,
		EnableCompression: false,
	}
	// gorilla's DialContext only honors ctx while opening the TCP connection;
	// reading the HTTP upgrade response is bounded by HandshakeTimeout alone.
	// Keep a handle on the raw connection and close it when ctx is canceled so
	// shutdown does not have to wait out the handshake timeout.
	var mu sync.Mutex
	var rawConn net.Conn
	dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: dialer.HandshakeTimeout}).DialContext(ctx, network, addr)
		if err == nil {
			mu.Lock()
			rawConn = conn
			mu.Unlock()
		}
		return conn, err
	}
	stopClose := context.AfterFunc(a.ctx, func() {
		mu.Lock()
		conn := rawConn
		mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
	})
	defer stopClose()

	if a.cfg.InsecureTLS {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+a.cfg.Token)
	conn, resp, err := dialer.DialContext(a.ctx, a.wsURL, headers)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("dial master: %w", err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = conn.Close()
		return err
	}
	hello := Hello{
		SlaveID:         a.slaveID,
		Name:            a.cfg.Name,
		NodeID:          a.cfg.NodeID,
		Version:         a.cfg.Version,
		ProtocolVersion: ProtocolVersion,
		Kind:            kind,
	}
	if err := conn.WriteJSON(hello); err != nil {
		_ = conn.Close()
		return fmt.Errorf("send hello: %w", err)
	}
	_ = conn.SetWriteDeadline(time.Time{})

	session, err := yamux.Server(newWSConn(conn), yamuxConfig())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("start yamux: %w", err)
	}
	go serveTunnel(a.ctx, session, a.handler)

	slog.Info("cluster: tunnel connected", "slave", a.slaveID, "kind", kind, "master", a.wsURL)

	select {
	case <-a.ctx.Done():
	case <-session.CloseChan():
	}
	_ = session.Close()
	_ = conn.Close()
	return nil
}
