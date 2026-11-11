package creativeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type characterImageReader struct {
	bytes  []byte
	failed string
	// down fails like object storage would, not like a bad picture.
	down string
}

func (r characterImageReader) AgentImage(_ context.Context, _ store.AccountScope, id string, _ int64) (*llmgateway.ImageInput, error) {
	if id == r.failed {
		return nil, creativemedia.ErrUnsupported
	}
	if id == r.down {
		return nil, errors.New("object storage unavailable")
	}
	return &llmgateway.ImageInput{
		RevisionID: id, Digest: "sha256-" + strings.Repeat("a", 64), MIME: "image/png", ByteSize: int64(len(r.bytes)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(r.bytes)), nil },
	}, nil
}

func TestCharacterReadAssetsAttachesAllowedViewsInOrder(t *testing.T) {
	f := setup(t)
	useModel(t, f, func(m *llmgateway.ModelConfig) { m.Capability.ImageInput = true })
	f.queue()
	ctx := t.Context()
	front := f.unlistedImage()
	broken := f.unlistedImage()
	side := f.unlistedImage()
	original := f.unlistedImage()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	f.service.SetImageReader(characterImageReader{bytes: buf.Bytes(), failed: broken})
	payload := creativecontent.CharacterPayload{
		Name: "云", Aliases: []string{"Yun"}, Appearance: "青绿色短发",
		IP: &creativecontent.CharacterIP{Name: "FATE"}, Work: "FGO",
		Views: []creativecontent.CharacterView{
			{ContentRevisionID: front, View: "front"}, {ContentRevisionID: broken, View: "back"}, {ContentRevisionID: side, View: "side"},
		},
		Sources: []creativecontent.CharacterSource{{ContentRevisionID: original}},
	}
	receipt, err := creativelibrary.CreateCharacter(ctx, f.alice, command(t, creativelibrary.CreateCharacterInput{Character: payload}))
	if err != nil {
		t.Fatal(err)
	}
	var card creativelibrary.AssetResult
	if err := json.Unmarshal(receipt.Outcome.Response, &card); err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":8192}`, card.ContentRevisionID)
	external := f.external(false)
	found, err := external.Call(ctx, f.principal("alice"), searchAssetsTool, json.RawMessage(`{"kind":"character","query":"Yun","character_ip":"fate","character_work":"FGO","limit":5}`))
	if err != nil || !strings.Contains(string(found.Structured), card.ContentRevisionID) || !strings.Contains(string(found.Structured), "青绿色短发") {
		t.Fatalf("search role cards: %+v %v", found, err)
	}
	result, err := external.Call(ctx, f.principal("alice"), readAssetTool, json.RawMessage(args))
	if err != nil || len(result.Images) != 2 || !strings.Contains(string(result.Structured), "image_unreadable") ||
		!strings.Contains(string(result.Structured), original) {
		t.Fatalf("external read: %s images=%d, %v", result.Structured, len(result.Images), err)
	}
	if _, err := external.Call(ctx, f.principal("bob"), readAssetTool, json.RawMessage(args)); externalCode(err) != "not_found" {
		t.Fatalf("cross-account read: %v", err)
	}
	f.service.SetImageReader(characterImageReader{bytes: buf.Bytes(), failed: broken, down: side})
	degraded, err := external.Call(ctx, f.principal("alice"), readAssetTool, json.RawMessage(args))
	if err != nil || len(degraded.Images) != 1 || strings.Count(string(degraded.Structured), "image_unreadable") != 2 ||
		!strings.Contains(degraded.Summary, "另有 2 张没有附上") || strings.Contains(degraded.Summary, "已附上图片。") {
		t.Fatalf("one failing view hid the card: %s %q images=%d, %v", degraded.Structured, degraded.Summary, len(degraded.Images), err)
	}
	f.service.SetImageReader(characterImageReader{bytes: buf.Bytes(), failed: broken})
	c := f.conversation(f.alice, f.canvasID)
	grant, err := f.service.GrantConsent(ctx, f.alice, command(t, map[string]any{
		"conversation_id": c.ID, "purpose": "creative_assistance", "mode": ScopeAccountLibrary,
		"data_classes": []string{"text", "media_metadata", "image_preview"}, "selected_revision_ids": []string{card.ContentRevisionID},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var consent Consent
	if err := json.Unmarshal(grant.Outcome.Response, &consent); err != nil {
		t.Fatal(err)
	}
	textOnly := f.conversation(f.alice, f.canvasID)
	narrow, err := f.service.GrantConsent(ctx, f.alice, command(t, map[string]any{
		"conversation_id": textOnly.ID, "purpose": "creative_assistance", "mode": ScopeAccountLibrary,
		"data_classes": []string{"text"}, "selected_revision_ids": []string{card.ContentRevisionID},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var narrowConsent Consent
	if err := json.Unmarshal(narrow.Outcome.Response, &narrowConsent); err != nil {
		t.Fatal(err)
	}
	// Refused when sent, before the run reserves budget.
	if _, err := f.startRun(f.alice, textOnly.ID, narrowConsent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: card.ContentRevisionID}); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("card reference without media_metadata: %v", err)
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: card.ContentRevisionID})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(ctx, f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &runtimeHandler{service: f.service, scope: f.alice, held: held, revisions: []string{card.ContentRevisionID}}
	read, err := h.readAsset(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []bool{false, true} {
		var source []*schema.Message
		if tool {
			// Frozen under a model that could not see images: this turn's
			// outcome replaces the stale omissions.
			var stale map[string]any
			if err := json.Unmarshal([]byte(read), &stale); err != nil {
				t.Fatal(err)
			}
			stale["images_omitted"] = []characterImageOmission{{front, "model_cannot_see_images"}}
			frozen, err := json.Marshal(stale)
			if err != nil {
				t.Fatal(err)
			}
			source = append(source, &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				{ID: "read-card", Function: schema.FunctionCall{Name: readAssetTool, Arguments: args}},
			}}, schema.ToolMessage(string(frozen), "read-card"))
		} else {
			var message *schema.Message
			if err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
				var err error
				message, _, err = f.service.resolveReference(ctx, tx, run.ID, card.ContentRevisionID, consent, true)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			source = append(source, message)
		}
		messages := make([]llmgateway.Message, len(source))
		for i, m := range source {
			messages[i] = llmgateway.Message{Role: llmgateway.Role(m.Role), Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: m.Content}}, ToolCallID: m.ToolCallID}
		}
		bound, err := h.bindMessages(ctx, source, messages)
		if err != nil {
			t.Fatal(err)
		}
		var attached []string
		var omitted bool
		for _, m := range bound {
			for _, b := range m.Blocks {
				if b.Image != nil {
					attached = append(attached, b.Image.RevisionID)
				}
				omitted = omitted || strings.Contains(b.Text, "image_unreadable")
				if strings.Contains(b.Text, "model_cannot_see_images") {
					t.Fatalf("tool=%v: stale omission kept: %s", tool, b.Text)
				}
			}
		}
		if !slices.Equal(attached, []string{front, side}) || !omitted {
			t.Fatalf("tool=%v: images=%v, omitted=%v", tool, attached, omitted)
		}
	}

	if err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		ref, err := f.service.readReference(ctx, tx, card.ContentRevisionID, consent, false)
		if err == nil && (ref.character == nil || len(ref.character.omitted) != 3 || ref.character.omitted[0].Reason != "model_cannot_see_images") {
			t.Fatalf("non-visual model did not receive character metadata with omissions: %+v", ref)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.editCanvas(ctx, f.canvasID, "place-character", fmt.Sprintf(
		`{"actions":[{"type":"add_node","key":"card","node_type":"character","asset_id":%q,"content_revision_id":%q},{"type":"add_node","key":"image","node_type":"image"},{"type":"connect","source_node_id":"card","target_node_id":"image"}]}`,
		card.ID, card.ContentRevisionID,
	)); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(ctx, f.alice, f.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(canvas.Nodes, func(node creativecanvas.Node) bool {
		return node.TypeKey == "core.character" && node.ContentRevisionID != nil && *node.ContentRevisionID == card.ContentRevisionID &&
			slices.ContainsFunc(canvas.Edges, func(edge creativecanvas.Edge) bool { return edge.SourceNodeID == node.ID })
	}) {
		t.Fatal("assistant did not connect the pinned role card")
	}

	// The canvas keeps its card when the asset goes to the trash, and an
	// external assistant reads that version like the built-in one does.
	if _, err := creativelibrary.Trash(ctx, f.alice, command(t, creativelibrary.AssetVersion{ID: card.ID, Revision: card.Revision})); err != nil {
		t.Fatal(err)
	}
	if trashed, err := external.Call(ctx, f.principal("alice"), readAssetTool, json.RawMessage(args)); err != nil || len(trashed.Images) != 2 {
		t.Fatalf("pinned version after trash: %+v %v", trashed, err)
	}
}

func TestFitCharacterFactsDropsNotesBeforeRefusing(t *testing.T) {
	urls := make([]string, 10)
	for i := range urls {
		urls[i] = "https://example.com/" + strings.Repeat("<&>", 600)
	}
	card := &creativecontent.CharacterPayload{
		Name: "芙宁娜", Appearance: strings.Repeat("白", 2000), Attribution: strings.Repeat("注", 2000),
		Views: []creativecontent.CharacterView{{ContentRevisionID: "ccrv_view", View: "front"}}, SourceURLs: urls,
	}
	result := map[string]any{"content_revision_id": "ccrv_card"}
	if err := fitCharacterFacts(result, &characterReference{card: card}, 16<<10); err != nil {
		t.Fatal(err)
	}
	fitted, _ := result["character"].(*creativecontent.CharacterPayload)
	omitted, _ := result["character_omitted"].([]string)
	if fitted == nil || fitted.SourceURLs != nil || fitted.Attribution == "" || !slices.Equal(omitted, []string{"source_urls"}) {
		t.Fatalf("fitted card: %+v, omitted %v", fitted, omitted)
	}
	if len(card.SourceURLs) != 10 {
		t.Fatal("fitting changed the stored card")
	}
	if err := fitCharacterFacts(map[string]any{}, &characterReference{card: card}, 1<<10); err == nil {
		t.Fatal("an oversized card was not refused")
	}
}
