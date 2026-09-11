package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
)

// Existing node state is seeded by explicit migration, never fabricated by Undo.
func seedCreativeGraphIdentities(url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }() // Migration owns this connection.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // Safe after commit.
	var completed bool
	if err = tx.QueryRowContext(ctx, `SELECT completed FROM creative_graph_migration_state WHERE id=1 FOR UPDATE`).Scan(&completed); err != nil {
		return err
	}
	if completed {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.account_id,n.id,n.canvas_id,n.placement_revision,n.data_revision,to_jsonb(n) FROM creative_nodes n LEFT JOIN creative_graph_identities i ON i.account_id=n.account_id AND i.id=n.id WHERE i.id IS NULL ORDER BY n.account_id,n.id`)
	if err != nil {
		return err
	}
	type entry struct {
		account, id, canvas string
		p, d                int64
		raw                 json.RawMessage
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.account, &e.id, &e.canvas, &e.p, &e.d, &e.raw); err != nil {
			_ = rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, e := range entries {
		heads := map[string]creativegraph.Head{}
		for key, value := range creativegraph.NodeFields(e.raw, nil) {
			heads[key] = creativegraph.NewHead(value)
		}
		raw, err := json.Marshal(heads)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO creative_graph_identities(account_id,id,canvas_id,kind,is_live,placement_revision,data_revision,effect_heads) VALUES($1,$2,$3,'node',true,$4,$5,$6)`, e.account, e.id, e.canvas, e.p, e.d, raw); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE creative_graph_migration_state SET completed=true WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}
