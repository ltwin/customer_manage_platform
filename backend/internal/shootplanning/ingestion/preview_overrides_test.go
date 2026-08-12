package ingestion

import (
	"errors"
	"testing"
)

func TestApplyPreviewOverridesPersistsAckRestoreAndReadinessSelections(t *testing.T) {
	revision := int64(2)
	parsed := ParseOutput{
		ContentCandidates:       []ContentCandidate{{CandidateID: "shot-1", Kind: "shot", Title: "旧标题", Action: "needs_confirmation", SourceStatus: "source_changed", SourceChangeRevision: &revision}, {CandidateID: "shot-2", Kind: "shot", Title: "侧身", Action: "keep", SourceStatus: "current"}, {CandidateID: "ready-1", Kind: "readiness", Title: "手套", Action: "keep", SourceStatus: "current"}},
		ReferenceLinkCandidates: []ReferenceLinkCandidate{{CandidateID: "ref-1", RawURL: "https://example.com", TargetKind: "plan", Action: "keep", SourceStatus: "current"}},
		DroppedCandidates:       []DroppedCandidate{{CandidateID: "drop-1", Original: "补拍侧身", SourceLineRefs: []int{4}, Action: "discard"}},
	}
	current := Session{ID: "ing-1", ParserVersion: ParserVersionV1, Revision: 1, CandidateSnapshot: CandidateSnapshot{
		ReadinessLinkCandidates: []ReadinessLinkCandidate{{CandidateID: "link-old", ReadinessClientOrIDRef: "ready-1", ShotClientOrIDRef: "shot-1", Action: "keep", SourceStatus: "current"}},
	}}
	if err := applyPreviewOverrides(&parsed, current, PreviewInput{
		ContentOverrides:        []ContentCandidateOverride{{CandidateID: "shot-1", Kind: "shot", Title: "新标题", Action: "keep", AcknowledgeSourceChangeRevision: &revision}, {CandidateID: "drop-1", Kind: "shot", Title: "恢复侧身", Action: "keep"}},
		ReferenceLinkOverrides:  []ReferenceLinkCandidateOverride{{CandidateID: "ref-1", Action: "keep"}},
		ReadinessLinkOverrides:  []ReadinessLinkCandidateOverride{{CandidateID: "link-old", ReadinessClientOrIDRef: "ready-1", ShotClientOrIDRef: "shot-1", Action: "discard"}},
		ReadinessLinkSelections: []ReadinessLinkSelection{{ReadinessClientOrIDRef: "ready-1", ShotClientOrIDRef: "shot-2"}},
	}); err != nil {
		t.Fatalf("apply overrides: %v", err)
	}
	if len(parsed.ContentCandidates) != 4 || parsed.ContentCandidates[0].Action != "keep" || parsed.ContentCandidates[0].Title != "新标题" {
		t.Fatalf("content overrides not persisted: %+v", parsed.ContentCandidates)
	}
	if parsed.ContentCandidates[0].SourceStatus != "current" || parsed.ContentCandidates[0].AcknowledgedSourceChangeRevision == nil {
		t.Fatalf("source acknowledgement not persisted: %+v", parsed.ContentCandidates[0])
	}
	if len(parsed.ReadinessLinkCandidates) != 2 || parsed.ReadinessLinkCandidates[0].Action != "keep" || parsed.ReadinessLinkCandidates[1].Action != "discard" {
		t.Fatalf("readiness link selection/discard not persisted: %+v", parsed.ReadinessLinkCandidates)
	}
}

func TestApplyPreviewOverridesRejectsStaleSourceAcknowledgement(t *testing.T) {
	revision := int64(3)
	parsed := ParseOutput{ContentCandidates: []ContentCandidate{{CandidateID: "c-1", Kind: "shot", Title: "候选", Action: "needs_confirmation", SourceStatus: "source_changed", SourceChangeRevision: &revision}}}
	err := applyPreviewOverrides(&parsed, Session{}, PreviewInput{ContentOverrides: []ContentCandidateOverride{{CandidateID: "c-1", Action: "keep", AcknowledgeSourceChangeRevision: int64Ptr(2)}}})
	if !errors.Is(err, ErrPreviewOverride) {
		t.Fatalf("expected stale acknowledgement rejection, got %v", err)
	}
}

func TestApplyPreviewOverridesIsOrderIndependentAfterDroppedRestore(t *testing.T) {
	parsed := ParseOutput{
		ContentCandidates: []ContentCandidate{{CandidateID: "shot-1", Kind: "shot", Title: "旧标题", Action: "keep", SourceStatus: "current"}},
		DroppedCandidates: []DroppedCandidate{{CandidateID: "drop-1", Original: "恢复项", SourceLineRefs: []int{2}, Action: "discard"}},
	}
	if err := applyPreviewOverrides(&parsed, Session{}, PreviewInput{ContentOverrides: []ContentCandidateOverride{
		{CandidateID: "drop-1", Kind: "shot", Title: "已恢复", Action: "keep"},
		{CandidateID: "shot-1", Kind: "shot", Title: "新标题", Action: "keep"},
	}}); err != nil {
		t.Fatal(err)
	}
	if len(parsed.ContentCandidates) != 2 || parsed.ContentCandidates[0].Title != "新标题" || parsed.ContentCandidates[1].Title != "已恢复" {
		t.Fatalf("override order changed persistence result: %#v", parsed.ContentCandidates)
	}
	if len(parsed.DroppedCandidates) != 0 {
		t.Fatalf("restored candidate must be consumed from dropped collection: %#v", parsed.DroppedCandidates)
	}
}

func TestApplyPreviewOverridesRequiresReadinessLinkAcknowledgement(t *testing.T) {
	revision := int64(4)
	current := Session{ID: "ing-link-ack", ParserVersion: ParserVersionV1, Revision: 3, CandidateSnapshot: CandidateSnapshot{ReadinessLinkCandidates: []ReadinessLinkCandidate{{CandidateID: "link-1", ReadinessClientOrIDRef: "ready-1", ShotClientOrIDRef: "shot-1", Action: "needs_confirmation", SourceStatus: "source_changed", SourceChangeRevision: &revision}}}}
	parsed := ParseOutput{ContentCandidates: []ContentCandidate{{CandidateID: "ready-1", Kind: "readiness", Action: "keep", SourceStatus: "current"}, {CandidateID: "shot-1", Kind: "shot", Action: "keep", SourceStatus: "current"}}, ReadinessLinkCandidates: append([]ReadinessLinkCandidate(nil), current.CandidateSnapshot.ReadinessLinkCandidates...)}
	input := PreviewInput{
		ReadinessLinkSelections: []ReadinessLinkSelection{{ReadinessClientOrIDRef: "ready-1", ShotClientOrIDRef: "shot-1"}},
		ReadinessLinkOverrides:  []ReadinessLinkCandidateOverride{{CandidateID: "link-1", ReadinessClientOrIDRef: "ready-1", ShotClientOrIDRef: "shot-1", Action: "keep"}},
	}
	if err := applyPreviewOverrides(&parsed, current, input); !errors.Is(err, ErrPreviewOverride) {
		t.Fatalf("expected missing readiness-link acknowledgement rejection, got %v", err)
	}
	input.ReadinessLinkOverrides[0].AcknowledgeSourceChangeRevision = &revision
	if err := applyPreviewOverrides(&parsed, current, input); err != nil {
		t.Fatal(err)
	}
	if len(parsed.ReadinessLinkCandidates) != 1 || parsed.ReadinessLinkCandidates[0].SourceStatus != "current" || parsed.ReadinessLinkCandidates[0].AcknowledgedSourceChangeRevision == nil {
		t.Fatalf("readiness-link acknowledgement was not persisted: %#v", parsed.ReadinessLinkCandidates)
	}
}

func int64Ptr(value int64) *int64 { return &value }
