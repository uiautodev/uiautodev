import type {
  AndroidKeyCode,
  ScrcpyInjectKeyCodeControlMessage,
  ScrcpyInjectScrollControlMessage,
  ScrcpyInjectTouchControlMessage,
} from "@yume-chan/scrcpy";

export type { ConnectionStatus as ScreenConnectionStatus } from "$lib/hooks/connection";

export interface VideoEncoderSelection {
  name: string;
  codec: string;
  /** Request the H.264 Baseline profile, which the TinyH264 software decoder requires. */
  baseline?: boolean;
}

export interface ScreenHandler {
  mount(canvas: HTMLCanvasElement): () => void;
  injectTouch(message: Omit<ScrcpyInjectTouchControlMessage, "type">): Promise<void>;
  injectKeyCode(message: Omit<ScrcpyInjectKeyCodeControlMessage, "type">): Promise<void>;
  injectScroll?(message: Omit<ScrcpyInjectScrollControlMessage, "type">): Promise<void> | void;

  pasteClipboard?(text: string): void;
  rotateDevice?(): void;
  setScreenPowerMode?(on: boolean): void;
  snapshot?(): Promise<Blob | undefined>;
  startRecord?(): void;
  stopRecord?(): Promise<void>;
}
