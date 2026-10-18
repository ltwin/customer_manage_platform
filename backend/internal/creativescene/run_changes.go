package creativescene

import (
	"context"
	"slices"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	// maxRunRevisions bounds the revisions one run's change list is built
	// from; past it the photographer restores from the history list.
	maxRunRevisions = 500
	// maxHistoryRows bounds the history read from the run's first write on.
	maxHistoryRows = 5000
)

// Why an entry of a run's changes cannot be put back on its own.
const (
	BlockedByOthers       = "changed_by_others"
	BlockedByWholeRestore = "restored_whole"
	BlockedHierarchy      = "hierarchy_not_restorable"
)

// Change kinds of an entry, relative to the scene before the run.
const (
	ChangeAdded    = "added"
	ChangeRemoved  = "removed"
	ChangeModified = "modified"
)

// RunChangeSet is what one assistant run changed in a scene and how to put
// each change back (api: 读取运行的场景改动).
type RunChangeSet struct {
	RunID      string                 `json:"run_id"`
	DocumentID string                 `json:"document_id"`
	Revisions  []creativeops.Revision `json:"revisions"`
	Entries    []RunChange            `json:"entries"`
	UndoAll    []RestoreBatch         `json:"undo_all"`
}

// RunChange is one changed object, or the environment, render settings or
// title. Restore is nil when it cannot be put back on its own.
type RunChange struct {
	Target  ChangeTarget  `json:"target"`
	Name    string        `json:"name"`
	Change  string        `json:"change"`
	Restore *RestoreBatch `json:"restore"`
	Blocked *Blocked      `json:"blocked"`
}

type ChangeTarget struct {
	Kind     string `json:"kind"` // object | unit
	ObjectID string `json:"object_id,omitempty"`
	Unit     string `json:"unit,omitempty"`
}

// RestoreBatch is one restore_units command: its source revision, its units
// and the expected versions as read now.
type RestoreBatch struct {
	FromRevision creativeops.Revision             `json:"from_revision"`
	Units        []string                         `json:"units"`
	Expected     map[string]*creativeops.Revision `json:"expected"`
}

type Blocked struct {
	Reason    string               `json:"reason"`
	Revision  creativeops.Revision `json:"revision"`
	ActorKind string               `json:"actor_kind"`
}

// RunChanges lists what a run changed in a scene, in a read snapshot. The
// caller checks that the run belongs to the account.
func RunChanges(ctx context.Context, scope store.AccountScope, documentID, runID string) (RunChangeSet, error) {
	var out RunChangeSet
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		var err error
		out, err = runChanges(ctx, tx, func(revision int64) (creativedocument.Current, error) {
			return creativedocument.ReadAtInSnapshot(ctx, tx, documentID, TypeKey, revision)
		}, documentID, runID)
		return err
	})
	return out, err
}

// RunChangesInTx is RunChanges inside a write, for the assistant taking back
// its own changes.
func RunChangesInTx(ctx context.Context, tx store.TxAccountScope, documentID, runID string) (RunChangeSet, error) {
	return runChanges(ctx, tx, func(revision int64) (creativedocument.Current, error) {
		return creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, revision)
	}, documentID, runID)
}

func runChanges(ctx context.Context, pages creativedocument.PageReader, at func(int64) (creativedocument.Current, error), documentID, runID string) (RunChangeSet, error) {
	return runChangesFor(ctx, pages, at, documentID, runID, nil)
}

// runChangesFor is runChanges whose undo_all covers only the selected
// entries (all of them when selected is nil).
func runChangesFor(ctx context.Context, pages creativedocument.PageReader, at func(int64) (creativedocument.Current, error), documentID, runID string, selected func(ChangeTarget) bool) (RunChangeSet, error) {
	if runID == "" {
		return RunChangeSet{}, creativeops.Explain(creativeops.ErrValidation, "缺少 run_id")
	}
	head, err := at(0)
	if err != nil {
		return RunChangeSet{}, err
	}
	out := RunChangeSet{RunID: runID, DocumentID: documentID, Revisions: []creativeops.Revision{}, Entries: []RunChange{}, UndoAll: []RestoreBatch{}}
	revisions, err := creativedocument.RunRevisions(ctx, pages, documentID, runID, maxRunRevisions+1)
	if err != nil {
		return RunChangeSet{}, err
	}
	if len(revisions) > maxRunRevisions {
		return RunChangeSet{}, creativeops.Explain(creativeops.ErrValidation, "这次运行改了超过 %d 个版本，请在历史版本里恢复", maxRunRevisions)
	}
	if len(revisions) == 0 {
		return out, nil
	}
	for _, r := range revisions {
		out.Revisions = append(out.Revisions, creativeops.Revision(r))
	}
	history, err := creativedocument.History(ctx, pages, documentID, revisions[0]-1, maxHistoryRows+1)
	if err != nil {
		return RunChangeSet{}, err
	}
	if len(history) > maxHistoryRows {
		return RunChangeSet{}, creativeops.Explain(creativeops.ErrValidation, "这次运行之后的修订太多，请在历史版本里恢复")
	}
	headScene, err := decodeScene(head.Content.Payload)
	if err != nil {
		return RunChangeSet{}, err
	}
	scenes := map[int64]Scene{head.Revision: headScene}
	sceneAt := func(revision int64) (Scene, error) {
		if s, ok := scenes[revision]; ok {
			return s, nil
		}
		cur, err := at(revision)
		if err != nil {
			return Scene{}, err
		}
		s, err := decodeScene(cur.Content.Payload)
		scenes[revision] = s
		return s, err
	}
	out.Entries, out.UndoAll, err = planRunChanges(head.UnitRevs, headScene, revisions, history, sceneAt, selected)
	return out, err
}

// runHistory is a run's writes and everyone else's, indexed by unit.
type runHistory struct {
	first  map[string]int64                         // unit -> the run's first write of it
	others map[string][]creativedocument.HistoryRow // unit -> other writers' changes, oldest first
	wholes []creativedocument.HistoryRow            // other writers' whole restores
	// hierarchy is the run's revisions that moved objects in the tree
	// without changing any object's own units.
	hierarchy []int64
}

func indexRunHistory(revisions []int64, history []creativedocument.HistoryRow) runHistory {
	inRun := make(map[int64]bool, len(revisions))
	for _, r := range revisions {
		inRun[r] = true
	}
	h := runHistory{first: map[string]int64{}, others: map[string][]creativedocument.HistoryRow{}}
	for _, row := range history {
		units := append(slices.Clone(row.Changed), row.Removed...)
		if !inRun[row.Revision] {
			if row.WholeRestore() {
				h.wholes = append(h.wholes, row)
				continue
			}
			for _, u := range units {
				h.others[u] = append(h.others[u], row)
			}
			continue
		}
		// The first revision has nothing before it to go back to, and the
		// assistant's tools cannot restore a whole scene.
		if row.Revision == 1 || row.WholeRestore() {
			continue
		}
		objectUnits := false
		for _, u := range units {
			if _, _, ok := objectOfUnit(u); ok {
				objectUnits = true
			}
			if _, seen := h.first[u]; !seen && u != unitObjects {
				h.first[u] = row.Revision
			}
		}
		if !objectUnits && slices.Contains(units, unitObjects) {
			h.hierarchy = append(h.hierarchy, row.Revision)
		}
	}
	return h
}

// obstacle is the first change by someone else to u in (after, through];
// through 0 means no upper bound.
func (h runHistory) obstacle(u string, after, through int64) *creativedocument.HistoryRow {
	var found *creativedocument.HistoryRow
	inRange := func(r int64) bool { return r > after && (through == 0 || r <= through) }
	for i := range h.others[u] {
		if inRange(h.others[u][i].Revision) {
			found = &h.others[u][i]
			break
		}
	}
	for i := range h.wholes {
		if inRange(h.wholes[i].Revision) && (found == nil || h.wholes[i].Revision < found.Revision) {
			found = &h.wholes[i]
			break
		}
	}
	return found
}

func blockedBy(row *creativedocument.HistoryRow) *Blocked {
	reason := BlockedByOthers
	if row.WholeRestore() {
		reason = BlockedByWholeRestore
	}
	return &Blocked{Reason: reason, Revision: creativeops.Revision(row.Revision), ActorKind: row.ActorKind}
}

func earliest(a, b *creativedocument.HistoryRow) *creativedocument.HistoryRow {
	if a == nil || b != nil && b.Revision < a.Revision {
		return b
	}
	return a
}

// changeGroup is one entry's units: an object's placement and content, or one
// scene-wide unit.
type changeGroup struct {
	target ChangeTarget
	units  []string
	from   int64 // the earliest unit's state before the run
}

func (h runHistory) groups() []*changeGroup {
	byKey := map[string]*changeGroup{}
	var out []*changeGroup
	for u, first := range h.first {
		key, target := u, ChangeTarget{Kind: "unit", Unit: u}
		if id, _, ok := objectOfUnit(u); ok {
			key, target = placementUnit(id), ChangeTarget{Kind: "object", ObjectID: id}
		}
		g := byKey[key]
		if g == nil {
			g = &changeGroup{target: target, from: first - 1}
			byKey[key] = g
			out = append(out, g)
		}
		g.units = append(g.units, u)
		g.from = min(g.from, first-1)
	}
	for _, g := range out {
		sort.Strings(g.units)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from != out[j].from {
			return out[i].from < out[j].from
		}
		return out[i].units[0] < out[j].units[0]
	})
	return out
}

// cleanFrom is what keeps g from being read from revision k: the first change
// by someone else to one of its units between k and the run's first write of
// it, or nil when there is none.
func (h runHistory) cleanFrom(g *changeGroup, k int64) *creativedocument.HistoryRow {
	var found *creativedocument.HistoryRow
	for _, u := range g.units {
		found = earliest(found, h.obstacle(u, k, h.first[u]-1))
	}
	return found
}

var unitLabels = map[string]string{unitEnvironment: "环境", unitRender: "渲染设置", creativedocument.MetaUnit: "标题", unitObjects: "层级结构"}

// planRunChanges is RunChanges over loaded history: one entry per changed
// object or unit, each restorable from the state before the run's first write
// of it unless someone else changed it since, and undo_all batching the
// restorable ones by source revision (the selected ones, when selected is not
// nil).
func planRunChanges(unitRevs map[string]int64, head Scene, revisions []int64, history []creativedocument.HistoryRow, sceneAt func(int64) (Scene, error), selected func(ChangeTarget) bool) ([]RunChange, []RestoreBatch, error) {
	h := indexRunHistory(revisions, history)
	entries := []RunChange{}
	var restorable []*changeGroup
	headIdx := indexObjects(head.Objects)
	for _, g := range h.groups() {
		before, err := sceneAt(g.from)
		if err != nil {
			return nil, nil, err
		}
		entry := RunChange{Target: g.target, Name: unitLabels[g.target.Unit], Change: ChangeModified}
		if g.target.Kind == "object" {
			id := g.target.ObjectID
			bi, inBefore := indexObjects(before.Objects)[id]
			hi, inHead := headIdx[id]
			switch {
			case !inBefore && !inHead:
				continue // added and removed within the run
			case !inBefore:
				entry.Change, entry.Name = ChangeAdded, head.Objects[hi].Name
			case !inHead:
				entry.Change, entry.Name = ChangeRemoved, before.Objects[bi].Name
			default:
				entry.Name = head.Objects[hi].Name
			}
		}
		var block *creativedocument.HistoryRow
		for _, u := range g.units {
			block = earliest(block, h.obstacle(u, h.first[u], 0))
		}
		if block == nil {
			// Units read from different revisions fit one batch only when no
			// one else wrote them in between; otherwise this one is left to
			// the history list rather than split.
			block = h.cleanFrom(g, g.from)
		}
		if block != nil {
			entry.Blocked = blockedBy(block)
		} else {
			entry.Restore = &RestoreBatch{FromRevision: creativeops.Revision(g.from), Units: g.units, Expected: expectedFor(g.units, before, head, unitRevs)}
			if selected == nil || selected(g.target) {
				restorable = append(restorable, g)
			}
		}
		entries = append(entries, entry)
	}
	if len(h.hierarchy) > 0 {
		entries = append(entries, RunChange{Target: ChangeTarget{Kind: "unit", Unit: unitObjects}, Name: unitLabels[unitObjects], Change: ChangeModified,
			Blocked: &Blocked{Reason: BlockedHierarchy, Revision: creativeops.Revision(h.hierarchy[0]), ActorKind: "agent"}})
	}
	undo := []RestoreBatch{}
	for len(restorable) > 0 {
		k := restorable[0].from
		for _, g := range restorable {
			k = min(k, g.from)
		}
		var units []string
		var rest []*changeGroup
		for _, g := range restorable {
			if g.from == k || h.cleanFrom(g, k) == nil {
				units = append(units, g.units...)
			} else {
				rest = append(rest, g)
			}
		}
		before, err := sceneAt(k)
		if err != nil {
			return nil, nil, err
		}
		sort.Strings(units)
		undo = append(undo, RestoreBatch{FromRevision: creativeops.Revision(k), Units: units, Expected: expectedFor(units, before, head, unitRevs)})
		restorable = rest
	}
	return entries, undo, nil
}

// expectedFor is what restore_units checks for units read from before: each
// unit's current version (nil when it does not exist now), and the object
// list when an object comes back or goes away.
func expectedFor(units []string, before, head Scene, unitRevs map[string]int64) map[string]*creativeops.Revision {
	beforeIdx, headIdx := indexObjects(before.Objects), indexObjects(head.Objects)
	out := make(map[string]*creativeops.Revision, len(units)+1)
	existence := false
	for _, u := range units {
		out[u] = currentRev(unitRevs, u)
		if id, _, ok := objectOfUnit(u); ok {
			_, inBefore := beforeIdx[id]
			_, inHead := headIdx[id]
			existence = existence || inBefore != inHead
		}
	}
	if existence {
		out[unitObjects] = currentRev(unitRevs, unitObjects)
	}
	return out
}

func currentRev(unitRevs map[string]int64, u string) *creativeops.Revision {
	r, ok := unitRevs[u]
	if !ok {
		return nil
	}
	v := creativeops.Revision(r)
	return &v
}
