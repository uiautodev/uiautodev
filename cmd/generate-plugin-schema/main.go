package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: generate-plugin-schema <plugin.go>")
		os.Exit(1)
	}
	srcFile := os.Args[1]

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcFile, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	stringEnums := collectStringEnums(f)

	var fields []schemaField
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "PluginManifest" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			if len(f.Names) == 0 {
				continue
			}
			name := f.Names[0].Name
			jsonName, omitempty := parseJSONTag(f.Tag)
			if jsonName == "-" {
				continue
			}
			if jsonName == "" {
				jsonName = strings.ToLower(name)
			}
			schemaType, items := goTypeToSchema(f.Type)
			var enumValues []string
			if ident, ok := f.Type.(*ast.Ident); ok {
				enumValues = stringEnums[ident.Name]
			}
			desc := ""
			if f.Comment != nil {
				desc = strings.TrimSpace(f.Comment.Text())
			}
			fields = append(fields, schemaField{
				GoName:     name,
				JSONName:   jsonName,
				OmitEmpty:  omitempty,
				SchemaType: schemaType,
				Items:      items,
				Enum:       enumValues,
				Required:   !omitempty,
				Desc:       desc,
			})
		}
		return false
	})

	// Build schema
	props := make(map[string]any)
	var required []string
	for _, f := range fields {
		prop := map[string]any{"type": f.SchemaType}
		if f.Desc != "" {
			prop["description"] = f.Desc
		}
		if len(f.Enum) > 0 {
			prop["enum"] = f.Enum
		}
		if f.Items != nil {
			prop["items"] = f.Items
		}
		if f.SchemaType == "array" && f.Items != nil {
			if enum, ok := f.Items["enum"]; ok {
				prop["items"] = map[string]any{"type": "string", "enum": enum}
			}
		}
		props[f.JSONName] = prop
		if f.Required {
			required = append(required, f.JSONName)
		}
	}

	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       "UIAutoDev Plugin Manifest",
		"type":        "object",
		"properties":  props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(1)
	}
	out = append(out, '\n')

	dir := filepath.Dir(srcFile)
	outFile := filepath.Join(dir, "plugin.schema.json")
	if err := os.WriteFile(outFile, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("generated %s\n", outFile)
}

type schemaField struct {
	GoName     string
	JSONName   string
	OmitEmpty  bool
	SchemaType string
	Items      map[string]any
	Enum       []string
	Required   bool
	Desc       string
}

func parseJSONTag(tag *ast.BasicLit) (name string, omitempty bool) {
	if tag == nil {
		return "", false
	}
	v := strings.Trim(tag.Value, "`")
	for _, part := range strings.Split(v, " ") {
		if !strings.HasPrefix(part, "json:") {
			continue
		}
		val := strings.Trim(part[5:], "\"")
		opts := strings.Split(val, ",")
		name = opts[0]
		omitempty = slices.Contains(opts, "omitempty")
		return
	}
	return "", false
}

func goTypeToSchema(expr ast.Expr) (schemaType string, items map[string]any) {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "string", nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64":
			return "integer", nil
		case "bool":
			return "boolean", nil
		default:
			return "string", nil
		}
	case *ast.ArrayType:
		inner, _ := goTypeToSchema(t.Elt)
		items = map[string]any{"type": inner}
		return "array", items
	case *ast.SelectorExpr:
		return "string", nil
	default:
		return "string", nil
	}
}

// collectStringEnums collects custom string types and their const values for enum generation.
// It finds "type Foo string" declarations and const values of that type.
func collectStringEnums(f *ast.File) map[string][]string {
	stringTypes := make(map[string]bool)
	enumValues := make(map[string][]string)

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch genDecl.Tok {
		case token.TYPE:
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if ident, ok := typeSpec.Type.(*ast.Ident); ok && ident.Name == "string" {
					stringTypes[typeSpec.Name.Name] = true
				}
			}
		case token.CONST:
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if valueSpec.Type == nil {
					continue
				}
				ident, ok := valueSpec.Type.(*ast.Ident)
				if !ok || !stringTypes[ident.Name] {
					continue
				}
				for _, val := range valueSpec.Values {
					if lit, ok := val.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						enumValues[ident.Name] = append(enumValues[ident.Name], v)
					}
				}
			}
		}
	}
	return enumValues
}
