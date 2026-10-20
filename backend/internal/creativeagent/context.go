package creativeagent

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	readAssetTool   = "read_asset"
	readCanvasTool  = "read_canvas"
	readHistoryTool = "read_history"
	imageRefsKey    = "creative_image_refs"
)

//go:embed instructions/base.md
var assistantInstruction string

func referenceLabel(id string) string { return "<<<" + id + ">>>" }

func consentHas(c Consent, class string) bool { return slices.Contains(c.Scope.DataClasses, class) }

func approvedRevision(ctx context.Context, tx store.TxAccountScope, consentID, id string) (bool, error) {
	return tx.Exists(ctx, "creative_egress_consent_contents", "consent_id=$2 AND content_revision_id=$3", consentID, id)
}

// readableInTx combines explicit input, current canvas, conversation and library
// access. Revoked references are denied before any branch can grant access.
func readableInTx(ctx context.Context, tx store.TxAccountScope, runID, consentID, id string) (bool, error) {
	return readableRevisionInTx(ctx, tx, runID, consentID, id, true)
}

func readableRevisionInTx(ctx context.Context, tx store.TxAccountScope, runID, consentID, id string, library bool) (bool, error) {
	var canvasID, conversationID string
	if err := tx.QueryRow(ctx, "creative_agent_runs", "canvas_id,conversation_id", "id=$2", runID).Scan(&canvasID, &conversationID); err != nil {
		return false, err
	}
	revoked, err := tx.Exists(ctx, "creative_egress_consent_contents", "content_revision_id=$2 AND consent_id IN (SELECT id FROM creative_egress_consents WHERE account_id=$1 AND conversation_id=$3 AND revoked_at IS NOT NULL)", id, conversationID)
	if err != nil || revoked {
		return false, err
	}
	if approved, err := approvedRevision(ctx, tx, consentID, id); approved || err != nil {
		return approved, err
	}
	if present, err := tx.Exists(ctx, "creative_nodes", "canvas_id=$2 AND content_revision_id=$3", canvasID, id); present || err != nil {
		return present, err
	}
	present, err := tx.Exists(ctx, "creative_message_content_refs", "content_revision_id=$2 AND message_id IN (SELECT id FROM creative_agent_messages WHERE account_id=$1 AND conversation_id=$3 AND status='complete' AND (ordinal < (SELECT ordinal FROM creative_agent_messages WHERE account_id=$1 AND id=(SELECT trigger_message_id FROM creative_agent_runs WHERE account_id=$1 AND id=$4)) OR run_id=$4))", id, conversationID, runID)
	if err != nil || present || !library {
		return present, err
	}
	return tx.Exists(ctx, "creative_assets", "content_revision_id=$2 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM creative_egress_consents WHERE account_id=$1 AND id=$3 AND scope->>'mode'='account_library' AND revoked_at IS NULL)", id, consentID)
}

// resolveReference builds serializable framework input. Executable image
// handles are bound later, outside checkpoint state, at the gateway boundary.
// The flag reports whether the revision's content is attached, which is what
// the dispatch check has to keep re-examining; a bare label carries nothing.
func (s *Service) resolveReference(ctx context.Context, tx store.TxAccountScope, runID, id string, consent Consent, imageCapable bool) (*schema.Message, bool, error) {
	approved, err := readableInTx(ctx, tx, runID, consent.ID, id)
	if err != nil {
		return nil, false, err
	}
	if !approved {
		return schema.UserMessage(referenceLabel(id) + "（本轮未授权读取，只有引用标识）"), false, nil
	}
	ref, err := s.readReference(ctx, tx, id, consent, imageCapable)
	if err != nil {
		return nil, false, err
	}
	message, err := referenceMessage(id, ref)
	return message, err == nil, err
}

// referenceMessage writes what a reference gives into a user message: a text
// body quoted, a model's facts as JSON, an image to bind later.
func referenceMessage(id string, ref reference) (*schema.Message, error) {
	message := schema.UserMessage(referenceLabel(id))
	switch {
	case ref.model != nil:
		facts, err := json.Marshal(modelReference{Model: ref.model, ThumbnailOmitted: ref.thumbnailOmitted})
		if err != nil {
			return nil, err
		}
		message.Content += "\n" + string(facts)
	case ref.body != nil:
		message.Content += "\n" + quotedContentOpen + "\n" + *ref.body + "\n" + quotedContentClose
	}
	if ref.image != "" {
		message.Extra = map[string]any{imageRefsKey: ref.image}
	}
	return message, nil
}

// reference is what an approved revision gives the model: text, an image to
// bind, or a model's facts with its thumbnail when the model may see it.
type reference struct {
	body             *string
	image            string
	model            *creativecontent.ModelFacts
	thumbnailOmitted string
}

// modelReference is a model's facts as written into the conversation.
type modelReference struct {
	Model            *creativecontent.ModelFacts `json:"model"`
	ThumbnailOmitted string                      `json:"thumbnail_omitted,omitempty"`
}

// withoutThumbnail keeps a model's facts when its thumbnail is not sent: the
// image locator goes and thumbnail_omitted says why. A read_asset result is
// the facts as JSON; a reference is its label, a newline, then the JSON. ok
// is false for anything else, whose message carries the image alone.
func withoutThumbnail(content, reason string) (string, bool) {
	label, body, found := strings.Cut(content, "\n")
	if found {
		label += "\n"
	} else {
		label, body = "", content
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &fields) != nil || fields["model"] == nil {
		return content, false
	}
	why, err := json.Marshal(reason)
	if err != nil {
		return content, false
	}
	delete(fields, "image_revision_id")
	fields["thumbnail_omitted"] = why
	raw, err := json.Marshal(fields)
	if err != nil {
		return content, false
	}
	return label + string(raw), true
}

// readReference reads an approved revision under the consent's data classes.
func (s *Service) readReference(ctx context.Context, tx store.TxAccountScope, id string, consent Consent, imageCapable bool) (reference, error) {
	rev, err := creativecontent.RequireUsable(ctx, tx, id, "display")
	if err != nil {
		return reference{}, translateContent(err)
	}
	switch rev.Kind {
	case "text":
		if !consentHas(consent, "text") {
			return reference{}, ErrEgressRequired
		}
		return reference{body: rev.Payload.Body}, nil
	case "image":
		// Capability first: a model that cannot see images never asks for
		// image egress, and saying "not authorised" would send the
		// photographer looking for a permission that does not exist.
		if !imageCapable || s.images == nil {
			return reference{}, ErrModelCapability
		}
		if !consentHas(consent, "image_preview") {
			return reference{}, ErrEgressRequired
		}
		return reference{image: id}, nil
	case "model":
		if !consentHas(consent, "media_metadata") {
			return reference{}, ErrEgressRequired
		}
		facts, ok := creativecontent.ModelFactsOf(rev.Payload)
		if !ok {
			return reference{}, ErrUnsupportedSegment
		}
		// Size and parameters are useful on their own, so a missing thumbnail
		// is said, never refused.
		ref := reference{model: &facts}
		switch {
		case !imageCapable || s.images == nil:
			ref.thumbnailOmitted = "model_cannot_see_images"
		case !consentHas(consent, "image_preview"):
			ref.thumbnailOmitted = "image_preview_not_allowed"
		default:
			ref.image = id
		}
		return ref, nil
	default:
		return reference{}, ErrUnsupportedSegment
	}
}

func (h *runtimeHandler) bindMessages(ctx context.Context, source []*schema.Message, messages []llmgateway.Message) ([]llmgateway.Message, error) {
	imageAuthorized := false
	readable := map[string]bool{}
	if err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		c, err := readConsentForUpdate(ctx, tx, h.held.ConsentID)
		if err != nil {
			return err
		}
		imageAuthorized = consentHas(c, "image_preview")
		for _, message := range source {
			id := messageImageID(message)
			if id == "" || readable[id] {
				continue
			}
			if readable[id], err = readableInTx(ctx, tx, h.held.RunID, h.held.ConsentID, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var total int64
	var delayed []llmgateway.Message
	toolNames := map[string]string{}
	for _, message := range source {
		for _, call := range message.ToolCalls {
			toolNames[call.ID] = call.Function.Name
		}
	}
	for _, i := range imageMessageOrder(source) {
		message := source[i]
		id, _ := message.Extra[imageRefsKey].(string)
		if message.Role == schema.Tool && (toolNames[message.ToolCallID] == readAssetTool || toolNames[message.ToolCallID] == renderViewTool) {
			var result struct {
				ImageID string `json:"image_revision_id"`
			}
			if json.Unmarshal([]byte(message.Content), &result) == nil {
				id = result.ImageID
			}
		}
		if id == "" || seen[id] {
			continue
		}
		if !readable[id] || h.service.images == nil || !h.held.Model.Capability.ImageInput || !imageAuthorized {
			continue // Historical tool locators carry no renewed image authority.
		}
		optional := message.Role == schema.Tool
		if group, _ := message.Extra["creative_history_group"].(string); group != "" {
			optional = true
		}
		handle, err := h.service.images.AgentImage(ctx, h.scope, id, h.service.limits.ImagePreviewBytes)
		var sizeLimit *creativemedia.ImagePreviewLimitError
		if errors.As(err, &sizeLimit) {
			reason := fmt.Sprintf("图片大小为 %s，超过单图上限 %s，压缩后的预览仍无法满足限制。", imageBytesLabel(sizeLimit.SizeBytes), imageBytesLabel(sizeLimit.LimitBytes))
			if err := h.omitLimitedImage(ctx, message, &messages[i], id, "creative_image_size_limit", reason); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil && unreadableImage(err) {
			// A model's facts stand without its thumbnail; only the image goes.
			if facts, ok := withoutThumbnail(message.Content, "thumbnail_unreadable"); ok {
				delete(message.Extra, imageRefsKey)
				message.Content = facts
				messages[i].Blocks = []llmgateway.Block{{Kind: llmgateway.BlockText, Text: message.Content}}
				continue
			}
			// An earlier image that can no longer be prepared must not take the
			// whole conversation down with it; the model is told it is missing.
			if optional {
				delete(message.Extra, imageRefsKey)
				message.Content = referenceLabel(id) + "（这张图片已无法读取，没有发送给你）"
				messages[i].Blocks = []llmgateway.Block{{Kind: llmgateway.BlockText, Text: message.Content}}
				continue
			}
			return nil, fmt.Errorf("%w: image %s: %v", ErrNotFound, id, err)
		}
		if err != nil {
			return nil, err
		}
		if len(seen) >= h.service.limits.ImagePreviewCount {
			reason := fmt.Sprintf("图片数量超过上下文容量：本轮已加载 %d 张，最多允许 %d 张。", len(seen), h.service.limits.ImagePreviewCount)
			if err := h.omitLimitedImage(ctx, message, &messages[i], id, "creative_image_count_limit", reason); err != nil {
				return nil, err
			}
			continue
		}
		if total+handle.ByteSize > h.service.limits.ImagePreviewTotalBytes {
			reason := fmt.Sprintf("图片总大小超过上下文容量：已加载 %s，本图 %s，合计 %s，每轮上限 %s。", imageBytesLabel(total), imageBytesLabel(handle.ByteSize), imageBytesLabel(total+handle.ByteSize), imageBytesLabel(h.service.limits.ImagePreviewTotalBytes))
			if err := h.omitLimitedImage(ctx, message, &messages[i], id, "creative_image_total_limit", reason); err != nil {
				return nil, err
			}
			continue
		}
		seen[id] = true
		total += handle.ByteSize
		block := llmgateway.Block{Kind: llmgateway.BlockImageInput, Image: handle}
		if message.Role == schema.Tool {
			label := "（read_asset 读取结果）"
			if toolNames[message.ToolCallID] == renderViewTool {
				label = "（render_view 截图）"
			}
			delayed = append(delayed, llmgateway.Message{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: referenceLabel(id) + label}, block}})
		} else {
			messages[i].Blocks = append(messages[i].Blocks, block)
		}
	}
	messages = append(messages, delayed...)
	skillBytes := 0
	if h.held.Skill != nil {
		skillBytes = len(h.held.Skill.Instructions)
	}
	h.setComposition(composeMessages(source, messages, skillBytes, toolNames))
	return messages, nil
}

type promptContext struct {
	DraftRevision int64           `json:"draft_revision"`
	Text          string          `json:"text"`
	Model         string          `json:"model"`
	Parameters    json.RawMessage `json:"parameters"`
}

type canvasNodeContext struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Type     string  `json:"type"`
	Parent   *string `json:"parent_id,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Revision *string `json:"content_revision_id,omitempty"`
	// Text is the start of a text node's body; read_asset reads all of it.
	Text   string         `json:"text,omitempty"`
	Prompt *promptContext `json:"prompt,omitempty"`
	// ExportSource is set on an image exported from a scene that still shows
	// the exported picture: which camera, which pass, and its lens.
	ExportSource *exportSourceContext `json:"export_source,omitempty"`
	Versions     int                  `json:"versions,omitempty"`
	Generation   string               `json:"generation_state,omitempty"`
	Error        *string              `json:"generation_error,omitempty"`
}

// exportSourceContext is an export image's based_on as the model reads it
// (contracts「导出图来源」): lens is null for images exported before it was
// recorded; state is current, stale (the scene changed what the image shows)
// or unavailable.
type exportSourceContext struct {
	SceneDocumentID string                          `json:"scene_document_id"`
	SceneRevision   creativeops.Revision            `json:"scene_revision"`
	CameraObjectID  string                          `json:"camera_object_id"`
	Pass            string                          `json:"pass"`
	Lens            *creativecanvas.SceneExportLens `json:"lens"`
	State           string                          `json:"state"`
}

type canvasContext struct {
	ContentRevisionIDs []string            `json:"content_revision_ids,omitempty"`
	ID                 string              `json:"canvas_id"`
	Name               string              `json:"name"`
	NodeCount          int                 `json:"node_count"`
	Nodes              []canvasNodeContext `json:"nodes"`
	Edges              []map[string]string `json:"edges"`
	NextOffset         *int                `json:"next_offset,omitempty"`
}

// canvasSummary is the canvas line frozen into a run's input: its name and
// size. The nodes themselves are read live with read_canvas.
func canvasSummary(ctx context.Context, tx store.TxAccountScope, canvasID string) (canvasContext, error) {
	out := canvasContext{ID: canvasID, Nodes: []canvasNodeContext{}, Edges: []map[string]string{}}
	if err := tx.QueryRow(ctx, "creative_canvases", "name", "id=$2", canvasID).Scan(&out.Name); err != nil {
		return out, err
	}
	count, err := tx.Count(ctx, "creative_nodes", "canvas_id=$2", canvasID)
	out.NodeCount = int(count)
	return out, err
}

// textPreviewBytes bounds the body read_canvas inlines per text node.
const textPreviewBytes = 600

// canvasView pages the live canvas for the model: what each node is, where it
// sits, what it would generate with, and the edges touching the page.
func canvasView(c creativecanvas.Canvas, offset, limit int) canvasContext {
	out := canvasContext{ID: c.ID, Name: c.ProjectName, NodeCount: len(c.Nodes), Nodes: []canvasNodeContext{}, Edges: []map[string]string{}}
	nodes := slices.Clone(c.Nodes)
	slices.SortFunc(nodes, func(a, b creativecanvas.Node) int { return strings.Compare(a.ID, b.ID) })
	if offset > len(nodes) {
		offset = len(nodes)
	}
	page := nodes[offset:]
	if len(page) > limit {
		page = page[:limit]
		next := offset + limit
		out.NextOffset = &next
	}
	on := map[string]bool{}
	for _, n := range page {
		on[n.ID] = true
		node := canvasNodeContext{ID: n.ID, Title: n.Title, Type: strings.TrimPrefix(n.TypeKey, "core."), Parent: n.ParentID, X: n.X, Y: n.Y, Width: n.Width, Height: n.Height,
			Revision: n.ContentRevisionID, Generation: n.Status.GenerationState, Error: n.Status.Error}
		if n.Content != nil && n.Content.Payload.Body != nil && n.TypeKey == "core.text" {
			node.Text = utf8Prefix(*n.Content.Payload.Body, textPreviewBytes)
		}
		if p := n.Prompt; p != nil && p.ActionKey == creativecanvas.GenerationAction {
			node.Prompt = &promptContext{DraftRevision: int64(p.Revision), Text: p.Text, Model: p.ModelKey, Parameters: p.Parameters}
		}
		if n.Versions != nil {
			node.Versions = n.Versions.Count
		}
		if b, ok := creativecanvas.SceneExportOf(n); ok {
			node.ExportSource = &exportSourceContext{SceneDocumentID: b.SceneDocumentID, SceneRevision: b.SceneRevision, CameraObjectID: b.CameraObjectID, Pass: b.Pass, Lens: b.Lens, State: n.BasedOnState}
		}
		out.Nodes = append(out.Nodes, node)
	}
	for _, e := range c.Edges {
		if on[e.SourceNodeID] || on[e.TargetNodeID] {
			out.Edges = append(out.Edges, map[string]string{"source": e.SourceNodeID, "target": e.TargetNodeID})
		}
	}
	return out
}

func (h *runtimeHandler) readCanvas(ctx context.Context, raw string) (string, error) {
	var args struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return "", err
	}
	if args.Offset < 0 || args.Limit < 1 || args.Limit > 50 {
		return "", creativeops.ErrValidation
	}
	var canvasID string
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		consent, err := readConsentForUpdate(ctx, tx, h.held.ConsentID)
		if err != nil {
			return err
		}
		if !consentHas(consent, "canvas_metadata") {
			return ErrNotFound
		}
		return tx.QueryRow(ctx, "creative_agent_conversations", "canvas_id", "id=$2", h.held.ConversationID).Scan(&canvasID)
	})
	if err != nil {
		return "", err
	}
	canvas, err := creativecanvas.GetCanvas(ctx, h.scope, canvasID)
	if err != nil {
		return "", err
	}
	view := canvasView(canvas, args.Offset, args.Limit)
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		for i := range view.Nodes {
			node := &view.Nodes[i]
			if node.Text == "" || node.Revision == nil {
				continue
			}
			readable, err := readableInTx(ctx, tx, h.held.RunID, h.held.ConsentID, *node.Revision)
			if err != nil {
				return err
			}
			if !readable {
				node.Text = ""
				continue
			}
			if _, err := creativecontent.RequireUsable(ctx, tx, *node.Revision, "display"); err != nil {
				return err
			}
			if !slices.Contains(view.ContentRevisionIDs, *node.Revision) {
				view.ContentRevisionIDs = append(view.ContentRevisionIDs, *node.Revision)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(view)
	return string(out), err
}

func (h *runtimeHandler) readAsset(ctx context.Context, raw string) (string, error) {
	var args struct {
		RevisionID string `json:"content_revision_id"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return "", err
	}
	if args.RevisionID == "" || args.Offset < 0 || args.Limit < 1 || args.Limit > 8192 {
		return "", creativeops.ErrValidation
	}
	var result map[string]any
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		c, err := readConsentForUpdate(ctx, tx, h.held.ConsentID)
		if err != nil {
			return err
		}
		// Checked against the database, never against what the model claims.
		readable, err := readableInTx(ctx, tx, h.held.RunID, c.ID, args.RevisionID)
		if err != nil {
			return err
		}
		if !readable {
			return errReadRefused
		}
		ref, err := h.service.readReference(ctx, tx, args.RevisionID, c, h.held.Model.Capability.ImageInput)
		if err != nil {
			return err
		}
		result = map[string]any{"reference": referenceLabel(args.RevisionID), "content_revision_id": args.RevisionID}
		if ref.image != "" {
			result["image_revision_id"] = ref.image
		}
		// A model's facts come in one result, with its thumbnail.
		if ref.model != nil {
			if ref.thumbnailOmitted != "" {
				result["thumbnail_omitted"] = ref.thumbnailOmitted
			}
			return fitModelFacts(result, *ref.model, h.service.limits.ToolResultBytes)
		}
		if ref.image != "" {
			return nil
		}
		message, err := referenceMessage(args.RevisionID, ref)
		if err != nil {
			return err
		}
		if args.Offset > len(message.Content) {
			return creativeops.ErrValidation
		}
		text := message.Content[args.Offset:]
		if !validTextBoundary(message.Content, args.Offset) {
			return creativeops.ErrValidation
		}
		text = utf8Prefix(text, args.Limit)
		result["text"] = text
		result["offset"] = args.Offset
		result["next_offset"] = args.Offset + len(text)
		result["has_more"] = args.Offset+len(text) < len(message.Content)
		return nil
	})
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(result)
	return string(out), err
}

// fitModelFacts puts a model's facts into a read_asset result of at most
// limit bytes. Past it, parts and anchors go first, then the parameter
// definitions; omitted names them, so partial facts never pass as whole.
func fitModelFacts(result map[string]any, f creativecontent.ModelFacts, limit int) error {
	fits := func() (bool, error) {
		result["model"] = f
		raw, err := json.Marshal(result)
		return len(raw) <= limit, err
	}
	if ok, err := fits(); ok || err != nil {
		return err
	}
	f.Parts, f.Anchors, f.Omitted = nil, nil, []string{"parts", "anchors"}
	if ok, err := fits(); ok || err != nil {
		return err
	}
	f.ParamsSchema, f.Omitted = nil, append(f.Omitted, "params_schema")
	if ok, err := fits(); ok || err != nil {
		return err
	}
	return refuseTool("这个模型的信息超出单次读取上限")
}

func validTextBoundary(text string, offset int) bool {
	return offset == len(text) || text[offset]&0xc0 != 0x80
}

func (h *runtimeHandler) readHistory(ctx context.Context, raw string) (string, error) {
	var args struct {
		Before int64 `json:"before_ordinal"`
		Limit  int   `json:"limit"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return "", err
	}
	if args.Before < 0 || args.Limit < 1 || args.Limit > 20 {
		return "", creativeops.ErrValidation
	}
	// The trigger fences history even when another window later appends messages.
	var trigger int64
	var triggerID string
	if err := h.scope.QueryRow(ctx, "creative_agent_runs", "trigger_message_id", "id=$2", h.held.RunID).Scan(&triggerID); err != nil {
		return "", err
	}
	if err := h.scope.QueryRow(ctx, "creative_agent_messages", "ordinal", "id=$2", triggerID).Scan(&trigger); err != nil {
		return "", err
	}
	if args.Before == 0 || args.Before > trigger {
		args.Before = trigger
	}
	rows, err := h.scope.QueryPage(ctx, "creative_agent_messages", "ordinal,role,body", "conversation_id=$2 AND ordinal<$3 AND status='complete'", []store.OrderBy{{Column: "ordinal", Desc: true}}, args.Limit, 0, h.held.ConversationID, args.Before)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	items := []map[string]any{}
	var next int64
	for rows.Next() {
		var role string
		var body []byte
		var ordinal int64
		if err = rows.Scan(&ordinal, &role, &body); err != nil {
			return "", err
		}
		var b Body
		if err = json.Unmarshal(body, &b); err != nil {
			return "", err
		}
		// References are locators only. read_asset separately authorizes bytes.
		refs := []string{}
		for _, block := range b.Blocks {
			if block.Type == "content_ref" {
				refs = append(refs, block.RefID)
			}
		}
		text := utf8Prefix(renderedText(body), 1024)
		if skillHistoryTool(b.ToolName) {
			text = skillHistoryUnloaded
		}
		if contentReadTool(b.ToolName) {
			text = "曾读取内容；正文请用 read_asset 重新读取"
		}
		items = append(items, map[string]any{"ordinal": fmt.Sprint(ordinal), "role": role, "text": text, "references": refs, "tool_call_id": b.ToolCallID})
		next = ordinal
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	out, err := json.Marshal(map[string]any{"messages": items, "next_before_ordinal": fmt.Sprint(next), "order": "newest_first"})
	return string(out), err
}

func marshalContext(v any) (string, error) { b, err := json.Marshal(v); return string(b), err }

// Compact old tool payloads and whole history turns, preserving call/result
// pairs and the current instruction. This is explicit lossy reduction, not an
// invented semantic summary; read_history/read_asset can recover source data.
func compactMessages(messages []*schema.Message, budget int) []*schema.Message {
	over := func(size int) bool { return size > budget || len(messages) > requestMessageCap }
	size := func() int {
		n := 0
		for _, m := range messages {
			n += len(m.Content) + 16
			for _, c := range m.ToolCalls {
				n += len(c.Function.Arguments) + len(c.Function.Name)
			}
			if messageImageID(m) != "" {
				n += 4096
			}
		}
		return n
	}
	if !over(size()) {
		return messages
	}
	for _, m := range messages {
		if group, _ := m.Extra["creative_history_group"].(string); group != "" && messageImageID(m) != "" {
			omitImage(m)
			if !over(size()) {
				return messages
			}
		}
	}
	for _, m := range messages {
		if group, _ := m.Extra["creative_history_group"].(string); group != "" && m.Role == schema.Tool && len(m.Content) > 1024 {
			m.Content = utf8Prefix(m.Content, 768) + "\n[omitted: 旧工具结果正文已缩减；按来源重新读取]"
		}
		if !over(size()) {
			return messages
		}
	}
	// Only messages from earlier turns carry this tag. Remove complete turns.
	for over(size()) {
		group := ""
		for _, m := range messages {
			if g, _ := m.Extra["creative_history_group"].(string); g != "" {
				group = g
				break
			}
		}
		if group == "" {
			break
		}
		kept := messages[:0]
		for _, m := range messages {
			g, _ := m.Extra["creative_history_group"].(string)
			if g != group {
				kept = append(kept, m)
			}
		}
		messages = kept
	}
	return messages
}

// requestMessageCap keeps a request under the gateway's 400 messages with
// room for the turn's own answer.
const requestMessageCap = 380

func joinInstruction(skill string) string {
	return strings.TrimSpace(assistantInstruction + "\n\n" + skill)
}

func contextToolInfo(name string) *schema.ToolInfo {
	integer := func(desc string) *schema.ParameterInfo {
		return &schema.ParameterInfo{Type: schema.Integer, Required: true, Desc: desc}
	}
	params := map[string]*schema.ParameterInfo{}
	desc := ""
	switch name {
	case readAssetTool:
		desc = "读取当前画布、本次引用、本对话出现过的内容或资产库素材。内容修订 ID 取自 read_canvas、list_conversation_assets、search_assets 或 <<<...>>> 引用。图片作为真实模型附件返回；文字按 UTF-8 字节分页。模型返回尺寸（米）、挂载方式、面数、材质槽、部件、挂点、参数定义与当前值，并附缩略图；不能附图时 thumbnail_omitted 说明原因；超出单次结果上限时依次省略部件与挂点、参数定义，omitted 列出省略项。不返回生成代码。"
		params["content_revision_id"] = &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "内容修订 ID"}
		params["offset"] = integer("文字起始字节，初次读取填 0")
		params["limit"] = integer("文字最多字节数，1 到 8192；图片也填写此参数")
	case readCanvasTool:
		desc = "按页读取本会话画布：每个节点的类型、标题、位置尺寸、内容修订 ID、文字节点正文开头、生成设置（提示词、模型、参数、draft_revision）、版本数和生成状态，以及相关连线（source 是 target 的参考）。场景导出的图另带 export_source：来自哪台摄像机、哪种导出方式、导出时的镜头 lens，以及与场景是否还一致（state）。图片和完整正文用 read_asset 读取。"
		params["offset"] = integer("分页起点，首次填 0")
		params["limit"] = integer("节点数量，1 到 50")
	case readHistoryTool:
		desc = "按最新到最早顺序读取本次提交之前的会话消息摘录、引用标识和工具身份；不展开历史媒体。"
		params["before_ordinal"] = integer("读取此序号之前的消息，首次填 0")
		params["limit"] = integer("消息数量，1 到 20")
	}
	return &schema.ToolInfo{Name: name, Desc: desc, ParamsOneOf: schema.NewParamsOneOfByParams(params)}
}

func (h *runtimeHandler) persistToolMessage(ctx context.Context, tx store.TxAccountScope, stepID, callID, name, result, original string) (Message, error) {
	message := newMessage{Role: "tool", Status: "complete", RunID: h.held.RunID, SourceStepID: stepID, Body: Body{Blocks: []Block{{Type: "text", Text: result}}, ToolCallID: callID, ToolName: name}}
	for _, id := range toolContentRevisions(name, original) {
		if contentReadTool(name) {
			readable, err := readableInTx(ctx, tx, h.held.RunID, h.held.ConsentID, id)
			if err != nil {
				return Message{}, err
			}
			if !readable {
				return Message{}, errReadRefused
			}
			if _, err := creativecontent.RequireUsable(ctx, tx, id, "display"); err != nil {
				return Message{}, err
			}
		}
		message.ContentRefs = append(message.ContentRefs, ContentRef{RevisionID: id, Role: "result"})
		message.Body.Blocks = append(message.Body.Blocks, Block{Type: "content_ref", RefID: id})
	}

	// Provenance is retained even when the displayed text is shortened. Charge
	// every source ID and tool identity before choosing the text budget.
	available := h.service.limits.MessageBodyBytes - (bodyBytes(message.Body) - len(result))
	if len(result) > available {
		const omitted = "\n[omitted: 完整结果保存在原运行步骤中]"
		if available < len(omitted) {
			return Message{}, ErrLimit
		}
		message.Body.Blocks[0].Text = utf8Prefix(result, available-len(omitted)) + omitted
	}
	return h.service.appendMessageInTx(ctx, tx, h.held.ConversationID, message)
}

// unreadableImage is a failure of this one image — gone, not allowed, not a
// picture we can decode — as opposed to the store being unavailable.
func unreadableImage(err error) bool {
	return errors.Is(err, creativecontent.ErrNotFound) || errors.Is(err, creativecontent.ErrUsageDenied) || errors.Is(err, creativemedia.ErrUnsupported) ||
		errors.Is(err, llmgateway.ErrValidation) || errors.Is(err, llmgateway.ErrCapability)
}

// Keep newer visual inputs within the bounded attachment window. Removing an
// image never removes its locator; the model can explicitly request it again.
func messageImageID(m *schema.Message) string {
	if id, _ := m.Extra[imageRefsKey].(string); id != "" {
		return id
	}
	if m.Role == schema.Tool {
		var result struct {
			ID string `json:"image_revision_id"`
		}
		if json.Unmarshal([]byte(m.Content), &result) == nil {
			return result.ID
		}
	}
	return ""
}
func omitImage(m *schema.Message) {
	id := messageImageID(m)
	delete(m.Extra, imageRefsKey)
	if facts, ok := withoutThumbnail(m.Content, "context_limit"); ok {
		m.Content = facts
		return
	}
	m.Content = referenceLabel(id) + "（仅本轮因上下文容量限制未附加图片；这不表示此前读取失败。若已观察并记录过该图，沿用已有观察；只有尚未观察或需要新的视觉判断时才用 read_asset 重读，不要为核验历史加载状态重复读图。）"
}

// Reserve explicit current inputs before optional history/tool images. Both
// count pruning and byte binding use this order, newest first within each set.
// Indices preserve the conversation's original message order.
func imageMessageOrder(messages []*schema.Message) []int {
	var current, optional []int
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if messageImageID(m) == "" {
			continue
		}
		if group, _ := m.Extra["creative_history_group"].(string); group == "" && m.Role != schema.Tool {
			current = append(current, i)
		} else {
			optional = append(optional, i)
		}
	}
	return append(current, optional...)
}

func limitHistoryImages(messages []*schema.Message, limit int) {
	seen := map[string]bool{}
	for _, i := range imageMessageOrder(messages) {
		m := messages[i]
		id := messageImageID(m)
		if id == "" || seen[id] {
			continue
		}
		if group, _ := m.Extra["creative_history_group"].(string); (group != "" || m.Role == schema.Tool) && len(seen) >= limit {
			omitImage(m)
			continue
		}
		seen[id] = true
	}
}
