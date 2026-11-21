package device

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScaleFrameToDisplay(t *testing.T) {
	tests := []struct {
		name                 string
		x, y, frameW, frameH float64
		dispW, dispH         int
		wantX, wantY         int
	}{
		{
			name: "half-size capture is doubled",
			x:    10, y: 5, frameW: 550, frameH: 1190,
			dispW: 1100, dispH: 2380,
			wantX: 20, wantY: 10,
		},
		{
			name: "matching sizes are unchanged",
			x:    100, y: 200, frameW: 1100, frameH: 2380,
			dispW: 1100, dispH: 2380,
			wantX: 100, wantY: 200,
		},
		{
			name: "missing frame dimensions pass through",
			x:    10, y: 20, frameW: 0, frameH: 0,
			dispW: 1100, dispH: 2380,
			wantX: 10, wantY: 20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotX, gotY := scaleFrameToDisplay(tt.x, tt.y, tt.frameW, tt.frameH, tt.dispW, tt.dispH)
			require.Equal(t, tt.wantX, gotX)
			require.Equal(t, tt.wantY, gotY)
		})
	}
}

func TestParseHarmonyDisplaySize(t *testing.T) {
	portrait := `---------------- Display ID: 0 ----------------
[DISPLAY INFO]
Width:                        1100
Height:                       2380
DisplayNode:                  7009386627074
Rotation:                     0
DensityInCurResolution:       2.9`
	landscape := `---------------- Display ID: 0 ----------------
[DISPLAY INFO]
Width:                        2380
Height:                       1100
DisplayNode:                  7009386627074
Rotation:                     270`

	t.Run("portrait", func(t *testing.T) {
		w, h, err := parseHarmonyDisplaySize(portrait)
		require.NoError(t, err)
		require.Equal(t, 1100, w)
		require.Equal(t, 2380, h)
	})
	t.Run("landscape", func(t *testing.T) {
		w, h, err := parseHarmonyDisplaySize(landscape)
		require.NoError(t, err)
		require.Equal(t, 2380, w)
		require.Equal(t, 1100, h)
	})
	t.Run("missing", func(t *testing.T) {
		_, _, err := parseHarmonyDisplaySize("no display info here")
		require.Error(t, err)
	})
}

func TestHarmonyHandleControlMessage(t *testing.T) {
	d := NewHarmonyOSDevice("test-key")
	ctx := context.Background()

	t.Run("invalid json", func(t *testing.T) {
		require.Error(t, d.HandleControlMessage(ctx, []byte("not json")))
	})
	t.Run("unknown type", func(t *testing.T) {
		require.Error(t, d.HandleControlMessage(ctx, []byte(`{"type":"nope"}`)))
	})
	t.Run("empty text is a no-op", func(t *testing.T) {
		require.NoError(t, d.HandleControlMessage(ctx, []byte(`{"type":"text","text":""}`)))
	})
}

func TestParseBoundsString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Rect
		wantErr bool
	}{
		{
			name:  "standard bounds",
			input: "[0,139][1100,2380]",
			want:  Rect{X: 0, Y: 139, Width: 1100, Height: 2241},
		},
		{
			name:  "small bounds",
			input: "[22,139][1078,2332]",
			want:  Rect{X: 22, Y: 139, Width: 1056, Height: 2193},
		},
		{
			name:  "single pixel",
			input: "[100,200][101,201]",
			want:  Rect{X: 100, Y: 200, Width: 1, Height: 1},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "not-bounds",
			wantErr: true,
		},
		{
			name:    "partial brackets",
			input:   "[100,200]",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBoundsString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("parseBoundsString(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseHarmonyLayoutToNode_NewFormat(t *testing.T) {
	layoutJSON := `{
		"attributes": {
			"bounds": "[0,0][1100,2380]",
			"type": ""
		},
		"children": [
			{
				"attributes": {
					"bounds": "[0,139][1100,2380]",
					"type": "WindowScene",
					"id": "session21",
					"text": "",
					"description": "",
					"clickable": "false",
					"enabled": "true",
					"focused": "true",
					"visible": "true"
				},
				"children": [
					{
						"attributes": {
							"bounds": "[0,139][1100,2380]",
							"type": "Stack",
							"id": "ScreenLockRootComponent",
							"clickable": "false",
							"enabled": "true",
							"text": "",
							"description": ""
						},
						"children": []
					}
				]
			}
		]
	}`

	node, err := parseHarmonyLayoutToNode(layoutJSON)
	if err != nil {
		t.Fatalf("parseHarmonyLayoutToNode error: %v", err)
	}

	// Root node from attributes with empty type is tagged as hierarchy
	if node.TagName != defaultRootTagName {
		t.Errorf("root TagName = %q, want %q", node.TagName, defaultRootTagName)
	}
	if node.Rect != (Rect{X: 0, Y: 0, Width: 1100, Height: 2380}) {
		t.Errorf("root Rect = %+v, want {0 0 1100 2380}", node.Rect)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}

	// First child: WindowScene
	child := node.Children[0]
	if child.TagName != "WindowScene" {
		t.Errorf("child TagName = %q, want %q", child.TagName, "WindowScene")
	}
	if child.Rect != (Rect{X: 0, Y: 139, Width: 1100, Height: 2241}) {
		t.Errorf("child Rect = %+v, want {0 139 1100 2241}", child.Rect)
	}
	if child.Props["id"] != "session21" {
		t.Errorf("child Props[id] = %q, want %q", child.Props["id"], "session21")
	}

	if len(child.Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(child.Children))
	}

	gc := child.Children[0]
	if gc.TagName != "Stack" {
		t.Errorf("grandchild TagName = %q, want %q", gc.TagName, "Stack")
	}
	if gc.Props["class"] != "Stack" {
		t.Errorf("grandchild Props[class] = %q, want %q", gc.Props["class"], "Stack")
	}
}

func TestParseHarmonyLayoutToNode_FromBytes(t *testing.T) {
	input := `{"attributes":{"bounds":"[10,20][500,1000]","type":"Column"},"children":[]}`
	root, err := parseHarmonyLayoutToNode([]byte(input))
	if err != nil {
		t.Fatalf("parseHarmonyLayoutToNode error: %v", err)
	}
	if root.TagName != defaultRootTagName {
		t.Errorf("TagName = %q, want %q", root.TagName, defaultRootTagName)
	}
	if root.Rect != (Rect{X: 10, Y: 20, Width: 490, Height: 980}) {
		t.Errorf("Rect = %+v, want {10 20 490 980}", root.Rect)
	}
}

func TestParseHarmonyLayoutToNode_RealSample(t *testing.T) {
	sample := map[string]any{
		"attributes": map[string]any{
			"bounds":          "[22,139][1078,2332]",
			"type":            "Stack",
			"id":              "ScreenLockRootComponent",
			"clickable":       "false",
			"enabled":         "true",
			"focused":         "true",
			"visible":         "true",
			"accessibilityId": "397",
			"description":     "",
			"text":            "",
			"longClickable":   "false",
		},
		"children": []any{
			map[string]any{
				"attributes": map[string]any{
					"bounds":  "[22,139][1078,2332]",
					"type":    "Stack",
					"id":      "BackGroundView",
					"visible": "true",
					"text":    "",
				},
				"children": []any{},
			},
			map[string]any{
				"attributes": map[string]any{
					"bounds": "[22,139][1078,2332]",
					"type":   "Stack",
					"id":     "",
				},
				"children": []any{
					map[string]any{
						"attributes": map[string]any{
							"bounds": "[22,139][1078,2332]",
							"type":   "Stack",
							"id":     "MainPageView_Screen_Lock_Home",
							"text":   "hello",
						},
						"children": []any{},
					},
				},
			},
		},
	}

	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}

	root, err := parseHarmonyLayoutToNode(data)
	if err != nil {
		t.Fatalf("parseHarmonyLayoutToNode error: %v", err)
	}

	if root.TagName != defaultRootTagName {
		t.Errorf("root TagName = %q, want %q", root.TagName, defaultRootTagName)
	}
	if root.Props["id"] != "ScreenLockRootComponent" {
		t.Errorf("root Props[id] = %q, want %q", root.Props["id"], "ScreenLockRootComponent")
	}
	if root.Props["class"] != "Stack" {
		t.Errorf("root Props[class] = %q, want %q", root.Props["class"], "Stack")
	}

	if len(root.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(root.Children))
	}

	if root.Children[0].Props["id"] != "BackGroundView" {
		t.Errorf("child 0 id = %q, want %q", root.Children[0].Props["id"], "BackGroundView")
	}

	if len(root.Children[1].Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(root.Children[1].Children))
	}
	if root.Children[1].Children[0].Props["text"] != "hello" {
		t.Errorf("grandchild text = %q, want %q", root.Children[1].Children[0].Props["text"], "hello")
	}
}

func TestParseHarmonyLayoutToNode_Empty(t *testing.T) {
	_, err := parseHarmonyLayoutToNode(nil)
	if err == nil {
		t.Errorf("expected error for nil input")
	}
}

func TestHarmonyTreeToXML(t *testing.T) {
	layoutJSON := `{
		"attributes": {
			"bounds": "[0,0][1100,2380]",
			"type": ""
		},
		"children": [
			{
				"attributes": {
					"bounds": "[0,139][1100,2380]",
					"type": "WindowScene",
					"id": "session21",
					"text": "hello"
				},
				"children": []
			}
		]
	}`

	node, err := parseHarmonyLayoutToNode(layoutJSON)
	if err != nil {
		t.Fatalf("parseHarmonyLayoutToNode error: %v", err)
	}
	xmlStr, err := node.ToXML()
	if err != nil {
		t.Fatalf("ToXML error: %v", err)
	}
	if !strings.Contains(xmlStr, `<hierarchy`) {
		t.Errorf("ToXML output missing hierarchy root: %s", xmlStr)
	}
	if !strings.Contains(xmlStr, `WindowScene`) {
		t.Errorf("ToXML output missing WindowScene node: %s", xmlStr)
	}
}

func TestHarmonyKeyName(t *testing.T) {
	tests := []struct {
		input     KeyName
		want      harmonyKeyCode
		wantError bool
	}{
		{KeyHome, harmonyKeyCodeHome, false},
		{KeyBack, harmonyKeyCodeBack, false},
		{KeyMenu, 0, true},
		{KeyRecents, harmonyKeyCodeAppSelect, false},
		{KeyPower, harmonyKeyCodePower, false},
		{KeyVolumeUp, harmonyKeyCodeVolumeUp, false},
		{KeyVolumeDown, harmonyKeyCodeVolumeDown, false},
		{KeyTab, harmonyKeyCodeTab, false},
		{KeySpace, harmonyKeyCodeSpace, false},
		{KeyEnter, harmonyKeyCodeEnter, false},
		{KeyEscape, harmonyKeyCodeEscape, false},
		{KeyBackspace, harmonyKeyCodeDel, false},
		{KeyDelete, harmonyKeyCodeForwardDel, false},
		{KeyCapsLock, harmonyKeyCodeCapsLock, false},
		{KeyPageUp, harmonyKeyCodePageUp, false},
		{KeyPageDown, harmonyKeyCodePageDown, false},
		{KeyInsert, harmonyKeyCodeInsert, false},
		{KeyDpadUp, harmonyKeyCodeDpadUp, false},
		{KeyDpadDown, harmonyKeyCodeDpadDown, false},
		{KeyDpadLeft, harmonyKeyCodeDpadLeft, false},
		{KeyDpadRight, harmonyKeyCodeDpadRight, false},
		{KeyDpadCenter, harmonyKeyCodeDpadCenter, false},
		{KeyComma, harmonyKeyCodeComma, false},
		{KeyPeriod, harmonyKeyCodePeriod, false},
		{KeyMinus, harmonyKeyCodeMinus, false},
		{KeyEquals, harmonyKeyCodeEquals, false},
		{KeySemicolon, harmonyKeyCodeSemicolon, false},
		{KeyQuote, harmonyKeyCodeApostrophe, false},
		{KeySlash, harmonyKeyCodeSlash, false},
		{KeyBackslash, harmonyKeyCodeBackslash, false},
		{KeyLeftBracket, harmonyKeyCodeLBracket, false},
		{KeyRightBracket, harmonyKeyCodeRBracket, false},
		{KeyBackquote, harmonyKeyCodeGrave, false},
		{KeyPlus, harmonyKeyCodePlus, false},
		{KeyAt, harmonyKeyCodeAt, false},
		{KeyName("a"), harmonyKeyCodeA, false},
		{KeyName("z"), harmonyKeyCodeA + 25, false},
		{KeyName("0"), harmonyKeyCodeZero, false},
		{KeyName("9"), harmonyKeyCodeZero + 9, false},
		{"", 0, true},
		{KeyName("unknown"), 0, true},
		{KeyName(" "), 0, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got, err := harmonyKeyName(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("harmonyKeyName(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("harmonyKeyName(%q) = %d, want %d", tt.input, got, tt.want)
			}
			if got.String() != strconv.Itoa(int(tt.want)) {
				t.Errorf("harmonyKeyName(%q).String() = %q, want %q", tt.input, got.String(), strconv.Itoa(int(tt.want)))
			}
		})
	}
}

func TestParseKeyName(t *testing.T) {
	tests := []struct {
		input     string
		want      KeyName
		wantError bool
	}{
		{"home", KeyHome, false},
		{"HOME", KeyHome, false},
		{"Back", KeyBack, false},
		{"volume_up", KeyVolumeUp, false},
		{"recents", KeyRecents, false},
		{"a", KeyName("a"), false},
		{"Z", KeyName("z"), false},
		{"5", KeyName("5"), false},
		{"tab", KeyTab, false},
		{"space", KeySpace, false},
		{"dpad_center", KeyDpadCenter, false},
		{"", "", true},
		{"unknown", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseKeyName(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseKeyName(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseKeyName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHarmonyKeyCodes(t *testing.T) {
	tests := []struct {
		name      string
		key       KeyName
		meta      MetaState
		want      []harmonyKeyCode
		wantError bool
	}{
		{"single", KeyName("v"), MetaNone, []harmonyKeyCode{harmonyKeyCodeA + 21}, false},
		{"ctrl+v", KeyName("v"), MetaCtrl, []harmonyKeyCode{harmonyKeyCodeCtrlLeft, harmonyKeyCodeA + 21}, false},
		{"ctrl_left+v", KeyName("v"), MetaCtrlLeft, []harmonyKeyCode{harmonyKeyCodeCtrlLeft, harmonyKeyCodeA + 21}, false},
		{"ctrl_right+v", KeyName("v"), MetaCtrlRight, []harmonyKeyCode{harmonyKeyCodeCtrlRight, harmonyKeyCodeA + 21}, false},
		{"shift+a", KeyName("a"), MetaShift, []harmonyKeyCode{harmonyKeyCodeShiftLeft, harmonyKeyCodeA}, false},
		{"alt+enter", KeyEnter, MetaAlt, []harmonyKeyCode{harmonyKeyCodeAltLeft, harmonyKeyCodeEnter}, false},
		{"meta+tab", KeyTab, MetaMeta, []harmonyKeyCode{harmonyKeyCodeMetaLeft, harmonyKeyCodeTab}, false},
		{"ctrl+shift+z", KeyName("z"), MetaCtrl | MetaShift, []harmonyKeyCode{harmonyKeyCodeCtrlLeft, harmonyKeyCodeShiftLeft, harmonyKeyCodeA + 25}, false},
		{"home+ctrl", KeyHome, MetaCtrl, []harmonyKeyCode{harmonyKeyCodeCtrlLeft, harmonyKeyCodeHome}, false},
		{"unsupported", KeyMenu, MetaNone, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := harmonyKeyCodes(tt.key, tt.meta)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for %q", tt.name)
				}
				return
			}
			if err != nil {
				t.Errorf("harmonyKeyCodes unexpected error: %v", err)
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("harmonyKeyCodes(%q, %d) = %v, want %v", tt.key, tt.meta, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("harmonyKeyCodes(%q, %d)[%d] = %d, want %d", tt.key, tt.meta, i, got[i], tt.want[i])
				}
			}
		})
	}
}
