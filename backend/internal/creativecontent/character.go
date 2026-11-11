package creativecontent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"golang.org/x/text/unicode/norm"
)

const (
	CharacterKind     = "character"
	MaxCharacterViews = 4
)

type CharacterView struct {
	ContentRevisionID string `json:"content_revision_id"`
	View              string `json:"view"`
	Note              string `json:"note,omitempty"`
}

type CharacterSource struct {
	ContentRevisionID string `json:"content_revision_id"`
}

type CharacterIP struct {
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

type CharacterPayload struct {
	Name        string            `json:"name"`
	Appearance  string            `json:"appearance"`
	Views       []CharacterView   `json:"views"`
	Sources     []CharacterSource `json:"sources"`
	Aliases     []string          `json:"aliases,omitempty"`
	Variant     string            `json:"variant,omitempty"`
	Origin      string            `json:"origin,omitempty"`
	IP          *CharacterIP      `json:"ip,omitempty"`
	Work        string            `json:"work,omitempty"`
	Attribution string            `json:"attribution,omitempty"`
	SourceURLs  []string          `json:"source_urls,omitempty"`
}

type CharacterFieldError struct {
	Fields []string
}

func (e *CharacterFieldError) Error() string {
	return "invalid character fields: " + strings.Join(e.Fields, ", ")
}

func (e *CharacterFieldError) Unwrap() error { return creativeops.ErrValidation }

// CharacterImageError identifies rejected inputs without revealing which
// checks failed or whether an ID exists in another account.
type CharacterImageError struct {
	RevisionIDs []string
}

func (e *CharacterImageError) Error() string { return "character images unavailable" }

// CharacterIdentityKey compares names and aliases without changing the
// photographer's spelling in a custom IP or work name.
func CharacterIdentityKey(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(value))), " ")
}

// NormalizeCharacter validates the payload and returns its canonical storage
// form. The asset library resolves catalog keys and work aliases separately.
func NormalizeCharacter(in CharacterPayload) (CharacterPayload, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Appearance = strings.TrimSpace(in.Appearance)
	out.Variant = strings.TrimSpace(in.Variant)
	out.Attribution = strings.TrimSpace(in.Attribution)
	out.Work = strings.TrimSpace(in.Work)
	if in.IP != nil {
		ip := *in.IP
		ip.Name = strings.TrimSpace(ip.Name)
		out.IP = &ip
	}

	var fields []string
	for _, field := range []struct {
		path, value string
		min, max    int
	}{
		{"name", out.Name, 1, 60},
		{"appearance", out.Appearance, 1, 2000},
		{"variant", out.Variant, 0, 40},
		{"attribution", out.Attribution, 0, 2000},
		{"work", out.Work, 0, 100},
	} {
		if !characterLength(field.value, field.min, field.max) {
			fields = append(fields, field.path)
		}
	}
	switch {
	case out.IP == nil && out.Work != "":
		fields = append(fields, "work")
	case out.IP != nil && !characterLength(out.IP.Name, 1, 60):
		fields = append(fields, "ip.name")
	case out.IP != nil && CharacterIdentityKey(out.IP.Name) == CharacterIdentityKey(out.Work):
		out.Work = ""
	}
	if out.Origin != "" && out.Origin != "original" && out.Origin != "derivative" {
		fields = append(fields, "origin")
	}
	fields = append(fields, normalizeCharacterImages(&out)...)
	fields = append(fields, normalizeCharacterLabels(&out)...)
	if len(fields) > 0 {
		return CharacterPayload{}, &CharacterFieldError{Fields: fields}
	}
	return out, nil
}

func characterLength(value string, min, max int) bool {
	count := utf8.RuneCountInString(value)
	return count >= min && count <= max
}

func normalizeCharacterImages(card *CharacterPayload) []string {
	var fields []string
	if len(card.Views) == 0 || len(card.Views) > MaxCharacterViews {
		fields = append(fields, "views")
	} else {
		card.Views = slices.Clone(card.Views)
		seen := make(map[string]bool, len(card.Views))
		for i, view := range card.Views {
			if !validCharacterImageID(view.ContentRevisionID) || seen[view.ContentRevisionID] {
				fields = append(fields, fmt.Sprintf("views[%d].content_revision_id", i))
			}
			seen[view.ContentRevisionID] = true
			if !validCharacterView(view.View) {
				fields = append(fields, fmt.Sprintf("views[%d].view", i))
			}
			card.Views[i].Note = strings.TrimSpace(view.Note)
			if !characterLength(card.Views[i].Note, 0, 200) {
				fields = append(fields, fmt.Sprintf("views[%d].note", i))
			}
		}
	}
	if card.Sources == nil || len(card.Sources) > 12 {
		return append(fields, "sources")
	}
	card.Sources = slices.Clone(card.Sources)
	for i, source := range card.Sources {
		if !validCharacterImageID(source.ContentRevisionID) {
			fields = append(fields, fmt.Sprintf("sources[%d].content_revision_id", i))
		}
	}
	return fields
}

func normalizeCharacterLabels(card *CharacterPayload) []string {
	var fields []string
	if len(card.Aliases) > 10 {
		fields = append(fields, "aliases")
	} else {
		card.Aliases = slices.Clone(card.Aliases)
		seen := map[string]bool{CharacterIdentityKey(card.Name): true}
		for i, alias := range card.Aliases {
			alias = strings.TrimSpace(alias)
			key := CharacterIdentityKey(alias)
			if !characterLength(alias, 1, 60) || seen[key] {
				fields = append(fields, fmt.Sprintf("aliases[%d]", i))
			}
			card.Aliases[i] = alias
			seen[key] = true
		}
	}
	if len(card.SourceURLs) > 10 {
		return append(fields, "source_urls")
	}
	card.SourceURLs = slices.Clone(card.SourceURLs)
	for i, raw := range card.SourceURLs {
		raw = strings.TrimSpace(raw)
		card.SourceURLs[i] = raw
		u, err := url.Parse(raw)
		if !characterLength(raw, 1, 2048) || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			fields = append(fields, fmt.Sprintf("source_urls[%d]", i))
		}
	}
	return fields
}

func validCharacterImageID(id string) bool {
	return len(id) > len("ccrv_") && len(id) <= 128 && strings.HasPrefix(id, "ccrv_") && strings.TrimSpace(id) == id
}

func validCharacterView(view string) bool {
	switch view {
	case "front", "three_quarter", "side", "back", "face", "full_body", "detail", "other":
		return true
	default:
		return false
	}
}

// WriteCharacterAndRetain creates or appends a card. Its caller retains the
// new revision from the asset row in the same account write transaction.
func WriteCharacterAndRetain(ctx context.Context, tx store.TxAccountScope, card CharacterPayload, baseContentID string, retain func(Revision) error) (Revision, error) {
	if retain == nil {
		return Revision{}, ErrMissingRoot
	}
	card, err := NormalizeCharacter(card)
	if err != nil {
		return Revision{}, err
	}
	imageIDs, err := requireCharacterImages(ctx, tx, card)
	if err != nil {
		return Revision{}, err
	}
	draft := Draft{Kind: CharacterKind, Payload: Payload{Character: &card}}
	return write(ctx, tx, draft, writeOptions{baseContentID: baseContentID}, func(r Revision) error {
		if err := holdRevisionImages(ctx, tx, r.ID, CharacterKind, imageIDs); err != nil {
			return err
		}
		return retain(r)
	}, validate)
}

func requireCharacterImages(ctx context.Context, tx store.TxAccountScope, card CharacterPayload) ([]string, error) {
	references := make(map[string]bool, len(card.Views)+len(card.Sources))
	for _, source := range card.Sources {
		references[source.ContentRevisionID] = false
	}
	for _, view := range card.Views {
		references[view.ContentRevisionID] = true
	}
	ids := slices.Sorted(maps.Keys(references))
	var unavailable []string
	for _, id := range ids {
		purpose := "display"
		if references[id] {
			purpose = "generation_reference"
		}
		r, err := RequireUsable(ctx, tx, id, purpose)
		switch {
		case err == nil && r.Kind == "image":
		case err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrUsageDenied) ||
			errors.Is(err, ErrMissingRoot) || errors.Is(err, store.ErrNoRows):
			unavailable = append(unavailable, id)
		default:
			return nil, err
		}
	}
	if len(unavailable) > 0 {
		return nil, &CharacterImageError{RevisionIDs: unavailable}
	}
	return ids, nil
}
