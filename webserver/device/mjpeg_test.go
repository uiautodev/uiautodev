package device

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// buildMJPEGBody builds a WebDriverAgent-style MJPEG multipart body containing
// the given JPEG frames.
func buildMJPEGBody(frames ...[]byte) []byte {
	var buf bytes.Buffer
	for _, frame := range frames {
		buf.WriteString("--BoundaryString\r\n")
		buf.WriteString("Content-Type: image/jpeg\r\n")
		fmt.Fprintf(&buf, "Content-Length: %d\r\n", len(frame))
		buf.WriteString("\r\n")
		buf.Write(frame)
		buf.WriteString("\r\n")
	}
	buf.WriteString("--BoundaryString--\r\n")
	return buf.Bytes()
}

func TestExtractJPEGFrames(t *testing.T) {
	frame1 := []byte{0xFF, 0xD8, 0x01, 0x02, 0x03, 0xFF, 0xD9}
	frame2 := []byte{0xFF, 0xD8, 0xAA, 0xBB, 0xFF, 0xD9}
	body := buildMJPEGBody(frame1, frame2)

	var got [][]byte
	err := extractJPEGFrames(context.Background(), bytes.NewReader(body), func(frame []byte) error {
		got = append(got, append([]byte(nil), frame...))
		return nil
	})
	if err != nil {
		t.Fatalf("extractJPEGFrames() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(got))
	}
	if !bytes.Equal(got[0], frame1) {
		t.Errorf("frame1 mismatch: got %v want %v", got[0], frame1)
	}
	if !bytes.Equal(got[1], frame2) {
		t.Errorf("frame2 mismatch: got %v want %v", got[1], frame2)
	}
}

func TestExtractJPEGFramesStopsOnError(t *testing.T) {
	body := buildMJPEGBody([]byte{0xFF, 0xD8, 0x00, 0xFF, 0xD9}, []byte{0xFF, 0xD8, 0x00, 0xFF, 0xD9})
	wantErr := fmt.Errorf("stop")
	calls := 0
	err := extractJPEGFrames(context.Background(), bytes.NewReader(body), func(frame []byte) error {
		calls++
		return wantErr
	})
	if err != wantErr {
		t.Fatalf("expected onFrame error to propagate, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 callback, got %d", calls)
	}
}

func TestExtractJPEGFramesContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := extractJPEGFrames(ctx, bytes.NewReader(buildMJPEGBody([]byte{0xFF, 0xD8, 0xFF, 0xD9})), func(frame []byte) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error on cancel, got %v", err)
	}
	if called {
		t.Error("onFrame should not be called after context cancel")
	}
}
