package webserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/uiautodev/uiautodev/webserver/device"
	"image/png"
	"io"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const argDeviceID = "device_id"

// getDeviceFromArgs resolves the target device from the tool's device_id argument.
func getDeviceFromArgs(req mcp.CallToolRequest) (*device.DeviceWrapper, error) {
	deviceId, ok := req.GetArguments()[argDeviceID].(string)
	if !ok || deviceId == "" {
		return nil, errors.New("missing or invalid device_id argument")
	}
	d, err := device.NewDevice(deviceId)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func mcpWindowSizeHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	w, h, err := d.GetScreenSize()
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Width: %d, Height: %d", w, h)), nil
}

func mcpDeviceInfoHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	info := d.Info()
	infoJson, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(infoJson)), nil
}

func mcpDeviceTapHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	xFloat, ok := req.GetArguments()["x"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid x argument")
	}
	yFloat, ok := req.GetArguments()["y"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid y argument")
	}
	x, y, err := normalizeTapCoords(xFloat, yFloat, req.GetArguments()["width"], req.GetArguments()["height"], d)
	if err != nil {
		return nil, err
	}
	err = d.Tap(x, y)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Tapped at (%d, %d)", x, y)), nil
}

// getNormalizedScale resolves the width/height normalization arguments and
// returns the x/y scale factors to convert coordinates from the normalized
// width x height space to device pixels. Returns 1,1 when width/height are
// absent, meaning coordinates are treated as device pixels directly.
func getNormalizedScale(widthArg, heightArg any, d *device.DeviceWrapper) (float64, float64, error) {
	widthAbsent := widthArg == nil
	heightAbsent := heightArg == nil
	if widthAbsent && heightAbsent {
		return 1, 1, nil
	}
	if widthAbsent != heightAbsent {
		return 0, 0, errors.New("width and height must be provided together for normalized coordinates")
	}
	width, ok := widthArg.(float64)
	if !ok {
		return 0, 0, fmt.Errorf("width must be a number, got %T", widthArg)
	}
	height, ok := heightArg.(float64)
	if !ok {
		return 0, 0, fmt.Errorf("height must be a number, got %T", heightArg)
	}
	if width <= 0 || height <= 0 {
		return 0, 0, errors.New("width and height must be positive for normalized coordinates")
	}
	devW, devH, err := d.GetScreenSize()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get device screen size for coordinate scaling: %w", err)
	}
	sx, sy := normalizedScaleFactors(devW, devH, width, height)
	return sx, sy, nil
}

// normalizedScaleFactors computes x/y scale factors for converting normalized
// coordinates in a width x height space to devW x devH device pixels.
func normalizedScaleFactors(devW, devH int, width, height float64) (float64, float64) {
	return float64(devW) / width, float64(devH) / height
}

func normalizeTapCoords(x, y float64, widthArg, heightArg any, d *device.DeviceWrapper) (int, int, error) {
	sx, sy, err := getNormalizedScale(widthArg, heightArg, d)
	if err != nil {
		return 0, 0, err
	}
	return int(x * sx), int(y * sy), nil
}

func normalizeSwipeCoords(x1, y1, x2, y2 float64, widthArg, heightArg any, d *device.DeviceWrapper) (int, int, int, int, error) {
	sx, sy, err := getNormalizedScale(widthArg, heightArg, d)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return int(x1 * sx), int(y1 * sy), int(x2 * sx), int(y2 * sy), nil
}

func mcpScreenshotHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	img, err := d.Screenshot()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	// Encode the image data as base64 for proper transport
	base64Data := base64.StdEncoding.EncodeToString(buf.Bytes())
	return mcp.NewToolResultImage("screenshot", base64Data, "image/png"), nil
}

func mcpDumpXMLHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	xmlData, err := d.DumpXML()
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(xmlData), nil
}

func mcpSwipeHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	x1Float, ok := req.GetArguments()["x1"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid x1 argument")
	}
	y1Float, ok := req.GetArguments()["y1"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid y1 argument")
	}
	x2Float, ok := req.GetArguments()["x2"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid x2 argument")
	}
	y2Float, ok := req.GetArguments()["y2"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid y2 argument")
	}
	durationFloat, ok := req.GetArguments()["durationMs"].(float64)
	if !ok {
		durationFloat = 500 // default duration
	}
	x1, y1, x2, y2, err := normalizeSwipeCoords(x1Float, y1Float, x2Float, y2Float,
		req.GetArguments()["width"], req.GetArguments()["height"], d)
	if err != nil {
		return nil, err
	}
	durationMs := int(durationFloat)
	err = d.Swipe(x1, y1, x2, y2, durationMs)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Swiped from (%d, %d) to (%d, %d) over %d ms", x1, y1, x2, y2, durationMs)), nil
}

func mcpInputTextHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	text, ok := req.GetArguments()["text"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid text argument")
	}
	err = d.InputText(text)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Input text: %s", text)), nil
}

func mcpClearTextHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	err = d.ClearText()
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("Cleared text"), nil
}

func mcpPressKeyHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	keyStr, ok := req.GetArguments()["key"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid key argument")
	}
	key, err := device.ParseKeyName(keyStr)
	if err != nil {
		return nil, err
	}
	var metaState device.MetaState
	if ms, ok := req.GetArguments()["metaState"].(float64); ok {
		metaState = device.MetaState(int64(ms))
	}
	err = d.PressKey(key, metaState)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Pressed key: %s (metaState: %d)", key, metaState)), nil
}

func mcpShellHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	command, ok := req.GetArguments()["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("invalid command argument")
	}
	var timeoutMs int64
	if timeout, ok := req.GetArguments()["timeout"].(float64); ok {
		timeoutMs = int64(timeout)
	}
	out, err := runDeviceShell(d, command, timeoutMs)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(out), nil
}

func mcpFindElementByXpathHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := getDeviceFromArgs(req)
	if err != nil {
		return nil, err
	}
	xpath, ok := req.GetArguments()["xpath"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid xpath argument")
	}
	xmlContents, err := d.FindElementsByXpath(xpath)
	if err != nil {
		return nil, err
	}
	// Wrap array in an object as MCP expects a record (object), not an array
	result := map[string]interface{}{
		"elements": xmlContents,
	}
	return mcp.NewToolResultJSON(result)
}

func appControllerFromArgs(req mcp.CallToolRequest) (device.AppController, error) {
	deviceID, ok := req.GetArguments()[argDeviceID].(string)
	if !ok || deviceID == "" {
		return nil, errors.New("missing or invalid device_id argument")
	}
	_, controller, err := device.GetAppController(deviceID)
	return controller, err
}

func mcpAppListHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	controller, err := appControllerFromArgs(req)
	if err != nil {
		return nil, err
	}
	scopeValue, _ := req.GetArguments()["scope"].(string)
	scope, err := device.ParseAppScope(scopeValue)
	if err != nil {
		return nil, err
	}
	apps, err := controller.ListApps(scope)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultJSON(map[string]any{"apps": apps})
}

func mcpAppStartHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	controller, err := appControllerFromArgs(req)
	if err != nil {
		return nil, err
	}
	appID, ok := req.GetArguments()["app_id"].(string)
	if !ok || appID == "" {
		return nil, errors.New("missing or invalid app_id argument")
	}
	activity, _ := req.GetArguments()["activity"].(string)
	current, err := controller.StartApp(appID, activity)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultJSON(current)
}

func mcpAppStopHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	controller, err := appControllerFromArgs(req)
	if err != nil {
		return nil, err
	}
	appID, ok := req.GetArguments()["app_id"].(string)
	if !ok || appID == "" {
		return nil, errors.New("missing or invalid app_id argument")
	}
	if err := controller.StopApp(appID); err != nil {
		return nil, err
	}
	return mcp.NewToolResultJSON(map[string]any{"app_id": appID})
}

func mcpAppCurrentHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	controller, err := appControllerFromArgs(req)
	if err != nil {
		return nil, err
	}
	current, err := controller.CurrentApp()
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultJSON(current)
}

func mcpListDevicesHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	types := allDeviceTypes()
	if raw, ok := req.GetArguments()["platforms"]; ok && raw != nil {
		arr, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid platforms argument: expected an array of strings")
		}
		types = types[:0]
		for _, item := range arr {
			name, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("invalid platforms argument: expected an array of strings")
			}
			deviceType, err := parsePlatformName(name)
			if err != nil {
				return nil, err
			}
			types = append(types, deviceType)
		}
	}
	return mcp.NewToolResultJSON(listDevicesByPlatform(types))
}

// parsePlatformName maps a platform name to its DeviceType.
func parsePlatformName(name string) (device.DeviceType, error) {
	switch name {
	case device.Android.String():
		return device.Android, nil
	case device.IOS.String():
		return device.IOS, nil
	case device.HarmonyOS.String(), "huawei":
		return device.HarmonyOS, nil
	case device.Demo.String():
		return device.Demo, nil
	default:
		return 0, fmt.Errorf("unknown platform %q, valid platforms: android, ios, harmony, huawei, demo", name)
	}
}

// mcpSSEHandler multiplexes a single URL between the legacy SSE transport and
// the streamable HTTP transport. Requests that can only belong to streamable
// HTTP (POST, or a GET/DELETE carrying a session id) are forwarded there;
// everything else falls back to the legacy SSE handler.
func mcpSSEHandler(sse, streamable http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Header.Get(server.HeaderKeySessionID) != "" {
			streamable.ServeHTTP(w, r)
			return
		}
		sse.ServeHTTP(w, r)
	})
}

func buildSSEServer(disableDeviceListing bool) *http.ServeMux {
	gate := newMCPToolGate()
	mcpServer := server.NewMCPServer("Mobile UI Automation", "0.0.1",
		server.WithToolHandlerMiddleware(gate.middleware()))

	toolsOrder := []string{}
	addTool := func(tool mcp.Tool, handler server.ToolHandlerFunc) {
		gate.register(tool)
		mcpServer.AddTool(tool, handler)
		toolsOrder = append(toolsOrder, tool.Name)
	}

	deviceIDDesc := `The device ID to operate on, e.g. "a-SERIAL" for a local device or "r-SLAVEID-a-SERIAL" for a remote device.`
	if !disableDeviceListing {
		deviceIDDesc += ` Use list_devices to get available IDs.`
	}
	deviceIDOption := func() mcp.ToolOption {
		return mcp.WithString(argDeviceID,
			mcp.Required(),
			mcp.Description(deviceIDDesc),
		)
	}

	if !disableDeviceListing {
		addTool(mcp.NewTool("list_devices",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDescription("Lists the IDs of all connected devices grouped by platform. Returns an object mapping each platform to its device IDs, e.g. {\"android\": [\"a-xxx\"], \"ios\": [], \"harmony\": []}"),
			mcp.WithArray("platforms",
				mcp.WithStringItems(),
				mcp.Description("Optional list of platforms to include, e.g. [\"android\", \"ios\"]. Defaults to all platforms. Valid values: android, ios, harmony, huawei (alias for harmony)."),
			),
		), mcpListDevicesHandler)
	}

	registerMCPPresenceTools(addTool, deviceIDOption)

	addTool(mcp.NewTool("get_window_size",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Gets the device window size"),
		deviceIDOption(),
	), mcpWindowSizeHandler)

	addTool(mcp.NewTool("get_device_info",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Gets the device information"),
		deviceIDOption(),
	), mcpDeviceInfoHandler)

	addTool(mcp.NewTool("screenshot",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Takes a screenshot of the device"),
		deviceIDOption(),
	), mcpScreenshotHandler)

	addTool(mcp.NewTool("dump_xml",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Dumps the UI hierarchy as XML"),
		deviceIDOption(),
	), mcpDumpXMLHandler)

	addTool(mcp.NewTool("tap",
		mcp.WithDescription("Simulates a tap on the device. Coordinates can be provided either in device pixels, or normalized against an image width/height (e.g. the screenshot dimensions) in which case they are scaled to the device screen automatically"),
		deviceIDOption(),
		mcp.WithNumber("x",
			mcp.Required(),
			mcp.Description("The x coordinate"),
		),
		mcp.WithNumber("y",
			mcp.Required(),
			mcp.Description("The y coordinate"),
		),
		mcp.WithNumber("width",
			mcp.Description("Optional width of the normalized coordinate space (e.g. screenshot width). Must be provided together with height"),
		),
		mcp.WithNumber("height",
			mcp.Description("Optional height of the normalized coordinate space (e.g. screenshot height). Must be provided together with width"),
		),
	), mcpDeviceTapHandler)

	addTool(mcp.NewTool("swipe",
		mcp.WithDescription("Simulates a swipe gesture on the device. Coordinates can be provided either in device pixels, or normalized against an image width/height (e.g. the screenshot dimensions) in which case they are scaled to the device screen automatically"),
		deviceIDOption(),
		mcp.WithNumber("x1",
			mcp.Required(),
			mcp.Description("The starting x coordinate"),
		),
		mcp.WithNumber("y1",
			mcp.Required(),
			mcp.Description("The starting y coordinate"),
		),
		mcp.WithNumber("x2",
			mcp.Required(),
			mcp.Description("The ending x coordinate"),
		),
		mcp.WithNumber("y2",
			mcp.Required(),
			mcp.Description("The ending y coordinate"),
		),
		mcp.WithNumber("durationMs",
			mcp.Description("Duration of the swipe in milliseconds, default 500"),
		),
		mcp.WithNumber("width",
			mcp.Description("Optional width of the normalized coordinate space (e.g. screenshot width). Must be provided together with height"),
		),
		mcp.WithNumber("height",
			mcp.Description("Optional height of the normalized coordinate space (e.g. screenshot height). Must be provided together with width"),
		),
	), mcpSwipeHandler)

	addTool(mcp.NewTool("input_text",
		mcp.WithDescription("Send input text to device"),
		deviceIDOption(),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("The text to send"),
		),
	), mcpInputTextHandler)

	addTool(mcp.NewTool("clear_text",
		mcp.WithDescription("Select all text in the currently focused input field and delete it"),
		deviceIDOption(),
	), mcpClearTextHandler)

	addTool(mcp.NewTool("press_key",
		mcp.WithDescription("Press a hardware key on the device, optionally with modifier keys (metaState) for combination key events"),
		deviceIDOption(),
		mcp.WithString("key",
			mcp.Required(),
			mcp.Description("The key to press (case-insensitive). Supported: home, back, menu, recents, power, volume_up, volume_down, a-z, 0-9, tab, space, enter, escape, backspace, delete, dpad keys, dpad_center and common punctuation."),
		),
		mcp.WithNumber("metaState",
			mcp.Description("Optional bitmask of modifier keys (AndroidKeyEventMeta). e.g. Ctrl=0x1000, Alt=0x2, Shift=0x1. Combination key support is HarmonyOS only."),
		),
	), mcpPressKeyHandler)

	addTool(mcp.NewTool("shell",
		mcp.WithDescription("Runs a shell command on the device and returns its output. Only supported on Android and HarmonyOS devices"),
		deviceIDOption(),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("The shell command to run"),
		),
		mcp.WithNumber("timeout",
			mcp.Description("Optional timeout in milliseconds. Runs without a timeout when omitted or <= 0"),
		),
	), mcpShellHandler)

	addTool(mcp.NewTool("find_elements_by_xpath",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Finds elements by XPath and returns their XML representations as an array"),
		deviceIDOption(),
		mcp.WithString("xpath",
			mcp.Required(),
			mcp.Description("The XPath query to find elements"),
		),
	), mcpFindElementByXpathHandler)

	addTool(mcp.NewTool("app_list",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Lists installed app IDs on the device"),
		deviceIDOption(),
		mcp.WithString("scope",
			mcp.Enum("user", "system", "all"),
			mcp.Description("Which apps to list: user (default), system, or all"),
		),
	), mcpAppListHandler)

	addTool(mcp.NewTool("app_start",
		mcp.WithDescription("Starts an app by app_id. On Android, resolves the launcher activity when activity is omitted"),
		deviceIDOption(),
		mcp.WithString("app_id",
			mcp.Required(),
			mcp.Description("The app ID (Android package, iOS bundle ID, or HarmonyOS bundle name)"),
		),
		mcp.WithString("activity",
			mcp.Description("Optional activity/ability name. Android only; when omitted the launcher activity is resolved"),
		),
	), mcpAppStartHandler)

	addTool(mcp.NewTool("app_stop",
		mcp.WithDescription("Stops an app by app_id"),
		deviceIDOption(),
		mcp.WithString("app_id",
			mcp.Required(),
			mcp.Description("The app ID (Android package, iOS bundle ID, or HarmonyOS bundle name)"),
		),
	), mcpAppStopHandler)

	addTool(mcp.NewTool("app_current",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Returns the foreground app and activity/ability"),
		deviceIDOption(),
	), mcpAppCurrentHandler)

	sseServer := server.NewSSEServer(
		mcpServer,
		server.WithStaticBasePath("/mcp"),
		server.WithUseFullURLForMessageEndpoint(true),
		// Propagate the original query (including ?token=) to the message
		// endpoint so token-in-URL clients stay authenticated on POST.
		server.WithAppendQueryToMessageEndpoint(),
		// Copy the resolved identity into the context of tool handlers.
		server.WithSSEContextFunc(mcpContextWithIdentity),
	)

	// Modern streamable HTTP transport (MCP 2025-03-26), served at /mcp/http.
	// Legacy SSE clients use /mcp/sse; newer clients such as Pi require
	// streamable HTTP. The path lives under /mcp/ (rather than at /mcp) so it
	// does not collide with the frontend's /mcp page route.
	streamableServer := server.NewStreamableHTTPServer(
		mcpServer,
		server.WithEndpointPath("/mcp/http"),
		server.WithHTTPContextFunc(mcpContextWithIdentity),
	)

	mux := http.NewServeMux()
	// /mcp/sse serves both the legacy SSE transport and the streamable HTTP
	// transport so existing clients can keep their URL after upgrading. Legacy
	// SSE clients only GET /mcp/sse and POST to /mcp/message, whereas
	// streamable HTTP clients POST straight to the endpoint (and carry
	// Mcp-Session-Id on later GET/DELETE requests).
	mux.Handle("/mcp/sse", mcpSSEHandler(sseServer.SSEHandler(), streamableServer))
	mux.Handle("/mcp/message", sseServer.MessageHandler())
	mux.Handle("/mcp/http", streamableServer)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.RequestURI)
		io.WriteString(w, "MCP Server is running.\n")
	})

	type ToolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	mux.HandleFunc("/mcp/tools", func(w http.ResponseWriter, r *http.Request) {
		tools := []ToolInfo{}
		for _, toolName := range toolsOrder {
			tool := mcpServer.GetTool(toolName)
			tools = append(tools, ToolInfo{
				Name:        tool.Tool.Name,
				Description: tool.Tool.Description,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0,
			"data": tools,
		})
	})
	return mux
}
