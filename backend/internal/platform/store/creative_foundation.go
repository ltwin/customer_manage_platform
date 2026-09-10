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

// CreativeCapabilities is a read projection. Every effect rechecks under the
// existing account writer barrier; a missing row denies all new capabilities.
func (sc AccountScope) CreativeCapabilities(ctx context.Context) (CreativeCapabilities, error) {
	var c CreativeCapabilities
	err := sc.QueryRow(ctx, "creative_account_capabilities", "read_enabled, manual_write_enabled, media_write_enabled, agent_start_enabled, node_generate_enabled, generation_apply_enabled, gc_delete_enabled", "").Scan(&c.Read, &c.ManualWrite, &c.MediaWrite, &c.AgentStart, &c.NodeGenerate, &c.GenerationApply, &c.GCDelete)
	if errors.Is(err, ErrNoRows) {
		return c, nil
	}
	return c, err
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
