package creativeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	checkpointRuntime    = "eino/v0.9.19"
	checkpointSerializer = "adk-gob/creative-1"
	checkpointRegistry   = "creative-runtime/2"
	checkpointMaxBytes   = 1 << 20
)

var (
	errCheckpointMissing  = errors.New("creative run has a journal but no checkpoint")
	errCheckpointVersion  = errors.New("creative checkpoint is incompatible with this runtime")
	errCheckpointJournal  = errors.New("creative checkpoint cannot be reconciled with its journal")
	errCheckpointConflict = errors.New("creative checkpoint revision changed")
	errRunInterrupted     = errors.New("creative execution interrupted with a saved checkpoint")
)

// The opaque framework payload supplies control flow, never authority. The
// account, current claim and schema identities come from this fresh adapter.
// One adapter belongs to one execution; revision is its optimistic write fence.
type runCheckpoint struct {
	runtime        *runtimeHandler
	registryDigest string
	skillDigest    string
	mu             sync.Mutex
	revision       int64
}

var _ adk.CheckPointStore = (*runCheckpoint)(nil)
var _ adk.CheckPointDeleter = (*runCheckpoint)(nil)

func newRunCheckpoint(h *runtimeHandler) (*runCheckpoint, error) {
	registry, err := json.Marshal(struct {
		Version string      `json:"version"`
		Tools   []ToolEntry `json:"tools"`
		Limits  Limits      `json:"limits"`
		Model   any         `json:"model"`
	}{
		Version: checkpointRegistry,
		Tools:   h.service.registeredTools(),
		Limits:  h.service.limits,
		Model:   h.service.models.Snapshot(h.held.Model),
	})
	if err != nil {
		return nil, err
	}
	skill, err := json.Marshal(h.held.Skill)
	if err != nil {
		return nil, err
	}
	return &runCheckpoint{runtime: h, registryDigest: digestBytes(registry), skillDigest: digestBytes(skill)}, nil
}
func digestBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

type checkpointStep struct {
	ID         string `json:"id"`
	Ordinal    int64  `json:"ordinal"`
	Kind       string `json:"kind"`
	InputHash  string `json:"input_hash"`
	Parent     string `json:"parent"`
	State      string `json:"state"`
	OutputHash string `json:"output_hash"`
}

func (c *runCheckpoint) journal(ctx context.Context, tx store.TxAccountScope) ([]checkpointStep, error) {
	h := c.runtime
	limit := h.service.limits.ModelTurns + h.service.limits.ToolCalls
	rows, err := tx.QueryPage(ctx, "creative_agent_steps", "id,ordinal,kind,input_hash,parent_model_step_id,state,output",
		"run_id=$2", []store.OrderBy{{Column: "ordinal"}}, limit+1, 0, h.held.RunID)
	if err != nil {
		return nil, err
	}
	var steps []checkpointStep
	for rows.Next() {
		var step checkpointStep
		var output []byte
		var parent *string
		if err = rows.Scan(&step.ID, &step.Ordinal, &step.Kind, &step.InputHash, &parent, &step.State, &output); err != nil {
			rows.Close()
			return nil, err
		}
		if parent != nil {
			step.Parent = *parent
		}
		if len(output) > 0 {
			step.OutputHash = digestBytes(output)
		}
		steps = append(steps, step)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(steps) > limit {
		return nil, errCheckpointJournal
	}
	return steps, nil
}

// A completed prefix is immutable. Prepared read-only plans may have committed
// since the checkpoint; their stored output will be replayed. Later model
// frames are checked against the full request hash at prepareModelStep, never
// selected merely because two prompts happen to look alike.
func compatibleJournal(saved, current []checkpointStep) bool {
	if len(saved) == 0 || len(current) < len(saved) {
		return false
	}
	for i, old := range saved {
		now := current[i]
		if old.ID != now.ID || old.Ordinal != now.Ordinal || old.Kind != now.Kind || old.InputHash != now.InputHash || old.Parent != now.Parent {
			return false
		}
		if old.State == now.State && old.OutputHash == now.OutputHash {
			continue
		}
		if old.Kind != "tool" || old.State != "prepared" || (now.State != "succeeded" && now.State != "failed") || now.OutputHash == "" {
			return false
		}
	}
	return true
}

func (c *runCheckpoint) Get(ctx context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.runtime
	if key != h.held.RunID {
		return nil, false, ErrNotFound
	}
	var payload []byte
	var found bool
	var currentStepID, binding string
	var loadedRevision int64
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
			return err
		}
		var runtime, serializer, registry, skill, digest string
		var savedEpoch int64
		var journal []byte
		var retained time.Time
		err := tx.QueryRow(ctx, "creative_agent_checkpoints", "runtime_version,serializer_version,registry_digest,skill_catalog_digest,execution_epoch,revision,model_step_id,journal,payload,payload_digest,retained_until",
			"run_id=$2", key).Scan(&runtime, &serializer, &registry, &skill, &savedEpoch, &loadedRevision, &currentStepID, &journal, &payload, &digest, &retained)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if runtime != checkpointRuntime || serializer != checkpointSerializer || registry != c.registryDigest || skill != c.skillDigest {
			return errCheckpointVersion
		}
		if !now.Before(retained) || savedEpoch > h.held.Epoch || len(payload) == 0 || len(payload) > checkpointMaxBytes || digestBytes(payload) != digest {
			return errCheckpointJournal
		}
		var saved []checkpointStep
		if err = json.Unmarshal(journal, &saved); err != nil {
			return errCheckpointJournal
		}
		current, err := c.journal(ctx, tx)
		if err != nil {
			return err
		}
		if !compatibleJournal(saved, current) {
			return errCheckpointJournal
		}
		var parent *checkpointStep
		for i := range saved {
			if saved[i].ID == currentStepID {
				parent = &saved[i]
			}
		}
		if parent == nil || parent.Kind != "model" || parent.State != "succeeded" {
			return errCheckpointJournal
		}
		// Every saved tool result must still have its complete context bytes. These
		// are controlled B2 outputs; arbitrary opaque payload strings are not roots.
		for _, step := range current {
			if step.Kind != "tool" || step.OutputHash == "" {
				continue
			}
			var itemPayload []byte
			var itemDigest string
			err = tx.QueryRow(ctx, "creative_agent_context_items", "payload,digest", "run_id=$2 AND source_step_id_snapshot=$3 AND retained_until>clock_timestamp()", key, step.ID).Scan(&itemPayload, &itemDigest)
			if errors.Is(err, store.ErrNoRows) {
				return errCheckpointJournal
			}
			if err != nil {
				return err
			}
			if digestBytes(itemPayload) != itemDigest {
				return errCheckpointJournal
			}
		}
		binding = turnBindingKey(key, parent.Ordinal)
		found = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if found {
		c.revision = loadedRevision
		h.current.set(currentStepID, binding)
	}
	return append([]byte(nil), payload...), found, nil
}

func (c *runCheckpoint) Set(ctx context.Context, key string, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.runtime
	if key != h.held.RunID {
		return ErrNotFound
	}
	if len(payload) == 0 || len(payload) > checkpointMaxBytes {
		return ErrLimit
	}
	modelStep, _ := h.current.get()
	if modelStep == "" {
		return errCheckpointJournal
	}
	nextRevision := c.revision + 1
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
			return err
		}
		steps, err := c.journal(ctx, tx)
		if err != nil {
			return err
		}
		hasParent := false
		for _, step := range steps {
			if step.ID == modelStep && step.Kind == "model" && step.State == "succeeded" {
				hasParent = true
			}
		}
		if !hasParent {
			return errCheckpointJournal
		}
		journal, err := json.Marshal(steps)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		retained := now.Add(contextRetention)
		if retained.Before(h.held.Deadline) {
			retained = h.held.Deadline
		}
		var id string
		var revision int64
		err = tx.QueryRowForUpdate(ctx, "creative_agent_checkpoints", "id,revision", "run_id=$2", key).Scan(&id, &revision)
		switch {
		case errors.Is(err, store.ErrNoRows):
			if c.revision != 0 {
				return errCheckpointConflict
			}
			id, err = creativeops.NewResourceID("cccp")
			if err != nil {
				return err
			}
			err = tx.Insert(ctx, "creative_agent_checkpoints", []string{"id", "run_id", "runtime_version", "serializer_version", "registry_digest", "skill_catalog_digest", "execution_epoch", "revision", "model_step_id", "journal", "payload", "payload_digest", "created_at", "updated_at", "retained_until"}, id, key, checkpointRuntime, checkpointSerializer, c.registryDigest, c.skillDigest, h.held.Epoch, nextRevision, modelStep, journal, payload, digestBytes(payload), now, now, retained)
		case err != nil:
			return err
		default:
			if revision != c.revision {
				return errCheckpointConflict
			}
			_, err = tx.Update(ctx, "creative_agent_checkpoints", "execution_epoch=$3,revision=$4,model_step_id=$5,journal=$6,payload=$7,payload_digest=$8,updated_at=$9,retained_until=$10", "id=$2", id, h.held.Epoch, nextRevision, modelStep, journal, payload, digestBytes(payload), now, retained)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Delete(ctx, "creative_checkpoint_content_refs", "checkpoint_id=$2", id); err != nil {
			return err
		}
		for _, revision := range h.revisions {
			if err = tx.Insert(ctx, "creative_checkpoint_content_refs", []string{"checkpoint_id", "content_revision_id"}, id, revision); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		c.revision = nextRevision
	}
	return err
}

func (c *runCheckpoint) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.runtime
	if key != h.held.RunID {
		return ErrNotFound
	}
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
			return err
		}
		var id string
		var revision int64
		err := tx.QueryRowForUpdate(ctx, "creative_agent_checkpoints", "id,revision", "run_id=$2", key).Scan(&id, &revision)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if revision != c.revision {
			return errCheckpointConflict
		}
		if _, err = tx.Delete(ctx, "creative_checkpoint_content_refs", "checkpoint_id=$2", id); err != nil {
			return err
		}
		_, err = tx.Delete(ctx, "creative_agent_checkpoints", "id=$2", id)
		return err
	})
	if err == nil {
		c.revision = 0
	}
	return err
}
