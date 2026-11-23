<script lang="ts">
  import { api, baseWSURL, getScreenshot, pressKey, inputText } from "$lib/api/api";
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
      console.error("[iOS] Screenshot failed:", err);
      return undefined;
    }
  }

  // Swipe gesture state tracking
  let swipeStartX = $state(0);
  let swipeStartY = $state(0);
  let swipeStartTime = $state(0);
  let isSwiping = $state(false);

  async function tapAction(x: number, y: number) {
    try {
      await api.post(`/api/v1/devices/${deviceId}/actions/tap`, { x, y, screenWidth, screenHeight });
    } catch (err) {
      console.error("[iOS] Tap failed:", err);
    }
  }

  async function swipeAction(x1: number, y1: number, x2: number, y2: number, durationMs: number) {
    try {
      await api.post(`/api/v1/devices/${deviceId}/actions/swipe`, {
        x1,
        y1,
        x2,
        y2,
        durationMs,
        screenWidth,
        screenHeight,
      });
    } catch (err) {
      console.error("[iOS] Swipe failed:", err);
    }
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
              console.error("[iOS] Frame decode error:", err);
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

  async function injectTouch(message: Omit<ScrcpyInjectTouchControlMessage, "type">) {
    if (message.action === 0) {
      // ACTION_DOWN: record start position
      isSwiping = false;
      swipeStartX = message.pointerX;
      swipeStartY = message.pointerY;
      swipeStartTime = performance.now();
    } else if (message.action === 2) {
      // ACTION_MOVE: track the latest position
      isSwiping = true;
    } else if (message.action === 1) {
      // ACTION_UP: if moved more than a small threshold, send swipe; otherwise tap
      const dx = message.pointerX - swipeStartX;
      const dy = message.pointerY - swipeStartY;
      const distance = Math.sqrt(dx * dx + dy * dy);
      const durationMs = Math.round(performance.now() - swipeStartTime);
      if (isSwiping || distance > screenWidth * 0.02) {
        await swipeAction(swipeStartX, swipeStartY, message.pointerX, message.pointerY, Math.max(durationMs, 50));
      } else {
        await tapAction(message.pointerX, message.pointerY);
      }
    }
  }

  async function injectKeyCode(message: Omit<ScrcpyInjectKeyCodeControlMessage, "type">) {
    if (message.action !== AndroidKeyEventAction.Down) {
      return;
    }
    const key = getKeyName(message.keyCode);
    if (!key) {
      return;
    }
    try {
      await pressKey(deviceId, key, message.metaState ?? AndroidKeyEventMeta.None);
    } catch (err) {
      console.error("[iOS] PressKey failed:", err);
    }
  }

  function pasteClipboard(text: string) {
    inputText(deviceId, text).catch((err) => {
      console.error("[iOS] InputText failed:", err);
    });
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
  supportedKeys={[AndroidKeyCode.AndroidHome, AndroidKeyCode.VolumeUp, AndroidKeyCode.VolumeDown]}
  {...restProps} />
