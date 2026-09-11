package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Opt-in browser test; the ordinary Go gate never launches a browser or a dev server.
func TestCreativeEditorBrowser(t *testing.T) {
	frontendURL := os.Getenv("CREATIVE_EDITOR_URL")
	if frontendURL == "" {
		t.Skip("set CREATIVE_EDITOR_URL to the isolated Vite server")
	}
	databaseURL := startCustomerPostgres(t)
	db, err := store.Open(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mail := &testMailSender{}
	tokens := auth.NewTokenIssuer("synthetic-creative-browser-secret")
	service := auth.NewService(db, tokens, auth.WithAttemptLimiter(db), auth.WithAuthMailSender(mail), auth.WithRegistrationAdmissionMode(auth.RegistrationPublic), auth.WithPublicBaseURL(frontendURL))
	// Real media pipeline: local adapter, ffprobe verification and an in-process worker.
	media, err := creativemedia.Compose(config.Config{AvatarStorageDriver: config.StorageDriverLocal, CreativeMediaLocalRoot: t.TempDir(), CreativeFFProbe: "ffprobe", CreativeMediaQuotaBytes: 1 << 30}, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	mediaJobs, err := db.NewJobRuntime(media.Handlers(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	media.SetRuntime(mediaJobs)
	if err := mediaJobs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mediaJobs.Stop(context.Background()) }()
	handler := httpapi.NewRouter(httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: db, ScopeFactory: db, Auth: service, PublicBaseURL: frontendURL, PublicRegistrationEnabled: true, CreativeMedia: media})
	email := "creative-browser@example.invalid"
	registered := authRequest(t, handler, http.MethodPost, "/api/v1/auth/register", `{"email":"`+email+`","password":"`+testPassword+`"}`, frontendURL, "")
	if registered.Code != 202 {
		t.Fatalf("register %d %s", registered.Code, registered.Body.String())
	}
	action, err := url.Parse(mail.Last().ActionURL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(action.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"token": fragment.Get("token")})
	if err != nil {
		t.Fatal(err)
	}
	verified := authRequest(t, handler, http.MethodPost, "/api/v1/auth/email/verify", string(body), frontendURL, "")
	if verified.Code != 200 {
		t.Fatalf("verify %d %s", verified.Code, verified.Body.String())
	}
	var access httpapi.AccessTokenResponse
	if err := json.Unmarshal(verified.Body.Bytes(), &access); err != nil {
		t.Fatal(err)
	}
	me := shootPlanningRequest(t, handler, "GET", "/api/v1/me", access.AccessToken, "", "")
	var account struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}

	// Isolated benchmark fixture endpoint exists only in this opt-in test server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__test/document" {
			if r.Method != http.MethodPost {
				w.WriteHeader(405)
				return
			}
			result, err := documentFixture(r.Context(), db.ScopeFor(auth.AccountContext{AccountID: account.ID}), r.URL.Query().Get("canvas_id"), r.URL.Query().Get("document_id"))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err = json.NewEncoder(w).Encode(result); err != nil {
				t.Error(err)
			}
			return
		}
		if r.URL.Path != "/__test/library-scale" {
			handler.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		seedDB, e := sql.Open("pgx", databaseURL)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer func() { _ = seedDB.Close() }()
		tx, e := seedDB.BeginTx(r.Context(), nil)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer func() { _ = tx.Rollback() }()
		var seedID, accountID string
		if e = tx.QueryRow(`SELECT id,account_id FROM creative_assets WHERE kind='text' AND deleted_at IS NULL LIMIT 1`).Scan(&seedID, &accountID); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		_, e = tx.Exec(`INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,description,content_id,content_revision_id,is_favorite) SELECT 'ui-bench-'||g,a.account_id,a.kind,'摄影参考 '||g,'摄影参考 '||g,'窗边自然光与室内布光 100%',a.content_id,a.content_revision_id,g%3=0 FROM generate_series(1,10000)g CROSS JOIN creative_assets a WHERE a.account_id=$1 AND a.id=$2`, accountID, seedID)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		for _, q := range []string{
			`INSERT INTO creative_asset_groups(id,account_id,name,position) SELECT 'ui-g-'||g,$1,'参考组 '||g,g FROM generate_series(1,100)g`,
			`INSERT INTO creative_tags(id,account_id,name,normalized_name,color) SELECT 'ui-t-'||g,$1,'色调 '||g,'色调 '||g,'#ABCDEF' FROM generate_series(1,200)g`,
			`INSERT INTO creative_asset_group_members(account_id,asset_id,group_id) SELECT $1,'ui-bench-'||g,'ui-g-'||(g%100+1) FROM generate_series(1,10000)g`,
			`INSERT INTO creative_asset_tags(account_id,asset_id,tag_id) SELECT $1,'ui-bench-'||g,'ui-t-'||(g%200+1) FROM generate_series(1,10000)g`,
			`UPDATE creative_library_settings SET hierarchy_revision=hierarchy_revision+1,library_revision=library_revision+1 WHERE account_id=$1`,
		} {
			if _, e = tx.Exec(q, accountID); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		_, e = tx.Exec(`INSERT INTO creative_asset_search(account_id,asset_id,normalized_text,asset_revision,normalization_version) SELECT account_id,id,normalized_title||E'\n'||description,revision,$2 FROM creative_assets a WHERE a.account_id=$1 AND a.id LIKE 'ui-bench-%'`, accountID, textindex.Version)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	scripts := []string{"creative-text-canvas.e2e.mjs", "creative-canvas-commands.e2e.mjs", "creative-media.e2e.mjs"}
	if selected := os.Getenv("CREATIVE_EDITOR_SCRIPT"); selected != "" {
		known := false
		for _, name := range scripts {
			known = known || name == selected
		}
		if !known {
			t.Fatal("unknown browser script")
		}
		scripts = []string{selected}
	}
	for _, name := range scripts {
		script := filepath.Join(root, "frontend/scripts", name)
		cmd := exec.CommandContext(t.Context(), "node", script)
		cmd.Env = append(os.Environ(), "CREATIVE_EDITOR_API="+server.URL, "CREATIVE_EDITOR_EMAIL="+email, "CREATIVE_EDITOR_PASSWORD="+testPassword, "CREATIVE_MEDIA_SAMPLES="+filepath.Join(root, "backend/internal/creativemedia/testdata"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("browser %s: %v\n%s", name, err, out)
		}
		t.Log(string(out))
	}
}
