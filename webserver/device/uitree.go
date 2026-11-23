package device

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/antchfx/xmlquery"
	goxpath "github.com/antchfx/xpath"
	"github.com/pkg/errors"
)

const defaultNodeTagName = "node"
const defaultRootTagName = "hierarchy"

// xmlIDAttr is the attribute name used to carry the generated node ID when
// serializing a Node tree to XML. It is prefixed with "_" to distinguish it
// from data attributes coming from the source (Android/WDA/Harmony).
const xmlIDAttr = "_id"

// ErrNoNodesFound is returned by FindElementsByXpath when the XPath matches no nodes.
var ErrNoNodesFound = errors.New("no nodes found for the given xpath")

// ErrInvalidXPath is returned when the XPath expression is invalid.
var ErrInvalidXPath = errors.New("invalid xpath expression")

// Rect represents a rectangle on screen.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Node represents a UI node returned by uiautomator dump
type Node struct {
	ID       string            `json:"id"`
	TagName  string            `json:"tagName"`
	Rect     Rect              `json:"rect"`
	Props    map[string]string `json:"props"`
	Children []Node            `json:"children,omitempty"`
}

func (node Node) ToXML() (string, error) {
	return NodeToXML(&node)
}

// ParseUIXML parses a uiautomator window_dump XML and returns the root Node.
func ParseUIXML(xmlContent string) (*Node, error) {
	// Define XML structures matching uiautomator output
	type xmlNode struct {
		XMLName  xml.Name   `xml:""`
		Class    string     `xml:"class,attr"`
		Bounds   string     `xml:"bounds,attr"`
		Children []xmlNode  `xml:",any"`
		Attrs    []xml.Attr `xml:",any,attr"`
	}
	type hierarchy struct {
		XMLName xml.Name  `xml:"hierarchy"`
		Width   int       `xml:"width,attr"`
		Height  int       `xml:"height,attr"`
		Nodes   []xmlNode `xml:",any"`
	}

	var h hierarchy
	if err := xml.Unmarshal([]byte(xmlContent), &h); err != nil {
		return nil, errors.Wrap(err, "failed to parse uiautomator xml")
	}

	idCounter := 0
	makeID := func() string {
		idCounter++
		return fmt.Sprintf("n-%d", idCounter)
	}

	parseBounds := func(b string) Rect {
		var r Rect
		b = strings.TrimSpace(b)
		if b == "" {
			return r
		}
		re := regexp.MustCompile(`\[(\d+),(\d+)\]\[(\d+),(\d+)\]`)
		if m := re.FindStringSubmatch(b); len(m) == 5 {
			left, _ := strconv.Atoi(m[1])
			top, _ := strconv.Atoi(m[2])
			right, _ := strconv.Atoi(m[3])
			bottom, _ := strconv.Atoi(m[4])
			r.X = left
			r.Y = top
			r.Width = right - left
			r.Height = bottom - top
		}
		return r
	}

	var convert func(x xmlNode) Node
	convert = func(x xmlNode) Node {
		n := Node{
			ID:      makeID(),
			TagName: defaultNodeTagName,
			Props:   map[string]string{},
		}
		if x.Bounds != "" {
			n.Rect = parseBounds(x.Bounds)
			n.Props["bounds"] = x.Bounds
		}
		// tagName from class attribute if present
		if x.Class != "" {
			n.TagName = x.Class
			n.Props["class"] = x.Class
		}
		if x.XMLName.Local != defaultNodeTagName {
			n.TagName = x.XMLName.Local
		}
		n.Props["class"] = n.TagName

		// Add all attributes generically (overrides duplicates with same name)
		for _, a := range x.Attrs {
			if a.Name.Local != "" {
				n.Props[a.Name.Local] = a.Value
			}
		}

		for _, c := range x.Children {
			child := convert(c)
			n.Children = append(n.Children, child)
		}
		return n
	}

	root := &Node{ID: makeID(),
		TagName:  defaultRootTagName,
		Rect:     Rect{X: 0, Y: 0, Width: h.Width, Height: h.Height},
		Props:    map[string]string{},
		Children: []Node{}}
	for _, xn := range h.Nodes {
		root.Children = append(root.Children, convert(xn))
	}

	return root, nil
}

// isValidXMLName checks if the name matches XML name regex: ^(?![Xx][Mm][Ll])[A-Za-z_][A-Za-z0-9_.-]*$
func isValidXMLName(name string) bool {
	if name == "" {
		return false
	}
	// Check if starts with XML (case insensitive)
	if strings.HasPrefix(strings.ToUpper(name), "XML") {
		return false
	}
	// XML name regex
	reg := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	return reg.MatchString(name)
}

// xmlNode is a helper type for XML marshaling with dynamic tag names
type xmlNode struct {
	Attrs    []xml.Attr
	Children []xmlNode
}

// MarshalXML implements xml.Marshaler interface for xmlNode
func (xn xmlNode) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// Determine tag name from class attribute or use "node"
	tagName := defaultNodeTagName
	for _, attr := range xn.Attrs {
		if attr.Name.Local == "class" && isValidXMLName(attr.Value) {
			tagName = attr.Value
			break
		}
	}
	if tagName != defaultNodeTagName {
		// Remove class attribute to avoid duplication
		var filteredAttrs []xml.Attr
		for _, attr := range xn.Attrs {
			if attr.Name.Local != "class" {
				filteredAttrs = append(filteredAttrs, attr)
			}
		}
		xn.Attrs = filteredAttrs
	}

	// Create start element with dynamic tag name
	start.Name.Local = tagName
	start.Attr = xn.Attrs

	if err := e.EncodeToken(start); err != nil {
		return err
	}

	// Encode children
	for _, child := range xn.Children {
		if err := e.Encode(child); err != nil {
			return err
		}
	}

	// End element
	if err := e.EncodeToken(start.End()); err != nil {
		return err
	}

	return nil
}

// NodeToXML converts a Node tree back to XML format (uiautomator dump compatible).
func NodeToXML(node *Node) (string, error) {
	if node == nil {
		return "", errors.New("node is nil")
	}

	var convertToXML func(n Node, isRoot bool) xmlNode
	convertToXML = func(n Node, isRoot bool) xmlNode {
		xn := xmlNode{
			Attrs:    []xml.Attr{},
			Children: []xmlNode{},
		}

		// Add generated ID attribute first
		xn.Attrs = append(xn.Attrs, xml.Attr{Name: xml.Name{Local: xmlIDAttr}, Value: n.ID})

		// Add the rect for non-root nodes to ease XML -> Node conversion
		if !isRoot {
			xn.Attrs = append(xn.Attrs, xml.Attr{Name: xml.Name{Local: "_rect"}, Value: rectToStr(n.Rect)})
		}

		// Add all props as attributes (Props are strings)
		for k, v := range n.Props {
			xn.Attrs = append(xn.Attrs, xml.Attr{Name: xml.Name{Local: k}, Value: v})
		}
		// Convert children
		for _, child := range n.Children {
			xn.Children = append(xn.Children, convertToXML(child, false))
		}
		return xn
	}

	var root xmlNode
	if node.TagName == defaultRootTagName {
		// uiautomator-style hierarchy root. The "class" attribute is consumed by
		// MarshalXML to emit the <hierarchy> tag.
		root = xmlNode{
			Attrs:    []xml.Attr{{Name: xml.Name{Local: "class"}, Value: defaultRootTagName}},
			Children: []xmlNode{},
		}
		for _, child := range node.Children {
			root.Children = append(root.Children, convertToXML(child, false))
		}
	} else {
		// Non-hierarchy roots (e.g. iOS Application) are serialized as-is,
		// preserving their tag name and attributes.
		root = convertToXML(*node, true)
	}

	// Generated size attribute on the root element, prefixed with "_" to
	// distinguish it from data attributes coming from the source.
	root.Attrs = ensureAttr(root.Attrs, "_size", fmt.Sprintf("%dx%d", node.Rect.Width, node.Rect.Height))

	// Marshal to XML with indentation
	output, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal XML")
	}

	// Add XML declaration
	xmlStr := xml.Header + string(output)
	return xmlStr, nil
}

// ensureAttr appends the attribute name=value to attrs unless an attribute with
// the same name already exists.
func ensureAttr(attrs []xml.Attr, name, value string) []xml.Attr {
	for _, a := range attrs {
		if a.Name.Local == name {
			return attrs
		}
	}
	return append(attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}

// rectToStr formats a Rect as "x,y,width,height".
func rectToStr(r Rect) string {
	return fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.Width, r.Height)
}

func FindElementsByXpath(xmlContent string, xpath string) ([]string, error) {
	doc, err := xmlquery.Parse(strings.NewReader(xmlContent))
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse XML content")
	}
	_, err = goxpath.Compile(xpath) // Validate XPath
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidXPath, err)
	}
	nodes := xmlquery.Find(doc, xpath)
	if len(nodes) == 0 {
		return nil, ErrNoNodesFound
	}

	var results []string
	for _, node := range nodes {
		results = append(results, node.OutputXML(true))
	}
	return results, nil
}

// FindNodesByXPathInTree finds nodes matching XPath within a Node tree
func FindNodesByXPathInTree(tree *Node, xpath string) ([]Node, error) {
	// Convert tree to XML with ID attributes
	xmlContent, err := tree.ToXML()
	if err != nil {
		return nil, err
	}

	// Query using XPath
	matchedXMLs, err := FindElementsByXpath(xmlContent, xpath)
	if err != nil {
		// If no nodes found, return empty array (not an error)
		if errors.Is(err, ErrNoNodesFound) {
			return []Node{}, nil
		}
		return nil, err
	}

	// Extract IDs from matched XML
	matchedIDSet := make(map[string]bool)
	for _, xmlStr := range matchedXMLs {
		id := extractIDFromXML(xmlStr)
		if id != "" {
			matchedIDSet[id] = true
		}
	}

	if len(matchedIDSet) == 0 {
		return []Node{}, nil
	}

	// Collect matched nodes preserving order from tree
	var matchedNodes []Node
	collectNodesInOrder(tree, matchedIDSet, &matchedNodes)

	// Clear children to avoid returning large subtrees for each match.
	for i := range matchedNodes {
		matchedNodes[i].Children = nil
	}

	return matchedNodes, nil
}

// extractIDFromXML extracts the generated ID attribute (_id) from a node XML string
func extractIDFromXML(xmlStr string) string {
	doc, err := xmlquery.Parse(strings.NewReader(xmlStr))
	if err != nil {
		return ""
	}
	// Find the first element node (skip non-element nodes like XML declaration)
	node := doc.FirstChild
	for node != nil && node.Type != xmlquery.ElementNode {
		node = node.NextSibling
	}
	if node != nil {
		for _, attr := range node.Attr {
			if attr.Name.Local == xmlIDAttr {
				return attr.Value
			}
		}
	}
	return ""
}

// collectNodesInOrder collects nodes in tree traversal order if their ID is in the set
func collectNodesInOrder(node *Node, idSet map[string]bool, result *[]Node) {
	if idSet[node.ID] {
		*result = append(*result, *node)
	}
	for i := range node.Children {
		collectNodesInOrder(&node.Children[i], idSet, result)
	}
}
