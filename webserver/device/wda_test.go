package device

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseWDASourceToNode(t *testing.T) {
	jsonData := `{
		"value": {
			"rect": {
				"y": 0,
				"x": 0,
				"width": 390,
				"height": 844
			},
			"label": " ",
			"rawIdentifier": null,
			"traits": "",
			"frame": "{{0, 0}, {390, 844}}",
			"isVisible": "1",
			"type": "Application",
			"value": null,
			"isEnabled": "1",
			"children": [
				{
					"rect": {
						"y": 0,
						"x": 0,
						"width": 1170,
						"height": 2532
					},
					"label": null,
					"rawIdentifier": null,
					"traits": "",
					"frame": "{{0, 0}, {1170, 2532}}",
					"isVisible": "0",
					"type": "Window",
					"value": null,
					"isEnabled": "1",
					"isAccessible": "0",
					"isFocused": "0",
					"name": null,
					"nativeFrame": "{{0, 0}, {1170, 2532}}",
					"children": [
						{
							"rect": {
								"y": 100,
								"x": 50,
								"width": 200,
								"height": 50
							},
							"label": "Tap Me",
							"type": "Button",
							"isEnabled": "1",
							"customField": "customValue"
						},
						{
							"rect": {
								"y": 160,
								"x": 50,
								"width": 200,
								"height": 50
							},
							"label": "Cancel",
							"type": "Button",
							"isEnabled": "0",
							"anotherField": 123
						}
					]
				}
			]
		}
	}`

	node, err := parseWDASourceToNode([]byte(jsonData))
	if err != nil {
		t.Fatalf("parseWDASourceToNode() error: %v", err)
	}
	if node == nil {
		t.Fatal("parseWDASourceToNode() returned nil")
		return
	}

	// Check root node
	if node.TagName != "Application" {
		t.Errorf("expected root TagName 'Application', got '%s'", node.TagName)
	}
	if node.ID != "n-1" {
		t.Errorf("expected root ID 'n-1', got '%s'", node.ID)
	}
	if node.Rect.X != 0 || node.Rect.Y != 0 || node.Rect.Width != 390 || node.Rect.Height != 844 {
		t.Errorf("unexpected root rect: %+v", node.Rect)
	}
	if node.Props["label"] != " " {
		t.Errorf("expected root label ' ', got '%s'", node.Props["label"])
	}
	if node.Props["isVisible"] != "1" {
		t.Errorf("expected root isVisible '1', got '%s'", node.Props["isVisible"])
	}
	// Check that null value is converted to empty string
	if node.Props["value"] != "" {
		t.Errorf("expected root value '' (empty string for null), got '%s'", node.Props["value"])
	}
	// Check that the WDA type is mirrored into class/type props
	if node.Props["class"] != "Application" {
		t.Errorf("expected root class 'Application', got '%s'", node.Props["class"])
	}
	if node.Props["type"] != "Application" {
		t.Errorf("expected root type 'Application', got '%s'", node.Props["type"])
	}

	// Check Window child
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
	window := node.Children[0]
	if window.TagName != "Window" {
		t.Errorf("expected window TagName 'Window', got '%s'", window.TagName)
	}
	if window.ID != "n-2" {
		t.Errorf("expected window ID 'n-2', got '%s'", window.ID)
	}
	if window.Rect.X != 0 || window.Rect.Y != 0 || window.Rect.Width != 1170 || window.Rect.Height != 2532 {
		t.Errorf("unexpected window rect: %+v", window.Rect)
	}
	if window.Props["isAccessible"] != "0" {
		t.Errorf("expected window isAccessible '0', got '%s'", window.Props["isAccessible"])
	}
	if window.Props["isFocused"] != "0" {
		t.Errorf("expected window isFocused '0', got '%s'", window.Props["isFocused"])
	}
	if window.Props["nativeFrame"] != "{{0, 0}, {1170, 2532}}" {
		t.Errorf("expected window nativeFrame '{{0, 0}, {1170, 2532}}', got '%s'", window.Props["nativeFrame"])
	}
	if window.Props["class"] != "Window" {
		t.Errorf("expected window class 'Window', got '%s'", window.Props["class"])
	}
	if window.Props["type"] != "Window" {
		t.Errorf("expected window type 'Window', got '%s'", window.Props["type"])
	}

	// Check Button grandchildren
	if len(window.Children) != 2 {
		t.Fatalf("expected 2 window children, got %d", len(window.Children))
	}

	btn1 := window.Children[0]
	if btn1.TagName != "Button" {
		t.Errorf("expected btn1 TagName 'Button', got '%s'", btn1.TagName)
	}
	if btn1.ID != "n-3" {
		t.Errorf("expected btn1 ID 'n-3', got '%s'", btn1.ID)
	}
	if btn1.Props["label"] != "Tap Me" {
		t.Errorf("expected btn1 label 'Tap Me', got '%s'", btn1.Props["label"])
	}
	if btn1.Props["isEnabled"] != "1" {
		t.Errorf("expected btn1 isEnabled '1', got '%s'", btn1.Props["isEnabled"])
	}
	if btn1.Props["customField"] != "customValue" {
		t.Errorf("expected btn1 customField 'customValue', got '%s'", btn1.Props["customField"])
	}
	if btn1.Rect.X != 50 || btn1.Rect.Y != 100 || btn1.Rect.Width != 200 || btn1.Rect.Height != 50 {
		t.Errorf("unexpected btn1 rect: %+v", btn1.Rect)
	}

	btn2 := window.Children[1]
	if btn2.TagName != "Button" {
		t.Errorf("expected btn2 TagName 'Button', got '%s'", btn2.TagName)
	}
	if btn2.ID != "n-4" {
		t.Errorf("expected btn2 ID 'n-4', got '%s'", btn2.ID)
	}
	if btn2.Props["label"] != "Cancel" {
		t.Errorf("expected btn2 label 'Cancel', got '%s'", btn2.Props["label"])
	}
	if btn2.Props["isEnabled"] != "0" {
		t.Errorf("expected btn2 isEnabled '0', got '%s'", btn2.Props["isEnabled"])
	}
	if btn2.Props["anotherField"] != "123" {
		t.Errorf("expected btn2 anotherField '123', got '%s'", btn2.Props["anotherField"])
	}
}

func TestWDAToXMLPreservesType(t *testing.T) {
	jsonData := `{
		"value": {
			"rect": {"y": 0, "x": 0, "width": 390, "height": 844},
			"label": "钱包",
			"type": "Application",
			"children": [
				{
					"rect": {"y": 0, "x": 0, "width": 390, "height": 844},
					"type": "Window",
					"children": [
						{
							"rect": {"y": 51, "x": 220, "width": 37, "height": 36},
							"label": "添加卡片",
							"type": "Button"
						}
					]
				}
			]
		}
	}`

	root, err := parseWDASourceToNode([]byte(jsonData))
	if err != nil {
		t.Fatalf("parseWDASourceToNode() error: %v", err)
	}
	xmlOut, err := root.ToXML()
	if err != nil {
		t.Fatalf("ToXML() error: %v", err)
	}
	t.Log(xmlOut)

	// The WDA type must be preserved: root tag is <Application> (not swallowed by
	// <hierarchy>), and the type is available both as tag name and attribute.
	if !strings.Contains(xmlOut, "<Application ") {
		t.Errorf("xml missing <Application> root tag: %s", xmlOut)
	}
	if !strings.Contains(xmlOut, `type="Application"`) {
		t.Errorf("xml missing type attribute on root: %s", xmlOut)
	}
	if !strings.Contains(xmlOut, "<Window ") {
		t.Errorf("xml missing <Window> tag: %s", xmlOut)
	}
	if !strings.Contains(xmlOut, "<Button ") {
		t.Errorf("xml missing <Button> tag: %s", xmlOut)
	}
}

func TestBuildSwipePayload(t *testing.T) {
	data, err := buildSwipePayload(0, 0, 100, 200, 0)
	if err != nil {
		t.Fatalf("buildSwipePayload() error: %v", err)
	}
	// x/y must be present even when zero so WDA does not reject the move.
	if !strings.Contains(string(data), `"x":0`) || !strings.Contains(string(data), `"y":0`) {
		t.Fatalf("zero coordinates were dropped: %s", data)
	}

	var req WDAActionsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Actions) != 1 {
		t.Fatalf("expected 1 pointer source, got %d", len(req.Actions))
	}
	actions := req.Actions[0].Actions
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d", len(actions))
	}
	if actions[0].Type != "pointerMove" || actions[1].Type != "pointerDown" || actions[3].Type != "pointerUp" {
		t.Errorf("unexpected action sequence: %+v", actions)
	}
	if actions[0].X == nil || *actions[0].X != 0 || actions[0].Y == nil || *actions[0].Y != 0 {
		t.Errorf("unexpected start point: %+v", actions[0])
	}
	if actions[2].Type != "pointerMove" {
		t.Errorf("expected drag move, got %s", actions[2].Type)
	}
	if actions[2].X == nil || *actions[2].X != 100 || *actions[2].Y != 200 {
		t.Errorf("unexpected end point: %+v", actions[2])
	}
	if actions[2].Duration != 300 {
		t.Errorf("expected default duration 300, got %d", actions[2].Duration)
	}
}

func TestBuildKeysPayload(t *testing.T) {
	data, err := buildKeysPayload("hello 世界")
	if err != nil {
		t.Fatalf("buildKeysPayload() error: %v", err)
	}
	var req WDAKeysRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Value) != 1 || req.Value[0] != "hello 世界" {
		t.Errorf("unexpected keys payload: %+v", req)
	}
}

func TestBuildPressKeyActions(t *testing.T) {
	data, err := buildPressKeyActions(KeyEnter, MetaMeta|MetaShift)
	if err != nil {
		t.Fatalf("buildPressKeyActions() error: %v", err)
	}
	var req WDAKeyActionsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Actions) != 1 || req.Actions[0].Type != "key" {
		t.Fatalf("expected a single key input source, got %+v", req.Actions)
	}
	actions := req.Actions[0].Actions
	// shift down, meta down, enter down, enter up, meta up, shift up
	if len(actions) != 6 {
		t.Fatalf("expected 6 key actions, got %d: %+v", len(actions), actions)
	}
	if actions[0].Value != wdaKeyShift || actions[0].Type != "keyDown" {
		t.Errorf("expected shift down first, got %+v", actions[0])
	}
	if actions[1].Value != wdaKeyMeta || actions[1].Type != "keyDown" {
		t.Errorf("expected meta down second, got %+v", actions[1])
	}
	if actions[2].Type != "keyDown" || actions[2].Value != "\uE007" {
		t.Errorf("expected enter down, got %+v", actions[2])
	}
	if actions[5].Value != wdaKeyShift || actions[5].Type != "keyUp" {
		t.Errorf("expected shift released last, got %+v", actions[5])
	}
}

func TestWdaKeyValueUnsupported(t *testing.T) {
	if _, err := wdaKeyValue(KeyMenu); err == nil {
		t.Error("expected error for unsupported key menu")
	}
	if v, err := wdaKeyValue(KeyName("z")); err != nil || v != "z" {
		t.Errorf("expected z to map to z, got %q err %v", v, err)
	}
}

func TestWdaHardwareButton(t *testing.T) {
	if name, ok := wdaHardwareButton(KeyHome); !ok || name != "home" {
		t.Errorf("expected home hardware button, got %q ok=%v", name, ok)
	}
	if _, ok := wdaHardwareButton(KeyBack); ok {
		t.Error("back should not be a hardware button")
	}
}
