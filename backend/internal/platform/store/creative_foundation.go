package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrCreativeAccessDenied = errors.New("creative capability unavailable")

type CreativeCapabilities struct {
	Read            bool
	ManualWrite     bool
	MediaWrite      bool
	AgentStart      bool
	NodeGenerate    bool
	GenerationApply bool
	GCDelete        bool
}

// CreativeCapabilities describes implemented product capabilities for an active account.
func (sc AccountScope) CreativeCapabilities(ctx context.Context) (CreativeCapabilities, error) {
	var status string
	err := sc.execRunner().QueryRow(ctx, "SELECT status FROM accounts WHERE id=$1", sc.AccountID()).Scan(&status)
	if errors.Is(err, ErrNoRows) {
		return CreativeCapabilities{}, nil
	}
	if err != nil {
		return CreativeCapabilities{}, err
	}
	active := status == "active"
	return CreativeCapabilities{Read: active, ManualWrite: active, MediaWrite: active, AgentStart: active}, nil
}

func (sc TxAccountScope) RequireCreativeCapability(ctx context.Context, capability string) error {
	if err := sc.LockCreativeWrite(ctx); err != nil {
		return err
	}
	var status string
	if err := sc.scope.execRunner().QueryRow(ctx, "SELECT status FROM accounts WHERE id = $1 FOR SHARE", sc.AccountID()).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return ErrCreativeAccessDenied
	}
	c, err := sc.scope.CreativeCapabilities(ctx)
	if err != nil {
		return err
	}
	enabled := false
	switch capability {
	case "creative_read":
		enabled = c.Read
	case "manual_write":
		enabled = c.Read && c.ManualWrite
	case "media_write":
		enabled = c.Read && c.MediaWrite
	case "agent_start":
		enabled = c.Read && c.AgentStart
	case "node_generate":
		enabled = c.Read && c.NodeGenerate
	case "generation_apply":
		enabled = c.Read && c.GenerationApply
	case "gc_delete":
		enabled = c.GCDelete
	}
	if !enabled {
		return ErrCreativeAccessDenied
	}
	return nil
}

// LockCreativeOperation must precede capability and domain locks. Hash collisions
// only serialize extra operations; the account/key primary key is authoritative.
func (sc TxAccountScope) LockCreativeOperation(ctx context.Context, operationID string) error {
	if sc.AccountID() == "" {
		return ErrEmptyAccountScope
	}
	_, err := sc.scope.execRunner().Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('creative-operation:' || $1::text || ':' || $2::text, 0))", sc.AccountID(), operationID)
	if err != nil {
		return fmt.Errorf("lock creative operation: %w", err)
	}
	return nil
}

func (sc TxAccountScope) CreativeNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := sc.scope.execRunner().QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
	return now, err
}

// RequireCreativeRead checks the active account inside the caller's read-only snapshot.
func (sc ReadTxAccountScope) RequireCreativeRead(ctx context.Context) error {
	c, err := sc.scope.CreativeCapabilities(ctx)
	if err != nil {
		return err
	}
	if !c.Read {
		return ErrCreativeAccessDenied
	}
	return nil
}
