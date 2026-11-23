<script lang="ts">
  import { baseWSURL, getScreenshot, tapDevice } from "$lib/api/api";
  import {
    ConnectionController,
    ConnectionStatus,
    type ConnectionState,
    type ConnectHandle,
  } from "$lib/hooks/connection";
  import Screen from "./screen.svelte";
  import type { ScreenHandler } from "./types";
  import {
    AndroidMotionEventAction,
    type ScrcpyInjectTouchControlMessage,
    type ScrcpyInjectKeyCodeControlMessage,
  } from "@yume-chan/scrcpy";

  let { deviceId, ...restProps } = $props();
  let screenWidth = $state(200);
  let screenHeight = $state(400);
  let connection = $state<ConnectionState>({
    status: ConnectionStatus.Connecting,
    retrySeconds: 0,
  });

  const framesURL = `${baseWSURL}/stream/devices/${deviceId}/screen/frames`;

  async function snapshot(): Promise<Blob> {
    return getScreenshot(deviceId);
  }

  let connectFn: (() => void) | undefined;

  function mount(canvas: HTMLCanvasElement): () => void {
    const ctx = canvas.getContext("2d");

    const controller = new ConnectionController({
      onChange: (state) => (connection = state),
      connect: (onDisconnect) =>
        new Promise<ConnectHandle>((resolve, reject) => {
          const socket = new WebSocket(framesURL);
          socket.binaryType = "arraybuffer";
          let opened = false;

          socket.onopen = () => {
            opened = true;
            resolve({ dispose: () => socket.close() });
          };
          socket.onmessage = async (event: MessageEvent) => {
            if (!(event.data instanceof ArrayBuffer)) return;
            try {
              const blob = new Blob([event.data], { type: "image/jpeg" });
              const bitmap = await createImageBitmap(blob);
              // Update screen dimensions FIRST; the bound canvas width/height
              // attributes are applied on the next frame and reset the bitmap, so
              // draw only after that.
              screenWidth = bitmap.width;
              screenHeight = bitmap.height;
              await new Promise((resolve) => requestAnimationFrame(resolve));
              ctx?.drawImage(bitmap, 0, 0);
              bitmap.close();
            } catch (err) {
              console.error("[demo] Frame decode error:", err);
            }
          };
          socket.onerror = () => {
            socket.close();
          };
          socket.onclose = () => {
            if (opened) onDisconnect();
            else reject(new Error("websocket closed before open"));
          };
        }),
    });

    connectFn = () => controller.reconnect();
    controller.start();

    return () => {
      controller.dispose();
      connectFn = undefined;
    };
  }

  function reconnect() {
    connectFn?.();
  }

  // Tapping the screen toggles the demo's Wi-Fi / Bluetooth rows on the backend;
  // the resulting frame is pushed back over the websocket automatically.
  async function injectTouch(msg: Omit<ScrcpyInjectTouchControlMessage, "type">) {
    if (msg.action !== AndroidMotionEventAction.Down) return;
    try {
      await tapDevice(deviceId, {
        x: msg.pointerX,
        y: msg.pointerY,
        screenWidth,
        screenHeight,
      });
    } catch (err) {
      console.error("[demo] Error sending tap:", err);
    }
  }

  async function injectKeyCode(_message: Omit<ScrcpyInjectKeyCodeControlMessage, "type">) {}

  const screenHandler: ScreenHandler = {
    mount,
    injectTouch,
    injectKeyCode,
    snapshot,
  };
</script>

<Screen
  {screenHandler}
  {screenWidth}
  {screenHeight}
  {deviceId}
  status={connection.status}
  retrySeconds={connection.retrySeconds}
  {reconnect}
  {...restProps} />
