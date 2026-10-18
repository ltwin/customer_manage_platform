package creativescene

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	// maxSummaryLines bounds a change summary; the rest is counted.
	maxSummaryLines = 30
	// maxSummaryRows bounds the history one summary reads.
	maxSummaryRows = 2000
)

// ChangeSummary is what others changed in a scene after a revision, in words
// the model reads: who changed which objects, what was added or deleted, and
// what was restored.
type ChangeSummary struct {
	DocumentID    string               `json:"document_id"`
	SinceRevision creativeops.Revision `json:"since_revision"`
	Revision      creativeops.Revision `json:"revision"`
	Lines         []string             `json:"lines"`
	More          int                  `json:"more,omitempty"`
}

// ChangesSinceInTx summarizes what others changed since the conversation's
// runs last wrote the scene. It is nil when they never wrote it.
func ChangesSinceInTx(ctx context.Context, tx store.TxAccountScope, documentID string, conversationRuns []string) (*ChangeSummary, error) {
	last, err := creativedocument.LastRunRevision(ctx, tx, documentID, conversationRuns)
	if err != nil || last == 0 {
		return nil, err
	}
	s, err := changesAfter(ctx, tx, func(revision int64) (creativedocument.Current, error) {
		return creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, revision)
	}, documentID, last, conversationRuns)
	return &s, err
}

// ChangesAfterInTx summarizes what others than the run changed after base:
// the gap between a stale baseline and now, which a conflict reports.
func ChangesAfterInTx(ctx context.Context, tx store.TxAccountScope, documentID string, base int64, runID string) (ChangeSummary, error) {
	return changesAfter(ctx, tx, func(revision int64) (creativedocument.Current, error) {
		return creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, revision)
	}, documentID, base, []string{runID})
}

func changesAfter(ctx context.Context, pages creativedocument.PageReader, at func(int64) (creativedocument.Current, error), documentID string, base int64, excluded []string) (ChangeSummary, error) {
	head, err := at(0)
	if err != nil {
		return ChangeSummary{}, err
	}
	out := ChangeSummary{DocumentID: documentID, SinceRevision: creativeops.Revision(base), Revision: creativeops.Revision(head.Revision), Lines: []string{}}
	if base >= head.Revision {
		return out, nil
	}
	history, err := creativedocument.History(ctx, pages, documentID, base, maxSummaryRows)
	if err != nil {
		return ChangeSummary{}, err
	}
	var rows []creativedocument.HistoryRow
	for _, row := range history {
		if row.RunID == "" || !slices.Contains(excluded, row.RunID) {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return out, nil
	}
	old, err := at(base)
	if err != nil {
		return ChangeSummary{}, err
	}
	before, err := decodeScene(old.Content.Payload)
	if err != nil {
		return ChangeSummary{}, err
	}
	now, err := decodeScene(head.Content.Payload)
	if err != nil {
		return ChangeSummary{}, err
	}
	lines := summarizeChanges(rows, before, now)
	if len(history) == maxSummaryRows && history[len(history)-1].Revision < head.Revision {
		lines = append(lines, fmt.Sprintf("第 %d 版之后还有改动未列出", history[len(history)-1].Revision))
	}
	if len(lines) > maxSummaryLines {
		out.More = len(lines) - maxSummaryLines
		lines = lines[:maxSummaryLines]
	}
	out.Lines = lines
	return out, nil
}

var actorLabels = map[string]string{"photographer": "摄影师", "agent": "另一次助手对话", "system": "系统"}

func actorLabel(kind string) string {
	if l, ok := actorLabels[kind]; ok {
		return l
	}
	return kind
}

// summarizeChanges words other writers' revisions over before → now, in the
// order they happened: restores one line each, and ordinary edits one line per
// writer and object (added, deleted, or which parts changed).
func summarizeChanges(rows []creativedocument.HistoryRow, before, now Scene) []string {
	beforeIdx, nowIdx := indexObjects(before.Objects), indexObjects(now.Objects)
	label := func(id string) string {
		name := ""
		if i, ok := nowIdx[id]; ok {
			name = now.Objects[i].Name
		} else if i, ok := beforeIdx[id]; ok {
			name = before.Objects[i].Name
		}
		if name == "" {
			return id
		}
		return fmt.Sprintf("「%s」(%s)", name, id)
	}
	type edit struct {
		at                 int64
		actor, target      string
		object             bool
		placement, content bool
		added, removed     bool
	}
	var order []string
	edits := map[string]*edit{}
	type line struct {
		at   int64
		text string
	}
	var restores []line
	for _, row := range rows {
		actor := actorLabel(row.ActorKind)
		switch {
		case row.WholeRestore():
			restores = append(restores, line{row.Revision, fmt.Sprintf("%s把整个场景恢复到第 %d 版", actor, row.RestoredFrom)})
			continue
		case row.RestoredFrom > 0:
			var names []string
			for _, target := range restoredTargets(append(slices.Clone(row.Changed), row.Removed...)) {
				if strings.HasPrefix(target, "sobj_") {
					names = append(names, label(target))
				} else {
					names = append(names, unitLabels[target])
				}
			}
			restores = append(restores, line{row.Revision, fmt.Sprintf("%s把%s恢复到第 %d 版", actor, strings.Join(names, "、"), row.RestoredFrom)})
			continue
		}
		note := func(target string, object bool) *edit {
			key := actor + "\x00" + target
			e := edits[key]
			if e == nil {
				e = &edit{at: row.Revision, actor: actor, target: target, object: object}
				edits[key] = e
				order = append(order, key)
			}
			return e
		}
		for _, u := range row.Changed {
			if id, isContent, ok := objectOfUnit(u); ok {
				e := note(id, true)
				_, existed := beforeIdx[id]
				switch {
				case !existed && !isContent:
					e.added = true
				case isContent:
					e.content = true
				default:
					e.placement = true
				}
			} else if u != unitObjects {
				note(u, false)
			}
		}
		for _, u := range row.Removed {
			if id, _, ok := objectOfUnit(u); ok {
				note(id, true).removed = true
			}
		}
	}
	for _, key := range order {
		e := edits[key]
		var text string
		_, inNow := nowIdx[e.target]
		switch {
		case !e.object:
			text = fmt.Sprintf("%s改了%s", e.actor, unitLabels[e.target])
		case e.added && e.removed || e.added && !inNow:
			continue // came and went after the baseline
		case e.added:
			text = fmt.Sprintf("%s新增了%s", e.actor, label(e.target))
		case e.removed:
			text = fmt.Sprintf("%s删除了%s", e.actor, label(e.target))
		default:
			var parts []string
			if e.placement {
				parts = append(parts, "摆放")
			}
			if e.content {
				parts = append(parts, "内容")
			}
			text = fmt.Sprintf("%s改了%s的%s", e.actor, label(e.target), strings.Join(parts, "和"))
		}
		restores = append(restores, line{e.at, text})
	}
	sort.SliceStable(restores, func(i, j int) bool { return restores[i].at < restores[j].at })
	out := make([]string, len(restores))
	for i, l := range restores {
		out[i] = l.text
	}
	return out
}

// restoredTargets is the objects and scene-wide units a restore named, once
// each and in a stable order.
func restoredTargets(units []string) []string {
	var out []string
	for _, u := range units {
		target := u
		if id, _, ok := objectOfUnit(u); ok {
			target = id
		} else if u == unitObjects {
			continue
		}
		if !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	sort.Strings(out)
	return out
}
