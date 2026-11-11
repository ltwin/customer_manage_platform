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
	if raw, ok := fields["character"]; ok {
		// A character card is found by its own words; catalog aliases of its
		// IP and work match at query time, so a rebuild loses nothing.
		var card struct {
			Name       string   `json:"name"`
			Variant    string   `json:"variant"`
			Appearance string   `json:"appearance"`
			Work       string   `json:"work"`
			Aliases    []string `json:"aliases"`
			IP         *struct {
				Name string `json:"name"`
			} `json:"ip"`
		}
		if err := json.Unmarshal(raw, &card); err != nil {
			return "", err
		}
		parts = append(parts, card.Name, card.Variant, card.Work, card.Appearance)
		parts = append(parts, card.Aliases...)
		if card.IP != nil {
			parts = append(parts, card.IP.Name)
		}
	}
	return Normalize(strings.Join(parts, "\n")), nil
}
