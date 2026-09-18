package creativeops

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

// Definition describes an implemented application capability. Future actions
// must not be listed until a real invocation is wired by the application.
type Definition struct {
	Category           string
	Key                string
	Description        string
	SchemaVersion      int
	InputSchema        json.RawMessage
	Kind               string
	RequiredCapability string
	OutputSchema       json.RawMessage
	Undoable           bool
	MayCharge          bool
	MayEgress          bool
}

type Catalog struct {
	definitions map[string]Definition
	inputs      map[string]*openapi3.Schema
	outputs     map[string]*openapi3.Schema
}

var toolKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func NewCatalog(definitions []Definition) (*Catalog, error) {
	c := &Catalog{definitions: make(map[string]Definition, len(definitions)), inputs: map[string]*openapi3.Schema{}, outputs: map[string]*openapi3.Schema{}}
	for _, d := range definitions {
		if !toolKeyPattern.MatchString(d.Key) || d.Description == "" || d.SchemaVersion < 1 {
			return nil, ErrValidation
		}
		input, err := compileSchema(d.InputSchema)
		if err != nil {
			return nil, ErrValidation
		}
		if d.Kind != "query" && d.Kind != "command" && d.Kind != "execution" {
			return nil, ErrValidation
		}
		switch d.RequiredCapability {
		case "creative_read", "manual_write", "media_write", "agent_start", "node_generate", "generation_apply", "gc_delete":
		default:
			return nil, ErrValidation
		}
		output, err := compileSchema(d.OutputSchema)
		if err != nil {
			return nil, ErrValidation
		}
		if _, exists := c.definitions[d.Key]; exists {
			return nil, ErrConflict
		}
		d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
		d.OutputSchema = append(json.RawMessage(nil), d.OutputSchema...)
		c.definitions[d.Key] = d
		c.inputs[d.Key], c.outputs[d.Key] = input, output
	}
	return c, nil
}
func (c *Catalog) List() []Definition {
	result := make([]Definition, 0, len(c.definitions))
	for _, d := range c.definitions {
		d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
		d.OutputSchema = append(json.RawMessage(nil), d.OutputSchema...)
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// Tool schemas are standalone OpenAPI 3.0 schema objects. The build generator
// resolves references; runtime compilation never loads files or network URLs.
func compileSchema(raw json.RawMessage) (*openapi3.Schema, error) {
	var schema openapi3.Schema
	if err := json.Unmarshal(raw, &schema); err != nil || schema.Type == nil || !schema.Type.Is("object") {
		return nil, ErrValidation
	}
	if err := schema.Validate(context.Background()); err != nil {
		return nil, ErrValidation
	}
	return &schema, nil
}
