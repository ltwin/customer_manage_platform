package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
)

// Rebuild only missing/versioned projections during explicit migration, never during page reads.
func rebuildCreativeLibraryIndex(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for {
		var account string
		err = db.QueryRowContext(ctx, `SELECT a.account_id FROM creative_assets a LEFT JOIN creative_asset_search s ON s.account_id=a.account_id AND s.asset_id=a.id WHERE s.asset_id IS NULL OR s.normalization_version<>$1 LIMIT 1`, textindex.Version).Scan(&account)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if err := rebuildLibraryAccount(ctx, db, account); err != nil {
			return fmt.Errorf("rebuild library index: %w", err)
		}
	}
}
func rebuildLibraryAccount(ctx context.Context, db *sql.DB, account string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('creative-write:' || $1::text,0))`, account); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.title,a.description,COALESCE(a.source_url,''),a.revision,r.payload FROM creative_assets a JOIN creative_content_revisions r ON r.account_id=a.account_id AND r.id=a.content_revision_id LEFT JOIN creative_asset_search s ON s.account_id=a.account_id AND s.asset_id=a.id WHERE a.account_id=$1 AND (s.asset_id IS NULL OR s.normalization_version<>$2) LIMIT 200`, account, textindex.Version)
	if err != nil {
		return err
	}
	type item struct {
		id, title, text string
		revision        int64
	}
	items := []item{}
	for rows.Next() {
		var i item
		var desc, source string
		var payload json.RawMessage
		if err = rows.Scan(&i.id, &i.title, &desc, &source, &i.revision, &payload); err != nil {
			_ = rows.Close()
			return err
		}
		i.text, err = textindex.Document(i.title, desc, source, payload)
		if err != nil {
			_ = rows.Close()
			return err
		}
		items = append(items, i)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("asset references missing content in account %s", account)
	}
	for _, i := range items {
		if _, err = tx.ExecContext(ctx, `UPDATE creative_assets SET normalized_title=$3 WHERE account_id=$1 AND id=$2`, account, i.id, textindex.Normalize(i.title)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO creative_asset_search(account_id,asset_id,normalized_text,asset_revision,normalization_version) VALUES($1,$2,$3,$4,$5) ON CONFLICT(account_id,asset_id) DO UPDATE SET normalized_text=EXCLUDED.normalized_text,asset_revision=EXCLUDED.asset_revision,normalization_version=EXCLUDED.normalization_version`, account, i.id, i.text, i.revision, textindex.Version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
