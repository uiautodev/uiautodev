package device

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/uiautodev/uiautodev/webserver/utils"
)

// clipboardHelperVersion is the version of the embedded clipboard helper.
// The Android clipboard only accepts text through yadb, so images go through
// this small app_process helper that sets and reads ClipData items by URI.
const clipboardHelperVersion = "1"

//go:embed static/clipboard-helper.jar
var clipboardHelperJar []byte

// clipboardHelperRemotePath embeds the version so bumping the helper always
// results in a new file being pushed.
var clipboardHelperRemotePath = yadbDir + "/uiautodev-clipboard-" + clipboardHelperVersion + ".jar"

// clipboardMediaURI is the MediaStore collection used to host clipboard
// images. Inserting there gives the pasting app a content URI plus the
// clipboard service's URI permission grant.
const (
	clipboardMediaURI      = "content://media/external/images/media"
	clipboardImageDir      = "Pictures/Uiautodev"
	clipboardImageNameBase = "uiautodev-clip-"
)

// ensureClipboardHelperPushed pushes the embedded helper when it is missing or
// its size differs from the embedded version.
func (d *AndroidDevice) ensureClipboardHelperPushed() error {
	if err := d.ensureFilePushed(clipboardHelperRemotePath, clipboardHelperJar, 0644); err != nil {
		return errors.Wrap(err, "failed to push clipboard helper")
	}
	return nil
}

// runClipboardHelper runs the embedded helper on the device via app_process.
func (d *AndroidDevice) runClipboardHelper(args []string, timeoutMs int64) (string, error) {
	if err := d.ensureClipboardHelperPushed(); err != nil {
		return "", err
	}
	shellArgs := []string{
		"app_process",
		"-Djava.class.path=" + clipboardHelperRemotePath,
		yadbDir,
		"com.uiautodev.clipboard.Main",
	}
	shellArgs = append(shellArgs, args...)
	return d.RunShell(utils.ShellJoin(shellArgs), timeoutMs)
}

// helperClip is the parsed result of `-getClip`, which reports what the
// clipboard holds when yadb sees no text: nothing, a content URI or (rarely)
// text that yadb could not read.
type helperClip struct {
	Kind string // "empty", "uri", "text" or "other"
	URI  string
	Mime string
	Text string
}

// parseClipboardHelperOutput parses the helper output lines. The helper prints
// "empty", "uri:<uri>\nmime:<mime>" or "text:<text>".
func parseClipboardHelperOutput(output string) helperClip {
	trimmed := strings.TrimRight(output, "\r\n")
	switch {
	case trimmed == "empty":
		return helperClip{Kind: "empty"}
	case strings.HasPrefix(trimmed, "uri:"):
		clip := helperClip{Kind: "uri"}
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case strings.HasPrefix(line, "uri:"):
				clip.URI = strings.TrimSpace(strings.TrimPrefix(line, "uri:"))
			case strings.HasPrefix(line, "mime:"):
				clip.Mime = strings.TrimSpace(strings.TrimPrefix(line, "mime:"))
			}
		}
		if clip.Mime == "" {
			clip.Mime = "image/png"
		}
		return clip
	case strings.HasPrefix(trimmed, "text:"):
		return helperClip{Kind: "text", Text: strings.TrimPrefix(trimmed, "text:")}
	default:
		return helperClip{Kind: "other"}
	}
}

// androidSDK returns the device API level, which decides how clipboard images
// can be stored in MediaStore.
func (d *AndroidDevice) androidSDK() (int, error) {
	output, err := d.RunShell("getprop ro.build.version.sdk", 5000)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read the Android SDK version")
	}
	sdk, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("invalid Android SDK version %q", strings.TrimSpace(output))
	}
	return sdk, nil
}

// clipboardImageExt maps an image mime type to the extension MediaStore uses
// for the display name.
func clipboardImageExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0])) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "image/bmp":
		return "bmp"
	default:
		return "png"
	}
}

// parseMediaStoreID extracts the row id from `content query` output such as
// "Row: 0 _id=1000000024, _display_name=clipboard.png".
func parseMediaStoreID(output string) (int64, bool) {
	index := strings.Index(output, "_id=")
	if index < 0 {
		return 0, false
	}
	rest := output[index+len("_id="):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	id, err := strconv.ParseInt(rest[:end], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// yadb prints the clipboard text with a trailing newline and the literal
// "null" when the clipboard is empty or unreadable. Android only allows
// package com.android.shell (the package yadb impersonates) to read the
// clipboard from Android 10 onwards.
func parseYadbClipboard(output string) string {
	text := strings.TrimRight(output, "\r\n")
	if text == "null" {
		return ""
	}
	return text
}

// yadbError reports whether yadb failed without a non-zero exit code. yadb
// prints Java exceptions to stdout, so an unsuccessful run still looks like a
// successful shell command.
func yadbError(output string) error {
	if strings.Contains(output, "MainException") || strings.Contains(output, "Invalid argument") {
		return fmt.Errorf("yadb command failed: %s", strings.TrimSpace(output))
	}
	return nil
}

// readTextClipboard returns the clipboard text. yadb reports only text.
func (d *AndroidDevice) readTextClipboard() (string, error) {
	output, err := d.runYadb([]string{"-readClipboard"}, clipboardTimeoutMs)
	if err != nil {
		return "", errors.Wrap(err, "failed to run yadb readClipboard")
	}
	if err := yadbError(output); err != nil {
		return "", err
	}
	return parseYadbClipboard(output), nil
}

// readImageClipboard asks the helper what the clipboard holds and pulls the
// image when it is a content URI. It reports false when there is no image.
func (d *AndroidDevice) readImageClipboard() (ClipboardContent, bool, error) {
	output, err := d.runClipboardHelper([]string{"-getClip"}, clipboardTimeoutMs)
	if err != nil {
		return ClipboardContent{}, false, errors.Wrap(err, "failed to read the clipboard")
	}
	if err := yadbError(output); err != nil {
		return ClipboardContent{}, false, err
	}
	clip := parseClipboardHelperOutput(output)
	if clip.Kind != "uri" {
		return ClipboardContent{}, false, nil
	}
	if clip.URI == "" {
		return ClipboardContent{}, false, errors.New("clipboard image has no uri")
	}

	path := yadbTempPath("clipboard", "img")
	defer d.removeRemoteFile(path)
	command := utils.ShellJoin([]string{"content", "read", "--uri", clip.URI}) + " > " + path
	if output, err := d.RunShell(command, clipboardTimeoutMs); err != nil {
		return ClipboardContent{}, false, errors.Wrap(err, "failed to read the clipboard image: "+output)
	}
	data, err := d.getAdbkitDevice().ReadFile(path)
	if err != nil {
		return ClipboardContent{}, false, errors.Wrap(err, "failed to pull the clipboard image")
	}
	if len(data) == 0 {
		return ClipboardContent{}, false, errors.New("the clipboard image is empty")
	}
	return ClipboardContent{
		Kind: ClipboardKindImage,
		Image: &ClipboardImage{
			Mime: clip.Mime,
			Data: base64.StdEncoding.EncodeToString(data),
		},
	}, true, nil
}

// ReadClipboard returns the device clipboard. yadb is tried first because it
// reads text without extra helpers; when it sees nothing the helper checks for
// an image.
func (d *AndroidDevice) ReadClipboard() (ClipboardContent, error) {
	text, err := d.readTextClipboard()
	if err != nil {
		return ClipboardContent{}, err
	}
	if text != "" {
		return ClipboardContent{Kind: ClipboardKindText, Text: text}, nil
	}
	image, ok, err := d.readImageClipboard()
	if err != nil {
		return ClipboardContent{}, err
	}
	if ok {
		return image, nil
	}
	return ClipboardContent{Kind: ClipboardKindText}, nil
}

// WriteClipboard replaces the device clipboard with text or an image.
func (d *AndroidDevice) WriteClipboard(content ClipboardContent) error {
	switch content.Kind {
	case ClipboardKindText:
		return d.writeTextClipboard(content.Text)
	case ClipboardKindImage:
		if content.Image == nil {
			return errors.New("image clipboard content is missing the image field")
		}
		return d.writeImageClipboard(content.Image)
	default:
		return fmt.Errorf("unsupported clipboard kind %q", content.Kind)
	}
}

// writeTextClipboard replaces the clipboard text. yadb prints nothing on
// success, so only an explicit exception is a failure.
func (d *AndroidDevice) writeTextClipboard(text string) error {
	output, err := d.runYadb([]string{"-writeClipboard", text}, clipboardTimeoutMs)
	if err != nil {
		return errors.Wrap(err, "failed to run yadb writeClipboard")
	}
	return yadbError(output)
}

// writeImageClipboard stores the image in MediaStore and points the clipboard
// at the resulting content URI. Pasting apps then receive the URI permission
// grant from the clipboard service instead of needing storage access.
func (d *AndroidDevice) writeImageClipboard(image *ClipboardImage) error {
	sdk, err := d.androidSDK()
	if err != nil {
		return err
	}
	if sdk < 29 {
		return fmt.Errorf("%w: Android 10 or newer is required", ErrClipboardImageUnsupported)
	}
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		return errors.New("image data is not valid base64")
	}

	path := yadbTempPath("clipboard", "img")
	defer d.removeRemoteFile(path)
	if err := d.getAdbkitDevice().Push(bytes.NewReader(data), path, 0644); err != nil {
		return errors.Wrap(err, "failed to push the clipboard image")
	}

	// Remove images from earlier writes so the gallery does not grow with
	// every paste; the current clipboard entry is re-created below.
	d.RunShell(utils.ShellJoin([]string{
		"content", "delete", "--uri", clipboardMediaURI,
		"--where", "_display_name LIKE '" + clipboardImageNameBase + "%'",
	}), 5000)

	name := clipboardImageNameBase + strconv.FormatInt(time.Now().UnixNano(), 10) + "." + clipboardImageExt(image.Mime)
	insert := utils.ShellJoin([]string{
		"content", "insert", "--uri", clipboardMediaURI,
		"--bind", "_display_name:s:" + name,
		"--bind", "mime_type:s:" + image.Mime,
		"--bind", "relative_path:s:" + clipboardImageDir,
	})
	if output, err := d.RunShell(insert, clipboardTimeoutMs); err != nil {
		return errors.Wrap(err, "failed to create the clipboard image: "+output)
	}

	query := utils.ShellJoin([]string{
		"content", "query", "--uri", clipboardMediaURI,
		"--projection", "_id",
		"--where", "_display_name='" + name + "'",
		"--sort", "_id DESC",
	})
	output, err := d.RunShell(query, clipboardTimeoutMs)
	if err != nil {
		return errors.Wrap(err, "failed to look up the clipboard image: "+output)
	}
	id, ok := parseMediaStoreID(output)
	if !ok {
		return fmt.Errorf("failed to look up the clipboard image: %s", strings.TrimSpace(output))
	}
	uri := fmt.Sprintf("%s/%d", clipboardMediaURI, id)

	write := utils.ShellJoin([]string{"content", "write", "--uri", uri}) + " < " + path
	if output, err := d.RunShell(write, clipboardTimeoutMs); err != nil {
		return errors.Wrap(err, "failed to write the clipboard image: "+output)
	}

	output, err = d.runClipboardHelper([]string{"-setClipUri", uri, image.Mime}, clipboardTimeoutMs)
	if err != nil {
		return errors.Wrap(err, "failed to set the clipboard image")
	}
	if !strings.Contains(output, "setClipUri success") {
		return fmt.Errorf("failed to set the clipboard image: %s", strings.TrimSpace(output))
	}
	return nil
}
