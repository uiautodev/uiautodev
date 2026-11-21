/**
 * MJPEG stream client — fetches an MJPEG HTTP stream, parses the multipart
 * boundary, decodes each JPEG frame to an ImageBitmap and calls the render
 * callback. Designed to replace setTimeout-based polling in screen components.
 */
export class MJPEGStreamClient {
  private abortController?: AbortController;
  private canvas: HTMLCanvasElement;
  private _onDisconnected?: () => void;
  private _onSizeChanged?: (size: { width: number; height: number }) => void;

  constructor(options: { canvas: HTMLCanvasElement }) {
    this.canvas = options.canvas;
  }

  /** Register a callback fired when the stream disconnects or errors. */
  onDisconnect(cb: () => void): void {
    this._onDisconnected = cb;
  }

  /** Register a callback fired when the frame dimensions change. */
  onSizeChange(cb: (size: { width: number; height: number }) => void): void {
    this._onSizeChanged = cb;
  }

  /**
   * Start consuming the MJPEG stream at `url`.
   * Resolves once the connection is established and the first frame is rendered.
   */
  async connect(url: string): Promise<void> {
    this.abortController = new AbortController();
    const { signal } = this.abortController;

    const response = await fetch(url, { signal });
    if (!response.ok || !response.body) {
      throw new Error(`MJPEG fetch failed: ${response.status}`);
    }

    // Extract boundary from Content-Type header
    const contentType = response.headers.get("content-type") || "";
    const boundaryMatch = contentType.match(/boundary=([^\s;]+)/i);
    const boundary = boundaryMatch ? boundaryMatch[1] : "--BoundaryString";
    const boundaryMarker = new TextEncoder().encode(`\r\n${boundary}\r\n`);

    const reader = response.body.getReader();
    const ctx = this.canvas.getContext("2d");
    if (!ctx) throw new Error("cannot get 2d context");

    // Buffer for accumulating bytes between boundaries
    let buffer = new Uint8Array(0);

    // Resolve the connect promise once the first frame renders
    let resolveFirstFrame: (() => void) | null = null;
    const firstFrame = new Promise<void>((resolve) => {
      resolveFirstFrame = resolve;
    });

    // Read loop
    const pump = async () => {
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          // Append new data to buffer
          const newBuf = new Uint8Array(buffer.length + value.length);
          newBuf.set(buffer);
          newBuf.set(value, buffer.length);
          buffer = newBuf;

          // Process as many complete frames as we can from the buffer
          while (true) {
            const boundaryIdx = this.indexOfBytes(buffer, boundaryMarker);
            if (boundaryIdx < 0) break; // No complete boundary yet

            // Extract everything before this boundary as a potential frame part
            const partData = buffer.slice(0, boundaryIdx);
            buffer = buffer.slice(boundaryIdx + boundaryMarker.length);

            if (partData.length === 0) continue; // Skip initial boundary

            // Parse Content-Length from the part preamble
            const preambleEnd = this.indexOfSequence(partData, new Uint8Array([0x0d, 0x0a, 0x0d, 0x0a]));
            if (preambleEnd < 0) continue;

            const preamble = new TextDecoder().decode(partData.slice(0, preambleEnd));
            const imgStart = preambleEnd + 4;
            const imgData = partData.slice(imgStart);

            // Check if this looks like a JPEG (optional sanity check)
            if (imgData.length < 2) continue;

            try {
              const blob = new Blob([imgData], { type: "image/jpeg" });
              const bitmap = await createImageBitmap(blob);

              // Update canvas size if dimensions changed
              if (this.canvas.width !== bitmap.width || this.canvas.height !== bitmap.height) {
                this.canvas.width = bitmap.width;
                this.canvas.height = bitmap.height;
                this._onSizeChanged?.({ width: bitmap.width, height: bitmap.height });
              }

              // Render frame
              ctx.drawImage(bitmap, 0, 0);
              bitmap.close();

              resolveFirstFrame?.();
              resolveFirstFrame = null;
            } catch {
              // Skip corrupt frame
            }
          }
        }
      } catch (err: any) {
        if (err?.name === "AbortError") return;
        throw err;
      } finally {
        this._onDisconnected?.();
      }
    };

    // Fire-and-forget the read loop; errors surface via onDisconnected
    pump().catch(() => this._onDisconnected?.());

    // Wait for first frame or error
    await firstFrame;
  }

  /** Stop the stream and cancel the HTTP request. */
  disconnect(): void {
    this.abortController?.abort();
  }

  // --- private helpers ---

  /** Find first occurrence of `needle` in `haystack`. */
  private indexOfBytes(haystack: Uint8Array, needle: Uint8Array): number {
    outer: for (let i = 0; i <= haystack.length - needle.length; i++) {
      for (let j = 0; j < needle.length; j++) {
        if (haystack[i + j] !== needle[j]) continue outer;
      }
      return i;
    }
    return -1;
  }

  /** Find first occurrence of a byte sequence (helper for CRLF CRLF detection). */
  private indexOfSequence(haystack: Uint8Array, needle: Uint8Array): number {
    return this.indexOfBytes(haystack, needle);
  }
}
