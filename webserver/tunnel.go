package webserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.ngrok.com/ngrok/v2"
)

const (
	defaultUpstream       = "http://127.0.0.1:33299"
	defaultConnectTimeout = 20 * time.Second
)

// TunnelConfig configures an ngrok tunnel to the local HTTP server.
type TunnelConfig struct {
	// Upstream is the local HTTP URL the tunnel forwards to,
	// e.g. "http://127.0.0.1:33299".
	Upstream string
	// Authtoken is the ngrok authtoken used to authenticate the session.
	Authtoken string
	// ConnectTimeout is how long to wait for the tunnel to become ready
	// before giving up. Defaults to 20s when zero.
	ConnectTimeout time.Duration
}

// Tunnel represents a running ngrok tunnel.
type Tunnel struct {
	forwarder ngrok.EndpointForwarder
	cancel    context.CancelFunc
	done      chan struct{}
}

// URL returns the public URL of the tunnel.
func (t *Tunnel) URL() string {
	return t.forwarder.URL().String()
}

// Stop gracefully closes the tunnel.
func (t *Tunnel) Stop() {
	t.cancel()
	<-t.done
}

// StartTunnel creates an ngrok tunnel forwarding to cfg.Upstream.
// It blocks until the tunnel is ready or an error occurs.
func StartTunnel(ctx context.Context, cfg TunnelConfig) (*Tunnel, error) {
	if cfg.Upstream == "" {
		cfg.Upstream = defaultUpstream
	}
	if cfg.Authtoken == "" {
		return nil, errors.New("ngrok authtoken is required (set -ngrok-authtoken or NGROK_AUTHTOKEN)")
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = defaultConnectTimeout
	}

	ctx, cancel := context.WithCancel(ctx)

	// ngrok treats authentication failures as transient and keeps retrying,
	// so a bad authtoken would loop until ConnectTimeout. Watch disconnect
	// events and bail out as soon as the service reports an unrecoverable
	// authtoken error.
	var (
		authErrMu sync.Mutex
		authErr   error
	)
	takeAuthErr := func() error {
		authErrMu.Lock()
		defer authErrMu.Unlock()
		return authErr
	}

	// ngrok discards all logs by default, which hides authentication and
	// network errors while it silently retries. Route them to the default
	// logger so failures are visible.
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(cfg.Authtoken),
		ngrok.WithLogger(slog.Default()),
		ngrok.WithEventHandler(func(evt ngrok.Event) {
			disconnected, ok := evt.(*ngrok.EventAgentDisconnected)
			if !ok || disconnected.Error == nil {
				return
			}
			if !isInvalidAuthtokenError(disconnected.Error) {
				return
			}
			authErrMu.Lock()
			if authErr == nil {
				authErr = disconnected.Error
			}
			authErrMu.Unlock()
			cancel()
		}),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	upstream := ngrok.WithUpstream(cfg.Upstream)
	if scheme := upstreamScheme(cfg.Upstream); scheme == "https" {
		// The local server may use a self-signed certificate; skip
		// verification so the tunnel can still forward to it.
		upstream = ngrok.WithUpstream(cfg.Upstream,
			ngrok.WithUpstreamTLSClientConfig(&tls.Config{InsecureSkipVerify: true}))
	}

	// Forward blocks while ngrok retries transient failures, so run it in a
	// goroutine and bound the wait. Without this a bad authtoken or a broken
	// network would hang forever.
	type forwardResult struct {
		forwarder ngrok.EndpointForwarder
		err       error
	}
	resultCh := make(chan forwardResult, 1)
	go func() {
		forwarder, err := agent.Forward(ctx, upstream)
		resultCh <- forwardResult{forwarder: forwarder, err: err}
	}()

	var forwarder ngrok.EndpointForwarder
	select {
	case r := <-resultCh:
		if r.err != nil {
			cancel()
			if ae := takeAuthErr(); ae != nil {
				return nil, fmt.Errorf("invalid ngrok authtoken: %w", ae)
			}
			return nil, r.err
		}
		forwarder = r.forwarder
	case <-time.After(cfg.ConnectTimeout):
		cancel()
		if ae := takeAuthErr(); ae != nil {
			return nil, fmt.Errorf("invalid ngrok authtoken: %w", ae)
		}
		return nil, fmt.Errorf("ngrok tunnel not ready within %s (check authtoken and network)", cfg.ConnectTimeout)
	case <-ctx.Done():
		cancel()
		if ae := takeAuthErr(); ae != nil {
			return nil, fmt.Errorf("invalid ngrok authtoken: %w", ae)
		}
		return nil, ctx.Err()
	}

	t := &Tunnel{
		forwarder: forwarder,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go func() {
		defer close(t.done)
		<-forwarder.Done()
	}()

	slog.Info("ngrok tunnel started", "url", t.URL(), "upstream", cfg.Upstream)
	return t, nil
}

// ngrokErrorCode is implemented by errors returned by the ngrok service and
// exposes their ngrok error code (e.g. "ERR_NGROK_105").
type ngrokErrorCode interface {
	ErrorCode() string
}

// authtokenErrorCodes are unrecoverable errors caused by an invalid authtoken.
// Retrying them is pointless, so the tunnel fails fast instead.
var authtokenErrorCodes = map[string]bool{
	"ERR_NGROK_105": true, // authtoken is malformed
	"ERR_NGROK_106": true, // ngrok v1 authtoken used with v2
	"ERR_NGROK_107": true, // well-formed but invalid/revoked authtoken
}

// isInvalidAuthtokenError reports whether err was caused by an invalid
// authtoken. It walks the error chain for an ngrok service error and inspects
// its error code.
func isInvalidAuthtokenError(err error) bool {
	var codeErr ngrokErrorCode
	if errors.As(err, &codeErr) {
		return authtokenErrorCodes[codeErr.ErrorCode()]
	}
	return false
}

// upstreamScheme extracts the scheme from an upstream URL, defaulting to
// "http" when no scheme is present.
func upstreamScheme(upstream string) string {
	if u, err := url.Parse(upstream); err == nil && u.Scheme != "" {
		return strings.ToLower(u.Scheme)
	}
	return "http"
}

// UpstreamFromAddr converts a listen address into a local HTTP URL
// suitable for the tunnel upstream. Supported forms:
//
//	":33299"                -> "http://127.0.0.1:33299"
//	"127.0.0.1:33299"       -> "http://127.0.0.1:33299"
//	"0.0.0.0:8080"          -> "http://127.0.0.1:8080"
//	"localhost:33299"       -> "http://localhost:33299"
//	"https://:8443"         -> "https://127.0.0.1:8443"
func UpstreamFromAddr(addr string) string {
	scheme := "http"
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		u, err := url.Parse(addr)
		if err == nil {
			scheme = u.Scheme
			addr = u.Host
		}
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}
