package ingestion

import (
	"errors"
	"strings"
)

var ErrPreviewOverride = errors.New("ingestion_preview_override_invalid")

// applyPreviewOverrides is the persistence boundary for editor mutations. A
// reparse produces a deterministic snapshot first; only then are explicit
// user decisions applied and revision-checked. This keeps source-change
// acknowledgement and readiness-link removal auditable in the session JSON.
func applyPreviewOverrides(parsed *ParseOutput, current Session, input PreviewInput) error {
	contentByID := make(map[string]int, len(parsed.ContentCandidates))
	for i := range parsed.ContentCandidates {
		contentByID[parsed.ContentCandidates[i].CandidateID] = i
	}
	droppedByID := make(map[string]DroppedCandidate, len(parsed.DroppedCandidates))
	for _, dropped := range parsed.DroppedCandidates {
		droppedByID[dropped.CandidateID] = dropped
	}
	seen := map[string]struct{}{}
	for _, override := range input.ContentOverrides {
		id := strings.TrimSpace(override.CandidateID)
		if id == "" {
			return ErrPreviewOverride
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrPreviewOverride
		}
		seen[id] = struct{}{}
		index, found := contentByID[id]
		if !found {
			if dropped, ok := droppedByID[id]; ok {
				if override.Action != "keep" || strings.TrimSpace(override.Kind) == "" || strings.TrimSpace(override.Title) == "" {
					return ErrPreviewOverride
				}
				candidate := ContentCandidate{
					CandidateID: id, Kind: strings.TrimSpace(override.Kind),
					SourceLineRefs: append([]int(nil), dropped.SourceLineRefs...), OriginalExcerpt: dropped.Original,
					NormalizedContent: dropped.Original, Title: strings.TrimSpace(override.Title),
					Action: "keep", SourceStatus: "current", UserModified: true,
				}
				parsed.ContentCandidates = append(parsed.ContentCandidates, candidate)
				filtered := parsed.DroppedCandidates[:0]
				for _, item := range parsed.DroppedCandidates {
					if item.CandidateID != id {
						filtered = append(filtered, item)
					}
				}
				parsed.DroppedCandidates = filtered
				index = len(parsed.ContentCandidates) - 1
				contentByID[id] = index
			} else {
				return ErrPreviewOverride
			}
		}
		candidate := &parsed.ContentCandidates[index]
		if err := acknowledgeSourceChange(candidate.SourceStatus, candidate.SourceChangeRevision, override.AcknowledgeSourceChangeRevision); err != nil {
			return err
		}
		if override.Kind != "" && override.Kind != "shot" && override.Kind != "readiness" {
			return ErrPreviewOverride
		}
		if override.Kind != "" {
			candidate.Kind = override.Kind
		}
		if override.Title != "" {
			candidate.Title = strings.TrimSpace(override.Title)
		}
		if override.Action != "keep" && override.Action != "discard" {
			return ErrPreviewOverride
		}
		candidate.Action = override.Action
		candidate.UserModified = true
		if candidate.SourceChangeRevision != nil && override.AcknowledgeSourceChangeRevision != nil {
			value := *override.AcknowledgeSourceChangeRevision
			candidate.AcknowledgedSourceChangeRevision = &value
			candidate.SourceStatus = "current"
		}
	}

	seen = map[string]struct{}{}
	for _, override := range input.ReferenceLinkOverrides {
		id := strings.TrimSpace(override.CandidateID)
		if id == "" {
			return ErrPreviewOverride
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrPreviewOverride
		}
		seen[id] = struct{}{}
		var candidate *ReferenceLinkCandidate
		for i := range parsed.ReferenceLinkCandidates {
			if parsed.ReferenceLinkCandidates[i].CandidateID == id {
				candidate = &parsed.ReferenceLinkCandidates[i]
				break
			}
		}
		if candidate == nil || (override.TargetKind != "" && override.TargetKind != "plan" && override.TargetKind != "shot") {
			return ErrPreviewOverride
		}
		if err := acknowledgeSourceChange(candidate.SourceStatus, candidate.SourceChangeRevision, override.AcknowledgeSourceChangeRevision); err != nil {
			return err
		}
		if override.Label != nil {
			candidate.Label = override.Label
		}
		if override.TargetKind != "" {
			candidate.TargetKind = override.TargetKind
		}
		if override.TargetClientOrIDRef != nil {
			candidate.TargetClientOrIDRef = override.TargetClientOrIDRef
		}
		if override.Action != "keep" && override.Action != "discard" {
			return ErrPreviewOverride
		}
		candidate.Action = override.Action
		candidate.UserModified = true
		if candidate.SourceChangeRevision != nil && override.AcknowledgeSourceChangeRevision != nil {
			value := *override.AcknowledgeSourceChangeRevision
			candidate.AcknowledgedSourceChangeRevision = &value
			candidate.SourceStatus = "current"
		}
	}

	if input.ReadinessLinkSelections != nil {
		links, err := BuildReadinessLinkCandidates(ParseInput{
			SessionID: current.ID, ParserVersion: current.ParserVersion,
			FirstSeenSessionRevision: current.Revision + 1,
		}, parsed.ContentCandidates, input.ReadinessLinkSelections)

		if err != nil {
			return ErrPreviewOverride
		}
		oldByPair := make(map[string]ReadinessLinkCandidate, len(current.CandidateSnapshot.ReadinessLinkCandidates))
		for _, old := range current.CandidateSnapshot.ReadinessLinkCandidates {
			oldByPair[old.ReadinessClientOrIDRef+"\x00"+old.ShotClientOrIDRef] = old
		}
		selected := make(map[string]struct{}, len(links))
		for i := range links {
			key := links[i].ReadinessClientOrIDRef + "\x00" + links[i].ShotClientOrIDRef
			selected[key] = struct{}{}
			if old, ok := oldByPair[key]; ok {
				links[i] = old
			}
		}
		for _, old := range current.CandidateSnapshot.ReadinessLinkCandidates {
			key := old.ReadinessClientOrIDRef + "\x00" + old.ShotClientOrIDRef
			if _, ok := selected[key]; ok {
				continue
			}
			old.Action = "discard"
			old.UserModified = true
			links = append(links, old)
		}
		parsed.ReadinessLinkCandidates = links
	}
	linkByID := make(map[string]*ReadinessLinkCandidate, len(parsed.ReadinessLinkCandidates))
	for i := range parsed.ReadinessLinkCandidates {
		linkByID[parsed.ReadinessLinkCandidates[i].CandidateID] = &parsed.ReadinessLinkCandidates[i]
	}
	seen = map[string]struct{}{}
	for _, override := range input.ReadinessLinkOverrides {
		id := strings.TrimSpace(override.CandidateID)
		if id == "" {
			return ErrPreviewOverride
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrPreviewOverride
		}
		seen[id] = struct{}{}
		candidate := linkByID[id]
		if candidate == nil || candidate.ReadinessClientOrIDRef != strings.TrimSpace(override.ReadinessClientOrIDRef) || candidate.ShotClientOrIDRef != strings.TrimSpace(override.ShotClientOrIDRef) {
			return ErrPreviewOverride
		}
		if err := acknowledgeSourceChange(candidate.SourceStatus, candidate.SourceChangeRevision, override.AcknowledgeSourceChangeRevision); err != nil {
			return err
		}
		if override.Action != "keep" && override.Action != "discard" {
			return ErrPreviewOverride
		}
		candidate.Action = override.Action
		candidate.UserModified = true
		if candidate.SourceChangeRevision != nil && override.AcknowledgeSourceChangeRevision != nil {
			value := *override.AcknowledgeSourceChangeRevision
			candidate.AcknowledgedSourceChangeRevision = &value
			candidate.SourceStatus = "current"
		}
	}
	for _, old := range current.CandidateSnapshot.ReadinessLinkCandidates {
		if _, ok := seen[old.CandidateID]; !ok {
			return ErrPreviewOverride
		}
	}
	return nil
}

func acknowledgeSourceChange(status string, revision, acknowledgement *int64) error {
	if status == "source_changed" || status == "source_missing" {
		if revision == nil || acknowledgement == nil || *revision != *acknowledgement {
			return ErrPreviewOverride
		}
	}
	return nil
}
