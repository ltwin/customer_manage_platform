package ingestion

import (
	"errors"
	"sort"
)

var ErrReparseInput = errors.New("reparse_input_invalid")

// Reconcile deterministically carries candidate identity and user edits across a reparse.
// It intentionally only uses exact fingerprints and the specified LCS/gap rules; it never
// performs similarity or edit-distance matching.
func Reconcile(old CandidateSnapshot, fresh ParseOutput, nextSessionRevision int64) (CandidateSnapshot, error) {
	if nextSessionRevision < 1 {
		return CandidateSnapshot{}, ErrReparseInput
	}
	if len(old.Segments) == 0 {
		return snapshotFromParse(fresh), nil
	}
	result := snapshotFromParse(fresh)
	missingByFP := map[string][]ContentCandidate{}
	for _, candidate := range old.ContentCandidates {
		if candidate.SourceStatus == "source_missing" {
			missingByFP[candidate.SourceFingerprint] = append(missingByFP[candidate.SourceFingerprint], candidate)
			continue
		}
	}
	for _, candidates := range missingByFP {
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].CandidateID < candidates[j].CandidateID })
	}
	pairs := lcsPairs(old.Segments, fresh.Segments)
	oldToNew := make(map[int]int)
	newToOld := make(map[int]int)
	for _, pair := range pairs {
		oldToNew[pair.old] = pair.new
		newToOld[pair.new] = pair.old
	}
	// Exact anchors keep the old candidate identity and override. For equal-sized gaps,
	// pair by ordinal and mark source_changed; unequal gaps remain source_missing/new.
	for oldIndex, newIndex := range oldToNew {
		carryContentAtSegment(&result, old.ContentCandidates, old.Segments[oldIndex], fresh.Segments[newIndex], false, nextSessionRevision)
	}
	referencePairs := append([]lcsPair(nil), pairs...)
	for _, gap := range rangeGaps(len(old.Segments), len(fresh.Segments), pairs) {
		if gap[1]-gap[0] == gap[3]-gap[2] {
			for offset := 0; offset < gap[1]-gap[0]; offset++ {
				referencePairs = append(referencePairs, lcsPair{old: gap[0] + offset, new: gap[2] + offset})
			}
		}
	}
	reconcileReferences(&result, old, fresh, referencePairs, nextSessionRevision)
	for _, gap := range rangeGaps(len(old.Segments), len(fresh.Segments), pairs) {
		gapOldStart, gapOldEnd, gapNewStart, gapNewEnd := gap[0], gap[1], gap[2], gap[3]
		oldCount, newCount := gapOldEnd-gapOldStart, gapNewEnd-gapNewStart
		if oldCount == newCount {
			for offset := 0; offset < oldCount; offset++ {
				oldToNew[gapOldStart+offset] = gapNewStart + offset
				newToOld[gapNewStart+offset] = gapOldStart + offset
				carryContentAtSegment(&result, old.ContentCandidates, old.Segments[gapOldStart+offset], fresh.Segments[gapNewStart+offset], true, nextSessionRevision)
			}
		} else {
			for _, candidate := range old.ContentCandidates {
				if candidate.SourceStatus == "source_missing" || !candidate.UserModified && candidate.Action == "discard" {
					continue
				}
				candidateSegment := segmentIndexForCandidate(old.Segments, candidate)
				if candidateSegment >= gapOldStart && candidateSegment < gapOldEnd {
					candidate.SourceStatus = "source_missing"
					candidate.Action = "needs_confirmation"
					setSourceRevision(&candidate, nextSessionRevision)
					replaceCandidate(&result.ContentCandidates, "", candidate)
				}
			}
		}
	}
	for _, gap := range rangeGaps(len(old.Segments), len(fresh.Segments), pairs) {
		if gap[1]-gap[0] != gap[3]-gap[2] {
			reconcileMissingReferences(&result, old, gap[0], gap[1], nextSessionRevision)
		}
	}
	// A source that disappeared and later reappears is attached only after the main LCS,
	// in candidate-id order, and still requires confirmation.
	for newIndex, segment := range fresh.Segments {
		if _, paired := newToOld[newIndex]; paired {
			continue
		}
		missing := missingByFP[segment.Fingerprint]
		if len(missing) == 0 {
			continue
		}
		candidate := missing[0]
		missingByFP[segment.Fingerprint] = missing[1:]
		candidate.SourceStatus = "source_changed"
		candidate.Action = "needs_confirmation"
		candidate.PriorSourceFingerprint = stringPtr(candidate.SourceFingerprint)
		candidate.SourceFingerprint = segment.Fingerprint
		candidate.SourceLineRefs = append([]int(nil), segment.LineRefs...)
		candidate.OriginalExcerpt = segment.Original
		candidate.NormalizedContent = segment.Normalized
		setSourceRevision(&candidate, nextSessionRevision)
		replaceCandidate(&result.ContentCandidates, freshCandidateIDForSegment(result.ContentCandidates, segment), candidate)
	}
	reconcileReadinessLinks(&result, old.ReadinessLinkCandidates, nextSessionRevision)
	reconcileDropped(&result, old.DroppedCandidates)
	result.Segments = append([]ParsedSegment(nil), fresh.Segments...)
	return result, nil
}

func reconcileReferences(result *CandidateSnapshot, old CandidateSnapshot, fresh ParseOutput, pairs []lcsPair, revision int64) {
	for _, pair := range pairs {
		oldRefs := referencesForFingerprint(old.ReferenceLinkCandidates, old.Segments[pair.old].Fingerprint)
		newRefs := referencesForFingerprint(result.ReferenceLinkCandidates, fresh.Segments[pair.new].Fingerprint)
		matches := digestLCSPairs(oldRefs, newRefs)
		oldMatched, newMatched := map[int]bool{}, map[int]bool{}
		for _, match := range matches {
			oldMatched[match.old], newMatched[match.new] = true, true
			carried := oldRefs[match.old]
			current := newRefs[match.new]
			carried.RawURL, carried.URLDigest, carried.URLOccurrence = current.RawURL, current.URLDigest, current.URLOccurrence
			carried.SourceLineRefs, carried.SourceFingerprint = append([]int(nil), current.SourceLineRefs...), current.SourceFingerprint
			carried.SourceStatus = "current"
			replaceReference(result, current.CandidateID, carried)
		}
		oldGap, newGap := unmatchedReferenceIndexes(len(oldRefs), oldMatched), unmatchedReferenceIndexes(len(newRefs), newMatched)
		if len(oldGap) == len(newGap) {
			for i := range oldGap {
				carried, current := oldRefs[oldGap[i]], newRefs[newGap[i]]
				carried.PriorSourceFingerprint = stringPtr(carried.SourceFingerprint)
				carried.RawURL, carried.URLDigest, carried.URLOccurrence = current.RawURL, current.URLDigest, current.URLOccurrence
				carried.SourceLineRefs, carried.SourceFingerprint = append([]int(nil), current.SourceLineRefs...), current.SourceFingerprint
				markReferenceChanged(&carried, revision, "source_changed")
				replaceReference(result, current.CandidateID, carried)
			}
		} else {
			for _, index := range oldGap {
				carried := oldRefs[index]
				if !carried.UserModified && carried.Action == "discard" {
					continue
				}
				markReferenceChanged(&carried, revision, "source_missing")
				result.ReferenceLinkCandidates = append(result.ReferenceLinkCandidates, carried)
			}
		}
	}
	sort.SliceStable(result.ReferenceLinkCandidates, func(i, j int) bool {
		return referenceLess(result.ReferenceLinkCandidates[i], result.ReferenceLinkCandidates[j])
	})
}

func reconcileReadinessLinks(result *CandidateSnapshot, old []ReadinessLinkCandidate, revision int64) {
	byID := map[string]ContentCandidate{}
	for _, candidate := range result.ContentCandidates {
		byID[candidate.CandidateID] = candidate
	}
	result.ReadinessLinkCandidates = make([]ReadinessLinkCandidate, 0, len(old))
	for _, link := range old {
		readiness, readinessFound := byID[link.ReadinessClientOrIDRef]
		shot, shotFound := byID[link.ShotClientOrIDRef]
		invalidReadiness := readinessFound && (readiness.Kind != "readiness" || readiness.SourceStatus != "current" || readiness.Action == "discard")
		invalidShot := shotFound && (shot.Kind != "shot" || shot.SourceStatus != "current" || shot.Action == "discard")
		if invalidReadiness || invalidShot || (!readinessFound && stringsHasSessionRef(link.ReadinessClientOrIDRef)) || (!shotFound && stringsHasSessionRef(link.ShotClientOrIDRef)) {
			link.SourceStatus, link.Action = "source_changed", "needs_confirmation"
			link.SourceChangeRevision, link.AcknowledgedSourceChangeRevision = &revision, nil
		} else {
			link.SourceStatus = "current"
		}
		result.ReadinessLinkCandidates = append(result.ReadinessLinkCandidates, link)
	}
}

func referencesForFingerprint(all []ReferenceLinkCandidate, fingerprint string) []ReferenceLinkCandidate {
	result := []ReferenceLinkCandidate{}
	for _, item := range all {
		if item.SourceFingerprint == fingerprint && item.SourceStatus != "source_missing" {
			result = append(result, item)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].URLOccurrence < result[j].URLOccurrence })
	return result
}
func digestLCSPairs(old, fresh []ReferenceLinkCandidate) []lcsPair {
	oldSegments := make([]ParsedSegment, len(old))
	newSegments := make([]ParsedSegment, len(fresh))
	for i := range old {
		oldSegments[i].Fingerprint = old[i].URLDigest
	}
	for i := range fresh {
		newSegments[i].Fingerprint = fresh[i].URLDigest
	}
	return lcsPairs(oldSegments, newSegments)
}
func unmatchedReferenceIndexes(length int, matched map[int]bool) []int {
	result := []int{}
	for i := 0; i < length; i++ {
		if !matched[i] {
			result = append(result, i)
		}
	}
	return result
}
func replaceReference(result *CandidateSnapshot, freshID string, carried ReferenceLinkCandidate) {
	for i := range result.ReferenceLinkCandidates {
		if result.ReferenceLinkCandidates[i].CandidateID == freshID {
			result.ReferenceLinkCandidates[i] = carried
			return
		}
	}
	result.ReferenceLinkCandidates = append(result.ReferenceLinkCandidates, carried)
}
func markReferenceChanged(candidate *ReferenceLinkCandidate, revision int64, status string) {
	candidate.SourceStatus, candidate.Action = status, "needs_confirmation"
	candidate.SourceChangeRevision = &revision
	candidate.AcknowledgedSourceChangeRevision = nil
}
func referenceLess(a, b ReferenceLinkCandidate) bool {
	al, bl := 0, 0
	if len(a.SourceLineRefs) > 0 {
		al = a.SourceLineRefs[0]
	}
	if len(b.SourceLineRefs) > 0 {
		bl = b.SourceLineRefs[0]
	}
	if al != bl {
		return al < bl
	}
	if a.URLOccurrence != b.URLOccurrence {
		return a.URLOccurrence < b.URLOccurrence
	}
	return a.CandidateID < b.CandidateID
}
func stringsHasSessionRef(value string) bool { return len(value) > 4 && value[:4] == "ing_" }

type lcsPair struct{ old, new int }

func lcsPairs(old, fresh []ParsedSegment) []lcsPair {
	n, m := len(old), len(fresh)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if old[i].Fingerprint == fresh[j].Fingerprint {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	pairs := []lcsPair{}
	for i, j := 0, 0; i < n && j < m; {
		if old[i].Fingerprint == fresh[j].Fingerprint && dp[i][j] == dp[i+1][j+1]+1 {
			pairs = append(pairs, lcsPair{i, j})
			i++
			j++
			continue
		}
		if dp[i+1][j] > dp[i][j+1] {
			i++
		} else {
			j++
		} // ties advance new index
	}
	return pairs
}

func rangeGaps(oldLen, newLen int, pairs []lcsPair) [][4]int {
	result := [][4]int{}
	oldAt, newAt := 0, 0
	for _, pair := range pairs {
		if oldAt < pair.old || newAt < pair.new {
			result = append(result, [4]int{oldAt, pair.old, newAt, pair.new})
		}
		oldAt, newAt = pair.old+1, pair.new+1
	}
	if oldAt < oldLen || newAt < newLen {
		result = append(result, [4]int{oldAt, oldLen, newAt, newLen})
	}
	return result
}

func snapshotFromParse(fresh ParseOutput) CandidateSnapshot {
	return CandidateSnapshot{
		Segments:                append([]ParsedSegment{}, fresh.Segments...),
		ContentCandidates:       append([]ContentCandidate{}, fresh.ContentCandidates...),
		ReadinessLinkCandidates: append([]ReadinessLinkCandidate{}, fresh.ReadinessLinkCandidates...),
		ReferenceLinkCandidates: append([]ReferenceLinkCandidate{}, fresh.ReferenceLinkCandidates...),
		DroppedCandidates:       append([]DroppedCandidate{}, fresh.DroppedCandidates...),
		AssetBindingCandidates:  append([]AssetBindingDecision{}, fresh.AssetBindingCandidates...),
	}
}

func normalizeCandidateSnapshot(snapshot *CandidateSnapshot) {
	if snapshot.Segments == nil {
		snapshot.Segments = []ParsedSegment{}
	}
	if snapshot.ContentCandidates == nil {
		snapshot.ContentCandidates = []ContentCandidate{}
	}
	if snapshot.ReadinessLinkCandidates == nil {
		snapshot.ReadinessLinkCandidates = []ReadinessLinkCandidate{}
	}
	if snapshot.ReferenceLinkCandidates == nil {
		snapshot.ReferenceLinkCandidates = []ReferenceLinkCandidate{}
	}
	if snapshot.DroppedCandidates == nil {
		snapshot.DroppedCandidates = []DroppedCandidate{}
	}
	if snapshot.AssetBindingCandidates == nil {
		snapshot.AssetBindingCandidates = []AssetBindingDecision{}
	}
}

func reconcileMissingReferences(result *CandidateSnapshot, old CandidateSnapshot, oldStart, oldEnd int, revision int64) {
	existing := make(map[string]struct{}, len(result.ReferenceLinkCandidates))
	for _, reference := range result.ReferenceLinkCandidates {
		existing[reference.CandidateID] = struct{}{}
	}
	for _, segment := range old.Segments[oldStart:oldEnd] {
		for _, reference := range referencesForFingerprint(old.ReferenceLinkCandidates, segment.Fingerprint) {
			if _, ok := existing[reference.CandidateID]; ok || (!reference.UserModified && reference.Action == "discard") {
				continue
			}
			markReferenceChanged(&reference, revision, "source_missing")
			result.ReferenceLinkCandidates = append(result.ReferenceLinkCandidates, reference)
			existing[reference.CandidateID] = struct{}{}
		}
	}
}

func carryContentAtSegment(result *CandidateSnapshot, old []ContentCandidate, oldSegment, newSegment ParsedSegment, changed bool, revision int64) {
	for _, candidate := range old {
		if candidate.SourceFingerprint != oldSegment.Fingerprint || candidate.SourceStatus == "source_missing" || len(candidate.SourceLineRefs) == 0 || candidate.SourceLineRefs[0] != oldSegment.FirstLine {
			continue
		}
		candidate.SourceLineRefs = append([]int(nil), newSegment.LineRefs...)
		candidate.OriginalExcerpt = newSegment.Original
		candidate.NormalizedContent = newSegment.Normalized
		if changed {
			candidate.PriorSourceFingerprint = stringPtr(candidate.SourceFingerprint)
			candidate.SourceFingerprint = newSegment.Fingerprint
			candidate.SourceStatus = "source_changed"
			candidate.Action = "needs_confirmation"
			setSourceRevision(&candidate, revision)
		} else {
			candidate.SourceStatus = "current"
		}
		replaceCandidate(&result.ContentCandidates, freshCandidateIDForSegment(result.ContentCandidates, newSegment), candidate)
	}
}

func reconcileDropped(result *CandidateSnapshot, old []DroppedCandidate) {
	byKey := make(map[string][]DroppedCandidate, len(old))
	for _, candidate := range old {
		key := candidate.Reason + "\x00" + candidate.Original
		byKey[key] = append(byKey[key], candidate)
	}
	for i := range result.DroppedCandidates {
		key := result.DroppedCandidates[i].Reason + "\x00" + result.DroppedCandidates[i].Original
		candidates := byKey[key]
		if len(candidates) == 0 {
			continue
		}
		carried := candidates[0]
		byKey[key] = candidates[1:]
		carried.Reason = result.DroppedCandidates[i].Reason
		carried.SourceLineRefs = append([]int(nil), result.DroppedCandidates[i].SourceLineRefs...)
		carried.Original = result.DroppedCandidates[i].Original
		carried.WinnerID = result.DroppedCandidates[i].WinnerID
		carried.Action = result.DroppedCandidates[i].Action
		result.DroppedCandidates[i] = carried
	}
}

func replaceCandidate(list *[]ContentCandidate, freshID string, candidate ContentCandidate) {
	for i := range *list {
		if (*list)[i].CandidateID == freshID || (*list)[i].CandidateID == candidate.CandidateID {
			(*list)[i] = candidate
			return
		}
	}
	*list = append(*list, candidate)
	sort.SliceStable(*list, func(i, j int) bool {
		if (*list)[i].SourceLineRefs[0] != (*list)[j].SourceLineRefs[0] {
			return (*list)[i].SourceLineRefs[0] < (*list)[j].SourceLineRefs[0]
		}
		return (*list)[i].CandidateID < (*list)[j].CandidateID
	})
}

func segmentIndexForCandidate(segments []ParsedSegment, candidate ContentCandidate) int {
	for i, segment := range segments {
		if segment.Fingerprint == candidate.SourceFingerprint && len(candidate.SourceLineRefs) > 0 && segment.FirstLine == candidate.SourceLineRefs[0] {
			return i
		}
	}
	return -1
}
func freshCandidateIDForSegment(candidates []ContentCandidate, segment ParsedSegment) string {
	for _, candidate := range candidates {
		if candidate.SourceFingerprint == segment.Fingerprint && len(candidate.SourceLineRefs) > 0 && candidate.SourceLineRefs[0] == segment.FirstLine {
			return candidate.CandidateID
		}
	}
	return ""
}
func setSourceRevision(candidate *ContentCandidate, revision int64) {
	candidate.SourceChangeRevision = &revision
	candidate.AcknowledgedSourceChangeRevision = nil
}
