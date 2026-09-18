// Package einoadapter binds Eino to the same application tool session as HTTP.
// Framework types stay at this edge, outside the tool catalog and domain code.
package einoadapter

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

type boundTool struct {
	session *creativeops.ToolSession
	key     string
	version int
}

func Bind(session *creativeops.ToolSession, key string, version int) (tool.InvokableTool, error) {
	if session == nil || session.Entrypoint() != creativeops.Agent || key == "" || version < 1 {
		return nil, creativeops.ErrValidation
	}
	return &boundTool{session: session, key: key, version: version}, nil
}
func (t *boundTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	d, err := t.session.Describe(ctx, t.key, t.version)
	if err != nil {
		return nil, err
	}
	var input jsonschema.Schema
	if err = json.Unmarshal(d.InputSchema, &input); err != nil {
		return nil, err
	}
	return &schema.ToolInfo{Name: d.Key, Desc: d.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&input)}, nil
}
func (t *boundTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	result, err := t.session.Invoke(ctx, t.key, t.version, creativeops.Command{Payload: json.RawMessage(raw)})
	if err != nil {
		return "", err
	}
	return string(result.Response()), nil
}

var _ tool.InvokableTool = (*boundTool)(nil)
