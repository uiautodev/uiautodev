package device

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tinyPNGBase64 is a 1x1 transparent PNG used by the clipboard tests.
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestParseYadbClipboard(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "text with trailing newline", output: "hello world\n", want: "hello world"},
		{name: "crlf newline", output: "hello\r\n", want: "hello"},
		{name: "multiline keeps inner newlines", output: "line1\nline2\n", want: "line1\nline2"},
		{name: "null means empty", output: "null\n", want: ""},
		{name: "empty output", output: "", want: ""},
		{name: "keeps surrounding spaces", output: "  hi  \n", want: "  hi  "},
		{name: "unicode and emoji", output: "中文 🚀\n", want: "中文 🚀"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseYadbClipboard(tt.output))
		})
	}
}

func TestYadbError(t *testing.T) {
	assert.NoError(t, yadbError("hello\n"))
	assert.NoError(t, yadbError("null\n"))
	assert.Error(t, yadbError("MainException: java.lang.NullPointerException\n"))
	assert.Error(t, yadbError("Invalid argument\n"))
}

func TestParseClipboardHelperOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   helperClip
	}{
		{
			name:   "empty clipboard",
			output: "empty\n",
			want:   helperClip{Kind: "empty"},
		},
		{
			name:   "image uri with mime",
			output: "uri:content://media/external/images/media/42\nmime:image/jpeg\n",
			want:   helperClip{Kind: "uri", URI: "content://media/external/images/media/42", Mime: "image/jpeg"},
		},
		{
			name:   "image uri without mime falls back to png",
			output: "uri:content://example/image\n",
			want:   helperClip{Kind: "uri", URI: "content://example/image", Mime: "image/png"},
		},
		{
			name:   "crlf from the device shell",
			output: "uri:content://example/image\r\nmime:image/png\r\n",
			want:   helperClip{Kind: "uri", URI: "content://example/image", Mime: "image/png"},
		},
		{
			name:   "text",
			output: "text:hello\n",
			want:   helperClip{Kind: "text", Text: "hello"},
		},
		{
			name:   "text with newlines",
			output: "text:line1\nline2\n",
			want:   helperClip{Kind: "text", Text: "line1\nline2"},
		},
		{
			name:   "unexpected output",
			output: "MainException: boom\n",
			want:   helperClip{Kind: "other"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseClipboardHelperOutput(tt.output))
		})
	}
}

func TestParseMediaStoreID(t *testing.T) {
	id, ok := parseMediaStoreID("Row: 0 _id=1000000024, _display_name=clipboard.png\n")
	require.True(t, ok)
	assert.Equal(t, int64(1000000024), id)

	id, ok = parseMediaStoreID("Row: 0 _id=7\nRow: 1 _id=3\n")
	require.True(t, ok)
	assert.Equal(t, int64(7), id)

	_, ok = parseMediaStoreID("No result found.")
	assert.False(t, ok)

	_, ok = parseMediaStoreID("Row: 0 _display_name=clipboard.png")
	assert.False(t, ok)
}

func TestClipboardImageExt(t *testing.T) {
	assert.Equal(t, "png", clipboardImageExt("image/png"))
	assert.Equal(t, "jpg", clipboardImageExt("image/jpeg"))
	assert.Equal(t, "jpg", clipboardImageExt("image/jpg"))
	assert.Equal(t, "webp", clipboardImageExt("image/webp"))
	assert.Equal(t, "gif", clipboardImageExt("image/gif"))
	assert.Equal(t, "bmp", clipboardImageExt("image/bmp"))
	assert.Equal(t, "png", clipboardImageExt("image/avif"))
	assert.Equal(t, "png", clipboardImageExt("IMAGE/PNG; charset=binary"))
}

func TestNormalizeClipboardContent(t *testing.T) {
	assert.Equal(t, ClipboardKindText, NormalizeClipboardContent(ClipboardContent{Text: "hi"}).Kind)
	assert.Equal(t, ClipboardKindImage, NormalizeClipboardContent(ClipboardContent{
		Image: &ClipboardImage{Mime: "image/png", Data: tinyPNGBase64},
	}).Kind)
	assert.Equal(t, ClipboardKindImage, NormalizeClipboardContent(ClipboardContent{
		Kind:  ClipboardKindImage,
		Image: &ClipboardImage{Mime: "image/png", Data: tinyPNGBase64},
	}).Kind)
}

func TestClipboardContentValidate(t *testing.T) {
	tests := []struct {
		name    string
		content ClipboardContent
		wantErr string
	}{
		{
			name:    "text",
			content: ClipboardContent{Kind: ClipboardKindText, Text: "hello 中文"},
		},
		{
			name:    "empty text",
			content: ClipboardContent{Kind: ClipboardKindText},
		},
		{
			name:    "text must not carry an image",
			content: ClipboardContent{Kind: ClipboardKindText, Image: &ClipboardImage{Mime: "image/png", Data: tinyPNGBase64}},
			wantErr: "must not carry an image",
		},
		{
			name:    "image",
			content: ClipboardContent{Kind: ClipboardKindImage, Image: &ClipboardImage{Mime: "image/png", Data: tinyPNGBase64}},
		},
		{
			name:    "image is missing",
			content: ClipboardContent{Kind: ClipboardKindImage},
			wantErr: "missing the image field",
		},
		{
			name:    "image mime must be an image",
			content: ClipboardContent{Kind: ClipboardKindImage, Image: &ClipboardImage{Mime: "text/plain", Data: tinyPNGBase64}},
			wantErr: "invalid image mime type",
		},
		{
			name:    "image data must be base64",
			content: ClipboardContent{Kind: ClipboardKindImage, Image: &ClipboardImage{Mime: "image/png", Data: "not base64!"}},
			wantErr: "not valid base64",
		},
		{
			name:    "image data must not be empty",
			content: ClipboardContent{Kind: ClipboardKindImage, Image: &ClipboardImage{Mime: "image/png", Data: ""}},
			wantErr: "image data is empty",
		},
		{
			name:    "image must stay below the size limit",
			content: ClipboardContent{Kind: ClipboardKindImage, Image: &ClipboardImage{Mime: "image/png", Data: strings.Repeat("A", (MaxClipboardImageBytes/3+1)*4)}},
			wantErr: "larger than",
		},
		{
			name:    "unknown kind",
			content: ClipboardContent{Kind: "video"},
			wantErr: "invalid clipboard kind",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.content.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestHarmonyClipboardRejectsImages(t *testing.T) {
	d := &HarmonyOSDevice{}
	err := d.WriteClipboard(ClipboardContent{
		Kind:  ClipboardKindImage,
		Image: &ClipboardImage{Mime: "image/png", Data: tinyPNGBase64},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClipboardImageUnsupported)
}

// TestClipboardIntegration exercises the real device clipboard. It is skipped
// unless UIAUTODEV_TEST_DEVICE is set, e.g.
//
//	UIAUTODEV_TEST_DEVICE=a-emulator-5554 go test ./webserver/device -run TestClipboardIntegration -v
func TestClipboardIntegration(t *testing.T) {
	deviceID := os.Getenv("UIAUTODEV_TEST_DEVICE")
	if deviceID == "" {
		t.Skip("set UIAUTODEV_TEST_DEVICE to run the integration test")
	}
	dev, err := NewDevice(deviceID)
	if err != nil {
		t.Fatalf("new device: %v", err)
	}
	clipboard, ok := dev.Device.(ClipboardDevice)
	if !ok {
		t.Fatalf("%s does not support the clipboard", deviceID)
	}

	const text = "ghik clipboard 中文 🚀"
	if err := clipboard.WriteClipboard(ClipboardContent{Kind: ClipboardKindText, Text: text}); err != nil {
		t.Fatalf("write clipboard text: %v", err)
	}
	content, err := clipboard.ReadClipboard()
	if err != nil {
		t.Fatalf("read clipboard text: %v", err)
	}
	if content.Kind != ClipboardKindText || content.Text != text {
		t.Fatalf("text clipboard round-trip mismatch: got %+v, want %q", content, text)
	}

	image := ClipboardContent{
		Kind:  ClipboardKindImage,
		Image: &ClipboardImage{Mime: "image/png", Data: tinyPNGBase64},
	}
	if err := clipboard.WriteClipboard(image); err != nil {
		t.Skipf("image clipboard is not supported on %s: %v", deviceID, err)
	}
	content, err = clipboard.ReadClipboard()
	if err != nil {
		t.Fatalf("read clipboard image: %v", err)
	}
	if content.Kind != ClipboardKindImage {
		t.Fatalf("clipboard image round-trip mismatch: got kind %q, want image", content.Kind)
	}
	if content.Image == nil || content.Image.Mime != "image/png" || content.Image.Data != tinyPNGBase64 {
		t.Fatalf("clipboard image round-trip mismatch: got %+v", content.Image)
	}
}
