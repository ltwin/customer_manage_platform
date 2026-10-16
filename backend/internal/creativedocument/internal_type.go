package creativedocument

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	InternalTypeKey = "internal.document"
	internalBody    = "body"
	previewRunes    = 200
)

// internalType is the built-in plain-text document. It can be read, edited
// and placed back on a canvas, but never created from the production menu.
// It registers with the package so every process and test sees it.
type internalType struct{}

func init() { Register(internalType{}) }

func (internalType) Key() string         { return InternalTypeKey }
func (internalType) NodeTypeKey() string { return InternalTypeKey }
func (internalType) ContentKind() string { return "text" }
func (internalType) Creatable() bool     { return false }

func (internalType) Empty(context.Context, store.TxAccountScope, string, json.RawMessage) (Snapshot, error) {
	return Snapshot{}, creativeops.Explain(creativeops.ErrValidation, "不能新建内部文档")
}

func (internalType) Units(creativecontent.Payload) ([]string, error) {
	return []string{internalBody}, nil
}

type setBodyOp struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

func decodeSetBody(raw json.RawMessage) (setBodyOp, error) {
	var op setBodyOp
	if err := creativeops.Decode(raw, &op); err != nil {
		return op, err
	}
	if op.Op != "set_body" {
		return op, creativeops.Explain(creativeops.ErrValidation, "内部文档不支持操作 %q", op.Op)
	}
	if n := utf8.RuneCountInString(op.Text); n == 0 || n > 100000 {
		return op, creativeops.Explain(creativeops.ErrValidation, "正文长度须在 1 到 100000 字之间")
	}
	return op, nil
}

func (internalType) Touched(_ context.Context, _ store.TxAccountScope, _ Current, _ Actor, ops []json.RawMessage) ([]string, error) {
	out := make([]string, 0, len(ops))
	for _, raw := range ops {
		if _, err := decodeSetBody(raw); err != nil {
			return nil, err
		}
		out = append(out, internalBody)
	}
	return out, nil
}

func (internalType) Apply(_ context.Context, _ store.TxAccountScope, cur Current, _ Actor, _ Retain, ops []json.RawMessage) (Snapshot, Changes, error) {
	payload := cur.Content.Payload
	for _, raw := range ops {
		op, err := decodeSetBody(raw)
		if err != nil {
			return Snapshot{}, Changes{}, err
		}
		text := op.Text
		payload.Body = &text
	}
	return Snapshot{Payload: payload}, Changes{Changed: []string{internalBody}}, nil
}

func (internalType) RestoreTouched(_ context.Context, _ store.TxAccountScope, _, _ Current, units []string) ([]string, error) {
	for _, u := range units {
		if u != internalBody {
			return nil, creativeops.Explain(creativeops.ErrValidation, "内部文档没有单元 %q", u)
		}
	}
	return units, nil
}

func (internalType) RestoreUnits(_ context.Context, _ store.TxAccountScope, old, _ Current, _ Retain, _ []string) (Snapshot, Changes, error) {
	return Snapshot{Payload: old.Content.Payload}, Changes{Changed: []string{internalBody}}, nil
}

func (internalType) View(_ context.Context, _ store.ReadTxAccountScope, cur Current, units []string) (json.RawMessage, error) {
	if units == nil {
		return json.Marshal(map[string]any{"content": cur.Content})
	}
	out := map[string]any{}
	for _, u := range units {
		if u == internalBody && cur.Content.Payload.Body != nil {
			out[internalBody] = *cur.Content.Payload.Body
		}
	}
	return json.Marshal(out)
}

func (internalType) Summary(_ context.Context, _ store.ReadTxAccountScope, cur Current) (json.RawMessage, error) {
	text := ""
	if cur.Content.Payload.Body != nil {
		text = *cur.Content.Payload.Body
	}
	if utf8.RuneCountInString(text) > previewRunes {
		text = string([]rune(text)[:previewRunes])
	}
	return json.Marshal(map[string]string{"preview": text})
}
