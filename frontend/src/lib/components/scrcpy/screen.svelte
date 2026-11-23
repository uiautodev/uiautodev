<script lang="ts">
  import {
    AndroidKeyEventAction,
    AndroidKeyEventMeta,
    AndroidKeyCode,
    AndroidMotionEventAction,
    AndroidMotionEventButton,
  } from "@yume-chan/scrcpy";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import RotatePhone from "$lib/components/icons/rotatephone.svelte";
  import { useElementSize } from "$lib/utils/size.svelte";
  import { getUiTree, getWindowSize } from "$lib/api/api";
  import type { UINode } from "$lib/api/schemas";
  import Separator from "$lib/components/ui/separator/separator.svelte";
  import { Spinner } from "$lib/components/ui/spinner";
  import { deviceStore } from "$lib/stores/device.svelte";
  import { hoverStore } from "$lib/stores/hover.svelte";
  import { emitScreenTap } from "$lib/stores/screen-events.js";
  import { onDestroy } from "svelte";
  import { t } from "svelte-i18n";
  import { type ScreenHandler, type ScreenConnectionStatus } from "./types";
  import { ConnectionStatus } from "$lib/hooks/connection";
  import { clamp } from "./utils";
  import {
    ArrowLeft,
    House,
    Menu,
    Volume1,
    Volume2,
    ListTree,
    BrushCleaning,
    MousePointer2,
    Eye,
    CircleStop,
    Snowflake,
    Power,
    Crop,
    Lightbulb,
    LightbulbOff,
    Link,
    Unlink,
    Cpu,
  } from "lucide-svelte";
  import ButtonCopyScreenshot from "./button-copy-screenshot.svelte";
  import ButtonRecord from "./button-record.svelte";
  import ButtonTooltip from "./button-tooltip.svelte";
  import CropDialog from "./crop-dialog.svelte";
  import EncoderDialog from "./encoder-dialog.svelte";
  import type { VideoEncoderSelection } from "./types.js";
  import DumpMode from "./modes/dump-mode.svelte";
  import { getScreenMode, setScreenMode, ScreenMode } from "$lib/stores/screen-mode.svelte";

  interface ScreenProps {
    name?: string;
    deviceId: string;
    sizeLimit?: string;
    message?: string;
    screenHandler: ScreenHandler;
    screenWidth: number;
    screenHeight: number;
    cropValue?: string;
    wsURL?: string;
    showRightBar?: boolean;
    navInHeader?: boolean;
    supportedKeys?: AndroidKeyCode[];
    status?: ScreenConnectionStatus;
    retrySeconds?: number;
    reconnect?: () => void;
    encoderPicker?: boolean;
    errorMessage?: string;
    videoEncoder?: VideoEncoderSelection | null;
    onVideoEncoderChange?: (value: VideoEncoderSelection | null) => void;
    [key: string]: any;
  }

  const bottomKeyButtons = $derived([
    { keyCode: AndroidKeyCode.AndroidBack, tooltip: $t("screen.toolbar.back"), icon: ArrowLeft },
    { keyCode: AndroidKeyCode.AndroidHome, tooltip: $t("screen.toolbar.home"), icon: House },
    { keyCode: AndroidKeyCode.AndroidAppSwitch, tooltip: $t("screen.toolbar.recents"), icon: Menu },
    { keyCode: AndroidKeyCode.Power, tooltip: $t("screen.toolbar.power"), icon: Power },
  ]);

  const extraKeyButtons = $derived([
    { keyCode: AndroidKeyCode.VolumeUp, tooltip: $t("screen.toolbar.volumeUp"), icon: Volume2 },
    { keyCode: AndroidKeyCode.VolumeDown, tooltip: $t("screen.toolbar.volumeDown"), icon: Volume1 },
  ]);

  const allKeyButtons = [
    AndroidKeyCode.AndroidBack,
    AndroidKeyCode.AndroidHome,
    AndroidKeyCode.AndroidAppSwitch,
    AndroidKeyCode.Power,
    AndroidKeyCode.VolumeUp,
    AndroidKeyCode.VolumeDown,
  ];

  let {
    name: deviceName = "",
    deviceId,
    sizeLimit = "",
    message = "",
    control = $bindable(true),
    hasToolbar = true,
    class: externalClass = "",
    screenHandler,
    supportedKeys: supportedKeys = allKeyButtons,
    screenWidth,
    screenHeight,
    cropValue = $bindable(""),
    wsURL = "",
    showRightBar = true,
    navInHeader = false,
    status = "connected",
    retrySeconds = 0,
    reconnect,
    encoderPicker = false,
    errorMessage = "",
    videoEncoder = null,
    onVideoEncoderChange,
    ...restProps
  }: ScreenProps = $props();

  let canvasEl: HTMLCanvasElement | undefined = $state();
  let pasteInput: HTMLInputElement | undefined = $state();
  let ctrlPressed = $state(false);
  let tapFlash = $state(false);
  let tapFlashTimer: ReturnType<typeof setTimeout> | undefined;

  // Ring cursor state, in CSS pixels relative to the canvas. The ring replaces
  // the hidden system cursor so it can be drawn inside the phone screen and
  // clamped to its edges while the pointer is dragged outside.
  let cursorX = $state(0);
  let cursorY = $state(0);
  let cursorVisible = $state(false);
  let cursorDragging = $state(false);

  onDestroy(() => {
    if (tapFlashTimer) clearTimeout(tapFlashTimer);
    tapFlashTimer = undefined;
  });
  const blockSize = useElementSize();
  const canvasSize = useElementSize();

  let isPortrait = $derived(screenHeight >= screenWidth);
  let wrapperStyle = $derived(isPortrait ? `width: ${sizeLimit};` : `height: ${sizeLimit};`);
  let isTiny = $derived(blockSize.width < 200);
  let mode = $derived(getScreenMode(deviceId));
  let recording = $derived(mode === ScreenMode.Recording);
  let controlsEnabled = $derived(
    control && mode !== ScreenMode.Readonly && !deviceStore.dumpImage && !deviceStore.node,
  );
  let capturing = $derived(ctrlPressed || recording);
  let cursorRingClass = $derived(tapFlash ? "cursor-ring-active" : capturing ? "cursor-ring-recording" : "cursor-ring");

  // The ring only exists while the screen accepts input; read-only and dump
  // overlays keep the system cursor (cursor-not-allowed) as the indicator.
  $effect(() => {
    if (!controlsEnabled) {
      cursorVisible = false;
      cursorDragging = false;
    }
  });
  let statusLabel = $derived(
    status === ConnectionStatus.Connected ? "已连接" : status === ConnectionStatus.Connecting ? "连接中..." : "已断开",
  );
  let statusIconClass = $derived(
    status === ConnectionStatus.Connected
      ? "text-green-500"
      : status === ConnectionStatus.Connecting
        ? "text-amber-500"
        : "text-red-500",
  );

  let modeTooltip = $derived(
    mode === ScreenMode.Recording
      ? $t("screen.recording.stop")
      : mode === ScreenMode.Readonly
        ? $t("screen.toolbar.enableTouch")
        : $t("screen.toolbar.toggleTouch"),
  );
  let modeButtonClass = $derived(
    mode === ScreenMode.Recording
      ? "rounded p-0.5 text-red-500 hover:bg-slate-200 dark:hover:bg-slate-700"
      : mode === ScreenMode.Readonly
        ? "rounded p-0.5 bg-blue-500 text-white hover:bg-blue-600"
        : "rounded p-0.5 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700",
  );

  function onModeClick() {
    if (mode === ScreenMode.Recording) {
      setScreenMode(deviceId, ScreenMode.Control);
    } else if (mode === ScreenMode.Control) {
      setScreenMode(deviceId, ScreenMode.Readonly);
    } else {
      setScreenMode(deviceId, ScreenMode.Control);
    }
  }

  let windowWidth = $state(0);
  let windowHeight = $state(0);

  $effect(() => {
    getWindowSize(deviceId)
      .then((size) => {
        if (size?.width && size?.height) {
          windowWidth = Number(size.width);
          windowHeight = Number(size.height);
        }
      })
      .catch(() => {});
  });

  const isLandscape = $derived(screenWidth > screenHeight);
  const displayWidth = $derived(
    isLandscape ? Math.max(windowWidth, windowHeight) : Math.min(windowWidth, windowHeight),
  );
  const displayHeight = $derived(
    isLandscape ? Math.min(windowWidth, windowHeight) : Math.max(windowWidth, windowHeight),
  );

  // Mode management
  let cropDialogOpen = $state(false);
  let encoderDialogOpen = $state(false);
  let screenOn = $state(true);

  const PointerEventButtonToAndroidButton = [
    AndroidMotionEventButton.Primary,
    AndroidMotionEventButton.Tertiary,
    AndroidMotionEventButton.Secondary,
    AndroidMotionEventButton.Back,
    AndroidMotionEventButton.Forward,
  ];

  async function inputKeyEvent(keyCode: AndroidKeyCode) {
    await screenHandler.injectKeyCode({
      action: AndroidKeyEventAction.Down,
      keyCode,
      repeat: 0,
      metaState: AndroidKeyEventMeta.None,
    });
    await screenHandler.injectKeyCode({
      action: AndroidKeyEventAction.Up,
      keyCode,
      repeat: 0,
      metaState: AndroidKeyEventMeta.None,
    });
  }

  async function snapshot(): Promise<Blob> {
    let blob: Blob | undefined;

    // Use screenHandler's snapshot method if available (works with WebGL)
    if (screenHandler.snapshot) {
      blob = await screenHandler.snapshot();
    }

    // Fallback to canvas toBlob() if snapshot is not available
    if (!blob && canvasEl) {
      blob = await new Promise<Blob | undefined>((resolve) => {
        canvasEl!.toBlob((blob) => resolve(blob ?? undefined), "image/png");
      });
    }

    if (!blob) {
      throw new Error("Failed to capture screenshot");
    }
    return blob;
  }

  function onMiddleClick() {
    inputKeyEvent(AndroidKeyCode.AndroidHome);
  }

  function onRightClick() {
    inputKeyEvent(AndroidKeyCode.AndroidBack);
  }

  async function dumpHierarchy() {
    try {
      deviceStore.freezeScreen(screenHandler.snapshot);
      await deviceStore.dumpHierarchy();
    } catch (e) {
      console.error("Failed to dump UI hierarchy:", e);
    }
  }

  async function clearHierarchy() {
    deviceStore.dumpImage = null;
    deviceStore.node = null;
    deviceStore.nodeSelected = null;
    console.log("Cleared UI hierarchy cache and overlay.");
  }

  function toggleDumpMode() {
    if (deviceStore.dumping) return;
    if (deviceStore.node) {
      clearHierarchy();
    } else {
      dumpHierarchy();
    }
  }

  function updateCursor(e: PointerEvent, rect: DOMRect) {
    // Touch never shows a ring: the contact point is its own indicator and the
    // system cursor is not over the screen anyway.
    if (!controlsEnabled || (e.pointerType !== "mouse" && e.pointerType !== "pen")) {
      cursorVisible = false;
      cursorDragging = false;
      return;
    }

    if (e.type === "pointerdown") {
      cursorDragging = true;
    } else if (e.type === "pointerup" || e.type === "pointercancel") {
      cursorDragging = false;
    }

    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const inside = x >= 0 && x <= rect.width && y >= 0 && y <= rect.height;
    // While a button is held the ring stays visible even outside the screen:
    // the axis that left the screen is clamped to its edge while the other
    // axis keeps following the pointer.
    if (inside || cursorDragging) {
      cursorX = clamp(x, 0, rect.width);
      cursorY = clamp(y, 0, rect.height);
      cursorVisible = true;
    } else {
      cursorVisible = false;
    }
  }

  // lostpointercapture is the fallback for a pointerup the canvas never sees
  // (window unfocused mid-drag). After a normal pointerup the drag flag is
  // already cleared, so this is a no-op.
  function handleLostPointerCapture() {
    if (!cursorDragging) return;
    cursorDragging = false;
    cursorVisible = false;
  }

  function handlePointer(e: PointerEvent) {
    e.preventDefault();
    e.stopPropagation();

    // Re-sync the Ctrl state from the physical modifier on every pointer event.
    // A keydown can be missed when Ctrl is pressed while the window is unfocused
    // (e.g. after clicking another app), which would otherwise leave the cursor
    // stuck without the Ctrl indicator.
    ctrlPressed = e.ctrlKey;

    const target = e.currentTarget as HTMLElement;
    const rect = target.getBoundingClientRect();

    const { type, clientX, clientY, button, buttons } = e;
    // screenWidth/screenHeight are the streamed video dimensions (device
    // resolution, or the cropped size when crop is enabled). pointerX/pointerY
    // are therefore in that video coordinate space, not in canvas CSS pixels;
    // this is the same space injectTouch expects.
    const percentageX = clamp((clientX - rect.x) / rect.width, 0, 1);
    const percentageY = clamp((clientY - rect.y) / rect.height, 0, 1);
    const pointerX = percentageX * screenWidth;
    const pointerY = percentageY * screenHeight;

    updateCursor(e, rect);

    if (type === "pointerleave") {
      hoverStore.visible = false;
      return;
    }

    if (type === "pointermove" || type === "pointerdown") {
      if (displayWidth && displayHeight) {
        hoverStore.x = percentageX * displayWidth;
        hoverStore.y = percentageY * displayHeight;
        hoverStore.percentX = percentageX;
        hoverStore.percentY = percentageY;
        hoverStore.visible = true;
      } else {
        hoverStore.visible = false;
      }
    }

    if (!controlsEnabled) {
      return;
    }

    // Capture before the recording branch too: the ring must keep receiving
    // pointermove events while the mouse is dragged outside the screen,
    // regardless of the mode the screen is in.
    target.setPointerCapture(e.pointerId);

    if (e.ctrlKey || recording) {
      // Only a left-button pointerdown starts a recording tap; other pointer
      // events and buttons are swallowed so they never reach the device.
      if (type === "pointerdown" && button === 0) {
        // Prevent the browser's Ctrl+click context menu / text selection.
        e.preventDefault();
        // x/y are in the video coordinate space matching screenWidth/screenHeight,
        // so the backend scales them by device/video ratio (a no-op when the
        // video size equals the device resolution).
        emitScreenTap({
          deviceId,
          x: pointerX,
          y: pointerY,
          screenWidth,
          screenHeight,
          timestamp: Date.now(),
        });
        tapFlash = true;
        if (tapFlashTimer) clearTimeout(tapFlashTimer);
        tapFlashTimer = setTimeout(() => (tapFlash = false), 150);
      }
      return;
    }

    if (button === 1 || button === 2) {
      if (type === "pointerdown") {
        if (button === 1) {
          onMiddleClick(); // Middle button
        } else if (button === 2) {
          onRightClick(); // Right button
        }
      }
      return;
    }
    let action: AndroidMotionEventAction;
    switch (type) {
      case "pointerdown":
        action = AndroidMotionEventAction.Down;
        pasteInput?.focus();
        break;
      case "pointermove":
        if (buttons === 0) {
          action = AndroidMotionEventAction.HoverMove;
        } else {
          action = AndroidMotionEventAction.Move;
        }
        break;
      case "pointerup":
        action = AndroidMotionEventAction.Up;
        break;
      default:
        throw new Error(`Unsupported event type: ${type}`);
    }

    screenHandler.injectTouch({
      action,
      pointerId: BigInt(e.pointerId),
      pointerX,
      pointerY,
      videoWidth: screenWidth,
      videoHeight: screenHeight,
      pressure: buttons === 0 ? 0 : 1,
      actionButton: PointerEventButtonToAndroidButton[button],
      buttons,
    });
  }

  function handleWheel(e: WheelEvent) {
    if (!controlsEnabled || !screenHandler.injectScroll) {
      return;
    }
    if (capturing) {
      return;
    }
    e.preventDefault();

    const target = e.currentTarget as HTMLElement;
    const rect = target.getBoundingClientRect();
    const percentageX = clamp((e.clientX - rect.x) / rect.width, 0, 1);
    const percentageY = clamp((e.clientY - rect.y) / rect.height, 0, 1);
    const pointerX = percentageX * screenWidth;
    const pointerY = percentageY * screenHeight;

    // Browser wheel deltas are in pixels (typically 100 per notch), lines or
    // pages depending on deltaMode. scrcpy expects scroll amounts in ticks.
    function toTicks(delta: number): number {
      switch (e.deltaMode) {
        case WheelEvent.DOM_DELTA_LINE:
          return delta;
        case WheelEvent.DOM_DELTA_PAGE:
          return delta * 3;
        default:
          return delta / 100;
      }
    }

    // Positive values scroll left/up, negative values scroll right/down, while
    // the browser reports positive deltas when scrolling right/down.
    void screenHandler.injectScroll({
      pointerX,
      pointerY,
      videoWidth: screenWidth,
      videoHeight: screenHeight,
      scrollX: -toTicks(e.deltaX),
      scrollY: -toTicks(e.deltaY),
      buttons: e.buttons,
    });
  }

  // Numeric keypad keys report Numpad* codes, which map to Android
  // KEYCODE_NUMPAD_* and are not handled by every app (and not mapped at all
  // for HarmonyOS/iOS). Normalize them to their main-keyboard equivalents so
  // typing digits on the numpad behaves like typing them normally.
  // Symbols are mapped to their physical-keyboard equivalents because
  // KEYCODE_PLUS/KEYCODE_STAR do not produce characters in most input fields
  // (and STAR is not mapped at all for HarmonyOS/iOS).
  const numpadKeyCodes: Record<string, { keyCode: AndroidKeyCode; extraMetaState?: AndroidKeyEventMeta }> = {
    Numpad0: { keyCode: AndroidKeyCode.Digit0 },
    Numpad1: { keyCode: AndroidKeyCode.Digit1 },
    Numpad2: { keyCode: AndroidKeyCode.Digit2 },
    Numpad3: { keyCode: AndroidKeyCode.Digit3 },
    Numpad4: { keyCode: AndroidKeyCode.Digit4 },
    Numpad5: { keyCode: AndroidKeyCode.Digit5 },
    Numpad6: { keyCode: AndroidKeyCode.Digit6 },
    Numpad7: { keyCode: AndroidKeyCode.Digit7 },
    Numpad8: { keyCode: AndroidKeyCode.Digit8 },
    Numpad9: { keyCode: AndroidKeyCode.Digit9 },
    NumpadAdd: { keyCode: AndroidKeyCode.Equal, extraMetaState: AndroidKeyEventMeta.Shift },
    NumpadSubtract: { keyCode: AndroidKeyCode.Minus },
    NumpadMultiply: { keyCode: AndroidKeyCode.Digit8, extraMetaState: AndroidKeyEventMeta.Shift },
    NumpadDivide: { keyCode: AndroidKeyCode.Slash },
    NumpadDecimal: { keyCode: AndroidKeyCode.Period },
    NumpadComma: { keyCode: AndroidKeyCode.Comma },
    NumpadEnter: { keyCode: AndroidKeyCode.Enter },
    NumpadEqual: { keyCode: AndroidKeyCode.Equal },
  };

  function getMetaState(e: KeyboardEvent): AndroidKeyEventMeta {
    let meta = AndroidKeyEventMeta.None;
    if (e.shiftKey) {
      meta |= AndroidKeyEventMeta.Shift;
    }
    if (e.ctrlKey) {
      meta |= AndroidKeyEventMeta.Ctrl;
    }
    if (e.altKey) {
      meta |= AndroidKeyEventMeta.Alt;
    }
    return meta as AndroidKeyEventMeta;
  }

  function handlePaste(event: ClipboardEvent) {
    if (!controlsEnabled || !screenHandler.pasteClipboard) {
      return;
    }
    event.preventDefault();
    const text = event.clipboardData?.getData("text/plain") ?? "";
    screenHandler.pasteClipboard(text);
    if (pasteInput) {
      pasteInput.value = "";
    }
  }

  // Forward text committed by the host input method (e.g. typing Chinese with
  // an IME). Browsers deliver the committed text via compositionend without a
  // trailing non-composing input event, so it must be handled here. The input
  // value is cleared so a following input event does not send it twice.
  function handleCompositionEnd(event: CompositionEvent) {
    if (!controlsEnabled || !screenHandler.pasteClipboard) {
      return;
    }
    const input = event.currentTarget as HTMLInputElement;
    const text = input.value;
    if (!text) {
      return;
    }
    screenHandler.pasteClipboard(text);
    input.value = "";
  }

  // Forward text inserted without composition (e.g. emoji picker, autofill).
  function handleInput(event: Event) {
    if (!controlsEnabled || !screenHandler.pasteClipboard) {
      return;
    }
    if ((event as InputEvent).isComposing) {
      return;
    }
    const input = event.currentTarget as HTMLInputElement;
    const text = input.value;
    if (!text) {
      return;
    }
    screenHandler.pasteClipboard(text);
    input.value = "";
  }

  async function toggleFreeze() {
    if (deviceStore.dumpImage) {
      deviceStore.dumpImage = null;
      return;
    }
    await deviceStore.freezeScreen(screenHandler.snapshot);
  }

  function toggleScreenPower() {
    screenOn = !screenOn;
    screenHandler.setScreenPowerMode?.(screenOn);
  }

  function syncCtrl(e: KeyboardEvent) {
    ctrlPressed = e.ctrlKey;
  }

  function clearCtrl() {
    ctrlPressed = false;
  }

  // Losing focus mid-drag can swallow pointerup, which would leave the ring
  // stuck on the screen edge forever.
  function handleWindowBlur() {
    clearCtrl();
    cursorVisible = false;
    cursorDragging = false;
  }

  function handleKeyevent(e: KeyboardEvent) {
    if (!controlsEnabled) {
      return;
    }
    // Meta will trigger Huawei phone voice assistant, ignore it
    if (e.key === "Meta") {
      return;
    }

    // Let the host input method compose non-ASCII text (e.g. Chinese); the
    // committed result is forwarded by handleCompositionEnd.
    // keyCode 229 is the legacy value browsers report for key events consumed
    // by an IME (e.g. composing pinyin), used by browsers/webviews that do not
    // set isComposing.
    if (e.isComposing || e.keyCode === 229) {
      return;
    }

    // Handle paste shortcuts (Ctrl+V or Cmd+V): skip key injection and let the
    // native paste event fire so the hidden input's onpaste can pick it up.
    if ((e.ctrlKey || e.metaKey) && e.key === "v") {
      return;
    }

    e.preventDefault();

    let keyCode: AndroidKeyCode;
    let extraMetaState: AndroidKeyEventMeta = AndroidKeyEventMeta.None;
    if (e.code in numpadKeyCodes) {
      const mapped = numpadKeyCodes[e.code];
      keyCode = mapped.keyCode;
      extraMetaState = mapped.extraMetaState ?? AndroidKeyEventMeta.None;
    } else if (e.code in AndroidKeyCode) {
      keyCode = AndroidKeyCode[e.code as keyof typeof AndroidKeyCode];
    } else {
      console.log("Unhandled key:", e.key, e.code);
      return;
    }
    const action = e.type === "keydown" ? AndroidKeyEventAction.Down : AndroidKeyEventAction.Up;
    let metaState = (getMetaState(e) | extraMetaState) as AndroidKeyEventMeta;
    screenHandler.injectKeyCode({
      action,
      keyCode,
      repeat: 0,
      metaState,
    });
  }
</script>

<svelte:window onkeydown={syncCtrl} onkeyup={syncCtrl} onblur={handleWindowBlur} />

{#snippet toolToolbar()}
  <div class="flex flex-col items-center w-[40px]">
    <ButtonGroup.Root orientation="vertical">
      {#each extraKeyButtons as { keyCode, tooltip, icon: Icon } (keyCode)}
        {#if supportedKeys.includes(keyCode)}
          <ButtonTooltip disabled={!controlsEnabled} size="sm" {tooltip} onclick={() => inputKeyEvent(keyCode)}>
            <Icon />
          </ButtonTooltip>
        {/if}
      {/each}
      {#if screenHandler.rotateDevice}
        <ButtonTooltip
          disabled={!controlsEnabled}
          size="sm"
          tooltip={$t("screen.toolbar.rotate")}
          onclick={() => screenHandler.rotateDevice?.()}>
          <RotatePhone />
        </ButtonTooltip>
      {/if}
      {#if encoderPicker}
        <ButtonTooltip
          size="sm"
          tooltip={$t("screen.toolbar.encoder")}
          class={[videoEncoder && "text-green-500"]}
          onclick={() => (encoderDialogOpen = true)}>
          <Cpu />
        </ButtonTooltip>
      {/if}
      <Separator />
      <ButtonTooltip
        size="sm"
        tooltip={$t("screen.toolbar.crop")}
        class={[cropValue && "text-green-500"]}
        onclick={() => (cropDialogOpen = true)}>
        <Crop />
      </ButtonTooltip>
      <Separator />
      <ButtonTooltip
        disabled={deviceStore.dumping}
        size="sm"
        class={["cursor-pointer", deviceStore.node && "text-green-500"]}
        tooltip={$t("screen.toolbar.dump")}
        onclick={dumpHierarchy}>
        {#if deviceStore.dumping}
          <Spinner />
        {:else}
          <ListTree />
        {/if}
      </ButtonTooltip>
      <ButtonTooltip size="sm" tooltip={$t("screen.toolbar.clearHierarchy")} onclick={clearHierarchy}>
        <BrushCleaning />
      </ButtonTooltip>
      <Separator />
      <ButtonCopyScreenshot {snapshot} />
      <ButtonRecord {screenHandler} />
    </ButtonGroup.Root>
  </div>
{/snippet}

{#snippet navButtons(inline: boolean)}
  {#each bottomKeyButtons as { keyCode, tooltip, icon: Icon }, i (keyCode)}
    <ButtonTooltip
      side={inline ? "bottom" : "top"}
      variant="ghost"
      disabled={!controlsEnabled || !supportedKeys.includes(keyCode)}
      {tooltip}
      aria-label={tooltip}
      onclick={() => inputKeyEvent(keyCode)}
      class={inline
        ? "h-7 px-2 rounded disabled:cursor-not-allowed disabled:!pointer-events-auto"
        : "w-full flex-1 rounded-none min-h-9 disabled:cursor-not-allowed disabled:!pointer-events-auto"}>
      <Icon />
    </ButtonTooltip>
    {#if !inline && i < bottomKeyButtons.length - 1}
      <Separator orientation="vertical" class="shrink-0 bg-slate-300 dark:bg-slate-600 self-stretch w-px" />
    {/if}
  {/each}
{/snippet}

{#snippet navToolbar()}
  <div class="shrink-0 flex w-full bg-slate-100 dark:bg-slate-800 border-t border-slate-200 dark:border-slate-700">
    {@render navButtons(false)}
  </div>
{/snippet}

{#snippet canvasWrapper()}
  <div
    class="relative flex max-h-full max-w-full min-h-0 min-w-0"
    style:max-width={isTiny || !showRightBar ? "100%" : "calc(100% - 40px)"}>
    {#key wsURL}
      <canvas
        class="max-h-full max-w-full object-contain border rounded-md border-gray-300 dark:border-slate-700 transition-opacity"
        class:opacity-40={status === ConnectionStatus.Disconnected}
        class:border-red-500={recording}
        class:dark:border-red-500={recording}
        class:cursor-none={controlsEnabled}
        class:cursor-not-allowed={!controlsEnabled}
        onpointerdown={handlePointer}
        onpointerup={handlePointer}
        onpointermove={handlePointer}
        onpointerleave={handlePointer}
        onpointercancel={handlePointer}
        onlostpointercapture={handleLostPointerCapture}
        onwheel={handleWheel}
        width={screenWidth}
        height={screenHeight}
        bind:this={canvasEl}
        oncontextmenu={(e) => e.preventDefault()}
        onclick={() => pasteInput?.focus()}
        {@attach canvasSize.observe}
        {@attach screenHandler.mount}></canvas>
      <input
        bind:this={pasteInput}
        class="absolute top-1/2 left-0 w-px h-px -translate-y-1/2 opacity-0 pointer-events-none"
        tabindex="-1"
        aria-label="Screen keyboard input"
        onkeydown={handleKeyevent}
        onkeyup={handleKeyevent}
        oninput={handleInput}
        oncompositionend={handleCompositionEnd}
        onpaste={handlePaste} />
    {/key}
    {#if cursorVisible}
      <div
        class={`pointer-events-none absolute z-10 size-5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 ${cursorRingClass}`}
        style:left="{cursorX}px"
        style:top="{cursorY}px"
        aria-hidden="true">
      </div>
    {/if}
  </div>
  {#if status === ConnectionStatus.Disconnected && !errorMessage}
    <div
      class="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 {!isTiny &&
      showRightBar
        ? 'pr-[40px]'
        : ''}"
      role="status"
      aria-label={$t("screen.status.disconnected")}>
      <Unlink class="size-10 text-red-500 drop-shadow-md" />
      {#if retrySeconds > 0}
        <span class="rounded bg-black/60 px-2 py-0.5 text-xs text-white">
          {$t("screen.status.reconnectIn", { values: { seconds: retrySeconds } })}
        </span>
      {/if}
    </div>
  {/if}

  {#if errorMessage}
    <div
      class="pointer-events-none absolute inset-0 flex items-center justify-center p-4 {!isTiny && showRightBar
        ? 'pr-[40px]'
        : ''}"
      role="alert"
      aria-label={errorMessage}>
      <div
        class="flex flex-col items-center gap-2 text-center"
        style:width={canvasSize.width ? `${canvasSize.width}px` : undefined}
        style:max-width="100%">
        <Unlink class="size-10 text-red-500 drop-shadow-md" />
        <span class="rounded bg-black/60 px-2 py-0.5 text-xs break-words text-white">{errorMessage}</span>
        {#if encoderPicker}
          <button
            type="button"
            class="pointer-events-auto rounded bg-primary px-3 py-1 text-xs text-primary-foreground hover:bg-primary/90"
            onclick={() => (encoderDialogOpen = true)}>
            {$t("screen.encoderUnsupportedAction")}
          </button>
        {/if}
      </div>
    </div>
  {/if}

  <DumpMode
    {screenWidth}
    {screenHeight}
    canvasWidth={canvasSize.width}
    canvasHeight={canvasSize.height}
    {isTiny}
    {showRightBar} />

  <CropDialog
    bind:active={cropDialogOpen}
    {deviceId}
    initialCrop={cropValue}
    onapply={(v) => (cropValue = v)}
    onclear={() => (cropValue = "")} />

  {#if encoderPicker}
    <EncoderDialog
      bind:active={encoderDialogOpen}
      {deviceId}
      selected={videoEncoder}
      onapply={(value) => onVideoEncoderChange?.(value)} />
  {/if}
{/snippet}
<div
  {@attach blockSize.observe}
  class={`flex flex-col bg-gray-200 dark:bg-slate-900 ${externalClass}`}
  aria-label="scrcpy-phone"
  style={wrapperStyle}
  {...restProps}>
  <header
    class="relative items-center gap-1 h-8 text-sm overflow-hidden bg-slate-100 dark:bg-slate-800 border-b border-slate-200 dark:border-slate-700 min-w-0 {navInHeader
      ? 'grid grid-cols-2'
      : 'flex justify-center'}">
    {#if deviceName && !navInHeader}
      <span class="absolute left-1 truncate max-w-[calc(100%-60px)] pointer-events-none">{deviceName}</span>
    {/if}
    <div class="flex items-center gap-1 justify-center">
      {#if message && status !== ConnectionStatus.Connecting}
        <span class="truncate text-muted-foreground">{message}</span>
      {/if}
      {#if reconnect}
        <Tooltip.Root>
          <Tooltip.Trigger>
            {#snippet child({ props })}
              <button
                {...props}
                type="button"
                aria-label={$t("screen.status.reconnect", { values: { label: statusLabel } })}
                class="rounded p-0.5 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700"
                onclick={reconnect}>
                {#if status === ConnectionStatus.Disconnected}
                  <Unlink class={`size-4 ${statusIconClass}`} />
                {:else}
                  <Link
                    class={`size-4 ${statusIconClass}${
                      status === ConnectionStatus.Connecting ? " animate-pulse" : ""
                    }`} />
                {/if}
              </button>
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content side="bottom">
            <p>{statusLabel}（点击重连）</p>
          </Tooltip.Content>
        </Tooltip.Root>
      {/if}
      {#if control}
        <Tooltip.Root>
          <Tooltip.Trigger>
            {#snippet child({ props })}
              <button {...props} type="button" aria-label={modeTooltip} class={modeButtonClass} onclick={onModeClick}>
                {#if recording}
                  <CircleStop class="size-4 animate-pulse" />
                {:else if mode === ScreenMode.Readonly}
                  <Eye class="size-4" />
                {:else}
                  <MousePointer2 class="size-4 text-green-500" />
                {/if}
              </button>
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content side="bottom">
            <p>{modeTooltip}</p>
          </Tooltip.Content>
        </Tooltip.Root>
      {/if}
      {#if screenHandler.setScreenPowerMode}
        <Tooltip.Root>
          <Tooltip.Trigger>
            {#snippet child({ props })}
              <button
                {...props}
                type="button"
                class="rounded p-0.5 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700"
                onclick={toggleScreenPower}>
                {#if screenOn}
                  <Lightbulb class="size-4 text-green-500" />
                {:else}
                  <LightbulbOff class="size-4 text-gray-400" />
                {/if}
              </button>
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content side="bottom">
            <p>{screenOn ? $t("screen.toolbar.turnScreenOff") : $t("screen.toolbar.turnScreenOn")}</p>
          </Tooltip.Content>
        </Tooltip.Root>
      {/if}
      <Tooltip.Root>
        <Tooltip.Trigger>
          {#snippet child({ props })}
            <button
              {...props}
              type="button"
              disabled={!screenHandler.snapshot}
              aria-pressed={!!deviceStore.dumpImage}
              class="rounded p-0.5 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700 disabled:pointer-events-none disabled:opacity-50"
              onclick={toggleFreeze}>
              {#if deviceStore.freezing}
                <Spinner class="size-4" />
              {:else if deviceStore.dumpImage}
                <Snowflake class="size-4 text-green-500" />
              {:else}
                <Snowflake class="size-4 text-gray-400" />
              {/if}
            </button>
          {/snippet}
        </Tooltip.Trigger>
        <Tooltip.Content side="bottom">
          <p>{$t("screen.toolbar.freeze")}</p>
        </Tooltip.Content>
      </Tooltip.Root>
      <Tooltip.Root>
        <Tooltip.Trigger>
          {#snippet child({ props })}
            <button
              {...props}
              type="button"
              disabled={deviceStore.dumping}
              aria-pressed={!!deviceStore.node}
              class="rounded p-0.5 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700 disabled:pointer-events-none disabled:opacity-50"
              onclick={toggleDumpMode}>
              {#if deviceStore.dumping}
                <Spinner class="size-4" />
              {:else if deviceStore.node}
                <ListTree class="size-4 text-green-500" />
              {:else}
                <ListTree class="size-4 text-gray-400" />
              {/if}
            </button>
          {/snippet}
        </Tooltip.Trigger>
        <Tooltip.Content side="bottom">
          <p>{deviceStore.node ? $t("screen.toolbar.exitInspect") : $t("screen.toolbar.dump")}</p>
        </Tooltip.Content>
      </Tooltip.Root>
    </div>
    {#if navInHeader}
      <div class="flex items-center h-full justify-center">
        {@render navButtons(true)}
        <ButtonCopyScreenshot {snapshot} inline />
        <ButtonRecord {screenHandler} inline />
      </div>
    {/if}
  </header>
  <main class="flex flex-col flex-1 justify-center items-center bg-gray-200 dark:bg-slate-900 overflow-hidden">
    <div class="flex flex-col w-full max-h-full min-h-0">
      <div class="flex flex-row w-full justify-center relative min-h-0">
        {@render canvasWrapper()}
        {#if !isTiny && showRightBar}{@render toolToolbar()}{/if}
      </div>
      {#if !isTiny && !navInHeader}
        {@render navToolbar()}
      {/if}
    </div>
  </main>
</div>
