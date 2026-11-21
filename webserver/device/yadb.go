package device

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/uiautodev/uiautodev/webserver/utils"
)

// yadbVersion is the version of the embedded yadb binary.
const yadbVersion = "1.1.2"

//go:embed static/yadb-v1.1.2
var yadbBinary []byte

const yadbDir = "/data/local/tmp"

// yadbRemotePath embeds yadbVersion so that bumping the embedded version always
// results in the new binary being pushed.
const yadbRemotePath = yadbDir + "/yadb-" + yadbVersion

// ensureYadbPushed pushes the embedded yadb binary to the device when it is
// missing or its size differs from the embedded version.
func (d *AndroidDevice) ensureYadbPushed() error {
	if err := d.ensureFilePushed(yadbRemotePath, yadbBinary, 0755); err != nil {
		return errors.Wrap(err, "failed to push yadb binary")
	}
	return nil
}

// ensureFilePushed pushes data to remotePath when the device copy is missing
// or its size differs, which covers embedded helper binaries that ship with
// the app instead of being installed.
func (d *AndroidDevice) ensureFilePushed(remotePath string, data []byte, mode uint32) error {
	device := d.getAdbkitDevice()
	if stat, err := device.Stat(remotePath); err == nil && stat.Size == uint32(len(data)) {
		return nil
	}
	return device.Push(bytes.NewReader(data), remotePath, mode)
}

// runYadb runs yadb on the device via app_process and returns its output.
func (d *AndroidDevice) runYadb(args []string, timeoutMs int64) (string, error) {
	if err := d.ensureYadbPushed(); err != nil {
		return "", err
	}
	shellArgs := []string{
		"app_process",
		"-Djava.class.path=" + yadbRemotePath,
		yadbDir,
		"com.ysbing.yadb.Main",
	}
	shellArgs = append(shellArgs, args...)
	return d.RunShell(utils.ShellJoin(shellArgs), timeoutMs)
}

var yadbTempSeq atomic.Uint64

// yadbTempPath returns a unique per-invocation path under yadbDir so concurrent
// calls on the same device do not overwrite each other.
func yadbTempPath(name, ext string) string {
	return fmt.Sprintf("%s/yadb_%s_%d_%d.%s", yadbDir, name, time.Now().UnixNano(), yadbTempSeq.Add(1), ext)
}

// removeRemoteFile best-effort removes a temporary file created on the device.
func (d *AndroidDevice) removeRemoteFile(path string) {
	if _, err := d.RunShell("rm -f "+path, 5000); err != nil {
		slog.Warn("failed to remove yadb temp file", "path", path, "error", err)
	}
}

// screenshotByYadb captures the screen using yadb's forced screenshot.
func (d *AndroidDevice) screenshotByYadb() (image.Image, error) {
	path := yadbTempPath("screenshot", "png")
	defer d.removeRemoteFile(path)
	output, err := d.runYadb([]string{"-screenshot", path}, 15000)
	if err != nil {
		return nil, errors.Wrap(err, "failed to run yadb screenshot")
	}
	if !strings.Contains(output, "screenshot success") {
		return nil, fmt.Errorf("yadb screenshot failed: %s", output)
	}
	data, err := d.getAdbkitDevice().ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read yadb screenshot")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode yadb screenshot")
	}
	return img, nil
}

// getPageXMLByYadb dumps the UI hierarchy using yadb's efficient layout dump.
func (d *AndroidDevice) getPageXMLByYadb() ([]byte, error) {
	path := yadbTempPath("layout", "xml")
	defer d.removeRemoteFile(path)
	output, err := d.runYadb([]string{"-layout", path}, 15000)
	if err != nil {
		return nil, errors.Wrap(err, "failed to run yadb layout dump")
	}
	if !strings.Contains(output, "layout dumped to:") {
		return nil, fmt.Errorf("yadb layout dump failed: %s", output)
	}
	data, err := d.getAdbkitDevice().ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read yadb layout dump")
	}
	return data, nil
}

// inputTextByYadb inputs text by writing it to the device clipboard and
// pasting it, which supports non-ASCII text such as Chinese.
func (d *AndroidDevice) inputTextByYadb(text string) error {
	output, err := d.runYadb([]string{"-keyboard", text}, 5000)
	if err != nil {
		return errors.Wrap(err, "failed to run yadb keyboard")
	}
	if !strings.Contains(output, "Copy text:true") {
		return fmt.Errorf("yadb input text failed: %s", output)
	}
	return nil
}

// clearTextByYadb selects all text in the focused field and deletes it. yadb
// prints nothing on success, so only explicit errors are treated as failures.
func (d *AndroidDevice) clearTextByYadb() error {
	output, err := d.runYadb([]string{"-keyboardClear"}, 5000)
	if err != nil {
		return errors.Wrap(err, "failed to run yadb keyboardClear")
	}
	if strings.Contains(output, "MainException") || strings.Contains(output, "Invalid argument") {
		return fmt.Errorf("yadb keyboardClear failed: %s", output)
	}
	return nil
}
