package ingestion

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	ParserVersionV1            = 1
	MaxSourceBytes             = 200 * 1024
	MaxSourceRunes             = 50_000
	MaxSourceLines             = 1_000
	MaxContentCandidates       = 300
	MaxReferenceLinkCandidates = 100
	MaxStagedAssetIntents      = 50
	MaxCoreFieldRunes          = 2_000
)

var (
	ErrInvalidUTF8            = errors.New("source_text_invalid_utf8")
	ErrSourceTooLarge         = errors.New("source_text_too_large")
	ErrCandidateLimitExceeded = errors.New("candidate_limit_exceeded")
	ErrInvalidParserVersion   = errors.New("invalid_parser_version")
)

// ParseInput is deliberately pure: it contains no account, database, clock or HTTP state.
type ParseInput struct {
	SessionID                string
	ParserVersion            int
	FirstSeenSessionRevision int64
	SourceText               string
	StagedAssetIntentCount   int
	StagedAssetIntents       []AssetBindingDecision
}

type ParseOutput struct {
	ParserVersion           int                      `json:"parser_version"`
	SourceChecksum          string                   `json:"source_checksum"`
	SourceLineCount         int                      `json:"source_line_count"`
	Segments                []ParsedSegment          `json:"segments"`
	ContentCandidates       []ContentCandidate       `json:"content_candidates"`
	ReadinessLinkCandidates []ReadinessLinkCandidate `json:"readiness_link_candidates"`
	ReferenceLinkCandidates []ReferenceLinkCandidate `json:"reference_link_candidates"`
	DroppedCandidates       []DroppedCandidate       `json:"dropped_candidates"`
	AssetBindingCandidates  []AssetBindingDecision   `json:"asset_binding_candidates"`
	Canonical               []byte                   `json:"-"`
}

type ParsedSegment struct {
	Fingerprint string `json:"fingerprint"`
	Original    string `json:"original"`
	Normalized  string `json:"normalized"`
	LineRefs    []int  `json:"line_refs"`
	FirstLine   int    `json:"first_line"`
}

type ContentCandidate struct {
	CandidateID                      string  `json:"candidate_id"`
	Kind                             string  `json:"kind"`
	SourceLineRefs                   []int   `json:"source_line_refs"`
	SourceFingerprint                string  `json:"source_fingerprint"`
	PriorSourceFingerprint           *string `json:"prior_source_fingerprint,omitempty"`
	OriginalExcerpt                  string  `json:"original_excerpt"`
	NormalizedContent                string  `json:"normalized_content"`
	Title                            string  `json:"title"`
	Category                         *string `json:"category,omitempty"`
	Requirement                      *string `json:"requirement,omitempty"`
	ResponsibilityHint               *string `json:"responsibility_hint,omitempty"`
	DefaultPreparationLeadDays       *int    `json:"default_preparation_lead_days,omitempty"`
	PreflightStatus                  *string `json:"preflight_status,omitempty"`
	Action                           string  `json:"action"`
	SourceStatus                     string  `json:"source_status"`
	UserModified                     bool    `json:"user_modified"`
	SupersededByCandidateID          *string `json:"superseded_by_candidate_id,omitempty"`
	SourceChangeRevision             *int64  `json:"source_change_revision,omitempty"`
	AcknowledgedSourceChangeRevision *int64  `json:"acknowledged_source_change_revision,omitempty"`
}

type ReadinessLinkCandidate struct {
	CandidateID                      string `json:"candidate_id"`
	ReadinessClientOrIDRef           string `json:"readiness_client_or_id_ref"`
	ShotClientOrIDRef                string `json:"shot_client_or_id_ref"`
	SourceLineRefs                   []int  `json:"source_line_refs"`
	Action                           string `json:"action"`
	SourceStatus                     string `json:"source_status"`
	UserModified                     bool   `json:"user_modified"`
	SourceChangeRevision             *int64 `json:"source_change_revision,omitempty"`
	AcknowledgedSourceChangeRevision *int64 `json:"acknowledged_source_change_revision,omitempty"`
}

type ReadinessLinkSelection struct {
	ReadinessClientOrIDRef string `json:"readiness_client_or_id_ref"`
	ShotClientOrIDRef      string `json:"shot_client_or_id_ref"`
}

type ContentCandidateOverride struct {
	CandidateID                     string `json:"candidate_id"`
	Kind                            string `json:"kind"`
	Title                           string `json:"title"`
	Action                          string `json:"action"`
	AcknowledgeSourceChangeRevision *int64 `json:"acknowledge_source_change_revision,omitempty"`
}

type ReferenceLinkCandidateOverride struct {
	CandidateID                     string  `json:"candidate_id"`
	Label                           *string `json:"label,omitempty"`
	TargetKind                      string  `json:"target_kind"`
	TargetClientOrIDRef             *string `json:"target_client_or_id_ref,omitempty"`
	Action                          string  `json:"action"`
	AcknowledgeSourceChangeRevision *int64  `json:"acknowledge_source_change_revision,omitempty"`
}

type ReadinessLinkCandidateOverride struct {
	CandidateID                     string `json:"candidate_id"`
	ReadinessClientOrIDRef          string `json:"readiness_client_or_id_ref"`
	ShotClientOrIDRef               string `json:"shot_client_or_id_ref"`
	Action                          string `json:"action"`
	AcknowledgeSourceChangeRevision *int64 `json:"acknowledge_source_change_revision,omitempty"`
}

type ReferenceLinkCandidate struct {
	CandidateID                      string  `json:"candidate_id"`
	RawURL                           string  `json:"raw_url"`
	URLDigest                        string  `json:"url_digest"`
	URLOccurrence                    int     `json:"url_occurrence"`
	SourceLineRefs                   []int   `json:"source_line_refs"`
	SourceFingerprint                string  `json:"source_fingerprint"`
	PriorSourceFingerprint           *string `json:"prior_source_fingerprint,omitempty"`
	Label                            *string `json:"label,omitempty"`
	SourceHint                       *string `json:"source_hint,omitempty"`
	TargetKind                       string  `json:"target_kind"`
	TargetClientOrIDRef              *string `json:"target_client_or_id_ref,omitempty"`
	Action                           string  `json:"action"`
	SourceStatus                     string  `json:"source_status"`
	UserModified                     bool    `json:"user_modified"`
	SourceChangeRevision             *int64  `json:"source_change_revision,omitempty"`
	AcknowledgedSourceChangeRevision *int64  `json:"acknowledged_source_change_revision,omitempty"`
}

type DroppedCandidate struct {
	CandidateID    string  `json:"candidate_id"`
	Reason         string  `json:"reason"`
	SourceLineRefs []int   `json:"source_line_refs"`
	Original       string  `json:"original,omitempty"`
	WinnerID       *string `json:"winner_candidate_id,omitempty"`
	Action         string  `json:"action"`
}

// CandidateSnapshot is the JSON shape persisted by an IngestionSession.
type CandidateSnapshot struct {
	Segments                []ParsedSegment          `json:"segments"`
	ContentCandidates       []ContentCandidate       `json:"content_candidates"`
	ReadinessLinkCandidates []ReadinessLinkCandidate `json:"readiness_link_candidates"`
	ReferenceLinkCandidates []ReferenceLinkCandidate `json:"reference_link_candidates"`
	DroppedCandidates       []DroppedCandidate       `json:"dropped_candidates"`
	AssetBindingCandidates  []AssetBindingDecision   `json:"asset_binding_candidates"`
}

type CandidateLimitError struct {
	ContentCandidates       int `json:"content_candidates"`
	ReferenceLinkCandidates int `json:"reference_link_candidates"`
	StagedAssetIntents      int `json:"staged_asset_intents"`
}

func (e CandidateLimitError) Error() string {
	return fmt.Sprintf("%s: content=%d references=%d assets=%d", ErrCandidateLimitExceeded, e.ContentCandidates, e.ReferenceLinkCandidates, e.StagedAssetIntents)
}
func (e CandidateLimitError) Unwrap() error { return ErrCandidateLimitExceeded }

var invalidHTTPSpan = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

// Parse executes parser_v1. It returns no partial output on a request/aggregate limit error.
func Parse(input ParseInput) (ParseOutput, error) {
	if input.ParserVersion == 0 {
		input.ParserVersion = ParserVersionV1
	}
	assetIntentCount := input.StagedAssetIntentCount
	if input.StagedAssetIntents != nil {
		assetIntentCount = len(input.StagedAssetIntents)
	}
	if input.ParserVersion != ParserVersionV1 || input.FirstSeenSessionRevision < 1 || strings.TrimSpace(input.SessionID) == "" || assetIntentCount < 0 {
		return ParseOutput{}, ErrInvalidParserVersion
	}
	for _, intent := range input.StagedAssetIntents {
		if !validAssetBindingDecision(intent) {
			return ParseOutput{}, ErrReparseInput
		}
	}
	if !utf8.ValidString(input.SourceText) {
		return ParseOutput{}, ErrInvalidUTF8
	}
	if strings.IndexByte(input.SourceText, 0) >= 0 || hasForbiddenControl(input.SourceText) {
		return ParseOutput{}, errors.New("source_text_control_character")
	}
	if len([]byte(input.SourceText)) > MaxSourceBytes || utf8.RuneCountInString(input.SourceText) > MaxSourceRunes {
		return ParseOutput{}, ErrSourceTooLarge
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(input.SourceText, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > MaxSourceLines {
		return ParseOutput{}, ErrSourceTooLarge
	}
	out := ParseOutput{ParserVersion: input.ParserVersion, SourceLineCount: len(lines), AssetBindingCandidates: append([]AssetBindingDecision(nil), input.StagedAssetIntents...)}
	sort.SliceStable(out.AssetBindingCandidates, func(i, j int) bool {
		return out.AssetBindingCandidates[i].CandidateID < out.AssetBindingCandidates[j].CandidateID
	})
	sum := sha256.Sum256([]byte(input.SourceText))
	out.SourceChecksum = "sha256-" + fmt.Sprintf("%x", sum[:])
	paragraphs, blanks := collectParagraphs(lines)
	out.DroppedCandidates = append(out.DroppedCandidates, blankDrops(input, blanks)...)
	occurrences := make(map[string]int)
	for _, p := range paragraphs {
		segment := parseSegment(input, p, occurrences)
		out.Segments = append(out.Segments, segment.segment)
		out.ContentCandidates = append(out.ContentCandidates, segment.contents...)
		out.ReferenceLinkCandidates = append(out.ReferenceLinkCandidates, segment.references...)
		out.DroppedCandidates = append(out.DroppedCandidates, segment.dropped...)
	}
	markDuplicates(&out, input)
	sortOutput(&out)
	if len(out.ContentCandidates) > MaxContentCandidates || len(out.ReferenceLinkCandidates) > MaxReferenceLinkCandidates || assetIntentCount > MaxStagedAssetIntents {
		return ParseOutput{}, CandidateLimitError{ContentCandidates: len(out.ContentCandidates), ReferenceLinkCandidates: len(out.ReferenceLinkCandidates), StagedAssetIntents: assetIntentCount}
	}
	normalizeParseOutput(&out)
	canonical, err := canonicalJSON(out)
	if err != nil {
		return ParseOutput{}, err
	}
	out.Canonical = canonical
	return out, nil
}

// BuildReadinessLinkCandidates turns the editor's explicit 0..N selections into
// stable candidates. Parser v1 never guesses these links from text.
func BuildReadinessLinkCandidates(input ParseInput, content []ContentCandidate, selections []ReadinessLinkSelection) ([]ReadinessLinkCandidate, error) {
	if input.ParserVersion == 0 {
		input.ParserVersion = ParserVersionV1
	}
	if input.ParserVersion != ParserVersionV1 || input.FirstSeenSessionRevision < 1 || strings.TrimSpace(input.SessionID) == "" {
		return nil, ErrInvalidParserVersion
	}
	byID := make(map[string]ContentCandidate, len(content))
	for _, candidate := range content {
		byID[candidate.CandidateID] = candidate
	}
	result := make([]ReadinessLinkCandidate, 0, len(selections))
	seen := map[string]struct{}{}
	for _, selection := range selections {
		readinessRef, shotRef := strings.TrimSpace(selection.ReadinessClientOrIDRef), strings.TrimSpace(selection.ShotClientOrIDRef)
		if readinessRef == "" || shotRef == "" {
			return nil, errors.New("readiness_link_ref_invalid")
		}
		readiness, readinessInSession := byID[readinessRef]
		shot, shotInSession := byID[shotRef]
		if !readinessInSession && !shotInSession {
			return nil, errors.New("readiness_link_requires_session_candidate")
		}
		if readinessInSession && readiness.Kind != "readiness" {
			return nil, errors.New("readiness_link_readiness_kind_invalid")
		}
		if readinessInSession && readiness.Action == "discard" {
			return nil, errors.New("readiness_link_readiness_discarded")
		}
		if shotInSession && shot.Kind != "shot" {
			return nil, errors.New("readiness_link_shot_kind_invalid")
		}
		if shotInSession && shot.Action == "discard" {
			return nil, errors.New("readiness_link_shot_discarded")
		}
		key := readinessRef + "\x00" + shotRef
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("readiness_link_duplicate")
		}
		seen[key] = struct{}{}
		lineRefs := []int{}
		if readinessInSession {
			lineRefs = append(lineRefs, readiness.SourceLineRefs...)
		}
		if shotInSession {
			lineRefs = append(lineRefs, shot.SourceLineRefs...)
		}
		lineRefs = sortedUniqueInts(lineRefs)
		id := encodeHash(frame([]byte("readiness-link"), []byte(input.SessionID), u64(uint64(input.ParserVersion)), []byte(readinessRef), []byte(shotRef), u64(uint64(input.FirstSeenSessionRevision))))
		result = append(result, ReadinessLinkCandidate{CandidateID: id, ReadinessClientOrIDRef: readinessRef, ShotClientOrIDRef: shotRef, SourceLineRefs: lineRefs, Action: "keep", SourceStatus: "current", UserModified: true})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].ReadinessClientOrIDRef != result[j].ReadinessClientOrIDRef {
			return result[i].ReadinessClientOrIDRef < result[j].ReadinessClientOrIDRef
		}
		if result[i].ShotClientOrIDRef != result[j].ShotClientOrIDRef {
			return result[i].ShotClientOrIDRef < result[j].ShotClientOrIDRef
		}
		return result[i].CandidateID < result[j].CandidateID
	})
	return result, nil
}

type segmentResult struct {
	segment    ParsedSegment
	contents   []ContentCandidate
	references []ReferenceLinkCandidate
	dropped    []DroppedCandidate
}

type paragraph struct {
	lines []string
	refs  []int
}

func collectParagraphs(lines []string) ([]paragraph, []paragraph) {
	paragraphs, blanks := []paragraph{}, []paragraph{}
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			start := i
			for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
				i++
			}
			blanks = append(blanks, paragraph{lines: append([]string(nil), lines[start:i]...), refs: rangeInts(start+1, i)})
			continue
		}
		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			i++
		}
		paragraphs = append(paragraphs, paragraph{lines: append([]string(nil), lines[start:i]...), refs: rangeInts(start+1, i)})
	}
	return paragraphs, blanks
}

func blankDrops(input ParseInput, blanks []paragraph) []DroppedCandidate {
	drops := make([]DroppedCandidate, 0, len(blanks))
	occurrences := make(map[string]int, len(blanks))
	for _, p := range blanks {
		blankFingerprint := fingerprint("blank", strings.Join(p.lines, "\n"))
		occurrence := occurrences[blankFingerprint]
		occurrences[blankFingerprint] = occurrence + 1
		id := candidateID(input, "dropped", blankFingerprint, occurrence)
		drops = append(drops, DroppedCandidate{CandidateID: id, Reason: "blank", SourceLineRefs: append([]int(nil), p.refs...), Original: strings.Join(p.lines, "\n"), Action: "discard"})
	}
	return drops
}

func parseSegment(input ParseInput, p paragraph, occurrences map[string]int) segmentResult {
	original := strings.Join(p.lines, "\n")
	urlSpans, cleanedLines, dropped := extractURLs(p.lines, p.refs, input, occurrences)
	cleaned := normalizeContent(cleanSpeakerPrefixes(cleanedLines))
	segmentFingerprint := fingerprint("segment", normalizeForFingerprint(strings.Join(p.lines, "\n")))
	segment := ParsedSegment{Fingerprint: segmentFingerprint, Original: original, Normalized: cleaned, LineRefs: append([]int(nil), p.refs...), FirstLine: p.refs[0]}
	result := segmentResult{segment: segment, dropped: dropped}
	for index, span := range urlSpans {
		digest := digestURL(span.raw)
		occurrence := index + 1
		id := referenceCandidateID(input, segmentFingerprint, occurrence, digest)
		result.references = append(result.references, ReferenceLinkCandidate{
			CandidateID: id, RawURL: span.raw, URLDigest: digest, URLOccurrence: occurrence, SourceLineRefs: []int{span.line},
			SourceFingerprint: segmentFingerprint, TargetKind: "plan", Action: "keep", SourceStatus: "current",
		})
	}
	if cleaned == "" {
		return result
	}
	if isPlaceholder(cleaned) {
		result.dropped = append(result.dropped, DroppedCandidate{CandidateID: candidateID(input, "dropped", segmentFingerprint, nextOccurrence(occurrences, "dropped:unsupported:"+segmentFingerprint)), Reason: "unsupported", SourceLineRefs: append([]int(nil), p.refs...), Original: original, Action: "discard"})
		return result
	}
	if runeLen(cleaned) > MaxCoreFieldRunes {
		result.dropped = append(result.dropped, DroppedCandidate{CandidateID: candidateID(input, "dropped", segmentFingerprint, nextOccurrence(occurrences, "dropped:over_limit:"+segmentFingerprint)), Reason: "over_limit", SourceLineRefs: append([]int(nil), p.refs...), Original: original, Action: "discard"})
		return result
	}
	occ := occurrences[segmentFingerprint]
	occurrences[segmentFingerprint] = occ + 1
	kind := "shot"
	if containsReadinessCue(cleaned) {
		kind = "readiness"
	}
	candidate := ContentCandidate{CandidateID: candidateID(input, "content", segmentFingerprint, occ), Kind: kind, SourceLineRefs: append([]int(nil), p.refs...), SourceFingerprint: segmentFingerprint, OriginalExcerpt: original, NormalizedContent: cleaned, Title: firstLogicalLine(cleanSpeakerPrefixes(cleanedLines)), Action: "keep", SourceStatus: "current"}
	if kind == "readiness" {
		candidate.Category = stringPtr("other")
		candidate.Requirement = stringPtr(cleaned)
		candidate.PreflightStatus = stringPtr("pending")
	}
	result.contents = append(result.contents, candidate)
	return result
}

type urlSpan struct {
	raw  string
	line int
}

func extractURLs(lines []string, refs []int, input ParseInput, occurrences map[string]int) ([]urlSpan, []string, []DroppedCandidate) {
	spans := []urlSpan{}
	cleaned := make([]string, len(lines))
	dropped := []DroppedCandidate{}
	for i, line := range lines {
		matches := invalidHTTPSpan.FindAllStringIndex(line, -1)
		if len(matches) == 0 {
			cleaned[i] = line
			continue
		}
		var b strings.Builder
		last := 0
		for _, m := range matches {
			b.WriteString(line[last:m[0]])
			raw := strings.TrimRightFunc(line[m[0]:m[1]], isURLTrailingPunctuation)
			if raw == "" {
				b.WriteString(line[m[0]:m[1]])
				last = m[1]
				continue
			}
			parsed, err := url.ParseRequestURI(raw)
			if err != nil || parsed.Host == "" || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") {
				fp := fingerprint("unsupported", raw)
				id := candidateID(input, "dropped", fp, nextOccurrence(occurrences, "dropped:unsupported-url:"+fp))
				dropped = append(dropped, DroppedCandidate{CandidateID: id, Reason: "unsupported", SourceLineRefs: []int{refs[i]}, Original: raw, Action: "discard"})
			}
			if err == nil && parsed.Host != "" && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
				spans = append(spans, urlSpan{raw: raw, line: refs[i]})
				b.WriteByte(' ')
			} else {
				b.WriteByte(' ')
			}
			last = m[1]
		}
		b.WriteString(line[last:])
		cleaned[i] = b.String()
	}
	return spans, cleaned, dropped
}

func cleanSpeakerPrefixes(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		idx := strings.IndexAny(trimmed, ":：")
		if idx <= 0 || idx >= len(trimmed)-1 {
			out[i] = line
			continue
		}
		_, colonSize := utf8.DecodeRuneInString(trimmed[idx:])
		prefix, rest := trimmed[:idx], trimmed[idx+colonSize:]
		if len([]rune(prefix)) > 40 || invalidSpeakerPrefix(prefix) || strings.HasPrefix(strings.TrimSpace(rest), "//") || strings.EqualFold(prefix, "http") || strings.EqualFold(prefix, "https") || !hasLetter(prefix) {
			out[i] = line
			continue
		}
		out[i] = strings.TrimSpace(rest)
	}
	return out
}

func normalizeContent(s []string) string {
	lines := make([]string, 0, len(s))
	for _, line := range s {
		line = norm.NFC.String(line)
		line = strings.Join(strings.FieldsFunc(line, unicode.IsSpace), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

func isPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "[图片]" || s == "<图片>" {
		return true
	}
	if strings.HasPrefix(s, "（图片") && strings.HasSuffix(s, "张）") {
		inner := strings.TrimSuffix(strings.TrimPrefix(s, "（图片"), "张）")
		return inner != "" && func() bool {
			for _, r := range inner {
				if !unicode.IsDigit(r) && !strings.ContainsRune("一二三四五六七八九十", r) {
					return false
				}
			}
			return true
		}()
	}
	return false
}

func containsReadinessCue(s string) bool {
	for _, cue := range []string{"记得", "要带", "带上", "需要准备", "准备好", "借一下", "租一下", "确认一下", "预约", "问一下"} {
		if strings.Contains(s, cue) {
			return true
		}
	}
	return false
}

func markDuplicates(out *ParseOutput, input ParseInput) {
	seenContent := map[string]string{}
	for i := range out.ContentCandidates {
		c := &out.ContentCandidates[i]
		key := c.Kind + "\x00" + normalizeForFingerprint(c.NormalizedContent)
		if winner, ok := seenContent[key]; ok {
			id := candidateID(input, "dropped", c.SourceFingerprint, 100+i)
			out.DroppedCandidates = append(out.DroppedCandidates, DroppedCandidate{CandidateID: id, Reason: "duplicate", SourceLineRefs: append([]int(nil), c.SourceLineRefs...), Original: c.OriginalExcerpt, WinnerID: stringPtr(winner), Action: "discard"})
			c.Action = "discard"
		} else {
			seenContent[key] = c.CandidateID
		}
	}
	seenRef := map[string]string{}
	for i := range out.ReferenceLinkCandidates {
		c := &out.ReferenceLinkCandidates[i]
		if winner, ok := seenRef[c.RawURL]; ok {
			id := candidateID(input, "dropped", c.URLDigest, 200+i)
			out.DroppedCandidates = append(out.DroppedCandidates, DroppedCandidate{CandidateID: id, Reason: "duplicate", SourceLineRefs: append([]int(nil), c.SourceLineRefs...), Original: c.RawURL, WinnerID: stringPtr(winner), Action: "discard"})
			c.Action = "discard"
		} else {
			seenRef[c.RawURL] = c.CandidateID
		}
	}
}

func sortOutput(out *ParseOutput) {
	sort.SliceStable(out.ContentCandidates, func(i, j int) bool {
		return candidateOrder(out.ContentCandidates[i].SourceLineRefs, 0, out.ContentCandidates[i].CandidateID, out.ContentCandidates[j].SourceLineRefs, 0, out.ContentCandidates[j].CandidateID)
	})
	sort.SliceStable(out.ReferenceLinkCandidates, func(i, j int) bool {
		return referenceLess(out.ReferenceLinkCandidates[i], out.ReferenceLinkCandidates[j])
	})
	sort.SliceStable(out.DroppedCandidates, func(i, j int) bool {
		return candidateOrder(out.DroppedCandidates[i].SourceLineRefs, 3, out.DroppedCandidates[i].CandidateID, out.DroppedCandidates[j].SourceLineRefs, 3, out.DroppedCandidates[j].CandidateID)
	})
	sort.SliceStable(out.Segments, func(i, j int) bool { return out.Segments[i].FirstLine < out.Segments[j].FirstLine })
}

func candidateOrder(a []int, ar int, aid string, b []int, br int, bid string) bool {
	la, lb := 0, 0
	if len(a) > 0 {
		la = a[0]
	}
	if len(b) > 0 {
		lb = b[0]
	}
	if la != lb {
		return la < lb
	}
	if ar != br {
		return ar < br
	}
	return aid < bid
}

func canonicalJSON(out ParseOutput) ([]byte, error) {
	clone := out
	clone.Canonical = nil
	return json.Marshal(clone)
}

func normalizeParseOutput(out *ParseOutput) {
	if out.Segments == nil {
		out.Segments = []ParsedSegment{}
	}
	if out.ContentCandidates == nil {
		out.ContentCandidates = []ContentCandidate{}
	}
	if out.ReadinessLinkCandidates == nil {
		out.ReadinessLinkCandidates = []ReadinessLinkCandidate{}
	}
	if out.ReferenceLinkCandidates == nil {
		out.ReferenceLinkCandidates = []ReferenceLinkCandidate{}
	}
	if out.DroppedCandidates == nil {
		out.DroppedCandidates = []DroppedCandidate{}
	}
	if out.AssetBindingCandidates == nil {
		out.AssetBindingCandidates = []AssetBindingDecision{}
	}
}

func validAssetBindingDecision(intent AssetBindingDecision) bool {
	return strings.TrimSpace(intent.CandidateID) != "" && strings.TrimSpace(intent.AssetID) != "" && intent.Generation >= 1 &&
		(intent.TargetKind == "plan" || intent.TargetKind == "shot") && strings.TrimSpace(intent.TargetClientOrIDRef) != "" && strings.TrimSpace(intent.Purpose) != ""
}

func nextOccurrence(occurrences map[string]int, key string) int {
	occurrence := occurrences[key]
	occurrences[key] = occurrence + 1
	return occurrence
}

func candidateID(input ParseInput, kind, fp string, occurrence int) string {
	return encodeHash(frame([]byte(kind), []byte(input.SessionID), u64(uint64(input.ParserVersion)), []byte(fp), u64(uint64(occurrence)), u64(uint64(input.FirstSeenSessionRevision))))
}
func referenceCandidateID(input ParseInput, fp string, occurrence int, digest string) string {
	return encodeHash(frame([]byte("reference"), []byte(input.SessionID), u64(uint64(input.ParserVersion)), []byte(fp), u64(uint64(occurrence)), []byte(digest), u64(uint64(input.FirstSeenSessionRevision))))
}
func fingerprint(kind, value string) string {
	return encodeHash(frame([]byte(kind), []byte(norm.NFC.String(value))))
}
func digestURL(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256-" + fmt.Sprintf("%x", sum[:])
}
func frame(fields ...[]byte) []byte {
	var b strings.Builder
	b.WriteString("ing-frame-v1")
	for _, f := range fields {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(f)))
		b.Write(length[:])
		b.Write(f)
	}
	return []byte(b.String())
}
func u64(v uint64) []byte { var b [8]byte; binary.BigEndian.PutUint64(b[:], v); return b[:] }
func encodeHash(input []byte) string {
	sum := sha256.Sum256(input)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func normalizeForFingerprint(s string) string {
	return strings.Join(strings.FieldsFunc(norm.NFC.String(s), unicode.IsSpace), " ")
}
func firstLogicalLine(lines []string) string {
	s := ""
	for _, line := range lines {
		s = strings.Join(strings.FieldsFunc(norm.NFC.String(line), unicode.IsSpace), " ")
		if s != "" {
			break
		}
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) > 160 {
		r = r[:160]
	}
	return string(r)
}
func invalidSpeakerPrefix(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || r == ':' || r == '：' || r == '/' {
			return true
		}
	}
	return false
}
func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.In(r, unicode.Han) {
			return true
		}
	}
	return false
}
func hasForbiddenControl(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}
func isURLTrailingPunctuation(r rune) bool { return strings.ContainsRune("。，,；;！？?、", r) }
func runeLen(s string) int                 { return utf8.RuneCountInString(s) }
func rangeInts(start, end int) []int {
	result := make([]int, 0, end-start)
	for i := start; i <= end; i++ {
		result = append(result, i)
	}
	return result
}
func stringPtr(s string) *string { return &s }
func sortedUniqueInts(values []int) []int {
	sort.Ints(values)
	result := make([]int, 0, len(values))
	for _, v := range values {
		if len(result) == 0 || result[len(result)-1] != v {
			result = append(result, v)
		}
	}
	return result
}
