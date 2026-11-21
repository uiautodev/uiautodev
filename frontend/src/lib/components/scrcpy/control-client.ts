import type { AndroidKeyCode, AndroidKeyEventMeta } from "@yume-chan/scrcpy";

export interface TouchMessage {
    x: number;
    y: number;
    action: "down" | "up" | "move";
    pointerId?: number;
    pressure?: number;
}

export interface DeviceConnection {
    disconnect(): void;
}

export interface DeviceControlClient {
    connect(): Promise<DeviceConnection>;
    disconnect(): void;
    injectTouch(message: TouchMessage): Promise<void>;
    injectText(text: string): Promise<void>;
    injectKeyEvent(keyCode: AndroidKeyCode): Promise<void>;
    disconnected(listener: () => void): void;
    sizeChanged(listener: (value: { width: number, height: number }) => void): void;
    snapshot(): Promise<Blob | undefined>;
    isSnapshotSupported(): boolean;
    startRecord?(): void;
    stopRecord?(): Promise<void>;
}

