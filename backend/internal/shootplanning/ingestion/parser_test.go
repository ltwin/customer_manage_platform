package ingestion

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestPreviewOverrideJSONUsesPublicSnakeCaseFields(t *testing.T) {
	var body struct {
		ContentOverrides        []ContentCandidateOverride       `json:"content_overrides"`
		ReferenceLinkOverrides  []ReferenceLinkCandidateOverride `json:"reference_link_overrides"`
		ReadinessLinkSelections []ReadinessLinkSelection         `json:"readiness_link_selections"`
	}
	if err := json.Unmarshal([]byte(`{
		"content_overrides":[{"candidate_id":"content-1","kind":"readiness","title":"手套","action":"keep","acknowledge_source_change_revision":2}],
		"reference_link_overrides":[{"candidate_id":"reference-1","target_kind":"shot","target_client_or_id_ref":"shot-1","action":"keep"}],
		"readiness_link_selections":[{"readiness_client_or_id_ref":"content-1","shot_client_or_id_ref":"shot-1"}]
	}`), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.ContentOverrides) != 1 || body.ContentOverrides[0].CandidateID != "content-1" || body.ContentOverrides[0].Kind != "readiness" || body.ContentOverrides[0].AcknowledgeSourceChangeRevision == nil || *body.ContentOverrides[0].AcknowledgeSourceChangeRevision != 2 {
		t.Fatalf("content override JSON did not decode public fields: %#v", body.ContentOverrides)
	}
	if len(body.ReferenceLinkOverrides) != 1 || body.ReferenceLinkOverrides[0].CandidateID != "reference-1" || body.ReferenceLinkOverrides[0].TargetClientOrIDRef == nil || *body.ReferenceLinkOverrides[0].TargetClientOrIDRef != "shot-1" {
		t.Fatalf("reference override JSON did not decode public fields: %#v", body.ReferenceLinkOverrides)
	}
	if len(body.ReadinessLinkSelections) != 1 || body.ReadinessLinkSelections[0].ReadinessClientOrIDRef != "content-1" || body.ReadinessLinkSelections[0].ShotClientOrIDRef != "shot-1" {
		t.Fatalf("readiness selection JSON did not decode public fields: %#v", body.ReadinessLinkSelections)
	}
}

func TestSnapshotFromParseUsesJSONArraysForEmptyCollections(t *testing.T) {
	snapshot := snapshotFromParse(ParseOutput{})
	if snapshot.Segments == nil || snapshot.ContentCandidates == nil || snapshot.ReadinessLinkCandidates == nil || snapshot.ReferenceLinkCandidates == nil || snapshot.DroppedCandidates == nil || snapshot.AssetBindingCandidates == nil {
		t.Fatalf("empty candidate collections must marshal as arrays: %#v", snapshot)
	}
}

func TestParserGoldenURLSpeakerAndReadiness(t *testing.T) {
	input := ParseInput{SessionID: "ses_1", ParserVersion: ParserVersionV1, FirstSeenSessionRevision: 1, SourceText: "小林：https://example.com/a?x=1#f。\n\n要带白手套\n\n（图片2张）"}
	one, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one.Canonical, two.Canonical) {
		t.Fatal("canonical output is not byte stable")
	}
	if len(one.ReferenceLinkCandidates) != 1 || one.ReferenceLinkCandidates[0].RawURL != "https://example.com/a?x=1#f" {
		t.Fatalf("unexpected reference candidates: %#v", one.ReferenceLinkCandidates)
	}
	if len(one.ContentCandidates) != 1 || one.ContentCandidates[0].Kind != "readiness" {
		t.Fatalf("unexpected content candidates: %#v", one.ContentCandidates)
	}
	if one.ContentCandidates[0].NormalizedContent != "要带白手套" {
		t.Fatalf("speaker/url cleanup changed content: %#v", one.ContentCandidates[0])
	}
	if len(one.DroppedCandidates) != 3 {
		t.Fatalf("expected blank runs and placeholder drops, got %#v", one.DroppedCandidates)
	}
	if one.DroppedCandidates[0].CandidateID == one.DroppedCandidates[1].CandidateID {
		t.Fatalf("blank dropped candidates must have unique IDs: %#v", one.DroppedCandidates)
	}
}

func TestParserDoesNotConfuseTimeOrProtocolWithSpeaker(t *testing.T) {
	out, err := Parse(ParseInput{SessionID: "ses_2", FirstSeenSessionRevision: 1, SourceText: "15:00 集合\nhttps://example.com/a\nftp://example.com/file"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ContentCandidates) != 1 {
		t.Fatalf("expected one paragraph candidate, got %#v", out.ContentCandidates)
	}
	if out.ContentCandidates[0].NormalizedContent != "15:00 集合 ftp://example.com/file" {
		t.Fatalf("time was treated as speaker: %#v", out.ContentCandidates[0])
	}
}

func TestParserLimitsAreAtomic(t *testing.T) {
	_, err := Parse(ParseInput{SessionID: "ses_3", FirstSeenSessionRevision: 1, SourceText: "a\x00b"})
	if err == nil {
		t.Fatal("expected control character error")
	}
	_, err = Parse(ParseInput{SessionID: "ses_3", FirstSeenSessionRevision: 1, StagedAssetIntentCount: MaxStagedAssetIntents + 1, SourceText: "shot"})
	var limit CandidateLimitError
	if !errors.As(err, &limit) || !errors.Is(err, ErrCandidateLimitExceeded) {
		t.Fatalf("expected candidate limit, got %v", err)
	}
}

func TestReconcileUsesLCSAndRequiresChangedAck(t *testing.T) {
	old, err := Parse(ParseInput{SessionID: "ses_4", FirstSeenSessionRevision: 1, SourceText: "first\n\nsecond"})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Parse(ParseInput{SessionID: "ses_4", FirstSeenSessionRevision: 2, SourceText: "inserted\n\nfirst\n\nchanged"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Reconcile(snapshotFromParse(old), fresh, 2)
	if err != nil {
		t.Fatal(err)
	}
	var foundChanged, foundCurrent bool
	for _, c := range result.ContentCandidates {
		if c.NormalizedContent == "changed" && c.SourceStatus == "source_changed" && c.Action == "needs_confirmation" {
			foundChanged = true
		}
		if c.NormalizedContent == "first" && c.SourceStatus == "current" {
			foundCurrent = true
		}
	}
	if len(result.ContentCandidates) != 3 {
		t.Fatalf("reconcile duplicated candidates: %#v", result.ContentCandidates)
	}
	if !foundChanged || !foundCurrent {
		t.Fatalf("unexpected reconciliation: %#v", result.ContentCandidates)
	}
}

func TestReconcileKeepsDuplicateSegmentOccurrencesSeparate(t *testing.T) {
	old, err := Parse(ParseInput{SessionID: "ses_dup_reparse", FirstSeenSessionRevision: 1, SourceText: "同一镜头\n\n同一镜头"})
	if err != nil {
		t.Fatal(err)
	}
	old.ContentCandidates[0].Title = "第一条已编辑"
	old.ContentCandidates[0].UserModified = true
	fresh, err := Parse(ParseInput{SessionID: "ses_dup_reparse", FirstSeenSessionRevision: 2, SourceText: "同一镜头\n\n同一镜头"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Reconcile(snapshotFromParse(old), fresh, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ContentCandidates) != 2 || result.ContentCandidates[0].CandidateID != old.ContentCandidates[0].CandidateID || result.ContentCandidates[1].CandidateID != old.ContentCandidates[1].CandidateID || result.ContentCandidates[0].Title != "第一条已编辑" {
		t.Fatalf("duplicate occurrences were not carried independently: %#v", result.ContentCandidates)
	}
}

func TestReconcileCarriesDroppedIdentityAcrossPreviewRevision(t *testing.T) {
	old, err := Parse(ParseInput{SessionID: "ses_drop_reparse", FirstSeenSessionRevision: 1, SourceText: "第一段\n\n第二段"})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Parse(ParseInput{SessionID: "ses_drop_reparse", FirstSeenSessionRevision: 2, SourceText: "第一段\n\n第二段"})
	if err != nil {
		t.Fatal(err)
	}
	if len(old.DroppedCandidates) != 1 || len(fresh.DroppedCandidates) != 1 {
		t.Fatalf("expected one blank dropped candidate: old=%#v fresh=%#v", old.DroppedCandidates, fresh.DroppedCandidates)
	}
	result, err := Reconcile(snapshotFromParse(old), fresh, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.DroppedCandidates[0].CandidateID != old.DroppedCandidates[0].CandidateID {
		t.Fatalf("dropped identity changed across preview: old=%s got=%s", old.DroppedCandidates[0].CandidateID, result.DroppedCandidates[0].CandidateID)
	}
}

func TestParserURLOccurrenceIsOneBasedAndCanonicalArraysAreNonNull(t *testing.T) {
	out, err := Parse(ParseInput{SessionID: "ses_url_occurrence", FirstSeenSessionRevision: 1, SourceText: "参考 https://example.com/a https://example.com/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ReferenceLinkCandidates) != 2 || out.ReferenceLinkCandidates[0].URLOccurrence != 1 || out.ReferenceLinkCandidates[1].URLOccurrence != 2 {
		t.Fatalf("URL occurrences are not one-based: %#v", out.ReferenceLinkCandidates)
	}
	blank, err := Parse(ParseInput{SessionID: "ses_empty_canonical", FirstSeenSessionRevision: 1, SourceText: "\n"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blank.Canonical, []byte(":null")) {
		t.Fatalf("canonical output contains null collections: %s", blank.Canonical)
	}
}

func TestParserURLOccurrenceFollowsParagraphOrderAcrossDigests(t *testing.T) {
	out, err := Parse(ParseInput{SessionID: "ses_url_order", FirstSeenSessionRevision: 1, SourceText: "A https://example.com/a B https://example.com/b C https://example.com/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ReferenceLinkCandidates) != 3 || out.ReferenceLinkCandidates[0].URLOccurrence != 1 || out.ReferenceLinkCandidates[1].URLOccurrence != 2 || out.ReferenceLinkCandidates[2].URLOccurrence != 3 {
		t.Fatalf("URL occurrence must follow source order: %#v", out.ReferenceLinkCandidates)
	}
}

func TestParserCarriesStagedAssetIntentsIntoSnapshot(t *testing.T) {
	intent := AssetBindingDecision{CandidateID: "asset-a", AssetID: "asset-a", Generation: 2, TargetKind: "plan", TargetClientOrIDRef: "plan-1", Purpose: "moodboard_display"}
	out, err := Parse(ParseInput{SessionID: "ses_asset", FirstSeenSessionRevision: 1, StagedAssetIntents: []AssetBindingDecision{intent}, SourceText: ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.AssetBindingCandidates) != 1 || out.AssetBindingCandidates[0] != intent {
		t.Fatalf("asset intent was not preserved: %#v", out.AssetBindingCandidates)
	}
}

func TestParserDuplicateProducesRecoverableDrop(t *testing.T) {
	out, err := Parse(ParseInput{SessionID: "ses_dup", FirstSeenSessionRevision: 1, SourceText: "同一镜头\n\n同一镜头"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ContentCandidates) != 2 || out.ContentCandidates[0].Action != "keep" || out.ContentCandidates[1].Action != "discard" {
		t.Fatalf("duplicate actions are not deterministic: %#v", out.ContentCandidates)
	}
	var duplicate *DroppedCandidate
	for i := range out.DroppedCandidates {
		if out.DroppedCandidates[i].Reason == "duplicate" {
			duplicate = &out.DroppedCandidates[i]
		}
	}
	if duplicate == nil || duplicate.WinnerID == nil || *duplicate.WinnerID != out.ContentCandidates[0].CandidateID {
		t.Fatalf("duplicate provenance missing: %#v", out.DroppedCandidates)
	}
}

func TestReconcileReferenceURLChangePreservesEditedIdentity(t *testing.T) {
	old, err := Parse(ParseInput{SessionID: "ses_ref", FirstSeenSessionRevision: 1, SourceText: "参考 https://example.com/old"})
	if err != nil {
		t.Fatal(err)
	}
	label := "站姿参考"
	target := "shot_existing"
	old.ReferenceLinkCandidates[0].Label, old.ReferenceLinkCandidates[0].TargetKind, old.ReferenceLinkCandidates[0].TargetClientOrIDRef, old.ReferenceLinkCandidates[0].UserModified = &label, "shot", &target, true
	fresh, err := Parse(ParseInput{SessionID: "ses_ref", FirstSeenSessionRevision: 2, SourceText: "参考 https://example.com/new"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Reconcile(snapshotFromParse(old), fresh, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReferenceLinkCandidates) != 1 {
		t.Fatalf("unexpected references: %#v", result.ReferenceLinkCandidates)
	}
	got := result.ReferenceLinkCandidates[0]
	if got.CandidateID != old.ReferenceLinkCandidates[0].CandidateID || got.Label == nil || *got.Label != label || got.Action != "needs_confirmation" || got.SourceChangeRevision == nil || *got.SourceChangeRevision != 2 {
		t.Fatalf("edited reference was not reconciled: %#v", got)
	}
}

func TestReconcileKeepsDeletedEditedReferenceAsSourceMissing(t *testing.T) {
	old, err := Parse(ParseInput{SessionID: "ses_ref_missing", FirstSeenSessionRevision: 1, SourceText: "第一段 https://example.com/keep\n\n第二段"})
	if err != nil {
		t.Fatal(err)
	}
	old.ReferenceLinkCandidates[0].UserModified = true
	fresh, err := Parse(ParseInput{SessionID: "ses_ref_missing", FirstSeenSessionRevision: 2, SourceText: "第二段"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Reconcile(snapshotFromParse(old), fresh, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReferenceLinkCandidates) != 1 || result.ReferenceLinkCandidates[0].CandidateID != old.ReferenceLinkCandidates[0].CandidateID || result.ReferenceLinkCandidates[0].SourceStatus != "source_missing" || result.ReferenceLinkCandidates[0].Action != "needs_confirmation" {
		t.Fatalf("deleted edited reference was not retained for confirmation: %#v", result.ReferenceLinkCandidates)
	}
}

func TestBuildReadinessLinkCandidatesZeroOneAndMany(t *testing.T) {
	parsed, err := Parse(ParseInput{SessionID: "ses_links", FirstSeenSessionRevision: 1, SourceText: "要带灯架\n\n站姿正面\n\n侧身回头"})
	if err != nil {
		t.Fatal(err)
	}
	readiness := parsed.ContentCandidates[0]
	shotA, shotB := parsed.ContentCandidates[1], parsed.ContentCandidates[2]
	zero, err := BuildReadinessLinkCandidates(ParseInput{SessionID: "ses_links", FirstSeenSessionRevision: 1}, parsed.ContentCandidates, nil)
	if err != nil || len(zero) != 0 {
		t.Fatalf("zero links failed: %#v %v", zero, err)
	}
	one, err := BuildReadinessLinkCandidates(ParseInput{SessionID: "ses_links", FirstSeenSessionRevision: 1}, parsed.ContentCandidates, []ReadinessLinkSelection{{ReadinessClientOrIDRef: readiness.CandidateID, ShotClientOrIDRef: shotA.CandidateID}})
	if err != nil || len(one) != 1 {
		t.Fatalf("one link failed: %#v %v", one, err)
	}
	many, err := BuildReadinessLinkCandidates(ParseInput{SessionID: "ses_links", FirstSeenSessionRevision: 1}, parsed.ContentCandidates, []ReadinessLinkSelection{{ReadinessClientOrIDRef: readiness.CandidateID, ShotClientOrIDRef: shotB.CandidateID}, {ReadinessClientOrIDRef: readiness.CandidateID, ShotClientOrIDRef: shotA.CandidateID}})
	if err != nil || len(many) != 2 {
		t.Fatalf("many links failed: %#v %v", many, err)
	}
	if len(many[0].SourceLineRefs) != 2 || many[0].CandidateID == many[1].CandidateID {
		t.Fatalf("link provenance/identity missing: %#v", many)
	}
	parsed.ContentCandidates[1].Action = "discard"
	if _, err := BuildReadinessLinkCandidates(ParseInput{SessionID: "ses_links", FirstSeenSessionRevision: 1}, parsed.ContentCandidates, []ReadinessLinkSelection{{ReadinessClientOrIDRef: readiness.CandidateID, ShotClientOrIDRef: shotA.CandidateID}}); err == nil {
		t.Fatal("discarded endpoint must not accept a new readiness link")
	}
}
