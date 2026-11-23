package webserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/uiautodev/uiautodev/webserver/device"
)

func TestGetDeviceFromArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{name: "missing device_id", args: map[string]any{}, wantErr: true},
		{name: "empty device_id", args: map[string]any{argDeviceID: ""}, wantErr: true},
		{name: "non-string device_id", args: map[string]any{argDeviceID: 123}, wantErr: true},
		{name: "invalid device_id prefix", args: map[string]any{argDeviceID: "zz-invalid"}, wantErr: true},
		{name: "valid android device", args: map[string]any{argDeviceID: "a-test"}, wantErr: false},
		{name: "valid harmony device", args: map[string]any{argDeviceID: "h-test"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "test",
					Arguments: tt.args,
				},
			}
			_, err := getDeviceFromArgs(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("getDeviceFromArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizedScaleFactors(t *testing.T) {
	tests := []struct {
		name   string
		width  float64
		height float64
		devW   int
		devH   int
		wantSX float64
		wantSY float64
	}{
		{name: "identity when canvas matches device", width: 1080, height: 1920, devW: 1080, devH: 1920, wantSX: 1, wantSY: 1},
		{name: "scaled down canvas", width: 540, height: 960, devW: 1080, devH: 1920, wantSX: 2, wantSY: 2},
		{name: "scaled up canvas", width: 2160, height: 3840, devW: 1080, devH: 1920, wantSX: 0.5, wantSY: 0.5},
		{name: "non-uniform scale", width: 1000, height: 2000, devW: 100, devH: 200, wantSX: 0.1, wantSY: 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSX, gotSY := normalizedScaleFactors(tt.devW, tt.devH, tt.width, tt.height)
			if gotSX != tt.wantSX || gotSY != tt.wantSY {
				t.Errorf("normalizedScaleFactors(%d, %d, %v, %v) = (%v, %v), want (%v, %v)",
					tt.devW, tt.devH, tt.width, tt.height, gotSX, gotSY, tt.wantSX, tt.wantSY)
			}
		})
	}
}

func TestNormalizeTapCoords(t *testing.T) {
	dev, err := device.NewDevice("a-test")
	if err != nil {
		t.Fatalf("failed to create test device: %v", err)
	}
	t.Run("no width/height passes through", func(t *testing.T) {
		x, y, err := normalizeTapCoords(100.5, 200.5, nil, nil, dev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if x != 100 || y != 200 {
			t.Errorf("got (%d, %d), want (100, 200)", x, y)
		}
	})
	t.Run("non-numeric width with numeric height errors", func(t *testing.T) {
		_, _, err := normalizeTapCoords(1, 1, "1080", 1920.0, dev)
		if err == nil {
			t.Fatal("expected error for non-numeric width with numeric height, got nil")
		}
	})
	t.Run("missing height errors", func(t *testing.T) {
		_, _, err := normalizeTapCoords(1, 1, 1080.0, nil, dev)
		if err == nil {
			t.Fatal("expected error when only width is provided, got nil")
		}
	})
	t.Run("missing width errors", func(t *testing.T) {
		_, _, err := normalizeTapCoords(1, 1, nil, 1920.0, dev)
		if err == nil {
			t.Fatal("expected error when only height is provided, got nil")
		}
	})
	t.Run("zero width errors", func(t *testing.T) {
		_, _, err := normalizeTapCoords(1, 1, 0.0, 1920.0, dev)
		if err == nil {
			t.Fatal("expected error for non-positive width, got nil")
		}
	})
}

func TestNormalizeSwipeCoords(t *testing.T) {
	dev, err := device.NewDevice("a-test")
	if err != nil {
		t.Fatalf("failed to create test device: %v", err)
	}
	t.Run("no width/height passes through", func(t *testing.T) {
		x1, y1, x2, y2, err := normalizeSwipeCoords(10.5, 20.5, 30.5, 40.5, nil, nil, dev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if x1 != 10 || y1 != 20 || x2 != 30 || y2 != 40 {
			t.Errorf("got (%d, %d, %d, %d), want (10, 20, 30, 40)", x1, y1, x2, y2)
		}
	})
	t.Run("missing height errors", func(t *testing.T) {
		_, _, _, _, err := normalizeSwipeCoords(1, 1, 2, 2, 1080.0, nil, dev)
		if err == nil {
			t.Fatal("expected error when only width is provided, got nil")
		}
	})
	t.Run("zero height errors", func(t *testing.T) {
		_, _, _, _, err := normalizeSwipeCoords(1, 1, 2, 2, 1080.0, 0.0, dev)
		if err == nil {
			t.Fatal("expected error for non-positive height, got nil")
		}
	})
}

func TestMcpListDevicesHandler(t *testing.T) {
	tests := []struct {
		name      string
		platforms []any
		wantKeys  []string
		wantErr   bool
	}{
		{name: "no platforms arg", wantKeys: []string{"android", "ios", "harmony", "demo"}},
		{name: "subset of platforms", platforms: []any{"ios", "harmony"}, wantKeys: []string{"ios", "harmony"}},
		{name: "duplicate platforms", platforms: []any{"android", "android"}, wantKeys: []string{"android"}},
		{name: "huawei alias maps to harmony", platforms: []any{"huawei"}, wantKeys: []string{"harmony"}},
		{name: "unknown platform", platforms: []any{"windows"}, wantErr: true},
		{name: "non-string platform string 123", platforms: []any{"android", "123"}, wantErr: true},
		{name: "non-string platform numeric", platforms: []any{"android", 123}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{}
			if tt.platforms != nil {
				args["platforms"] = tt.platforms
			}
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "list_devices",
					Arguments: args,
				},
			}
			result, err := mcpListDevicesHandler(context.Background(), req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := map[string][]string{}
			text, ok := result.Content[0].(mcp.TextContent)
			if !ok || text.Text == "" {
				t.Fatalf("result has no text content: %v", result.Content)
			}
			if err := json.Unmarshal([]byte(text.Text), &got); err != nil {
				t.Fatalf("result is not valid grouped JSON: %v", err)
			}
			for _, key := range tt.wantKeys {
				if _, ok := got[key]; !ok {
					t.Errorf("expected platform key %q in result, got %v", key, got)
				}
			}
			if len(got) != len(tt.wantKeys) {
				t.Errorf("expected %d platforms in result, got %d: %v", len(tt.wantKeys), len(got), got)
			}
		})
	}
}

func TestMcpShellHandler(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{name: "missing command", args: map[string]any{argDeviceID: "d-test"}, wantErr: true},
		{name: "empty command", args: map[string]any{argDeviceID: "d-test", "command": ""}, wantErr: true},
		{name: "non-string command", args: map[string]any{argDeviceID: "d-test", "command": 123}, wantErr: true},
		{name: "unsupported platform", args: map[string]any{argDeviceID: "d-test", "command": "echo hi"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "shell",
					Arguments: tt.args,
				},
			}
			_, err := mcpShellHandler(context.Background(), req)
			if (err != nil) != tt.wantErr {
				t.Errorf("mcpShellHandler() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	t.Run("unsupported platform returns sentinel error", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "shell",
				Arguments: map[string]any{argDeviceID: "d-test", "command": "echo hi"},
			},
		}
		_, err := mcpShellHandler(context.Background(), req)
		if !errors.Is(err, errShellUnsupported) {
			t.Errorf("expected errShellUnsupported, got %v", err)
		}
	})
}

func TestBuildSSEServerTools(t *testing.T) {
	ts := httptest.NewServer(buildSSEServer(false))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mcp/tools")
	if err != nil {
		t.Fatalf("GET /mcp/tools failed: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Code int `json:"code"`
		Data []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("unexpected code: %d", body.Code)
	}

	names := make([]string, 0, len(body.Data))
	for _, tool := range body.Data {
		names = append(names, tool.Name)
	}
	if len(names) == 0 {
		t.Fatalf("expected at least one tool, got none")
	}
	if names[0] != "list_devices" {
		t.Errorf("expected list_devices as first tool, got %q", names[0])
	}
	expected := []string{"get_window_size", "get_device_info", "screenshot", "dump_xml",
		"tap", "swipe", "input_text", "clear_text", "press_key", "shell", "find_elements_by_xpath"}
	for _, name := range expected {
		found := false
		for _, got := range names {
			if got == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool %q in list, got %v", name, names)
		}
	}
}

func TestBuildSSEServerToolsDisabled(t *testing.T) {
	ts := httptest.NewServer(buildSSEServer(true))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mcp/tools")
	if err != nil {
		t.Fatalf("GET /mcp/tools failed: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Code int `json:"code"`
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("unexpected code: %d", body.Code)
	}
	for _, tool := range body.Data {
		if tool.Name == "list_devices" {
			t.Fatalf("list_devices should be disabled, got %v", tool.Name)
		}
	}
	if len(body.Data) == 0 {
		t.Fatalf("expected tools to still be present, got none")
	}
}

func TestBuildSSEServerUnifiedEndpoint(t *testing.T) {
	ts := httptest.NewServer(buildSSEServer(false))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mcp/sse")
	if err != nil {
		t.Fatalf("GET /mcp/sse failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	// The first event is the endpoint event telling the client where to POST messages.
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			endpoint := strings.TrimPrefix(line, "data: ")
			if !strings.HasPrefix(endpoint, "/mcp/message?sessionId=") {
				t.Errorf("unexpected message endpoint: %q", endpoint)
			}
			return
		}
	}
	t.Fatal("no endpoint event received from /mcp/sse")
}

func TestStreamableHTTPEndpoint(t *testing.T) {
	ts := httptest.NewServer(buildSSEServer(false))
	defer ts.Close()

	assertStreamableClient(t, ts.URL+"/mcp/http")
}

func TestStreamableHTTPOverLegacySSEPath(t *testing.T) {
	ts := httptest.NewServer(buildSSEServer(false))
	defer ts.Close()

	assertStreamableClient(t, ts.URL+"/mcp/sse")
}

func assertStreamableClient(t *testing.T, endpoint string) {
	t.Helper()

	c, err := client.NewStreamableHttpClient(endpoint)
	if err != nil {
		t.Fatalf("create streamable client failed: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start client failed: %v", err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
		},
	}); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "list_devices" {
			return
		}
	}
	t.Fatalf("expected list_devices tool over streamable HTTP, got %v", res.Tools)
}
