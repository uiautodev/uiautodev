package device

import (
	"bufio"
	"context"
	"io"
)

// extractJPEGFrames reads a byte stream (typically an MJPEG multipart body) and
// invokes onFrame with every complete JPEG frame it finds. It scans for the
// JPEG SOI/EOI markers instead of parsing the multipart boundary, which keeps it
// tolerant of the slightly off-spec boundary ("--BoundaryString") used by the
// WebDriverAgent MJPEG server.
func extractJPEGFrames(ctx context.Context, r io.Reader, onFrame func([]byte) error) error {
	br := bufio.NewReaderSize(r, 64*1024)
	buf := make([]byte, 32*1024)

	var frame []byte
	inFrame := false
	prev := byte(0)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		n, readErr := br.Read(buf)
		for i := 0; i < n; i++ {
			b := buf[i]
			if !inFrame {
				if prev == 0xFF && b == 0xD8 {
					inFrame = true
					frame = append(frame[:0], 0xFF, 0xD8)
				}
			} else {
				frame = append(frame, b)
				if prev == 0xFF && b == 0xD9 {
					if err := onFrame(frame); err != nil {
						return err
					}
					inFrame = false
				}
			}
			prev = b
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}
