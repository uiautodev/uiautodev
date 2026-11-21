package device

import (
	"errors"
	"strings"
	"testing"
)

// Sample minimal uiautomator dump XML for tests
const sampleXML = `<?xml version='1.0' encoding='utf-8' standalone='yes' ?>
<hierarchy rotation="0" width="1080" height="1920">
  <node index="0" text="" resource-id="" class="android.widget.FrameLayout$" package="com.example" content-desc="" bounds="[0,0][1080,1920]">
    <node index="0" text="Hello" resource-id="com.example:id/text" class="android.widget.TextView" package="com.example" content-desc="" bounds="[10,10][200,50]"/>
    <node index="1" text="" resource-id="com.example:id/btn" class="android.widget.Button" package="com.example" content-desc="btn-desc" bounds="[20,60][200,120]"/>
  </node>
</hierarchy>`

// Sample XML with class names as tags
const sampleXMLWithClassTags = `<?xml version='1.0' encoding='utf-8' standalone='yes' ?>
<hierarchy rotation="0" width="1080" height="1920">
  <android.widget.FrameLayout index="0" text="" resource-id="" class="android.widget.FrameLayout" package="com.example" content-desc="" bounds="[0,0][1080,1920]">
    <android.widget.TextView index="0" text="Hello" resource-id="com.example:id/text" class="android.widget.TextView" package="com.example" content-desc="" bounds="[10,10][200,50]"/>
    <android.widget.Button index="1" text="" resource-id="com.example:id/btn" class="android.widget.Button" package="com.example" content-desc="btn-desc" bounds="[20,60][200,120]"/>
  </android.widget.FrameLayout>
</hierarchy>`

func TestParseUIXML(t *testing.T) {
	root, err := ParseUIXML(sampleXML)
	if err != nil {
		t.Fatalf("ParseUIXML failed: %v", err)
	}
	if root == nil {
		t.Fatalf("root is nil")
		return
	}
	if root.TagName != "hierarchy" {
		t.Fatalf("unexpected root name: %s", root.TagName)
	}
	// one child
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 children, got %d", len(root.Children))
	}
	// first child text
	c0 := root.Children[0]
	if pkg, ok := c0.Props["package"]; !ok || pkg != "com.example" {
		t.Fatalf("unexpected child0 package: %#v", c0.Props["package"])
	}
	if len(c0.Children) != 2 {
		t.Fatalf("expected 2 grandchildren, got %d", len(c0.Children))
	}
	// second child resource-id and content-desc
	c1 := c0.Children[1]
	if rid, ok := c1.Props["resource-id"]; !ok || rid != "com.example:id/btn" {
		t.Fatalf("unexpected child1 resource-id: %#v", c1.Props["resource-id"])
	}
	if cd, ok := c1.Props["content-desc"]; !ok || cd != "btn-desc" {
		t.Fatalf("unexpected child1 content-desc: %#v", c1.Props["content-desc"])
	}
}

func TestParseUIXMLWithClassTags(t *testing.T) {
	root, err := ParseUIXML(sampleXMLWithClassTags)
	if err != nil {
		t.Fatalf("ParseUIXML with class tags failed: %v", err)
	}
	if root == nil {
		t.Fatalf("root is nil")
		return
	}
	// root should be the FrameLayout node
	if root.TagName != "hierarchy" {
		t.Fatalf("unexpected root name: %s", root.TagName)
	}
	// one child
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 children, got %d", len(root.Children))
	}
	// first child text
	c0 := root.Children[0]
	if pkg, ok := c0.Props["package"]; !ok || pkg != "com.example" {
		t.Fatalf("unexpected child0 package: %#v", c0.Props["package"])
	}
	if len(c0.Children) != 2 {
		t.Fatalf("expected 2 grandchildren, got %d", len(c0.Children))
	}
}

func TestNodeToXML(t *testing.T) {
	// Parse sample XML
	root, err := ParseUIXML(sampleXML)
	if err != nil {
		t.Fatalf("ParseUIXML failed: %v", err)
	}

	// Convert back to XML
	xmlOut, err := NodeToXML(root)
	if err != nil {
		t.Fatalf("NodeToXML failed: %v", err)
	}
	t.Log("Generated XML:\n", xmlOut)

	// Basic checks
	if !strings.Contains(xmlOut, "<hierarchy") {
		t.Fatalf("output missing <hierarchy> tag")
	}
	if !strings.Contains(xmlOut, "android.widget.FrameLayout") {
		t.Fatalf("output missing FrameLayout class")
	}
	if !strings.Contains(xmlOut, "android.widget.TextView") {
		t.Fatalf("output missing TextView class")
	}
	if !strings.Contains(xmlOut, `text="Hello"`) {
		t.Fatalf("output missing text attribute")
	}
	if !strings.Contains(xmlOut, `content-desc="btn-desc"`) {
		t.Fatalf("output missing content-desc attribute")
	}

	// Round-trip: parse the generated XML
	root2, err := ParseUIXML(xmlOut)
	if err != nil {
		t.Fatalf("round-trip ParseUIXML failed: %v", err)
	}
	if len(root2.Children) == 0 {
		t.Fatalf("round-trip lost children")
	}
}

func TestNodeToXMLWithCustomRootTag(t *testing.T) {
	// Non-hierarchy roots (e.g. iOS Application) are preserved with their own tag.
	root := &Node{
		ID:      "app-1",
		TagName: "Application",
		Rect:    Rect{X: 0, Y: 0, Width: 320, Height: 640},
		Props: map[string]string{
			"class": "Application",
			"label": "钱包",
		},
		Children: []Node{
			{
				ID:      "btn-1",
				TagName: "Button",
				Rect:    Rect{X: 10, Y: 10, Width: 100, Height: 50},
				Props:   map[string]string{"class": "Button", "name": "OK"},
			},
		},
	}

	xmlOut, err := NodeToXML(root)
	if err != nil {
		t.Fatalf("NodeToXML failed for custom root tag: %v", err)
	}
	if !strings.Contains(xmlOut, "<Application") {
		t.Fatalf("output missing <Application> root tag: %s", xmlOut)
	}
	if !strings.Contains(xmlOut, `label="钱包"`) {
		t.Fatalf("output missing root attribute: %s", xmlOut)
	}
	if !strings.Contains(xmlOut, "<Button ") {
		t.Fatalf("output missing <Button> child tag: %s", xmlOut)
	}
	if !strings.Contains(xmlOut, `name="OK"`) {
		t.Fatalf("output missing child attribute: %s", xmlOut)
	}
}

func TestNodeToXMLWithInvalidClassName(t *testing.T) {
	// Create a root node with a child that has an invalid class name
	childNode := Node{
		ID:      "test-1",
		TagName: "node",
		Rect:    Rect{X: 0, Y: 0, Width: 100, Height: 100},
		Props: map[string]string{
			"class": "123invalid.Class", // Invalid: starts with digit
		},
		Children: []Node{},
	}

	root := &Node{
		ID:       "h-1",
		TagName:  "hierarchy",
		Rect:     Rect{X: 0, Y: 0, Width: 100, Height: 100},
		Props:    map[string]string{},
		Children: []Node{childNode},
	}

	xmlOut, err := NodeToXML(root)
	if err != nil {
		t.Fatalf("NodeToXML failed: %v", err)
	}
	t.Log("Generated XML with invalid class:\n", xmlOut)

	// Should use "node" as tag name instead of the invalid class
	if !strings.Contains(xmlOut, "<node ") {
		t.Fatalf("expected <node> tag for invalid class name, got: %s", xmlOut)
	}
}

func TestFindNodesByXPathInTree(t *testing.T) {
	// Create a test tree structure
	testTree := &Node{
		ID:      "root",
		TagName: "hierarchy",
		Rect:    Rect{X: 0, Y: 0, Width: 1080, Height: 1920},
		Props:   map[string]string{},
		Children: []Node{
			{
				ID:      "n-1",
				TagName: "android.widget.Button",
				Rect:    Rect{X: 100, Y: 100, Width: 200, Height: 100},
				Props: map[string]string{
					"text": "OK",
				},
				Children: []Node{},
			},
			{
				ID:      "n-2",
				TagName: "android.widget.TextView",
				Rect:    Rect{X: 100, Y: 200, Width: 300, Height: 80},
				Props: map[string]string{
					"text": "Hello World",
				},
				Children: []Node{},
			},
			{
				ID:      "n-3",
				TagName: "android.widget.Button",
				Rect:    Rect{X: 100, Y: 300, Width: 200, Height: 100},
				Props: map[string]string{
					"text": "Cancel",
				},
				Children: []Node{},
			},
		},
	}

	tests := []struct {
		name        string
		xpath       string
		wantCount   int
		wantIDs     []string
		expectError bool
	}{
		{
			name:        "find by text attribute",
			xpath:       "//node[@text='OK']",
			wantCount:   1,
			wantIDs:     []string{"n-1"},
			expectError: false,
		},
		{
			name:        "find by tag name - node",
			xpath:       "//node",
			wantCount:   3,
			wantIDs:     []string{"n-1", "n-2", "n-3"},
			expectError: false,
		},
		{
			name:        "find by text attribute",
			xpath:       "//node[@text='Hello World']",
			wantCount:   1,
			wantIDs:     []string{"n-2"},
			expectError: false,
		},
		{
			name:        "find with contains",
			xpath:       "//node[contains(@text, 'World')]",
			wantCount:   1,
			wantIDs:     []string{"n-2"},
			expectError: false,
		},
		{
			name:        "no matches",
			xpath:       "//node[@text='NotFound']",
			wantCount:   0,
			wantIDs:     []string{},
			expectError: false,
		},
		{
			name:        "invalid xpath",
			xpath:       "//node[invalid",
			wantCount:   0,
			wantIDs:     []string{},
			expectError: true,
		},
		{
			name:        "find all nodes using wildcard",
			xpath:       "//*",
			wantCount:   3,
			wantIDs:     []string{"n-1", "n-2", "n-3"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FindNodesByXPathInTree(testTree, tt.xpath)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if !errors.Is(err, ErrInvalidXPath) {
					t.Errorf("expected ErrInvalidXPath, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.wantCount {
				t.Errorf("got %d nodes, want %d", len(result), tt.wantCount)
			}

			// Check IDs match
			gotIDs := make([]string, len(result))
			for i, node := range result {
				gotIDs[i] = node.ID
			}

			if tt.wantCount > 0 && !equalStringSlices(gotIDs, tt.wantIDs) {
				t.Errorf("got IDs %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestExtractIDFromXML(t *testing.T) {
	tests := []struct {
		name     string
		xmlInput string
		wantID   string
	}{
		{
			name:     "extract id from node XML",
			xmlInput: `<node _id="n-1" class="android.widget.Button" bounds="[100,100][300,200]"/>`,
			wantID:   "n-1",
		},
		{
			name:     "no id attribute",
			xmlInput: `<node class="android.widget.Button" bounds="[100,100][300,200]"/>`,
			wantID:   "",
		},
		{
			name:     "empty XML",
			xmlInput: "",
			wantID:   "",
		},
		{
			name:     "invalid XML",
			xmlInput: "<node>",
			wantID:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIDFromXML(tt.xmlInput)
			if got != tt.wantID {
				t.Errorf("extractIDFromXML() = %v, want %v", got, tt.wantID)
			}
		})
	}
}

func TestCollectNodesInOrder(t *testing.T) {
	// Create a test tree
	testTree := &Node{
		ID:      "root",
		TagName: "hierarchy",
		Children: []Node{
			{ID: "n-1", TagName: "Node1", Children: []Node{}},
			{ID: "n-2", TagName: "Node2", Children: []Node{}},
			{ID: "n-3", TagName: "Node3", Children: []Node{}},
		},
	}

	// Test collecting all nodes
	idSet := map[string]bool{"n-1": true, "n-3": true}
	var result []Node
	collectNodesInOrder(testTree, idSet, &result)

	if len(result) != 2 {
		t.Errorf("got %d nodes, want 2", len(result))
	}

	// Check order is preserved
	if result[0].ID != "n-1" || result[1].ID != "n-3" {
		t.Errorf("nodes not in correct order, got %v", []string{result[0].ID, result[1].ID})
	}

	// Test empty set
	result = []Node{}
	idSet = map[string]bool{}
	collectNodesInOrder(testTree, idSet, &result)

	if len(result) != 0 {
		t.Errorf("got %d nodes, want 0", len(result))
	}
}

// Helper function to compare string slices
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
