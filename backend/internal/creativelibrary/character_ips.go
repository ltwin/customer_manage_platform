package creativelibrary

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

//go:embed character_ips.json
var characterIPData []byte

var characterIPKey = regexp.MustCompile(`^[a-z0-9-]{1,60}$`)

type characterIPEntry struct {
	Key     string          `json:"key"`
	Name    string          `json:"name"`
	Aliases []string        `json:"aliases,omitempty"`
	Works   []characterWork `json:"works,omitempty"`
	Retired bool            `json:"retired,omitempty"`
}

type characterWork struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

type characterIPCatalog struct {
	entries []characterIPEntry
	keys    map[string]int
	names   map[string]int
}

var builtinCharacterIPs, builtinCharacterIPsErr = readCharacterIPCatalog(characterIPData)

func readCharacterIPCatalog(data []byte) (characterIPCatalog, error) {
	var file struct {
		IPs []characterIPEntry `json:"ips"`
	}
	if err := creativeops.Decode(data, &file); err != nil {
		return characterIPCatalog{}, fmt.Errorf("read character IP catalog: %w", err)
	}
	catalog := characterIPCatalog{
		entries: file.IPs,
		keys:    make(map[string]int, len(file.IPs)),
		names:   make(map[string]int, len(file.IPs)),
	}
	for i, ip := range file.IPs {
		if _, exists := catalog.keys[ip.Key]; exists || !characterIPKey.MatchString(ip.Key) {
			return characterIPCatalog{}, fmt.Errorf("invalid or duplicate character IP key %q", ip.Key)
		}
		catalog.keys[ip.Key] = i
		if err := addCharacterIPLabels(catalog.names, i, 60, ip.Name, ip.Aliases); err != nil {
			return characterIPCatalog{}, fmt.Errorf("character IP %s: %w", ip.Key, err)
		}
		works := make(map[string]int, len(ip.Works))
		for j, work := range ip.Works {
			if err := addCharacterIPLabels(works, j, 100, work.Name, work.Aliases); err != nil {
				return characterIPCatalog{}, fmt.Errorf("character IP %s work %q: %w", ip.Key, work.Name, err)
			}
		}
	}
	return catalog, nil
}

func addCharacterIPLabels(index map[string]int, i, max int, name string, aliases []string) error {
	for _, label := range append([]string{name}, aliases...) {
		key := creativecontent.CharacterIdentityKey(label)
		if _, exists := index[key]; exists || key == "" || utf8.RuneCountInString(label) > max {
			return fmt.Errorf("invalid or duplicate label %q", label)
		}
		index[key] = i
	}
	return nil
}

func (c characterIPCatalog) entryByKey(key string) (characterIPEntry, bool) {
	i, ok := c.keys[key]
	if !ok {
		return characterIPEntry{}, false
	}
	return c.entries[i], true
}

func (c characterIPCatalog) entryByName(name string) (characterIPEntry, bool) {
	i, ok := c.names[creativecontent.CharacterIdentityKey(name)]
	if !ok {
		return characterIPEntry{}, false
	}
	return c.entries[i], true
}

func (ip characterIPEntry) workByName(name string) (characterWork, bool) {
	key := creativecontent.CharacterIdentityKey(name)
	for _, work := range ip.Works {
		if creativecontent.CharacterIdentityKey(work.Name) == key || slices.ContainsFunc(work.Aliases, func(alias string) bool {
			return creativecontent.CharacterIdentityKey(alias) == key
		}) {
			return work, true
		}
	}
	return characterWork{}, false
}

func normalizeLibraryCharacter(card creativecontent.CharacterPayload) (creativecontent.CharacterPayload, error) {
	card, err := creativecontent.NormalizeCharacter(card)
	if err != nil || card.IP == nil {
		return card, err
	}
	if builtinCharacterIPsErr != nil {
		return creativecontent.CharacterPayload{}, builtinCharacterIPsErr
	}
	ip, found := builtinCharacterIPs.entryByName(card.IP.Name)
	if card.IP.Key != "" {
		ip, found = builtinCharacterIPs.entryByKey(card.IP.Key)
		if !found {
			return creativecontent.CharacterPayload{}, &creativecontent.CharacterFieldError{Fields: []string{"ip.key"}}
		}
	}
	if !found {
		return card, nil
	}
	card.IP = &creativecontent.CharacterIP{Key: ip.Key, Name: ip.Name}
	if work, found := ip.workByName(card.Work); found {
		card.Work = work.Name
	}
	return creativecontent.NormalizeCharacter(card)
}

type CharacterWorkCandidate struct {
	IP   string `json:"ip"`
	Work string `json:"work"`
}

type AmbiguousCharacterWorkError struct {
	Candidates []CharacterWorkCandidate
}

func (e *AmbiguousCharacterWorkError) Error() string { return "ambiguous character work" }

func (e *AmbiguousCharacterWorkError) Unwrap() error { return creativeops.ErrValidation }

type characterFilter struct {
	ipKey   string
	ipNames []string
	works   []string
}

func characterLabels(name string, aliases []string) []string {
	values := make([]string, 0, len(aliases)+1)
	values = append(values, creativecontent.CharacterIdentityKey(name))
	for _, alias := range aliases {
		values = append(values, creativecontent.CharacterIdentityKey(alias))
	}
	return values
}

func resolveCharacterFilter(ipQuery, workQuery string) (characterFilter, error) {
	if ipQuery == "" && workQuery == "" {
		return characterFilter{}, nil
	}
	if builtinCharacterIPsErr != nil {
		return characterFilter{}, builtinCharacterIPsErr
	}
	return builtinCharacterIPs.filter(ipQuery, workQuery)
}

func (c characterIPCatalog) filter(ipQuery, workQuery string) (characterFilter, error) {
	var filter characterFilter
	ip, found := c.entryByKey(ipQuery)
	if !found {
		ip, found = c.entryByName(ipQuery)
	}
	switch {
	case found:
		filter.ipKey = ip.Key
		filter.ipNames = characterLabels(ip.Name, ip.Aliases)
	case ipQuery != "":
		filter.ipNames = []string{ipQuery}
	}
	if workQuery == "" {
		return filter, nil
	}
	filter.works = []string{workQuery}
	if found {
		if match, ok := ip.workByName(workQuery); ok {
			filter.works = characterLabels(match.Name, match.Aliases)
		}
		return filter, nil
	}
	if ipQuery != "" {
		return filter, nil
	}
	var candidates []CharacterWorkCandidate
	for _, ip := range c.entries {
		if match, ok := ip.workByName(workQuery); ok {
			candidates = append(candidates, CharacterWorkCandidate{IP: ip.Name, Work: match.Name})
			filter.works = characterLabels(match.Name, match.Aliases)
		}
	}
	if len(candidates) > 1 {
		return characterFilter{}, &AmbiguousCharacterWorkError{Candidates: candidates}
	}
	return filter, nil
}

func (f characterFilter) condition(args *sqlArgs) string {
	if len(f.ipNames) == 0 && len(f.works) == 0 {
		return ""
	}
	projection := ""
	if len(f.ipNames) > 0 {
		names := "s.character_ip_name=ANY(" + args.bind(f.ipNames) + "::text[])"
		if f.ipKey != "" {
			names = "(" + names + " OR s.character_ip_key=" + args.bind(f.ipKey) + ")"
		}
		projection += " AND " + names
	}
	if len(f.works) > 0 {
		projection += " AND s.character_work=ANY(" + args.bind(f.works) + "::text[])"
	}
	return " AND kind='character' AND EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=$1 AND s.asset_id=creative_assets.id" +
		projection + ") AND " + readableContent("creative_assets.content_revision_id", args)
}

// characterKeyword lets a keyword search reach cards through the catalog:
// "Genshin" finds 原神 cards and "FGO" finds Fate/Grand Order ones, even when
// the alias joined the catalog after the card was written.
type characterKeyword struct {
	ipKeys, ipNames, works []string
}

func (c characterIPCatalog) keyword(query string) characterKeyword {
	var out characterKeyword
	key := creativecontent.CharacterIdentityKey(query)
	if key == "" {
		return out
	}
	for _, ip := range c.entries {
		labels := characterLabels(ip.Name, ip.Aliases)
		if ip.Key == key || slices.Contains(labels, key) {
			out.ipKeys = append(out.ipKeys, ip.Key)
			out.ipNames = append(out.ipNames, labels...)
		}
		if work, ok := ip.workByName(key); ok {
			out.works = append(out.works, characterLabels(work.Name, work.Aliases)...)
		}
	}
	return out
}

func (k characterKeyword) condition(args *sqlArgs) string {
	if len(k.ipKeys) == 0 && len(k.works) == 0 {
		return ""
	}
	return ` OR EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=$1 AND s.asset_id=creative_assets.id AND (s.character_ip_key=ANY(` + args.bind(nonNil(k.ipKeys)) + `::text[]) OR s.character_ip_name=ANY(` + args.bind(nonNil(k.ipNames)) + `::text[]) OR s.character_work=ANY(` + args.bind(nonNil(k.works)) + `::text[])) AND ` + readableContent("creative_assets.content_revision_id", args) + `)`
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

type CharacterIPSuggestion struct {
	Name           string   `json:"name"`
	Key            string   `json:"key,omitempty"`
	Builtin        bool     `json:"builtin"`
	CharacterCount int      `json:"character_count"`
	Works          []string `json:"works"`
}

type CharacterIPSuggestions struct {
	Items []CharacterIPSuggestion `json:"items"`
}

func SuggestCharacterIPs(ctx context.Context, scope store.AccountScope, query string, limit int) (CharacterIPSuggestions, error) {
	if utf8.RuneCountInString(query) > 60 || limit < 1 || limit > 50 {
		return CharacterIPSuggestions{}, creativeops.ErrValidation
	}
	if builtinCharacterIPsErr != nil {
		return CharacterIPSuggestions{}, builtinCharacterIPsErr
	}
	cards, err := readCharacterIPs(ctx, scope)
	if err != nil {
		return CharacterIPSuggestions{}, err
	}
	return CharacterIPSuggestions{Items: builtinCharacterIPs.suggest(cards, creativecontent.CharacterIdentityKey(query), limit)}, nil
}

type characterIPUsage struct {
	ip   creativecontent.CharacterIP
	work string
}

func readCharacterIPs(ctx context.Context, scope store.AccountScope) ([]characterIPUsage, error) {
	var out []characterIPUsage
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		// One query over the current card revisions; only the IP and work are
		// decoded. Unreadable content is skipped like in the asset list.
		var args sqlArgs
		cond := "id IN (SELECT content_revision_id FROM creative_assets WHERE account_id=$1 AND kind='character' AND deleted_at IS NULL) AND " +
			readableContent("creative_content_revisions.id", &args)
		rows, err := tx.Query(ctx, "creative_content_revisions", "payload", cond, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload struct {
				Character *struct {
					IP   *creativecontent.CharacterIP `json:"ip"`
					Work string                       `json:"work"`
				} `json:"character"`
			}
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				return err
			}
			if card := payload.Character; card != nil && card.IP != nil {
				out = append(out, characterIPUsage{ip: *card.IP, work: card.Work})
			}
		}
		return rows.Err()
	})
	return out, err
}

func (c characterIPCatalog) suggest(usage []characterIPUsage, query string, limit int) []CharacterIPSuggestion {
	entries := make([]CharacterIPSuggestion, 0, len(c.entries))
	custom := map[string]int{}
	for _, ip := range c.entries {
		suggestion := CharacterIPSuggestion{Name: ip.Name, Key: ip.Key, Builtin: true, Works: make([]string, 0, len(ip.Works))}
		for _, work := range ip.Works {
			suggestion.Works = append(suggestion.Works, work.Name)
		}
		entries = append(entries, suggestion)
	}
	for _, item := range usage {
		name := creativecontent.CharacterIdentityKey(item.ip.Name)
		i, found := c.keys[item.ip.Key]
		if !found {
			i, found = c.names[name]
		}
		if !found {
			i, found = custom[name]
			if !found {
				i = len(entries)
				custom[name] = i
				entries = append(entries, CharacterIPSuggestion{Name: item.ip.Name, Works: []string{}})
			}
		}
		entry := &entries[i]
		entry.CharacterCount++
		work := item.work
		if entry.Builtin {
			if match, ok := c.entries[i].workByName(work); ok {
				work = match.Name
			}
		}
		key := creativecontent.CharacterIdentityKey(work)
		if key != "" && !slices.ContainsFunc(entry.Works, func(known string) bool { return creativecontent.CharacterIdentityKey(known) == key }) {
			entry.Works = append(entry.Works, work)
		}
	}
	var candidates = make([]CharacterIPSuggestion, 0, min(limit, len(entries)))
	for i, entry := range entries {
		labels := characterLabels(entry.Name, nil)
		if entry.Builtin {
			ip := c.entries[i]
			if ip.Retired && entry.CharacterCount == 0 {
				continue
			}
			labels = characterLabels(ip.Name, ip.Aliases)
			labels = append(labels, ip.Key)
		}
		if slices.ContainsFunc(labels, func(label string) bool { return strings.Contains(label, query) }) {
			candidates = append(candidates, entry)
		}
	}
	slices.SortStableFunc(candidates, func(a, b CharacterIPSuggestion) int {
		if a.CharacterCount != b.CharacterCount {
			return cmp.Compare(b.CharacterCount, a.CharacterCount)
		}
		if !a.Builtin && !b.Builtin {
			return cmp.Compare(creativecontent.CharacterIdentityKey(a.Name), creativecontent.CharacterIdentityKey(b.Name))
		}
		if a.Builtin != b.Builtin {
			if a.Builtin {
				return -1
			}
			return 1
		}
		return 0
	})
	return candidates[:min(len(candidates), limit)]
}
