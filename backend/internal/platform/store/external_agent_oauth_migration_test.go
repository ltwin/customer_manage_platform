package store_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackExternalAgentOAuth is the head of the rollback chain: every older
// migration's rollback helper undoes this one first.
func rollbackExternalAgentOAuth(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback external-agent-oauth", err)
	}
}

const (
	oauthAccountInsert    = `INSERT INTO accounts (id, password_hash) VALUES ('acct', 'hash')`
	oauthConnectionInsert = `INSERT INTO external_agent_connections(id,account_id,client_id,client_name,host_kind,display_name,scopes)
VALUES('cxcon_1','acct','oclient_1','Claude Code','claude_code','Claude Code','{creative}')`
)

func TestExternalAgentOAuthMigrationConstraintsAndLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackExternalAgentOAuth(t, url)
	if _, err = db.Exec(`SELECT 1 FROM external_agent_connections`); err == nil {
		t.Fatal("connections table survived rollback")
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{oauthAccountInsert, oauthConnectionInsert} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}

	rejected := []struct{ name, query string }{
		{"unknown host kind", `INSERT INTO external_agent_connections(id,account_id,client_id,client_name,host_kind,display_name,scopes)
VALUES('cxcon_2','acct','c','n','cursor','n','{creative}')`},
		{"id without prefix", `INSERT INTO external_agent_connections(id,account_id,client_id,client_name,host_kind,display_name,scopes)
VALUES('conn_2','acct','c','n','other','n','{creative}')`},
		{"other scope", `INSERT INTO external_agent_connections(id,account_id,client_id,client_name,host_kind,display_name,scopes)
VALUES('cxcon_2','acct','c','n','other','n','{creative,crm}')`},
		{"revoked without reason", `UPDATE external_agent_connections SET revoked_at=now() WHERE id='cxcon_1'`},
		{"reason without revoke", `UPDATE external_agent_connections SET revoked_reason='user' WHERE id='cxcon_1'`},
		{"cimd client with a secret method", `INSERT INTO oauth_clients(client_id,registration,client_name,redirect_uris,token_endpoint_auth_method,client_secret_hash,metadata,fetched_at)
VALUES('https://a.example/m.json','cimd','A','{http://127.0.0.1/cb}','client_secret_post','\x00','{}',now())`},
		{"cimd client never fetched", `INSERT INTO oauth_clients(client_id,registration,client_name,redirect_uris,token_endpoint_auth_method,metadata)
VALUES('https://a.example/m.json','cimd','A','{http://127.0.0.1/cb}','none','{}')`},
		{"confidential client without secret", `INSERT INTO oauth_clients(client_id,registration,client_name,redirect_uris,token_endpoint_auth_method,metadata)
VALUES('oclient_2','dcr','A','{https://a.example/cb}','client_secret_basic','{}')`},
		{"public client with secret", `INSERT INTO oauth_clients(client_id,registration,client_name,redirect_uris,token_endpoint_auth_method,client_secret_hash,metadata)
VALUES('oclient_2','dcr','A','{https://a.example/cb}','none','\x00','{}')`},
		{"six redirect uris", `INSERT INTO oauth_clients(client_id,registration,client_name,redirect_uris,token_endpoint_auth_method,metadata)
VALUES('oclient_2','dcr','A','{a,b,c,d,e,f}','none','{}')`},
		{"short code challenge", `INSERT INTO oauth_authorization_codes(code_hash,client_id,account_id,connection_id,redirect_uri,code_challenge,resource,expires_at)
VALUES('\x01','c','acct','cxcon_1','u','short','r',now())`},
	}
	for _, r := range rejected {
		if _, err := db.Exec(r.query); err == nil {
			t.Fatalf("%s: accepted", r.name)
		}
	}
	if _, err = db.Exec(`UPDATE external_agent_connections SET revoked_at=now(), revoked_reason='user' WHERE id='cxcon_1'`); err != nil {
		t.Fatal("revoke rejected", err)
	}

	refresh := `INSERT INTO oauth_refresh_tokens(token_hash,connection_id,account_id,state,absolute_expires_at) VALUES($1,'cxcon_1','acct',$2,now()+interval '90 days')`
	if _, err = db.Exec(refresh, []byte{1}, "current"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(refresh, []byte{2}, "rotated"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(refresh, []byte{3}, "current"); err == nil {
		t.Fatal("a second current refresh token accepted")
	}

	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "external agent connections exist") {
		t.Fatal("lossy rollback allowed", err)
	}
}

// Pages hear about a new connection, a revoke and a connection turning active;
// the once-a-minute touch while active stays silent.
func TestExternalAgentConnectionsNotifyOnlyVisibleChanges(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(oauthAccountInsert); err != nil {
		t.Fatal(err)
	}
	listener, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close(context.Background()) }()
	if _, err = listener.Exec(ctx, "LISTEN creative_realtime"); err != nil {
		t.Fatal(err)
	}
	notified := func() bool {
		t.Helper()
		wait, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		n, err := listener.WaitForNotification(wait)
		if errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(n.Payload, `"Kind" : "canvas"`) || !strings.Contains(n.Payload, `"Account" : "acct"`) {
			t.Fatalf("payload %s", n.Payload)
		}
		return true
	}
	steps := []struct {
		name   string
		query  string
		notify bool
	}{
		{"created", oauthConnectionInsert, true},
		{"first use", `UPDATE external_agent_connections SET last_used_at=now() WHERE id='cxcon_1'`, true},
		{"touch while active", `UPDATE external_agent_connections SET last_used_at=last_used_at+interval '1 minute' WHERE id='cxcon_1'`, false},
		{"active again after idling", `UPDATE external_agent_connections SET last_used_at=last_used_at+interval '6 minutes' WHERE id='cxcon_1'`, true},
		{"revoked", `UPDATE external_agent_connections SET revoked_at=now(), revoked_reason='user' WHERE id='cxcon_1'`, true},
	}
	for _, s := range steps {
		if _, err := db.Exec(s.query); err != nil {
			t.Fatal(s.name, err)
		}
		if got := notified(); got != s.notify {
			t.Fatalf("%s: notified=%v, want %v", s.name, got, s.notify)
		}
	}
}
