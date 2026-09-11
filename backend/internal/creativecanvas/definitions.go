package creativecanvas

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

// This catalog owns node behavior. Internal documents are readable extensions,
// deliberately absent from the production creation menu.
func NodeDefinitions() []NodeDefinition {
	return []NodeDefinition{
		{TypeKey: "core.text", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "body", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference"}, PromptMode: "draft"},
		{TypeKey: "core.link", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "url", "description", "placement"}, Ports: []string{"output", "reference"}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "edit", "reference"}, PromptMode: "draft"},
		{TypeKey: "core.group", SchemaVersion: 1, CreationSupported: true, EditableFields: []string{"title", "intent", "placement"}, Ports: []string{}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "ungroup"}, PromptMode: "scope"},
		{TypeKey: "internal.document", SchemaVersion: 1, CreationSupported: false, EditableFields: []string{"title", "intent", "placement"}, Ports: []string{}, Actions: []string{"move", "resize", "rename", "duplicate", "remove", "maximize"}, PromptMode: "unavailable", Maximize: true},
	}
}
func definition(key string) (NodeDefinition, bool) {
	for _, d := range NodeDefinitions() {
		if d.TypeKey == key {
			return d, true
		}
	}
	return NodeDefinition{}, false
}
func knownType(key string) bool { _, ok := definition(key); return ok }
func hasPorts(key string) bool  { d, ok := definition(key); return ok && len(d.Ports) > 0 }
