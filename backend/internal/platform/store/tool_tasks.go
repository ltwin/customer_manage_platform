package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// ToolTaskRef names one agent tool task a cross-account worker found due. The
// worker must handle it in the account's own scope.
type ToolTaskRef struct {
	AccountID string
	ID        string
}

// DueToolTasks pages agent tool tasks whose finishing work is due, oldest
// first, across accounts (agent-tool-background design「源状态 → 任务收尾」).
// It is a trusted maintenance read of identities only; every business read and
// write still goes through an AccountScope.
func (s *Store) DueToolTasks(ctx context.Context, limit int) ([]ToolTaskRef, error) {
	return s.toolTaskRefs(ctx, `SELECT account_id,id FROM creative_agent_tool_tasks
WHERE next_check_at IS NOT NULL AND next_check_at<=clock_timestamp() ORDER BY next_check_at,id LIMIT $1`, limit)
}

// WakeDueToolTasks pages tasks whose notice group is due for an automatic
// run, oldest first, across accounts. Same trust rules as DueToolTasks.
func (s *Store) WakeDueToolTasks(ctx context.Context, limit int) ([]ToolTaskRef, error) {
	return s.toolTaskRefs(ctx, `SELECT account_id,id FROM creative_agent_tool_tasks
WHERE wake_after IS NOT NULL AND wake_after<=clock_timestamp() ORDER BY wake_after,id LIMIT $1`, limit)
}

// NextToolTaskWake is the earliest pending wake time across accounts, nil
// when none is set: the waker sleeps until then instead of a full scan.
func (s *Store) NextToolTaskWake(ctx context.Context) (*time.Time, error) {
	var at *time.Time
	err := s.pool.QueryRow(ctx, `SELECT min(wake_after) FROM creative_agent_tool_tasks WHERE wake_after IS NOT NULL`).Scan(&at)
	return at, err
}

func (s *Store) toolTaskRefs(ctx context.Context, query string, limit int) ([]ToolTaskRef, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("tool task page must be between 1 and 1000")
	}
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []ToolTaskRef
	for rows.Next() {
		var ref ToolTaskRef
		if err := rows.Scan(&ref.AccountID, &ref.ID); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

var listenChannel = regexp.MustCompile(`^[a-z_]{1,63}$`)

// Listen wakes the caller after any committed NOTIFY on channel. It is a
// wake-up, not a message log: the payload is dropped, and every (re)connect
// sends one wake so the caller rescans for what it may have missed. The
// returned channel is closed when ctx ends.
func (s *Store) Listen(ctx context.Context, channel string) (<-chan struct{}, error) {
	if !listenChannel.MatchString(channel) {
		return nil, errors.New("listen channel must be a lower-case identifier")
	}
	wake := make(chan struct{}, 1)
	config := s.pool.Config().ConnConfig.Copy()
	go listen(ctx, config, channel, wake)
	return wake, nil
}

func listen(ctx context.Context, config *pgx.ConnConfig, channel string, wake chan struct{}) {
	defer close(wake)
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	delay := time.Second
	for ctx.Err() == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := pgx.ConnectConfig(connectCtx, config)
		if err == nil {
			_, err = conn.Exec(connectCtx, "LISTEN "+channel)
		}
		cancel()
		if err == nil {
			delay = time.Second
			signal()
			for ctx.Err() == nil {
				waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
				_, waitErr := conn.WaitForNotification(waitCtx)
				waitCancel()
				if errors.Is(waitErr, context.DeadlineExceeded) && ctx.Err() == nil {
					pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
					pingErr := conn.Ping(pingCtx)
					pingCancel()
					if pingErr == nil {
						continue
					}
				}
				if waitErr != nil {
					break
				}
				signal()
			}
		}
		if conn != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
			_ = conn.Close(closeCtx)
			closeCancel()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
