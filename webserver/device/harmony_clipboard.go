package device

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/uiautodev/uiautodev/webserver/utils"
)

// harmonyPasteboardPrefix is the stdout prefix of `testhelper get-pastedata`.
const harmonyPasteboardPrefix = "Pasteboard text: "

// errHarmonyClipboardImage is returned for image writes: testhelper only
// exposes clipboard text ("Pasteboard text: ...").
var errHarmonyClipboardImage = fmt.Errorf("%w: testhelper only exposes clipboard text", ErrClipboardImageUnsupported)

// parseHarmonyPasteboard extracts the clipboard text from `testhelper
// get-pastedata` output, which prints "Pasteboard text: <text>" or an
// "Error: ..." line when the pasteboard service is unavailable.
func parseHarmonyPasteboard(output string) (string, error) {
	text := strings.TrimRight(output, "\r\n")
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "Error:") {
		return "", fmt.Errorf("harmony clipboard: %s", strings.TrimSpace(strings.TrimPrefix(trimmed, "Error:")))
	}
	return strings.TrimPrefix(text, harmonyPasteboardPrefix), nil
}

// ReadClipboard returns the device clipboard text using testhelper, which
// requires HarmonyOS 7.0.0 or newer. HarmonyOS has no shell-level image
// clipboard API, so an image clipboard reads back as empty text.
func (d *HarmonyOSDevice) ReadClipboard() (ClipboardContent, error) {
	output, err := d.RunShell("testhelper get-pastedata", clipboardTimeoutMs)
	if err != nil {
		return ClipboardContent{}, errors.Wrap(err, "failed to get pastedata: "+output)
	}
	text, err := parseHarmonyPasteboard(output)
	if err != nil {
		return ClipboardContent{}, err
	}
	return ClipboardContent{Kind: ClipboardKindText, Text: text}, nil
}

// WriteClipboard replaces the device clipboard text using testhelper.
func (d *HarmonyOSDevice) WriteClipboard(content ClipboardContent) error {
	if content.Kind == ClipboardKindImage {
		return errHarmonyClipboardImage
	}
	if content.Kind != ClipboardKindText {
		return fmt.Errorf("unsupported clipboard kind %q", content.Kind)
	}
	output, err := d.RunShell("testhelper set-pastedata "+utils.ShellJoin([]string{content.Text}), clipboardTimeoutMs)
	if err != nil {
		return errors.Wrap(err, "failed to set pastedata: "+output)
	}
	if strings.HasPrefix(strings.TrimSpace(output), "Error:") {
		return fmt.Errorf("harmony clipboard: %s", strings.TrimSpace(output))
	}
	return nil
}
