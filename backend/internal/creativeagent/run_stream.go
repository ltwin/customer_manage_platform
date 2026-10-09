package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/realtime"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// RunSnapshot is a run with every block it has shown, read in one repeatable
// read: applying the events after SnapshotSeq to Blocks reproduces the list a
// reader that was following along has.
type RunSnapshot struct {
	Run
	Blocks      []RunBlock `json:"blocks"`
	SnapshotSeq Seq        `json:"snapshot_seq"`
}

// ReadRunSnapshot is the panel's starting point for one run.
func (s *Service) ReadRunSnapshot(ctx context.Context, scope store.AccountScope, runID string) (RunSnapshot, error) {
	var snapshot RunSnapshot
	if runID == "" {
		return snapshot, ErrNotFound
	}
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		run, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", runID))
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		snapshot.Run, snapshot.SnapshotSeq, snapshot.Blocks = run, run.LastEventSeq, []RunBlock{}
		rows, err := tx.QueryPage(ctx, "creative_agent_blocks", blockColumns, "run_id=$2",
			[]store.OrderBy{{Column: "ordinal"}}, maxBlocksPerRun, 0, runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			block, err := scanBlock(rows)
			if err != nil {
				return err
			}
			snapshot.Blocks = append(snapshot.Blocks, block)
		}
		return rows.Err()
	})
	return snapshot, err
}

// RunEvent is the envelope every run event travels in. The SSE id is the seq.
type RunEvent struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         string          `json:"run_id"`
	Seq           Seq             `json:"seq"`
	Type          string          `json:"type"`
	CreatedAt     time.Time       `json:"created_at"`
	Payload       json.RawMessage `json:"payload"`
}

// RunEventSource is a run's event log as a realtime stream. The log is the
// truth: a wake-up only says "read again after the cursor".
type RunEventSource struct{}

var _ realtime.Source = RunEventSource{}

func (RunEventSource) Kind() string { return "agent_run" }

func (RunEventSource) Authorize(ctx context.Context, scope store.AccountScope, id string) error {
	return lostRunAccess(scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		exists, err := tx.Exists(ctx, "creative_agent_runs", "id=$2", id)
		if err == nil && !exists {
			err = ErrNotFound
		}
		return err
	}))
}

// Cursor accepts the snapshot's seq or an SSE Last-Event-ID. A cursor the log
// no longer holds, or one it never handed out, needs a fresh snapshot.
func (RunEventSource) Cursor(ctx context.Context, scope store.AccountScope, id, raw string) (string, error) {
	cursor := int64(0)
	if raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 || strconv.FormatInt(value, 10) != raw {
			return "", fmt.Errorf("%w: event cursor", creativeops.ErrValidation)
		}
		cursor = value
	}
	var last, pruned int64
	if err := scope.QueryRow(ctx, "creative_agent_runs", "last_event_seq,pruned_through_seq", "id=$2", id).Scan(&last, &pruned); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return "", lostRunAccess(ErrNotFound)
		}
		return "", err
	}
	if cursor < pruned || cursor > last {
		return "", realtime.ErrSnapshotRequired
	}
	return strconv.FormatInt(cursor, 10), nil
}

// ReadAfter re-checks that the reader may still see the run in the same
// snapshot it reads the log from. done is set once the run has finished and
// its last event has been returned.
func (RunEventSource) ReadAfter(ctx context.Context, scope store.AccountScope, id, rawCursor string, limit int) ([]realtime.Event, bool, error) {
	cursor, err := strconv.ParseInt(rawCursor, 10, 64)
	if err != nil {
		return nil, false, realtime.ErrSnapshotRequired
	}
	var events []realtime.Event
	done := false
	err = scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		var last, pruned int64
		var finished *time.Time
		if err := tx.QueryRow(ctx, "creative_agent_runs", "last_event_seq,pruned_through_seq,finished_at", "id=$2", id).Scan(&last, &pruned, &finished); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if cursor < pruned {
			return realtime.ErrSnapshotRequired
		}
		rows, err := tx.QueryPage(ctx, "creative_run_events", "seq,schema_version,event_type,payload,created_at", "run_id=$2 AND seq>$3",
			[]store.OrderBy{{Column: "seq"}}, limit, 0, id, cursor)
		if err != nil {
			return err
		}
		defer rows.Close()
		reached := cursor
		for rows.Next() {
			event := RunEvent{RunID: id}
			var seq int64
			if err := rows.Scan(&seq, &event.SchemaVersion, &event.Type, &event.Payload, &event.CreatedAt); err != nil {
				return err
			}
			event.Seq = Seq(seq)
			data, err := json.Marshal(event)
			if err != nil {
				return err
			}
			events = append(events, realtime.Event{ID: strconv.FormatInt(seq, 10), Type: event.Type, Data: data})
			reached = seq
		}
		if err := rows.Err(); err != nil {
			return err
		}
		done = finished != nil && reached >= last
		return nil
	})
	if err != nil {
		return nil, false, lostRunAccess(err)
	}
	return events, done, nil
}

// lostRunAccess marks "this run is gone for you" so a running stream says
// unavailable once; the original stays wrapped for the JSON answer.
func lostRunAccess(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, store.ErrCreativeAccessDenied) {
		return fmt.Errorf("%w: %w", realtime.ErrUnavailable, err)
	}
	return err
}

// BlockDetail is a tool row's full record, read only when the photographer
// opens it. Results are the bounded projection the model saw.
type BlockDetail struct {
	BlockID   string          `json:"block_id"`
	StepID    string          `json:"step_id"`
	ToolKey   string          `json:"tool_key"`
	Arguments json.RawMessage `json:"arguments"`
	Result    string          `json:"result,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
}

func (s *Service) ReadBlockDetail(ctx context.Context, scope store.AccountScope, runID, blockID string) (BlockDetail, error) {
	detail := BlockDetail{BlockID: blockID}
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		var kind string
		var payload json.RawMessage
		if err := tx.QueryRow(ctx, "creative_agent_blocks", "type,payload", "id=$2 AND run_id=$3", blockID, runID).Scan(&kind, &payload); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var tool toolPayload
		if kind != BlockTool || json.Unmarshal(payload, &tool) != nil || tool.DetailRef == "" {
			return ErrNotFound
		}
		var input, output []byte
		var key, code *string
		if err := tx.QueryRow(ctx, "creative_agent_steps", "tool_key,input,output,error_code", "id=$2 AND run_id=$3 AND kind='tool'", tool.DetailRef, runID).
			Scan(&key, &input, &output, &code); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var call llmgateway.ToolCall
		if err := json.Unmarshal(input, &call); err != nil {
			return err
		}
		detail.StepID, detail.Arguments = tool.DetailRef, call.Arguments
		if key != nil {
			detail.ToolKey = *key
		}
		if code != nil {
			detail.ErrorCode = *code
		}
		if len(output) > 0 {
			var projection toolProjection
			if err := json.Unmarshal(output, &projection); err != nil {
				return err
			}
			detail.Result = projection.Result
		}
		return nil
	})
	return detail, err
}
