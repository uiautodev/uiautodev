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
