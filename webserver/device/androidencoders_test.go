package device

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEncoderList(t *testing.T) {
	output := `[server] INFO: Device: [deltainno] SMARTISAN DT1901A (Android 10)
[server] INFO: List of video encoders:
    --video-codec=h264 --video-encoder=OMX.qcom.video.encoder.avc     (hw) [vendor]
    --video-codec=h264 --video-encoder=c2.android.avc.encoder         (sw)
    --video-codec=h264 --video-encoder=OMX.google.h264.encoder        (sw) (alias for c2.android.avc.encoder)
    --video-codec=h265 --video-encoder=c2.android.hevc.encoder        (sw)
[server] INFO: List of audio encoders:
    --audio-codec=opus --audio-encoder=c2.android.opus.encoder        (sw)
    --audio-codec=aac --audio-encoder=OMX.google.aac.encoder          (hybrid)
`

	list := parseEncoderList(output)

	require.Len(t, list.Video, 4)
	require.Equal(t, EncoderInfo{
		Name:   "OMX.qcom.video.encoder.avc",
		Codec:  "h264",
		Type:   "hw",
		Vendor: true,
	}, list.Video[0])
	require.Equal(t, EncoderInfo{
		Name:  "c2.android.avc.encoder",
		Codec: "h264",
		Type:  "sw",
	}, list.Video[1])
	require.Equal(t, EncoderInfo{
		Name:  "OMX.google.h264.encoder",
		Codec: "h264",
		Type:  "sw",
	}, list.Video[2])

	require.Len(t, list.Audio, 2)
	require.Equal(t, "opus", list.Audio[0].Codec)
	require.Equal(t, "hybrid", list.Audio[1].Type)
}
