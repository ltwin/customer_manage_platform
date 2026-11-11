package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const characterRefKey = "creative_character_ref"

type characterImageOmission struct {
	RevisionID string `json:"content_revision_id"`
	Reason     string `json:"reason"`
}

type characterReference struct {
	card    *creativecontent.CharacterPayload
	sources map[string]creativecontent.TextureImage
	omitted []characterImageOmission
}

func (s *Service) readCharacterReference(ctx context.Context, tx store.TxAccountScope, rev creativecontent.Revision, consent Consent, imageCapable bool) (*characterReference, error) {
	card := rev.Payload.Character
	if card == nil {
		return nil, ErrUnsupportedSegment
	}
	sourceIDs := make([]string, 0, len(card.Sources))
	for _, source := range card.Sources {
		sourceIDs = append(sourceIDs, source.ContentRevisionID)
	}
	sources, err := creativecontent.TextureImagesInTx(ctx, tx, sourceIDs)
	if err != nil {
		return nil, err
	}
	facts := &characterReference{card: card, sources: sources}
	reason := characterImagePermission(consent, imageCapable, s.images != nil)
	if consent.Purpose != "external_agent" && reason != "" {
		for _, view := range card.Views {
			facts.omitted = append(facts.omitted, characterImageOmission{view.ContentRevisionID, reason})
		}
	}
	return facts, nil
}

func characterImagePermission(consent Consent, capable, available bool) string {
	switch {
	case !capable:
		return "model_cannot_see_images"
	case !consentHas(consent, "image_preview"):
		return "image_preview_not_allowed"
	case !available:
		return "image_preview_unavailable"
	default:
		return ""
	}
}

func (c *characterReference) addFacts(out map[string]any) {
	out["character"] = c.card
	out["source_images"] = c.sources
	if len(c.omitted) > 0 {
		out["images_omitted"] = c.omitted
	}
}

// fitCharacterFacts keeps a card within one tool result. The notes go first,
// longest last-resort fields last; name, appearance and views always stay.
func fitCharacterFacts(result map[string]any, c *characterReference, limit int) error {
	c.addFacts(result)
	card := *c.card
	var omitted []string
	fits := func() (bool, error) {
		result["character"] = &card
		if len(omitted) > 0 {
			result["character_omitted"] = omitted
		}
		raw, err := json.Marshal(result)
		return len(raw) <= limit, err
	}
	if ok, err := fits(); ok || err != nil {
		return err
	}
	card.SourceURLs, omitted = nil, append(omitted, "source_urls")
	if ok, err := fits(); ok || err != nil {
		return err
	}
	delete(result, "source_images")
	omitted = append(omitted, "source_images")
	if ok, err := fits(); ok || err != nil {
		return err
	}
	card.Attribution, omitted = "", append(omitted, "attribution")
	if ok, err := fits(); ok || err != nil {
		return err
	}
	return refuseTool("这张角色卡的信息超出单次读取上限")
}

func addCharacterImageOmission(out map[string]any, id, reason string) {
	old, _ := out["images_omitted"].([]characterImageOmission)
	out["images_omitted"] = append(old, characterImageOmission{id, reason})
}

func characterAttachmentID(m *schema.Message) string {
	if id, _ := m.Extra[characterRefKey].(string); id != "" {
		return id
	}
	if m.Role != schema.Tool {
		return ""
	}
	var fields struct {
		ID        string          `json:"content_revision_id"`
		Character json.RawMessage `json:"character"`
	}
	if json.Unmarshal([]byte(m.Content), &fields) == nil && len(fields.Character) > 0 {
		return fields.ID
	}
	return ""
}

type characterAttachments struct {
	id string
	// views are the card's views in order; omitted ones carry no position.
	views   []characterAttachmentView
	omitted []characterImageOmission
	// unavailable explains why no view could be checked (the card itself
	// is unreadable or its text may not leave); every listed view is omitted.
	unavailable string
}

type characterAttachmentView struct {
	id       string
	position int
}

func (h *runtimeHandler) characterAttachmentsInTx(ctx context.Context, tx store.TxAccountScope, id string, consent Consent) (characterAttachments, error) {
	out := characterAttachments{id: id}
	readable, err := readableInTx(ctx, tx, h.held.RunID, h.held.ConsentID, id)
	if err != nil {
		return out, err
	}
	if !readable || !consentHas(consent, "media_metadata") {
		out.unavailable = "media_metadata_not_allowed"
		return out, nil
	}
	r, err := creativecontent.RequireUsable(ctx, tx, id, "display")
	if unreadableImage(err) {
		out.unavailable = "character_unreadable"
		return out, nil
	}
	if err != nil || r.Kind != creativecontent.CharacterKind || r.Payload.Character == nil {
		return out, err
	}
	reason := characterImagePermission(consent, h.held.Model.Capability.ImageInput, h.service.images != nil)
	for i, view := range r.Payload.Character.Views {
		if reason != "" {
			out.omitted = append(out.omitted, characterImageOmission{view.ContentRevisionID, reason})
			continue
		}
		_, revoked, err := directlyReadableInTx(ctx, tx, h.held.RunID, h.held.ConsentID, view.ContentRevisionID, false)
		if err != nil {
			return out, err
		}
		if revoked {
			out.omitted = append(out.omitted, characterImageOmission{view.ContentRevisionID, "image_preview_not_allowed"})
			continue
		}
		out.views = append(out.views, characterAttachmentView{view.ContentRevisionID, i + 1})
	}
	return out, nil
}

// bindCharacterImages attaches the card's views and rewrites the card's
// images_omitted to this turn's outcome, so a result frozen under another
// model or grant never contradicts what is attached now. A view already
// attached for another reference is shared, not omitted.
func (h *runtimeHandler) bindCharacterImages(ctx context.Context, attachments characterAttachments, seen map[string]bool, total *int64, message *llmgateway.Message) ([]llmgateway.Block, string, error) {
	var blocks []llmgateway.Block
	var attached, shared []string
	for _, view := range attachments.views {
		reason := ""
		switch {
		case seen[view.id]:
			shared = append(shared, strconv.Itoa(view.position))
			continue
		case len(seen) >= h.service.limits.ImagePreviewCount:
			reason = "context_image_count_limit"
		default:
			handle, err := h.service.agentImage(ctx, h.scope, view.id, h.service.limits.ImagePreviewBytes)
			var limit *creativemedia.ImagePreviewLimitError
			switch {
			case errors.As(err, &limit):
				reason = "image_size_limit"
			case unreadableImage(err):
				reason = "image_unreadable"
			case err != nil:
				return nil, "", err
			case *total+handle.ByteSize > h.service.limits.ImagePreviewTotalBytes:
				reason = "context_image_byte_limit"
			default:
				seen[view.id] = true
				*total += handle.ByteSize
				attached = append(attached, strconv.Itoa(view.position))
				blocks = append(blocks, llmgateway.Block{Kind: llmgateway.BlockImageInput, Image: handle})
			}
		}
		if reason != "" {
			attachments.omitted = append(attachments.omitted, characterImageOmission{view.id, reason})
		}
	}
	for i := range message.Blocks {
		block := &message.Blocks[i]
		if block.Kind != llmgateway.BlockText {
			continue
		}
		label, body, found := strings.Cut(block.Text, "\n")
		if !found || !strings.HasPrefix(label, "<<<") {
			label, body = "", block.Text
		} else {
			label += "\n"
		}
		var fields map[string]any
		if json.Unmarshal([]byte(body), &fields) != nil || fields["character"] == nil {
			continue
		}
		omitted := attachments.omitted
		if attachments.unavailable != "" {
			omitted = nil
			for _, id := range characterViewIDs(fields["character"]) {
				omitted = append(omitted, characterImageOmission{id, attachments.unavailable})
			}
		}
		if len(omitted) != 0 {
			fields["images_omitted"] = omitted
		} else {
			delete(fields, "images_omitted")
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return nil, "", err
		}
		block.Text = label + string(raw)
		break
	}
	caption := ""
	if len(attached) > 0 {
		caption = "依次为 views 第 " + strings.Join(attached, "、") + " 张"
	}
	if len(shared) > 0 {
		if caption != "" {
			caption += "；"
		}
		caption += "第 " + strings.Join(shared, "、") + " 张与其他引用是同一张图，已在别处附上"
	}
	return blocks, caption, nil
}

func characterViewIDs(card any) []string {
	raw, err := json.Marshal(card)
	if err != nil {
		return nil
	}
	var payload creativecontent.CharacterPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	ids := make([]string, 0, len(payload.Views))
	for _, view := range payload.Views {
		ids = append(ids, view.ContentRevisionID)
	}
	return ids
}
