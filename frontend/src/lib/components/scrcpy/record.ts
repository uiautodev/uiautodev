// 参考代码：https://tangoadb.dev/scrcpy/video/record/#use-mediabunny
import {
    BufferTarget,
    EncodedPacket,
    EncodedVideoPacketSource,
    Mp4OutputFormat,
    Output,
    type VideoCodec,
} from "mediabunny";
import {
    Av1,
    h264ParseConfiguration,
    h265ParseConfiguration,
    ScrcpyVideoCodecId,
} from "@yume-chan/scrcpy";
import { getUint32LittleEndian } from "@yume-chan/no-data-view";
import type { ScrcpyMediaStreamPacket } from "@yume-chan/scrcpy";
import { saveFile } from "$lib/compat";

const VideoCodecMap: Record<number, VideoCodec> = {
    [ScrcpyVideoCodecId.H264]: "avc",
    [ScrcpyVideoCodecId.H265]: "hevc",
    [ScrcpyVideoCodecId.AV1]: "av1",
};

function hexTwoDigits(value: number) {
    return value.toString(16).toUpperCase().padStart(2, "0");
}

function hexDigits(value: number) {
    return value.toString(16).toUpperCase();
}

function decimalTwoDigits(value: number) {
    return value.toString(10).padStart(2, "0");
}

async function downloadBuffer(buffer: ArrayBuffer): Promise<void> {
    const now = new Date();
    const ts = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}${String(now.getDate()).padStart(2, "0")}_${String(now.getHours()).padStart(2, "0")}${String(now.getMinutes()).padStart(2, "0")}${String(now.getSeconds()).padStart(2, "0")}`;
    const filename = `uiautodev-record-${ts}.mp4`;
    await saveFile(buffer, filename);
}

export interface Recorder {
    readonly isRecording: boolean;
    start(): void;
    stop(): Promise<void>;
    writePacket(chunk: ScrcpyMediaStreamPacket): Promise<void>;
}

export function createRecorder(videoCodec: ScrcpyVideoCodecId): Recorder {
    let muxer: Output<Mp4OutputFormat, BufferTarget> | null = null;
    let videoTrack: EncodedVideoPacketSource | null = null;
    let videoCodecConfiguration: Uint8Array | undefined;
    let firstVideoTimestamp: bigint | undefined;
    let recording = false;
    let hasDecoderConfig = false;

    function initMuxer() {
        muxer = new Output({
            format: new Mp4OutputFormat(),
            target: new BufferTarget(),
        });
        videoTrack = new EncodedVideoPacketSource(VideoCodecMap[videoCodec]!);
        muxer.addVideoTrack(videoTrack);
        muxer.start();
        firstVideoTimestamp = undefined;
        videoCodecConfiguration = undefined;
        hasDecoderConfig = false;
    }

    return {
        get isRecording() {
            return recording;
        },
        start() {
            if (recording) {
                // Already recording; avoid reinitializing the muxer and leaking the previous session.
                return;
            }
            initMuxer();
            recording = true;
        },
        async stop() {
            recording = false;
            if (muxer) {
                await muxer.finalize();
                const buffer = muxer.target.buffer;
                if (buffer) {
                    await downloadBuffer(buffer);
                }
                muxer = null;
                videoTrack = null;
            }
        },
        async writePacket(chunk: ScrcpyMediaStreamPacket): Promise<void> {
            if (!recording || !videoTrack) return;

            if (chunk.type === "configuration") {
                videoCodecConfiguration = chunk.data;
                return;
            }

            if (firstVideoTimestamp === undefined) {
                if (chunk.keyframe) {
                    firstVideoTimestamp = chunk.pts!;
                } else {
                    // warn: first frame should be keyframe
                    return;
                }
            }

            // chunk.pts is in microseconds (us),
            // mediabunny requires timestamps in seconds
            const timestamp = Number((chunk.pts! - firstVideoTimestamp) / 1000n) / 1000;

            // AV1 doesn't need the configuration packet
            if (videoCodec === ScrcpyVideoCodecId.AV1) {
                if (!hasDecoderConfig && chunk.keyframe) {
                    const parser = new Av1(chunk.data);
                    const sequenceHeader = parser.searchSequenceHeaderObu();

                    if (sequenceHeader) {
                        const {
                            seq_profile: seqProfile,
                            seq_level_idx: [seqLevelIdx = 0],
                            max_frame_width_minus_1,
                            max_frame_height_minus_1,
                            color_config: {
                                BitDepth,
                                mono_chrome: monoChrome,
                                subsampling_x: subsamplingX,
                                subsampling_y: subsamplingY,
                                chroma_sample_position: chromaSamplePosition,
                                color_description_present_flag,
                            },
                        } = sequenceHeader;

                        let colorPrimaries: Av1.ColorPrimaries;
                        let transferCharacteristics: Av1.TransferCharacteristics;
                        let matrixCoefficients: Av1.MatrixCoefficients;
                        let colorRange: boolean;
                        if (color_description_present_flag) {
                            ({
                                color_primaries: colorPrimaries,
                                transfer_characteristics: transferCharacteristics,
                                matrix_coefficients: matrixCoefficients,
                                color_range: colorRange,
                            } = sequenceHeader.color_config);
                        } else {
                            colorPrimaries = Av1.ColorPrimaries.Bt709;
                            transferCharacteristics = Av1.TransferCharacteristics.Bt709;
                            matrixCoefficients = Av1.MatrixCoefficients.Bt709;
                            colorRange = false;
                        }

                        const codedWidth = max_frame_width_minus_1 + 1;
                        const codedHeight = max_frame_height_minus_1 + 1;
                        const codecString = [
                            "av01",
                            seqProfile.toString(16),
                            decimalTwoDigits(seqLevelIdx) + (sequenceHeader.seq_tier[0] ? "H" : "M"),
                            decimalTwoDigits(BitDepth),
                            monoChrome ? "1" : "0",
                            (subsamplingX ? "1" : "0") +
                                (subsamplingY ? "1" : "0") +
                                chromaSamplePosition.toString(),
                            decimalTwoDigits(colorPrimaries),
                            decimalTwoDigits(transferCharacteristics),
                            decimalTwoDigits(matrixCoefficients),
                            colorRange ? "1" : "0",
                        ].join(".");

                        await videoTrack.add(
                            new EncodedPacket(chunk.data, chunk.keyframe ? "key" : "delta", timestamp, 0),
                            {
                                decoderConfig: {
                                    codec: codecString,
                                    codedWidth,
                                    codedHeight,
                                },
                            },
                        );
                        hasDecoderConfig = true;
                        return;
                    }
                }

                await videoTrack.add(
                    new EncodedPacket(chunk.data, chunk.keyframe ? "key" : "delta", timestamp, 0),
                );
                return;
            }

            // For H.264/H.265, concat the configuration packet with the next frame packet
            if (videoCodecConfiguration) {
                const buffer = new Uint8Array(videoCodecConfiguration.length + chunk.data.length);
                buffer.set(videoCodecConfiguration);
                buffer.set(chunk.data, videoCodecConfiguration.length);

                let codedWidth: number;
                let codedHeight: number;
                let codecString: string;
                if (videoCodec === ScrcpyVideoCodecId.H264) {
                    const configuration = h264ParseConfiguration(videoCodecConfiguration);
                    codedWidth = configuration.encodedWidth;
                    codedHeight = configuration.encodedHeight;
                    codecString =
                        "avc1." +
                        hexTwoDigits(configuration.profileIndex) +
                        hexTwoDigits(configuration.constraintSet) +
                        hexTwoDigits(configuration.levelIndex);
                } else {
                    const configuration = h265ParseConfiguration(videoCodecConfiguration);
                    codedWidth = configuration.encodedWidth;
                    codedHeight = configuration.encodedHeight;
                    codecString = [
                        "hev1",
                        ["", "A", "B", "C"][configuration.generalProfileSpace]! +
                            configuration.generalProfileIndex.toString(),
                        hexDigits(getUint32LittleEndian(configuration.generalProfileCompatibilitySet, 0)),
                        (configuration.generalTierFlag ? "H" : "L") +
                            configuration.generalLevelIndex.toString(),
                        ...Array.from(configuration.generalConstraintSet, hexDigits),
                    ].join(".");
                }

                await videoTrack.add(
                    new EncodedPacket(buffer, chunk.keyframe ? "key" : "delta", timestamp, 0),
                    {
                        decoderConfig: {
                            codec: codecString,
                            codedWidth,
                            codedHeight,
                        },
                    },
                );

                videoCodecConfiguration = undefined;
                hasDecoderConfig = true;
                return;
            }

            await videoTrack.add(
                new EncodedPacket(chunk.data, chunk.keyframe ? "key" : "delta", timestamp, 0),
            );
        },
    };
}
