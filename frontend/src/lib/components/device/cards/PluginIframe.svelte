<script lang="ts">
  import {
    api,
    baseURL,
    getCurrentDeviceApp,
    listDeviceApps,
    postJevDecision,
    postOpenAICompletion,
    startDeviceApp,
    stopDeviceApp,
    tapDevice,
  } from "$lib/api/api.js";
  import { APP_SCOPE_LIST, type AppScope, type Plugin } from "$lib/api/schemas.js";
  import { deviceStore } from "$lib/stores/device.svelte";
  import { onScreenTap } from "$lib/stores/screen-events.js";
  import { getScrcpyTapInjector } from "$lib/stores/scrcpy-tap.js";
  import { onScreenModeChange, setScreenMode, ScreenMode } from "$lib/stores/screen-mode.svelte";
  import { themeStore } from "$lib/stores/theme.svelte";
  import { onMount } from "svelte";

  let {
    plugin,
    deviceId,
    class: className = "",
  }: {
    plugin: Plugin;
    deviceId: string;
    class?: string;
  } = $props();

  let iframeEl: HTMLIFrameElement | undefined = $state();
  let currentHeight = $state(plugin.manifest.height ?? 0);
  // Tab plugins fill the whole tab area instead of using a manifest/resized height.
  const isTab = $derived(plugin.manifest.display === "tab");
  let windowSearchParams = new URLSearchParams(window.location.search);

  const SCRCPY_TAP_TIMEOUT_MS = 2000;

  function withTimeout<T>(promise: Promise<T>, ms: number, label: string): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`${label} timed out after ${ms}ms`)), ms);
      promise.then(
        (value) => {
          clearTimeout(timer);
          resolve(value);
        },
        (e) => {
          clearTimeout(timer);
          reject(e);
        },
      );
    });
  }

  function getIframeSrc(): string {
    const base = baseURL || window.location.origin;
    const url = new URL(plugin.src, base);
    url.searchParams.set("deviceId", deviceId);
    windowSearchParams.forEach((value, key) => {
      url.searchParams.set(key, value);
    });
    url.searchParams.set("theme", themeStore.resolved);
    return url.toString();
  }

  // Resolves the iframe origin for postMessage. Returns undefined when the src
  // cannot be parsed so callers can bail out instead of throwing. The result is
  // memoized by src because this runs on every incoming plugin message.
  let cachedOriginSrc = "";
  let cachedOrigin: string | undefined;

  function getTargetOrigin(): string | undefined {
    try {
      const src = getIframeSrc();
      if (src === cachedOriginSrc) return cachedOrigin;
      cachedOriginSrc = src;
      cachedOrigin = new URL(src).origin;
      return cachedOrigin;
    } catch {
      cachedOriginSrc = "";
      cachedOrigin = undefined;
      return undefined;
    }
  }

  function sendTheme() {
    const targetOrigin = getTargetOrigin();
    if (!targetOrigin) return;
    iframeEl?.contentWindow?.postMessage({ type: "plugin-theme", theme: themeStore.resolved }, targetOrigin);
  }

  $effect(() => {
    sendTheme();
  });

  async function streamOpenAI(id: number, params: unknown) {
    const targetOrigin = getTargetOrigin();
    if (!targetOrigin) {
      console.warn("[plugin] cannot resolve iframe origin, dropping openai stream");
      return;
    }
    try {
      const resp = await fetch(`${baseURL}/api/v1/openai/chat/completions`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
        body: JSON.stringify(params),
      });
      if (!resp.ok) {
        const text = await resp.text();
        throw new Error(`openai stream failed (HTTP ${resp.status}): ${text}`);
      }
      if (!resp.body) throw new Error("streaming response has no body");
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        iframeEl?.contentWindow?.postMessage(
          { type: "plugin-api-stream", id, chunk: decoder.decode(value, { stream: true }) },
          targetOrigin,
        );
      }
      const tail = decoder.decode();
      if (tail) {
        iframeEl?.contentWindow?.postMessage({ type: "plugin-api-stream", id, chunk: tail }, targetOrigin);
      }
      iframeEl?.contentWindow?.postMessage({ type: "plugin-api-stream", id, done: true }, targetOrigin);
    } catch (e) {
      const error = e instanceof Error ? e.message : String(e);
      iframeEl?.contentWindow?.postMessage({ type: "plugin-api-stream", id, error }, targetOrigin);
    }
  }

  async function handlePluginAPI(id: number, method: string, args: unknown[]) {
    let result: unknown;
    let error: string | null = null;
    try {
      switch (method) {
        case "shell": {
          const opts = args[0] as { command: string; timeoutMs?: number };
          const res = await api.post(`/api/v1/devices/${deviceId}/shell`, {
            command: opts.command,
            timeout: opts.timeoutMs ?? 30000,
          });
          result = res.data;
          break;
        }
        case "screenshotAsBase64": {
          const res = await api.get(`/api/v1/devices/${deviceId}/screenshot`);
          result = `data:image/png;base64,${res.data}`;
          break;
        }
        case "setScreenImage": {
          const image = args[0];
          if (image != null && !(image instanceof Blob)) throw new Error("setScreenImage expects Blob|null");
          deviceStore.dumpImage = (image as Blob | null) ?? null;
          break;
        }
        case "inputText": {
          const text = args[0] as string;
          if (typeof text !== "string") throw new Error("inputText expects a string");
          await api.post(`/api/v1/devices/${deviceId}/actions/input_text`, { text });
          break;
        }
        case "setMode": {
          const mode = args?.[0];
          if (typeof mode !== "string" || !Object.values(ScreenMode).includes(mode as ScreenMode)) {
            throw new Error("setMode expects one of: " + Object.values(ScreenMode).join(", "));
          }
          setScreenMode(deviceId, mode as ScreenMode);
          break;
        }
        case "pressKey": {
          const { key, metaState } = args[0] as { key: string; metaState?: number };
          if (typeof key !== "string") throw new Error("pressKey expects a key string");
          await api.post(`/api/v1/devices/${deviceId}/actions/press_key`, { key, metaState: metaState ?? 0 });
          break;
        }
        case "tap": {
          const point = args?.[0] as { x: number; y: number; screenWidth: number; screenHeight: number };
          if (
            !point ||
            typeof point !== "object" ||
            ![point.x, point.y, point.screenWidth, point.screenHeight].every(Number.isFinite) ||
            point.screenWidth <= 0 ||
            point.screenHeight <= 0
          ) {
            throw new Error("tap expects finite numeric x, y and positive screenWidth/screenHeight");
          }
          const injector = getScrcpyTapInjector(deviceId);
          let injected = false;
          if (injector?.available()) {
            try {
              // scrcpy is the preferred path because it is much faster than
              // spawning an adb shell per tap. Only fall back when
              // the write itself fails or stalls (e.g. the socket is closing),
              // so a tap is never silently lost.
              await withTimeout(injector.tap(point), SCRCPY_TAP_TIMEOUT_MS, "scrcpy tap");
              injected = true;
            } catch (e) {
              console.warn("[plugin] scrcpy tap failed, falling back to adb tap:", e);
            }
          }
          if (!injected) {
            await tapDevice(deviceId, point);
          }
          break;
        }
        case "openai": {
          const params = args[0] as Record<string, any>;
          if (params?.stream === true) {
            await streamOpenAI(id, params);
            return;
          }
          result = await postOpenAICompletion(params);
          break;
        }
        case "jev": {
          result = await postJevDecision(args[0] as Record<string, any>);
          break;
        }
        case "appList": {
          const scope = (args?.[0] as AppScope | undefined) ?? "user";
          if (!APP_SCOPE_LIST.includes(scope)) {
            throw new Error("appList expects one of: " + APP_SCOPE_LIST.join(", "));
          }
          result = await listDeviceApps(deviceId, scope);
          break;
        }
        case "appStart": {
          const appId = args?.[0];
          const activity = args?.[1];
          if (typeof appId !== "string" || appId === "") throw new Error("appStart expects an app id");
          if (activity != null && typeof activity !== "string") throw new Error("appStart activity must be a string");
          result = await startDeviceApp(deviceId, appId, activity ?? undefined);
          break;
        }
        case "appStop": {
          const appId = args?.[0];
          if (typeof appId !== "string" || appId === "") throw new Error("appStop expects an app id");
          await stopDeviceApp(deviceId, appId);
          break;
        }
        case "appCurrent": {
          result = await getCurrentDeviceApp(deviceId);
          break;
        }
        default:
          error = `unknown method: ${method}`;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
    const targetOrigin = getTargetOrigin();
    if (targetOrigin) {
      iframeEl?.contentWindow?.postMessage({ type: "plugin-api-result", id, result, error }, targetOrigin);
    }
  }

  onMount(() => {
    function handleMessage(event: MessageEvent) {
      if (event.source !== iframeEl?.contentWindow) return;
      const targetOrigin = getTargetOrigin();
      if (!targetOrigin || event.origin !== targetOrigin) return;
      if (event.data?.type === "plugin-resize") {
        currentHeight = event.data.height;
        return;
      }

      if (event.data?.type === "plugin-api") {
        const { id, method, args } = event.data;
        handlePluginAPI(id, method, args);
      }
    }

    window.addEventListener("message", handleMessage);
    const unsubscribeTap = onScreenTap((event) => {
      if (event.deviceId !== deviceId) return;
      const targetOrigin = getTargetOrigin();
      if (!targetOrigin) {
        console.warn("[plugin] cannot resolve iframe origin, dropping screen tap");
        return;
      }
      iframeEl?.contentWindow?.postMessage({ type: "plugin-screen-tap", event }, targetOrigin);
    });
    const unsubscribeMode = onScreenModeChange(deviceId, (mode) => {
      const targetOrigin = getTargetOrigin();
      if (!targetOrigin) {
        console.warn("[plugin] cannot resolve iframe origin, dropping mode change");
        return;
      }
      iframeEl?.contentWindow?.postMessage({ type: "plugin-mode-change", mode }, targetOrigin);
    });
    return () => {
      unsubscribeTap();
      unsubscribeMode();
      window.removeEventListener("message", handleMessage);
    };
  });
</script>

<iframe
  title={plugin.manifest.name}
  src={getIframeSrc()}
  class={className}
  style:height={isTab ? "100%" : currentHeight + "px"}
  loading="eager"
  bind:this={iframeEl}></iframe>
