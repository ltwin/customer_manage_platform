// Package textindex owns the versioned library search normalization used by writes and migration rebuilds.
package textindex

import (
	"encoding/json"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const Version = "nfkc-lower/" + norm.Version + "/" + unicode.Version

func Normalize(value string) string {
	return strings.TrimSpace(strings.ToLower(norm.NFKC.String(value)))
}
func Document(title, description, source string, payload json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return "", err
	}
	parts := []string{title, description, source}
	for _, key := range []string{"body", "url", "title", "description", "caption"} {
		if raw, ok := fields[key]; ok {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", err
			}
			parts = append(parts, value)
		}
	}
	return Normalize(strings.Join(parts, "\n")), nil
}
