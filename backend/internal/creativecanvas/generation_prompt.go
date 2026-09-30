package creativecanvas

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var promptMention = regexp.MustCompile(`@\[([\w-]+)\]`)

type generationPromptRef struct {
	rank, ordinal                    int
	slot, id, revision, role, source string
}

// 引用排序与准入校验共用一份规则；句内 @ 不增删附件，也不改变主版本。
func orderedGenerationRefs(g graphState, node string) ([]generationPromptRef, map[string]map[string]bool, error) {
	refs := make([]generationPromptRef, 0)
	required := map[string]map[string]bool{node: {"data": true}}
	for _, input := range g.Inputs {
		if input.NodeID != node {
			continue
		}
		rank := 1
		if input.DraftID != nil {
			rank = 0
		}
		revision, sourceID := input.ContentRevisionID, ""
		if input.SourceNodeID != nil {
			source, ok := g.Nodes[*input.SourceNodeID]
			if !ok {
				return nil, nil, ErrGraphIntegrity
			}
			revision, sourceID = source.ContentRevisionID, source.ID
			required[source.ID] = map[string]bool{"data": true}
		}
		if revision == nil {
			return nil, nil, ErrGraphIntegrity
		}
		refs = append(refs, generationPromptRef{rank: rank, ordinal: input.Ordinal, slot: input.Slot, id: input.ID, revision: *revision, role: input.Role, source: sourceID})
	}
	for _, edge := range g.Edges {
		if edge.TargetNodeID != node {
			continue
		}
		source, ok := g.Nodes[edge.SourceNodeID]
		if !ok || source.ContentRevisionID == nil {
			return nil, nil, ErrGraphIntegrity
		}
		required[source.ID] = map[string]bool{"data": true}
		refs = append(refs, generationPromptRef{rank: 2, ordinal: edge.Ordinal, slot: edge.TargetPort, id: edge.ID, revision: *source.ContentRevisionID, role: edge.Role, source: source.ID})
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.slot != b.slot {
			return a.slot < b.slot
		}
		if a.ordinal != b.ordinal {
			return a.ordinal < b.ordinal
		}
		return a.id < b.id
	})
	return refs, required, nil
}

type CompiledPrompt struct{ Raw, Text string }

// CompileGenerationPromptInTx 只读账号范围内的快照。最终准入取得业务锁后再次编译，
// 确认提示词、附件顺序及主版本仍与已冻结的模型请求一致。
func CompileGenerationPromptInTx(ctx context.Context, tx store.TxAccountScope, in RequestExecutionInput) (CompiledPrompt, error) {
	prompt, err := readPrompt(ctx, tx, in.CanvasID, in.NodeID)
	if err != nil {
		return CompiledPrompt{}, err
	}
	if prompt == nil || prompt.Revision != in.DraftRevision {
		return CompiledPrompt{}, ErrVersionConflict
	}
	if !promptMention.MatchString(prompt.Text) {
		return CompiledPrompt{Raw: prompt.Text, Text: prompt.Text}, nil
	}
	g, _, err := loadGraph(ctx, tx, in.CanvasID)
	if err != nil {
		return CompiledPrompt{}, err
	}
	refs, _, err := orderedGenerationRefs(g, in.NodeID)
	if err != nil {
		return CompiledPrompt{}, err
	}
	compiled, err := compileGraphPrompt(ctx, tx, prompt.Text, refs)
	return CompiledPrompt{Raw: prompt.Text, Text: compiled}, err
}

type numberedPromptRef struct {
	source, kind, text, label string
	ordinal                   int
}

func compileGraphPrompt(ctx context.Context, tx store.TxAccountScope, text string, refs []generationPromptRef) (string, error) {
	if !promptMention.MatchString(text) {
		return text, nil
	}
	values := make([]numberedPromptRef, 0, len(refs))
	counts := map[string]int{}
	for i, ref := range refs {
		// 使用无锁的生成授权读取，锁顺序仍由 Gateway → 画布 → 内容保持。
		r, err := creativecontent.RequireGenerationReferenceRow(ctx, tx, tx.QueryRow, ref.revision)
		if err != nil {
			return "", err
		}
		counts[r.Kind]++
		prefix := map[string]string{"image": "图", "video": "视频", "audio": "音频", "text": "文本", "link": "链接"}[r.Kind]
		if prefix == "" {
			return "", creativeops.ErrValidation
		}
		body := ""
		if r.Payload.Body != nil {
			body = *r.Payload.Body
		}
		values = append(values, numberedPromptRef{source: ref.source, kind: r.Kind, text: body, label: fmt.Sprintf("%s%d", prefix, counts[r.Kind]), ordinal: i + 1})
	}
	return compilePromptText(text, values)
}

func compilePromptText(text string, refs []numberedPromptRef) (string, error) {
	bySource := map[string]numberedPromptRef{}
	for _, ref := range refs {
		if ref.source != "" {
			if _, exists := bySource[ref.source]; !exists {
				bySource[ref.source] = ref
			}
		}
	}
	var invalid bool
	compiled := promptMention.ReplaceAllStringFunc(text, func(token string) string {
		id := promptMention.FindStringSubmatch(token)[1]
		ref, ok := bySource[id]
		if !ok {
			invalid = true
			return ""
		}
		if ref.kind == "text" {
			return ref.text
		}
		return "[" + ref.label + "]"
	})
	if invalid {
		return "", creativeops.ErrValidation
	}
	var rules []string
	for _, ref := range refs {
		if ref.kind != "text" {
			rules = append(rules, fmt.Sprintf("[%s] 对应附件第 %d 项", ref.label, ref.ordinal))
		}
	}
	if len(rules) > 0 {
		compiled += "\n\n参考编号说明：" + strings.Join(rules, "；") + "。按提示词描述使用对应附件，未点名的附件仍作为整体风格与内容参考。"
	}
	return compiled, nil
}
