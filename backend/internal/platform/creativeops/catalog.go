package creativeops

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// Definition describes an implemented application capability. Future actions
// must not be listed until a real invocation is wired by the application.
type Definition struct {
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

type Catalog struct{ definitions map[string]Definition }

var toolKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func NewCatalog(definitions []Definition) (*Catalog, error) {
	c := &Catalog{definitions: make(map[string]Definition, len(definitions))}
	for _, d := range definitions {
		if !toolKeyPattern.MatchString(d.Key) || d.Description == "" || d.SchemaVersion < 1 {
			return nil, ErrValidation
		}
		var input jsonschema.Schema
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Type != "object" {
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
		var output jsonschema.Schema
		if json.Unmarshal(d.OutputSchema, &output) != nil || output.Type != "object" {
			return nil, ErrValidation
		}
		if _, exists := c.definitions[d.Key]; exists {
			return nil, ErrConflict
		}
		d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
		d.OutputSchema = append(json.RawMessage(nil), d.OutputSchema...)
		c.definitions[d.Key] = d
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

// BindEino receives a trusted, per-invocation application closure. Operation/run
// identities and account authority are captured there, never taken from the model.
// Full Harness epoch/step guards are added by FND-07/08, not implied by this adapter.
func (c *Catalog) BindEino(key string, invoke func(context.Context, json.RawMessage) (Receipt, error)) (tool.InvokableTool, error) {
	d, ok := c.definitions[key]
	if !ok {
		return nil, ErrNotFound
	}
	if invoke == nil {
		return nil, ErrValidation
	}
	return &einoCommand{definition: d, invoke: invoke}, nil
}

type einoCommand struct {
	definition Definition
	invoke     func(context.Context, json.RawMessage) (Receipt, error)
}

func (t *einoCommand) Info(context.Context) (*schema.ToolInfo, error) {
	var input jsonschema.Schema
	if err := json.Unmarshal(t.definition.InputSchema, &input); err != nil {
		return nil, err
	}
	return &schema.ToolInfo{Name: t.definition.Key, Desc: t.definition.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&input)}, nil
}
func (t *einoCommand) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (string, error) {
	var payload map[string]any
	if err := Decode([]byte(arguments), &payload); err != nil {
		return "", err
	}
	receipt, err := t.invoke(ctx, json.RawMessage(arguments))
	if err != nil {
		return "", err
	}
	return string(receipt.Outcome.Response), nil
}

var _ tool.InvokableTool = (*einoCommand)(nil)
