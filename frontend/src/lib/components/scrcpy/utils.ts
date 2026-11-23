import { Consumable, ReadableStream, WritableStream } from "@yume-chan/stream-extra";


export function createWebSocketReadableStream(
  socket: WebSocket
): ReadableStream<Uint8Array> {
  if (socket.binaryType !== "arraybuffer") {
    console.warn(
      "WebSocket binaryType should be set to 'arraybuffer' for optimal use with Uint8Array streams."
    );
    socket.binaryType = "arraybuffer";
  }
  return new ReadableStream<Uint8Array>({
    start(controller) {
      socket.addEventListener("message", (event: MessageEvent) => {
        if (event.data instanceof ArrayBuffer) {
          controller.enqueue(new Uint8Array(event.data));
        } else {
          console.warn("Received non-binary data, ignoring:", event.data);
        }
      });
      socket.addEventListener("close", () => {
        // if errored, close will not be called
        try {
          controller.close();
        } catch (e) { }
      });
      socket.addEventListener("error", (err) => controller.error(err));
    },
    cancel(reason) {
      console.log("Stream cancelled by consumer, closing WebSocket:", reason);
      if (socket.readyState === WebSocket.OPEN) {
        socket.close(1000, "Stream cancelled");
      }
    },
  });
}

export function createWebSocketWritableStream(ws: WebSocket): WritableStream<Consumable<Uint8Array>> {
  return new WritableStream<Consumable<Uint8Array>>({
    async write(chunk) {
      const value = chunk.value;
      chunk.consume();
      ws.send(value);
    },
    close() {
      ws.close();
    },
    abort(reason) {
      console.error("abort:", reason);
      ws.close();
    }
  });
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}