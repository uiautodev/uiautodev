<script lang="ts">
  import {
    AndroidKeyCode,
    AndroidMotionEventAction,
    AndroidMotionEventButton,
    AndroidScreenPowerMode,
    type ScrcpyInjectKeyCodeControlMessage,
    type ScrcpyInjectScrollControlMessage,
    type ScrcpyInjectTouchControlMessage,
  } from "@yume-chan/scrcpy";
  import type { ScreenHandler, VideoEncoderSelection } from "./types";
  import Screen from "./screen.svelte";
  import { ScrcpyDeviceControlClient } from "./scrcpy";
  import { WebCodecsVideoDecoder } from "@yume-chan/scrcpy-decoder-webcodecs";
  import { baseWSURL, getDeviceEncoders, type TapPoint } from "$lib/api/api.js";
  import type { DeviceEncoder } from "$lib/api/schemas.js";
  import { registerScrcpyTapInjector } from "$lib/stores/scrcpy-tap.js";
  import { useLocalStorage } from "$lib/utils/localstorage.svelte";
  import { ConnectionController, ConnectionStatus, type ConnectionState } from "$lib/hooks/connection";
  import { Spinner } from "$lib/components/ui/spinner";
  import { t } from "svelte-i18n";

  let screenWidth = $state(200);
  let screenHeight = $state(400);
  // Set once the stream reported a real video size, so the tap injector can
  // tell "not connected yet" apart from the initial placeholder size.
  let videoSizeReady = false;
  let connection = $state<ConnectionState>({
    status: ConnectionStatus.Connecting,
    retrySeconds: 0,
  });

  interface Props {
    deviceId: string;
    maxFps?: number;
    maxSize?: number;
    [key: string]: any;
  }
  let { deviceId, control = $bindable(true), maxFps = 30, maxSize = 2048, ...restProps }: Props = $props();

  const supportedKeys = [
    AndroidKeyCode.AndroidHome,
    AndroidKeyCode.AndroidBack,
    AndroidKeyCode.AndroidAppSwitch,
    AndroidKeyCode.Power,
    AndroidKeyCode.VolumeUp,
    AndroidKeyCode.VolumeDown,
  ];

  let scrcpyClient: ScrcpyDeviceControlClient | undefined;
  let cropValue = $state("");
  let desiredScreenOn = true;
  const savedEncoder = useLocalStorage<VideoEncoderSelection | null>(`uiautodev:video-encoder:${deviceId}`, null);
  // Without WebCodecs (e.g. a non-secure HTTP origin), the TinyH264 software
  // decoder is used. It only supports Baseline profile, so try hardware H.264
  // encoders first, then fall back to hybrid/software encoders.
  const usesSoftwareDecoder = !WebCodecsVideoDecoder.isSupported;
  let encoderCandidates = $state<string[] | null>(usesSoftwareDecoder ? null : []);
  let encoderIndex = $state(0);
  // Auto mode requests Baseline first because TinyH264 needs it, then retries
  // every candidate without it: some hardware encoders reject the profile
  // MediaFormat option and exit the scrcpy server right after starting.
  let baselinePass = $state(true);
  let encoderError = $state(false);
  // Bumped to re-run the encoder list fetch when reconnecting out of an error
  // state that was caused by the list request itself.
  let encoderReload = $state(0);

  function wantsBaseline(selection: VideoEncoderSelection | null, codec: string): boolean {
    if (codec !== "h264") return false;
    if (selection) return selection.baseline ?? usesSoftwareDecoder;
    return usesSoftwareDecoder && baselinePass;
  }

  function pickEncoderCandidates(encoders: DeviceEncoder[]): string[] {
    const h264 = encoders.filter((e) => e.codec === "h264");
    const ordered: string[] = [];
    const add = (name: string) => {
      if (!ordered.includes(name)) ordered.push(name);
    };

    for (const encoder of h264.filter((e) => e.type === "hw")) {
      add(encoder.name);
    }
    for (const encoder of h264.filter((e) => e.type === "hybrid")) {
      add(encoder.name);
    }
    for (const encoder of h264.filter((e) => e.type === "sw")) {
      add(encoder.name);
    }
    return ordered;
  }

  $effect(() => {
    void encoderReload;
    if (!usesSoftwareDecoder) return;
    let cancelled = false;
    encoderError = false;
    encoderCandidates = null;
    encoderIndex = 0;
    baselinePass = true;
    getDeviceEncoders(deviceId)
      .then((list) => {
        if (cancelled) return;
        const candidates = pickEncoderCandidates(list.video);
        if (candidates.length === 0) {
          console.warn("[scrcpy] no H.264 encoder found");
          encoderCandidates = [];
          encoderError = true;
          return;
        }
        encoderCandidates = candidates;
      })
      .catch((e) => {
        if (cancelled) return;
        console.warn("[scrcpy] failed to get encoder list:", e);
        encoderCandidates = [];
        encoderError = true;
      });
    return () => {
      cancelled = true;
    };
  });

  function setVideoEncoder(value: VideoEncoderSelection | null) {
    savedEncoder.current = value;
    encoderError = false;
    encoderIndex = 0;
    baselinePass = true;
  }

  function tryNextEncoder(): boolean {
    if (!encoderCandidates) {
      return false;
    }
    if (encoderIndex + 1 < encoderCandidates.length) {
      encoderIndex += 1;
      console.warn("[scrcpy] encoder failed, trying next:", encoderCandidates[encoderIndex]);
      return true;
    }
    if (usesSoftwareDecoder && baselinePass) {
      baselinePass = false;
      encoderIndex = 0;
      console.warn("[scrcpy] no encoder accepted Baseline, retrying without it");
      return true;
    }
    return false;
  }

  let wsURL = $derived.by(() => {
    const params = new URLSearchParams({
      max_size: String(maxSize),
      max_fps: String(maxFps),
    });
    const manual = savedEncoder.current;
    const videoCodec = manual?.codec ?? (usesSoftwareDecoder ? "h264" : "");
    if (manual) {
      params.set("video_codec", manual.codec);
      params.set("video_encoder", manual.name);
    } else if (usesSoftwareDecoder) {
      params.set("video_codec", "h264");
      const encoder = encoderCandidates?.[encoderIndex];
      if (encoder) {
        params.set("video_encoder", encoder);
      }
    }
    if (wantsBaseline(manual, videoCodec)) {
      params.set("video_codec_options", "profile:int=1");
    }
    if (cropValue) {
      params.set("crop", cropValue);
    }
    const url = `${baseWSURL}/stream/devices/${deviceId}/screen/scrcpy?${params.toString()}`;
    if (import.meta.env.DEV && cropValue) {
      console.log("[scrcpy] wsURL changed:", url);
    }
    return url;
  });

  let connectFn: (() => void) | undefined;

  function mount(node: HTMLCanvasElement): () => void {
    // The error state keeps the screen chrome (and its encoder picker) mounted,
    // so do not start another doomed connection until the user picks one.
    if (encoderError) {
      return () => {};
    }

    const client = new ScrcpyDeviceControlClient({ node, wsURL });
    scrcpyClient = client;

    client.sizeChanged(({ width, height }) => {
      if (scrcpyClient !== client || width <= 0 || height <= 0) return;
      videoSizeReady = true;
      screenWidth = width;
      screenHeight = height;
    });

    const controller = new ConnectionController({
      onChange: (state) => (connection = state),
      connect: async (onDisconnect) => {
        const manualEncoder = savedEncoder.current;
        const encoder = manualEncoder?.name ?? encoderCandidates?.[encoderIndex] ?? "default";
        if (usesSoftwareDecoder || manualEncoder) {
          console.log("[scrcpy] trying encoder:", encoder);
        }

        // A server that rejects the requested encoder exits before sending any
        // frame, which surfaces both as a socket close and as a connect()
        // rejection. Only the first one may advance the attempt list, otherwise
        // a single failure would skip the next candidate.
        let attemptSettled = false;
        const failAttempt = (): boolean => {
          if (attemptSettled) return true;
          attemptSettled = true;
          return manualEncoder ? false : tryNextEncoder();
        };

        const removeDisconnect = client.disconnected(() => {
          // An encoder that can't produce Baseline exits right after
          // connecting, before any frame is decoded.
          if ((usesSoftwareDecoder || manualEncoder) && client.framesRendered === 0) {
            removeDisconnect();
            if (failAttempt()) {
              return;
            }
            if (manualEncoder) {
              console.warn("[scrcpy] selected encoder failed:", manualEncoder.name);
              onDisconnect();
              return;
            }
            console.warn("[scrcpy] all encoders failed");
            encoderError = true;
            return;
          }
          onDisconnect();
        });

        let conn;
        try {
          conn = await client.connect();
        } catch (e) {
          if (failAttempt()) {
            return new Promise<never>(() => {});
          }
          removeDisconnect();
          if (manualEncoder) {
            console.warn("[scrcpy] failed to start selected encoder:", manualEncoder.name, e);
            throw e;
          }
          if (usesSoftwareDecoder) {
            console.warn("[scrcpy] failed to start encoder:", e);
            encoderError = true;
            return new Promise<never>(() => {});
          }
          throw e;
        }

        client.scrcpyWriter?.setScreenPowerMode(
          desiredScreenOn ? AndroidScreenPowerMode.Normal : AndroidScreenPowerMode.Off,
        );

        let usedEncoderTimer: ReturnType<typeof setInterval> | undefined;
        if (usesSoftwareDecoder || manualEncoder) {
          usedEncoderTimer = setInterval(() => {
            if (client.framesRendered > 0) {
              clearInterval(usedEncoderTimer);
              usedEncoderTimer = undefined;
              console.log("[scrcpy] using encoder:", encoder);
            }
          }, 200);
        }

        return {
          dispose: () => {
            if (usedEncoderTimer) {
              clearInterval(usedEncoderTimer);
            }
            removeDisconnect();
            conn.disconnect();
          },
        };
      },
    });

    const reconnectFn = () => controller.reconnect();
    connectFn = reconnectFn;
    controller.start();

    const unregisterTapInjector = registerScrcpyTapInjector(deviceId, {
      // Only report unavailable when injection would definitely not reach the
      // device: the control channel is closed or the video size is still the
      // placeholder. Everything else (including a size change after a device
      // rotation) keeps using scrcpy, since the adb fallback is much slower.
      available: () => !!scrcpyClient?.connected && videoSizeReady,
      tap: scrcpyTap,
    });

    return () => {
      unregisterTapInjector();
      controller.dispose();
      // Only clear shared state that still belongs to this mount; a newer
      // mount (e.g. after wsURL changes) may already own it.
      if (connectFn === reconnectFn) connectFn = undefined;
      if (scrcpyClient === client) {
        scrcpyClient = undefined;
        videoSizeReady = false;
      }
    };
  }

  function reconnect() {
    // The header reconnect button is also the way out of the encoder error
    // state: restart from the first Baseline pass (this changes wsURL, so the
    // screen remounts and connects again).
    if (encoderError) {
      encoderError = false;
      encoderIndex = 0;
      baselinePass = true;
      if (usesSoftwareDecoder && !encoderCandidates?.length) {
        // The failure was the encoder list request itself; fetch it again.
        encoderReload += 1;
      }
    }
    connectFn?.();
  }

  async function scrcpyTap(point: TapPoint): Promise<void> {
    const writer = scrcpyClient?.scrcpyWriter;
    if (!writer) {
      throw new Error("scrcpy is not connected");
    }
    // scrcpy ignores touch events whose video size doesn't match the current
    // frame. Plugins may pass device or cropped dimensions, so normalize the
    // coordinates to the actual video size before injecting.
    if (!videoSizeReady) {
      throw new Error("video size is not ready");
    }
    const videoWidth = screenWidth;
    const videoHeight = screenHeight;
    const scaleX = videoWidth / point.screenWidth;
    const scaleY = videoHeight / point.screenHeight;
    const jitter = () => Math.random() * 4 - 2;
    const pointerX = Math.min(Math.max(point.x * scaleX + jitter(), 0), videoWidth);
    const pointerY = Math.min(Math.max(point.y * scaleY + jitter(), 0), videoHeight);
    const base = {
      pointerId: BigInt(0),
      pointerX,
      pointerY,
      videoWidth,
      videoHeight,
      pressure: Math.random() * 0.4 + 0.5,
      actionButton: AndroidMotionEventButton.Primary,
      buttons: AndroidMotionEventButton.Primary,
    };
    await writer.injectTouch({ action: AndroidMotionEventAction.Down, ...base });
    await writer.injectTouch({ action: AndroidMotionEventAction.Up, ...base });
  }

  function injectTouch(message: Omit<ScrcpyInjectTouchControlMessage, "type">): void {
    scrcpyClient?.scrcpyWriter?.injectTouch(message);
  }
  function injectKeyCode(message: Omit<ScrcpyInjectKeyCodeControlMessage, "type">): void {
    scrcpyClient?.scrcpyWriter?.injectKeyCode(message);
  }
  function injectScroll(message: Omit<ScrcpyInjectScrollControlMessage, "type">): void {
    scrcpyClient?.scrcpyWriter?.injectScroll(message);
  }

  let pasteSequence = BigInt(1);

  function pasteClipboard(text: string): void {
    scrcpyClient?.scrcpyWriter?.setClipboard({
      sequence: pasteSequence++,
      paste: true,
      content: text,
    });
  }

  async function snapshot(): Promise<Blob | undefined> {
    return scrcpyClient?.snapshot();
  }

  function startRecord(): void {
    if (!scrcpyClient) {
      throw new Error("Cannot start recording: device is not connected.");
    }
    scrcpyClient.startRecord();
  }

  async function stopRecord(): Promise<void> {
    if (!scrcpyClient) {
      throw new Error("Cannot stop recording: device is not connected.");
    }
    await scrcpyClient.stopRecord();
  }

  function setScreenPowerMode(on: boolean): void {
    desiredScreenOn = on;
    scrcpyClient?.scrcpyWriter?.setScreenPowerMode(on ? AndroidScreenPowerMode.Normal : AndroidScreenPowerMode.Off);
  }

  function rotateDevice(): void {
    const p = scrcpyClient?.rotateDevice();
    if (p) {
      void p.catch((e) => console.warn("[scrcpy] rotateDevice failed:", e));
    }
  }

  let screenHandler = {
    mount,
    injectTouch,
    injectKeyCode,
    injectScroll,
    pasteClipboard,
    snapshot,
    startRecord,
    stopRecord,
    setScreenPowerMode,
    rotateDevice,
  } as ScreenHandler;
</script>

{#if encoderCandidates === null && !savedEncoder.current}
  <div class="flex h-full w-full items-center justify-center">
    <Spinner class="size-8" />
  </div>
{:else}
  <Screen
    bind:control
    {deviceId}
    {screenWidth}
    {screenHeight}
    {screenHandler}
    {wsURL}
    {supportedKeys}
    bind:cropValue
    status={connection.status}
    retrySeconds={connection.retrySeconds}
    {reconnect}
    encoderPicker
    errorMessage={encoderError ? $t("screen.encoderUnsupported") : ""}
    videoEncoder={savedEncoder.current}
    onVideoEncoderChange={setVideoEncoder}
    {...restProps} />
{/if}
