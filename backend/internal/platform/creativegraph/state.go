// Package creativegraph defines canonical field state for canvas undo and migration.
package creativegraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
)

type Head struct {
	Token string          `json:"state_token"`
	Hash  string          `json:"value_hash"`
	Value json.RawMessage `json:"value"`
}

func Canonical(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	_ = decoder.Decode(&normalized)
	b, _ = json.Marshal(normalized)
	return b
}
func Hash(v json.RawMessage) string {
	sum := sha256.Sum256(Canonical(v))
	return hex.EncodeToString(sum[:])
}
func NewHead(v json.RawMessage) Head {
	return Head{Token: uuid.NewString(), Hash: Hash(v), Value: Canonical(v)}
}
func NodeFields(raw json.RawMessage, relations []string) map[string]json.RawMessage {
	var n map[string]json.RawMessage
	_ = json.Unmarshal(raw, &n)
	fields := map[string]json.RawMessage{"existence": Canonical(n != nil)}
	if n == nil {
		return fields
	}
	for group, keys := range map[string][]string{"placement": {"parent_id", "x", "y", "width", "height", "z_order"}, "data": {"type_key", "type_version", "title", "intent", "config", "content_id", "content_revision_id", "selected_version_id", "document_id"}} {
		m := map[string]json.RawMessage{}
		for _, key := range keys {
			v := n[key]
			if len(v) == 0 {
				v = json.RawMessage("null")
			}
			m[key] = v
		}
		if group == "data" {
			v := n["reference_state"]
			if len(v) == 0 {
				v = json.RawMessage("[]")
			}
			m["reference_state"] = v
		}
		fields[group] = Canonical(m)
	}
	sort.Strings(relations)
	if relations == nil {
		relations = []string{}
	}
	fields["relations"] = Canonical(relations)
	return fields
}
