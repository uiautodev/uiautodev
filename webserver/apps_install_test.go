package webserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
)

// fakeAppController is an AppController that records install calls without a
// device.
type fakeAppController struct {
	extension  string
	installErr error

	installedPath    string
	installedContent []byte
}

func (f *fakeAppController) ListApps(device.AppScope) ([]string, error) { return nil, nil }
func (f *fakeAppController) StartApp(string, string) (device.CurrentApp, error) {
	return device.CurrentApp{}, nil
}
func (f *fakeAppController) StopApp(string) error { return nil }
func (f *fakeAppController) CurrentApp() (device.CurrentApp, error) {
	return device.CurrentApp{}, nil
}
func (f *fakeAppController) PackageExtension() string { return f.extension }
func (f *fakeAppController) InstallApp(localPath string) error {
	f.installedPath = localPath
	f.installedContent, _ = os.ReadFile(localPath)
	return f.installErr
}

func useFakeAppController(t *testing.T, controller *fakeAppController, lookupErr error) {
	t.Helper()
	original := appControllerLookup
	t.Cleanup(func() { appControllerLookup = original })
	appControllerLookup = func(string) (*device.DeviceWrapper, device.AppController, error) {
		if lookupErr != nil {
			return nil, nil, lookupErr
		}
		return nil, controller, nil
	}
}

func useAppPackageTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := appPackageTempDir
	t.Cleanup(func() { appPackageTempDir = original })
	appPackageTempDir = dir
	return dir
}

func newInstallUpload(t *testing.T, fileName string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write package: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func runInstallHandler(t *testing.T, body io.Reader, contentType string) (*httptest.ResponseRecorder, APIResponse) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/test/apps", body)
	req.Header.Set(echo.HeaderContentType, contentType)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("device_id")
	c.SetParamValues("test")
	if err := appInstallHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var response APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec, response
}

func TestAppInstallHandlerInstallsUploadedPackage(t *testing.T) {
	dir := useAppPackageTempDir(t)
	controller := &fakeAppController{extension: device.AndroidPackageExtension}
	useFakeAppController(t, controller, nil)

	body, contentType := newInstallUpload(t, "game.apk", []byte("apk-bytes"))
	rec, response := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if response.Code != 0 || response.Message != "ok" {
		t.Fatalf("unexpected response %+v", response)
	}
	if string(controller.installedContent) != "apk-bytes" {
		t.Fatalf("installer got %q", controller.installedContent)
	}
	if !strings.HasSuffix(controller.installedPath, device.AndroidPackageExtension) {
		t.Fatalf("temporary path %q lost the package extension", controller.installedPath)
	}
	if _, err := os.Stat(controller.installedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary package was not cleaned up: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("temporary directory is not empty: %v %v", entries, err)
	}
}

func TestAppInstallHandlerRejectsWrongExtension(t *testing.T) {
	useAppPackageTempDir(t)
	controller := &fakeAppController{extension: device.IOSPackageExtension}
	useFakeAppController(t, controller, nil)

	body, contentType := newInstallUpload(t, "game.apk", []byte("apk-bytes"))
	rec, response := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(response.Message, device.IOSPackageExtension) {
		t.Fatalf("message should name the expected extension: %q", response.Message)
	}
	if controller.installedPath != "" {
		t.Fatalf("installer should not run, got %q", controller.installedPath)
	}
}

func TestAppInstallHandlerRejectsTooLargePackage(t *testing.T) {
	dir := useAppPackageTempDir(t)
	originalLimit := maxAppPackageBytes
	t.Cleanup(func() { maxAppPackageBytes = originalLimit })
	maxAppPackageBytes = 4

	controller := &fakeAppController{extension: device.HarmonyPackageExtension}
	useFakeAppController(t, controller, nil)

	body, contentType := newInstallUpload(t, "game.hap", []byte("larger than four bytes"))
	rec, response := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if response.Message != errAppPackageTooLarge.Error() {
		t.Fatalf("message %q", response.Message)
	}
	if controller.installedPath != "" {
		t.Fatalf("installer should not run, got %q", controller.installedPath)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("temporary directory is not empty: %v %v", entries, err)
	}
}

func TestAppInstallHandlerReportsDeviceFailure(t *testing.T) {
	useAppPackageTempDir(t)
	controller := &fakeAppController{
		extension:  device.AndroidPackageExtension,
		installErr: errors.New("INSTALL_FAILED_INVALID_APK"),
	}
	useFakeAppController(t, controller, nil)

	body, contentType := newInstallUpload(t, "game.apk", []byte("apk-bytes"))
	rec, response := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(response.Message, "INSTALL_FAILED_INVALID_APK") {
		t.Fatalf("message %q", response.Message)
	}
	if _, err := os.Stat(controller.installedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary package was not cleaned up: %v", err)
	}
}

func TestAppInstallHandlerUnsupportedDevice(t *testing.T) {
	controller := &fakeAppController{extension: device.AndroidPackageExtension}
	useFakeAppController(t, controller, device.ErrAppManagementUnsupported)

	body, contentType := newInstallUpload(t, "game.apk", []byte("apk-bytes"))
	rec, response := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if response.Message != device.ErrAppManagementUnsupported.Error() {
		t.Fatalf("message %q", response.Message)
	}
}

// trackingReader records whether the handler touched the request body at all.
type trackingReader struct{ reads int }

func (r *trackingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

func TestAppInstallHandlerRejectsOversizedContentLengthBeforeReading(t *testing.T) {
	dir := useAppPackageTempDir(t)
	originalLimit := maxAppPackageBytes
	originalSlack := appPackageRequestSlack
	t.Cleanup(func() {
		maxAppPackageBytes = originalLimit
		appPackageRequestSlack = originalSlack
	})
	maxAppPackageBytes = 4
	appPackageRequestSlack = 0

	controller := &fakeAppController{extension: device.AndroidPackageExtension}
	useFakeAppController(t, controller, nil)

	body := &trackingReader{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/test/apps", body)
	req.ContentLength = 5
	req.Header.Set(echo.HeaderContentType, "multipart/form-data; boundary=test")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("device_id")
	c.SetParamValues("test")
	if err := appInstallHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if body.reads != 0 {
		t.Fatalf("handler read the oversized body %d times", body.reads)
	}
	if controller.installedPath != "" {
		t.Fatalf("installer should not run, got %q", controller.installedPath)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("temporary directory is not empty: %v %v", entries, err)
	}
}

func TestAppInstallHandlerRejectsSecondInstallOnSameDevice(t *testing.T) {
	useAppPackageTempDir(t)
	controller := &fakeAppController{extension: device.AndroidPackageExtension}
	useFakeAppController(t, controller, nil)

	release, err := appInstalls.acquire("test")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	body, contentType := newInstallUpload(t, "game.apk", []byte("apk-bytes"))
	rec, response := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if response.Message != errAppInstallBusy.Error() {
		t.Fatalf("message %q", response.Message)
	}
	if controller.installedPath != "" {
		t.Fatalf("installer should not run, got %q", controller.installedPath)
	}
}

func TestAppInstallHandlerRejectsWhenInstallSlotsAreExhausted(t *testing.T) {
	useAppPackageTempDir(t)
	originalLimit := maxAppPackageInstalls
	t.Cleanup(func() { maxAppPackageInstalls = originalLimit })
	maxAppPackageInstalls = 0

	controller := &fakeAppController{extension: device.AndroidPackageExtension}
	useFakeAppController(t, controller, nil)

	body, contentType := newInstallUpload(t, "game.apk", []byte("apk-bytes"))
	rec, _ := runInstallHandler(t, body, contentType)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if controller.installedPath != "" {
		t.Fatalf("installer should not run, got %q", controller.installedPath)
	}
}

func TestAppInstallGateAllowsOneInstallPerDeviceAndReusesSlots(t *testing.T) {
	gate := newAppInstallGate()
	releaseFirst, err := gate.acquire("test")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := gate.acquire("test"); !errors.Is(err, errAppInstallBusy) {
		t.Fatalf("second acquire on the same device: %v", err)
	}
	if _, err := gate.acquire("other"); err != nil {
		t.Fatalf("another device should get a slot: %v", err)
	}
	releaseFirst()
	releaseAgain, err := gate.acquire("test")
	if err != nil {
		t.Fatalf("slot was not released: %v", err)
	}
	releaseAgain()
}

func TestAppInstallHandlerRejectsOversizedFormField(t *testing.T) {
	useAppPackageTempDir(t)
	originalLimit := maxAppPackageFormFieldBytes
	t.Cleanup(func() { maxAppPackageFormFieldBytes = originalLimit })
	maxAppPackageFormFieldBytes = 4

	controller := &fakeAppController{extension: device.AndroidPackageExtension}
	useFakeAppController(t, controller, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("note", "too long for the form"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	rec, response := runInstallHandler(t, body, writer.FormDataContentType())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(response.Message, `unexpected form field "note"`) {
		t.Fatalf("message %q", response.Message)
	}
	if controller.installedPath != "" {
		t.Fatalf("installer should not run, got %q", controller.installedPath)
	}
}

func TestWriteAppErrorStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"too large", errAppPackageTooLarge, http.StatusRequestEntityTooLarge},
		{"install busy", errAppInstallBusy, http.StatusConflict},
		{"slots exhausted", errAppInstallLimited, http.StatusTooManyRequests},
		{"unsupported", device.ErrAppManagementUnsupported, http.StatusNotImplemented},
		{"other", errors.New("boom"), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			rec := httptest.NewRecorder()
			c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/devices/test/apps", nil), rec)
			if err := writeAppError(c, tc.err); err != nil {
				t.Fatalf("writeAppError: %v", err)
			}
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
