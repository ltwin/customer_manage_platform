// creative-tool-schema-gen derives tool schemas from the existing public routes.
// No second hand-maintained input/output schema is introduced by the tool layer.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func generate() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: creative-tool-schema-gen openapi.yaml output.go")
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(os.Args[1])
	if err != nil {
		return err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var root map[string]any
	if err = json.Unmarshal(raw, &root); err != nil {
		return err
	}
	var output bytes.Buffer
	output.WriteString("// Code generated from api/openapi.yaml by creative-tool-schema-gen. DO NOT EDIT.\npackage creativecanvas\n\n")
	routes := []struct{ name, path string }{
		{"readCanvas", "/creative/canvases/{id}"},
		{"readNodeVersions", "/creative/canvases/{id}/nodes/{node_id}/versions"},
		{"readNodeExecution", "/creative/canvases/{id}/executions/{execution_id}"},
	}
	for _, route := range routes {
		item := doc.Paths.Value(route.path)
		if item == nil || item.Get == nil {
			return fmt.Errorf("missing GET %s", route.path)
		}
		properties := map[string]any{}
		required := []string{}
		params := append(append(openapi3.Parameters(nil), item.Parameters...), item.Get.Parameters...)
		for _, ref := range params {
			p := ref.Value
			if p == nil || p.In != "path" || p.Schema == nil {
				return fmt.Errorf("unsupported tool route parameter: %s", route.path)
			}
			encoded, err := json.Marshal(p.Schema)
			if err != nil {
				return err
			}
			var value any
			if err = json.Unmarshal(encoded, &value); err != nil {
				return err
			}
			value, err = expand(value, root, map[string]bool{})
			if err != nil {
				return err
			}
			properties[p.Name] = value
			if p.Required {
				required = append(required, p.Name)
			}
		}
		sort.Strings(required)
		in := map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
		response := item.Get.Responses.Value("200")
		if response == nil || response.Value == nil {
			return fmt.Errorf("missing success schema: %s", route.path)
		}
		media := response.Value.Content["application/json"]
		if media == nil || media.Schema == nil {
			return fmt.Errorf("missing JSON schema: %s", route.path)
		}
		encoded, err := json.Marshal(media.Schema)
		if err != nil {
			return err
		}
		var value any
		if err = json.Unmarshal(encoded, &value); err != nil {
			return err
		}
		out, err := expand(value, root, map[string]bool{})
		if err != nil {
			return err
		}
		for _, entry := range []struct {
			suffix string
			value  any
		}{{"Input", in}, {"Output", out}} {
			raw, err := json.Marshal(entry.value)
			if err != nil {
				return err
			}
			fmt.Fprintf(&output, "const %s%sSchema = %q\n", route.name, entry.suffix, string(raw))
		}
	}
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return err
	}
	return os.WriteFile(os.Args[2], formatted, 0o644)
}

// The selected schemas are finite. Recursive or external references fail the
// build instead of exposing unresolved schemas or fetching arbitrary resources.
func expand(value any, root map[string]any, stack map[string]bool) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		if target, ok := v["$ref"].(string); ok {
			if !strings.HasPrefix(target, "#/") || stack[target] {
				return nil, fmt.Errorf("unsupported reference %s", target)
			}
			var resolved any = root
			for _, part := range strings.Split(strings.TrimPrefix(target, "#/"), "/") {
				object, ok := resolved.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid reference %s", target)
				}
				part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
				resolved, ok = object[part]
				if !ok {
					return nil, fmt.Errorf("missing reference %s", target)
				}
			}
			stack[target] = true
			result, err := expand(resolved, root, stack)
			delete(stack, target)
			return result, err
		}
		result := map[string]any{}
		for key, child := range v {
			expanded, err := expand(child, root, stack)
			if err != nil {
				return nil, err
			}
			result[key] = expanded
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, child := range v {
			expanded, err := expand(child, root, stack)
			if err != nil {
				return nil, err
			}
			result[i] = expanded
		}
		return result, nil
	default:
		return value, nil
	}
}
