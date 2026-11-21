<script lang="ts">
  import { baseWSURL, getScreenshot } from "$lib/api/api";
  import {
    ConnectionController,
    ConnectionStatus,
    type ConnectionState,
    type ConnectHandle,
  } from "$lib/hooks/connection";
  import Screen from "./screen.svelte";
  import { getKeyName } from "./keymap";
  import type { ScreenHandler } from "./types";
  import {
    AndroidKeyCode,
    AndroidKeyEventAction,
    AndroidKeyEventMeta,
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

  async function snapshot(): Promise<Blob | undefined> {
    try {
      return await getScreenshot(deviceId);
    } catch (err) {
      console.error("[Harmony] Screenshot failed:", err);
      return undefined;
    }
  }

  // Control commands are sent over the same websocket that carries screen
  // frames; the backend drives the device through hdckit's UiDriver.
  let socket: WebSocket | undefined;

  function sendControl(message: Record<string, unknown>) {
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(message));
    }
  }

  let connectFn: (() => void) | undefined;

  function mount(canvas: HTMLCanvasElement): () => void {
    const ctx = canvas.getContext("2d");

    const controller = new ConnectionController({
      onChange: (state) => (connection = state),
      connect: (onDisconnect) =>
        new Promise<ConnectHandle>((resolve, reject) => {
          const ws = new WebSocket(framesURL);
          ws.binaryType = "arraybuffer";
          let opened = false;

          ws.onopen = () => {
            opened = true;
            socket = ws;
            resolve({ dispose: () => ws.close() });
          };
          ws.onmessage = async (event: MessageEvent) => {
            if (!(event.data instanceof ArrayBuffer)) return;
            try {
              const blob = new Blob([event.data], { type: "image/jpeg" });
              const bitmap = await createImageBitmap(blob);
              if (canvas.width !== bitmap.width || canvas.height !== bitmap.height) {
                // Update screen dimensions FIRST; the bound canvas width/height
                // attributes are applied on the next frame and reset the bitmap, so
                // draw only after that.
                screenWidth = bitmap.width;
                screenHeight = bitmap.height;
                await new Promise((resolve) => requestAnimationFrame(resolve));
              }
              ctx?.drawImage(bitmap, 0, 0);
              bitmap.close();
            } catch (err) {
              console.error("[Harmony] Frame decode error:", err);
            }
          };
          ws.onerror = () => {
            ws.close();
          };
          ws.onclose = () => {
            if (socket === ws) socket = undefined;
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
      socket = undefined;
    };
  }

  function reconnect() {
    connectFn?.();
  }

  // Stream each touch event straight to the device so the UiDriver can track a
  // native down/move/up gesture. Hover moves (no button pressed) are ignored.
  async function injectTouch(message: Omit<ScrcpyInjectTouchControlMessage, "type">) {
    let action: string;
    switch (message.action) {
      case AndroidMotionEventAction.Down:
        action = "down";
        break;
      case AndroidMotionEventAction.Move:
        action = "move";
        break;
      case AndroidMotionEventAction.Up:
        action = "up";
        break;
      default:
        return;
    }
    sendControl({ type: "touch", action, x: message.pointerX, y: message.pointerY, screenWidth, screenHeight });
  }

  async function injectKeyCode(message: Omit<ScrcpyInjectKeyCodeControlMessage, "type">) {
    if (message.action !== AndroidKeyEventAction.Down) {
      return;
    }
    const key = getKeyName(message.keyCode);
    if (!key) {
      return;
    }
    sendControl({ type: "key", key, metaState: message.metaState ?? AndroidKeyEventMeta.None });
  }

  function pasteClipboard(text: string) {
    if (!text) return;
    sendControl({ type: "text", text });
  }

  const screenHandler: ScreenHandler = {
    mount,
    injectTouch,
    injectKeyCode,
    pasteClipboard,
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
  supportedKeys={[
    AndroidKeyCode.AndroidHome,
    AndroidKeyCode.AndroidBack,
    AndroidKeyCode.AndroidAppSwitch,
    AndroidKeyCode.Power,
    AndroidKeyCode.VolumeUp,
    AndroidKeyCode.VolumeDown,
  ]}
  {...restProps} />
