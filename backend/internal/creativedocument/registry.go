// Package creativedocument owns documents that sit behind canvas nodes: their
// revision history, per-unit concurrency, reference holders, trash and the
// assistant conversation each canvas keeps for them. It never interprets a
// payload; document types plug in through Type and own their units and
// operations.
package creativedocument

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// MetaUnit is reserved for the title; types never touch it.
const MetaUnit = "meta"

// Type is implemented by each document type and registered at startup.
type Type interface {
	// Key is the document type stored in creative_documents.type_key.
	Key() string
	// NodeTypeKey is the one canvas node type that shows this document.
	NodeTypeKey() string
	// ContentKind is the creative content kind of every snapshot.
	ContentKind() string
	// Creatable reports whether add_node may create a new document.
	Creatable() bool
	// Empty is the first snapshot; opts come from add_node's document_options.
	Empty(ctx context.Context, tx store.TxAccountScope, title string, opts json.RawMessage) (Snapshot, error)
	// Units lists every unit the payload holds, meta excluded.
	Units(payload creativecontent.Payload) ([]string, error)
	// Touched decides which units the operations touch, read-only. It returns
	// one entry per touch so a unit touched twice shows up twice, and adds the
	// collection unit whenever a unit appears or disappears.
	Touched(ctx context.Context, tx store.TxAccountScope, cur Current, actor Actor, ops []json.RawMessage) ([]string, error)
	// Apply runs after the version check. Content it creates in this
	// transaction must be retained through retain.
	Apply(ctx context.Context, tx store.TxAccountScope, cur Current, actor Actor, retain Retain, ops []json.RawMessage) (Snapshot, Changes, error)
	// RestoreTouched is Touched for restore_units, read-only.
	RestoreTouched(ctx context.Context, tx store.TxAccountScope, old, cur Current, units []string) ([]string, error)
	// RestoreUnits replaces those units of cur with their state in old.
	RestoreUnits(ctx context.Context, tx store.TxAccountScope, old, cur Current, retain Retain, units []string) (Snapshot, Changes, error)
	// View is the type's part of a read response; units, when set, narrows it.
	View(ctx context.Context, tx store.ReadTxAccountScope, cur Current, units []string) (json.RawMessage, error)
	// Summary is what a canvas card and a document list show.
	Summary(ctx context.Context, tx store.ReadTxAccountScope, cur Current) (json.RawMessage, error)
}

// RestoreHook lets a type adjust a whole-document restore, for example to keep
// a section confirmed when its confirmation matched the restored revision.
type RestoreHook interface {
	AfterRestore(ctx context.Context, tx store.TxAccountScope, old Current, restored Snapshot, revision int64) (Snapshot, error)
}

// Snapshot is a full payload plus every content revision it references.
type Snapshot struct {
	Payload creativecontent.Payload
	Refs    []string
}

// Changes says which units a write advanced, only annotated, or removed.
type Changes struct {
	Changed   []string
	Annotated []string
	Removed   []string
}

// Current is one revision of a document. For an old revision, UnitRevs and
// Refs are what that revision recorded.
type Current struct {
	Document Document
	Revision int64
	Title    string
	Content  creativecontent.Revision
	UnitRevs map[string]int64
	Refs     []string
}

// Actor is who writes; the server decides it, never the payload.
type Actor struct {
	Kind  string // photographer | agent | system
	RunID *string
}

// Photographer is the actor for every HTTP write.
var Photographer = Actor{Kind: "photographer"}

// Retain registers content created inside a write as held by the revision
// that write is producing. Pass it as creativecontent.WriteAndRetain's retain.
type Retain func(creativecontent.Revision) error

var registry = struct {
	sync.RWMutex
	byKey  map[string]Type
	byNode map[string]Type
}{byKey: map[string]Type{}, byNode: map[string]Type{}}

// Register adds a type at startup. A repeated key or node type is a wiring
// mistake and panics rather than letting one silently shadow the other.
func Register(t Type) {
	registry.Lock()
	defer registry.Unlock()
	if _, ok := registry.byKey[t.Key()]; ok {
		panic(fmt.Sprintf("creativedocument: type %q registered twice", t.Key()))
	}
	if _, ok := registry.byNode[t.NodeTypeKey()]; ok {
		panic(fmt.Sprintf("creativedocument: node type %q registered twice", t.NodeTypeKey()))
	}
	registry.byKey[t.Key()] = t
	registry.byNode[t.NodeTypeKey()] = t
}

// Cleaner is the part of testing.TB that RegisterForTest needs; production
// code does not import the testing package.
type Cleaner interface {
	Helper()
	Cleanup(func())
}

// RegisterForTest registers t for the duration of the test.
func RegisterForTest(tb Cleaner, t Type) {
	tb.Helper()
	Register(t)
	tb.Cleanup(func() {
		registry.Lock()
		defer registry.Unlock()
		delete(registry.byKey, t.Key())
		delete(registry.byNode, t.NodeTypeKey())
	})
}

// Lookup finds a type by its document type key.
func Lookup(key string) (Type, bool) {
	registry.RLock()
	defer registry.RUnlock()
	t, ok := registry.byKey[key]
	return t, ok
}

// ForNode finds the document type a canvas node type stands for.
func ForNode(nodeTypeKey string) (Type, bool) {
	registry.RLock()
	defer registry.RUnlock()
	t, ok := registry.byNode[nodeTypeKey]
	return t, ok
}

// NodeTypes lists every registered document node type.
func NodeTypes() []Type {
	registry.RLock()
	defer registry.RUnlock()
	out := make([]Type, 0, len(registry.byNode))
	for _, t := range registry.byNode {
		out = append(out, t)
	}
	return out
}
