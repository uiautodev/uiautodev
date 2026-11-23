package device

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestDiscardingReaderDropsInput makes sure a view-only session can read the
// websocket without forwarding anything to the scrcpy control socket.
func TestDiscardingReaderDropsInput(t *testing.T) {
	reader := &discardingReader{r: strings.NewReader("tap 10 20")}
	buf := make([]byte, 32)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read failed: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected to consume the input")
	}
	for _, b := range buf[:n] {
		if b != 0 {
			t.Fatalf("input leaked into the buffer sent to the device: %q", buf[:n])
		}
	}
}

// TestStreamReadOnlyHook covers the seam the presence layer installs so the
// device layer knows which streams belong to observers.
func TestStreamReadOnlyHook(t *testing.T) {
	ctx := context.Background()
	SetStreamReadOnlyHook(nil)
	t.Cleanup(func() { SetStreamReadOnlyHook(nil) })

	if isReadOnlyStream(ctx) {
		t.Fatalf("streams are writable when no hook is installed")
	}
	SetStreamReadOnlyHook(func(context.Context) bool { return true })
	if !isReadOnlyStream(ctx) {
		t.Fatalf("hook was not consulted")
	}
	SetStreamReadOnlyHook(nil)
	if isReadOnlyStream(ctx) {
		t.Fatalf("clearing the hook must restore writable streams")
	}
}
