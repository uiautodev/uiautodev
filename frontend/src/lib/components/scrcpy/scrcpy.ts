import { BitmapVideoFrameRenderer, WebCodecsVideoDecoder, WebGLVideoFrameRenderer, type VideoFrameRenderer } from "@yume-chan/scrcpy-decoder-webcodecs";
import { createWebSocketReadableStream, createWebSocketWritableStream } from "./utils";
import { AndroidKeyCode, AndroidKeyEventAction, AndroidKeyEventMeta, AndroidMotionEventAction, AndroidMotionEventButton, ScrcpyControlMessageWriter, ScrcpyOptions3_3_3, ScrcpyVideoCodecNameMap } from "@yume-chan/scrcpy";
import type { DeviceConnection, DeviceControlClient, TouchMessage } from "./control-client";
import { EventEmitter, StickyEventEmitter } from "@yume-chan/event";
import { TinyH264Decoder, type ScrcpyVideoDecoder } from "@yume-chan/scrcpy-decoder-tinyh264";
import { createRecorder, type Recorder } from "./record";
import { TransformStream } from "@yume-chan/stream-extra";
import type { ScrcpyMediaStreamPacket } from "@yume-chan/scrcpy";

function createVideoFrameRenderer(
    canvasElement: HTMLCanvasElement,
): VideoFrameRenderer {
    if (WebGLVideoFrameRenderer.isSupported) {
        return new WebGLVideoFrameRenderer(canvasElement);
    }
    return new BitmapVideoFrameRenderer(canvasElement);
}

const PointerEventButtonToAndroidButton = [
    AndroidMotionEventButton.Primary,
    AndroidMotionEventButton.Tertiary,
    AndroidMotionEventButton.Secondary,
    AndroidMotionEventButton.Back,
    AndroidMotionEventButton.Forward,
];

export interface ScrcpyDeviceControlClientOptions {
    node: HTMLCanvasElement;
    wsURL: string;
    videoBitrate?: number; // in kbps
    maxFps?: number;
}

export class ScrcpyDeviceControlClient implements DeviceControlClient {
    private options: ScrcpyDeviceControlClientOptions;
    private socket?: WebSocket;
    private decoder?: ScrcpyVideoDecoder;
    public scrcpyWriter?: ScrcpyControlMessageWriter;
    private recorder?: Recorder;
    #generation = 0;

    constructor(options: ScrcpyDeviceControlClientOptions) {
        this.options = options;
    }

    async connect(): Promise<DeviceConnection> {
        this.#teardown();
        const generation = this.#generation;

        const options = this.options;
        const scrcpyOptions = new ScrcpyOptions3_3_3({});

        const socket = new WebSocket(options.wsURL);
        this.socket = socket;
        socket.binaryType = "arraybuffer";

        socket.addEventListener("close", () => {
            if (socket !== this.socket) return;
            this.#disconnected.fire();
        });

        const videoSocket = createWebSocketReadableStream(socket);
        const { metadata: videoMetadata, stream: videoStream } =
            await scrcpyOptions.parseVideoStreamMetadata(videoSocket);

        // A newer connect() superseded this attempt while it was awaiting; drop
        // it instead of overwriting the current connection's state.
        if (generation !== this.#generation) {
            socket.close();
            throw new Error("connection superseded");
        }

        console.log("Video Metadata:", JSON.stringify(videoMetadata));
        if (videoMetadata.width && videoMetadata.height) {
            this.#sizeChanged.fire({ width: videoMetadata.width, height: videoMetadata.height });
        }

        const codecName =
            ScrcpyVideoCodecNameMap.get(videoMetadata.codec) ?? `unknown(${videoMetadata.codec})`;

        if (WebCodecsVideoDecoder.isSupported) {
            const renderer = createVideoFrameRenderer(options.node);
            console.log(
                `[scrcpy] decoder: WebCodecs, codec: ${codecName}, renderer: ${renderer.constructor.name}`,
            );
            this.decoder = new WebCodecsVideoDecoder({
                codec: videoMetadata.codec,
                renderer,
            });
        } else {
            console.log(`[scrcpy] decoder: TinyH264 (software), codec: ${codecName}`);
            this.decoder = new TinyH264Decoder({
                canvas: options.node,
            });
        }
        this.decoder.sizeChanged(({ width, height }) => {
            this.#sizeChanged.fire({ width, height });
        });

        this.recorder = createRecorder(videoMetadata.codec);
        const recorder = this.recorder;

        const tap = new TransformStream<ScrcpyMediaStreamPacket, ScrcpyMediaStreamPacket>({
            async transform(chunk, controller) {
                await recorder.writePacket(chunk);
                controller.enqueue(chunk);
            },
        });

        videoStream
            .pipeThrough(scrcpyOptions.createMediaStreamTransformer())
            .pipeThrough(tap)
            .pipeTo(this.decoder.writable)
            .catch((e) => console.error("Video stream pipe error: ", e));

        const controlStream = createWebSocketWritableStream(socket);
        this.scrcpyWriter = new ScrcpyControlMessageWriter(controlStream.getWriter(), scrcpyOptions);

        return {
            disconnect: () => {
                // Only tear down shared state if this connection is still the
                // active one; otherwise just close its own socket so a stale
                // attempt cannot drop a newer connection.
                if (generation === this.#generation) {
                    this.#teardown();
                } else {
                    socket.close();
                }
            },
        };
    }

    isSnapshotSupported(): boolean {
        // should call after connect
        return this.decoder instanceof WebCodecsVideoDecoder;
    }

    async snapshot(): Promise<Blob | undefined> {
        if (this.decoder instanceof WebCodecsVideoDecoder) {
            return this.decoder.snapshot();
        }
        return undefined;
    }

    // A disconnect is a one-shot notification, not sticky state: the reconnect
    // controller subscribes right before each attempt, so replaying a previous
    // disconnect would immediately mark every new attempt as disconnected.
    #disconnected = new EventEmitter<void>();
    get disconnected() {
        return this.#disconnected.event;
    }

    #sizeChanged = new StickyEventEmitter<{ width: number; height: number }>();
    get sizeChanged() {
        return this.#sizeChanged.event;
    }

    get framesRendered(): number {
        return this.decoder?.framesRendered ?? 0;
    }

    disconnect(): void {
        this.#teardown();
    }

    /** Whether the control channel is open and can accept messages. */
    get connected(): boolean {
        return this.socket?.readyState === WebSocket.OPEN && !!this.scrcpyWriter;
    }

    #teardown(): void {
        this.#generation++;
        this.decoder?.dispose();
        this.decoder = undefined;
        const socket = this.socket;
        this.socket = undefined;
        socket?.close();
        this.scrcpyWriter = undefined;
        this.recorder = undefined;
    }

    startRecord(): void {
        this.recorder?.start();
        this.scrcpyWriter?.resetVideo();
    }

    async stopRecord(): Promise<void> {
        await this.recorder?.stop();
    }

    injectText(text: string): Promise<void> {
        if (!this.scrcpyWriter) {
            return Promise.reject(new Error("Not connected"));
        }
        return this.scrcpyWriter.injectText(text);
    }

    async injectKeyEvent(keyCode: AndroidKeyCode) {
        if (!this.scrcpyWriter) {
            throw new Error("Not connected");
        }
        await this.scrcpyWriter.injectKeyCode({
            action: AndroidKeyEventAction.Down,
            keyCode,
            repeat: 0,
            metaState: AndroidKeyEventMeta.None,
        });
        await this.scrcpyWriter.injectKeyCode({
            action: AndroidKeyEventAction.Up,
            keyCode,
            repeat: 0,
            metaState: AndroidKeyEventMeta.None,
        });
    }

    async injectTouch(message: TouchMessage): Promise<void> {
        const { x, y, action, pointerId = 0, pressure = 0.5 } = message;
        let motionEventAction: AndroidMotionEventAction;
        switch (action) {
            case "down":
                motionEventAction = AndroidMotionEventAction.Down;
                break;
            case "up":
                motionEventAction = AndroidMotionEventAction.Up;
                break;
            case "move":
                motionEventAction = AndroidMotionEventAction.Move;
                break;
            default:
                throw new Error(`Unknown touch action: ${action}`);
        }
        if (!this.scrcpyWriter) {
            return Promise.reject(new Error("Not connected"));
        }
        return this.scrcpyWriter.injectTouch({
            action: motionEventAction,
            pointerId: BigInt(pointerId),
            pointerX: x,
            pointerY: y,
            videoWidth: 1,
            videoHeight: 1,
            pressure,
            actionButton: PointerEventButtonToAndroidButton[0],
            buttons: PointerEventButtonToAndroidButton[0],
        });
    }

    async rotateDevice(): Promise<void> {
        if (!this.scrcpyWriter) {
            throw new Error("Not connected");
        }
        await this.scrcpyWriter.rotateDevice();
    }
}
