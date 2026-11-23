package device

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
)

// streamConnHook lets an outer layer (the presence manager) observe the
// websocket connections of screen streams so it can close them when a session
// is preempted or released. The hook returns an unregister function.
var streamConnHook struct {
	mu sync.RWMutex
	fn func(ctx context.Context, conn *websocket.Conn) func()
}

// SetStreamConnHook installs the stream connection hook. Passing nil removes it.
func SetStreamConnHook(fn func(ctx context.Context, conn *websocket.Conn) func()) {
	streamConnHook.mu.Lock()
	streamConnHook.fn = fn
	streamConnHook.mu.Unlock()
}

// registerStreamConn notifies the configured hook about a new stream
// connection. It returns nil when no hook is installed.
func registerStreamConn(ctx context.Context, conn *websocket.Conn) func() {
	streamConnHook.mu.RLock()
	fn := streamConnHook.fn
	streamConnHook.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, conn)
}

// streamReadOnlyHook lets the presence layer mark a stream connection as
// belonging to a view-only session. Streaming websockets are bidirectional, so
// the device layer drops their input messages instead of trusting the client.
var streamReadOnlyHook struct {
	mu sync.RWMutex
	fn func(ctx context.Context) bool
}

// SetStreamReadOnlyHook installs the read-only hook. Passing nil removes it.
func SetStreamReadOnlyHook(fn func(ctx context.Context) bool) {
	streamReadOnlyHook.mu.Lock()
	streamReadOnlyHook.fn = fn
	streamReadOnlyHook.mu.Unlock()
}

// isReadOnlyStream reports whether the stream request belongs to a view-only
// session. It returns false when no hook is installed.
func isReadOnlyStream(ctx context.Context) bool {
	streamReadOnlyHook.mu.RLock()
	fn := streamReadOnlyHook.fn
	streamReadOnlyHook.mu.RUnlock()
	return fn != nil && fn(ctx)
}
