package creativecanvas

import (
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
)

type NodeDefinition struct {
	TypeKey           string   `json:"type_key"`
	SchemaVersion     int      `json:"schema_version"`
	CreationSupported bool     `json:"creation_supported"`
	EditableFields    []string `json:"editable_fields"`
	Ports             []string `json:"ports"`
	Actions           []string `json:"actions"`
	PromptMode        string   `json:"prompt_mode"`
	Maximize          bool     `json:"maximize"`
}

// coreDefinitions owns built-in node behavior. Document nodes come from the
// creativedocument registry instead; a registered type never shadows a core one.
var coreDefinitions = []NodeDefinition{
	{TypeKey: "core.text", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "body", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference", "save_to_library"}, PromptMode: "draft"},
	{TypeKey: "core.link", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "url", "description", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference", "save_to_library"}, PromptMode: "draft"},
	{TypeKey: "core.image", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "caption", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference", "maximize", "download", "replace", "save_to_library"}, PromptMode: "draft", Maximize: true},
	{TypeKey: "core.video", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "caption", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference", "maximize", "download", "replace", "save_to_library"}, PromptMode: "draft", Maximize: true},
	{TypeKey: "core.audio", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "caption", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference", "download", "replace", "save_to_library"}, PromptMode: "draft"},
	{TypeKey: "core.group", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "placement"}, Ports: []string{}, Actions: []string{"move", "rename", "duplicate", "remove", "ungroup"}, PromptMode: "scope"},
}

// documentDefinition is the same for every document node: the canvas only
// places it, and the document is edited in its own maximized view.
func documentDefinition(t creativedocument.Type) NodeDefinition {
	return NodeDefinition{TypeKey: t.NodeTypeKey(), SchemaVersion: 1, CreationSupported: t.Creatable(), EditableFields: []string{"title", "intent", "placement"}, Ports: []string{}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "maximize"}, PromptMode: "unavailable", Maximize: true}
}

func coreDefinition(key string) (NodeDefinition, bool) {
	for _, d := range coreDefinitions {
		if d.TypeKey == key {
			return d, true
		}
	}
	return NodeDefinition{}, false
}

// documentType is the registered document type behind a node type.
func documentType(key string) (creativedocument.Type, bool) {
	if _, core := coreDefinition(key); core {
		return nil, false
	}
	return creativedocument.ForNode(key)
}

// NodeDefinitions lists core nodes, then document nodes by type key.
func NodeDefinitions() []NodeDefinition {
	out := append([]NodeDefinition{}, coreDefinitions...)
	types := creativedocument.NodeTypes()
	sort.Slice(types, func(i, j int) bool { return types[i].NodeTypeKey() < types[j].NodeTypeKey() })
	for _, t := range types {
		if _, core := coreDefinition(t.NodeTypeKey()); !core {
			out = append(out, documentDefinition(t))
		}
	}
	return out
}
func definition(key string) (NodeDefinition, bool) {
	if d, ok := coreDefinition(key); ok {
		return d, true
	}
	if t, ok := documentType(key); ok {
		return documentDefinition(t), true
	}
	return NodeDefinition{}, false
}
func knownType(key string) bool { _, ok := definition(key); return ok }
func hasPorts(key string) bool  { d, ok := definition(key); return ok && len(d.Ports) > 0 }

// MediaTypeKey maps a verified content kind to its node type.
func MediaTypeKey(kind string) string { return "core." + kind }
