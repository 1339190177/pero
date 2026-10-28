package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/phodal/pero/server/internal/store"
)

func database(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("PERO_TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("PERO_REQUIRE_TEST_DATABASE") == "1" {
			t.Fatal("PERO_TEST_DATABASE_URL is required for strict PostgreSQL acceptance")
		}
		t.Skip("set PERO_TEST_DATABASE_URL for real GitHub intake transactions")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "github_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(ctx, u.String(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	return db
}

func TestOnlineIssueSyncDedupFrozenRunAndClosedQueue(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	worker, _, err := db.CreateWorker(ctx, "fixture", []string{"project"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := config()
	c.AutoDispatch = true
	c.WorkerID = worker.ID
	body, state, updated := "Original goal", "open", "2026-10-28T00:00:00Z"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("intake attempted upstream mutation")
		}
		if r.URL.Path == "/repos/acme/repo" {
			w.Write([]byte(`{"id":9007199254740993,"full_name":"acme/repo"}`))
			return
		}
		if r.URL.Query().Get("state") != "all" {
			t.Error("sync omitted closed issues")
		}
		json.NewEncoder(w).Encode([]any{
			map[string]any{"id": json.Number("9007199254740994"), "node_id": "I_issue", "number": 7, "title": "Fix issue", "body": body, "html_url": "https://github.com/acme/repo/issues/7", "state": state, "updated_at": updated, "labels": []any{map[string]string{"name": "pero:ready"}}},
			map[string]any{"id": 300, "number": 8, "pull_request": map[string]string{"url": "https://api.github.com/repos/acme/repo/pulls/8"}},
		})
	}))
	defer provider.Close()
	s, err := New(db, []store.GitHubRepository{c}, "", Options{BaseURL: provider.URL, Client: provider.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Sync(ctx)
	if err != nil || result.Imported != 1 || result.Dispatched != 1 || result.Partial {
		t.Fatalf("first sync: %+v %v", result, err)
	}
	page, err := db.Tasks(ctx, "", 50)
	if err != nil || len(page.Tasks) != 1 {
		t.Fatalf("source mapping: %+v %v", page, err)
	}
	id := page.Tasks[0].ID
	first, err := db.Task(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision := first.Run.Spec.Source.Revision
	result, err = s.Sync(ctx)
	if err != nil || result.Imported != 0 || result.Updated != 0 || result.Dispatched != 0 {
		t.Fatalf("duplicate sync: %+v %v", result, err)
	}
	body = "Edited after dispatch"
	updated = "2026-10-28T00:01:00Z"
	result, err = s.Sync(ctx)
	if err != nil || result.Updated != 1 || result.Dispatched != 0 {
		t.Fatalf("edited sync: %+v %v", result, err)
	}
	edited, err := db.Task(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if edited.LatestSource.Revision == oldRevision || edited.Source.Revision != oldRevision || edited.Run.Spec.Source.Revision != oldRevision || !strings.Contains(edited.Run.Spec.Prompt, "Original goal") || strings.Contains(edited.Prompt, "Edited") {
		t.Fatal("live Run goal or snapshot changed")
	}
	state = "closed"
	updated = "2026-10-28T00:02:00Z"
	if _, err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	closed, err := db.Task(ctx, id)
	if err != nil || closed.State != "cancelled" || closed.LatestSource.State != "closed" {
		t.Fatalf("closed queued source: %+v %v", closed, err)
	}
	if _, err = db.Dispatch(ctx, id, worker.ID, ""); err == nil {
		t.Fatal("closed source dispatched")
	}
	if run, err := db.Claim(ctx, worker.ID, "instance", "closed-claim"); err != nil || run != nil {
		t.Fatalf("closed queued Run executed: %+v %v", run, err)
	}
}

func TestPartialIssueCoverageAndProviderScope(t *testing.T) {
	db := database(t)
	c := config()
	mode := "valid"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/repo" {
			w.Write([]byte(`{"id":200,"full_name":"acme/repo"}`))
			return
		}
		if mode == "rate" {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(403)
			return
		}
		if mode == "scope" {
			w.Header().Set("Link", `<https://evil.test/repos/acme/repo/issues?page=2>; rel="next"`)
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"id": 300, "node_id": "I_300", "number": 7, "title": "valid", "body": "goal", "state": "open", "html_url": "https://github.com/acme/repo/issues/7", "updated_at": "2026-10-28T00:00:00Z", "labels": []any{}}, map[string]any{"id": 301, "node_id": "I_301", "number": 9, "title": strings.Repeat("x", 241)}})
	}))
	defer provider.Close()
	s, err := New(db, []store.GitHubRepository{c}, "", Options{BaseURL: provider.URL, Client: provider.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Sync(context.Background())
	if err != nil || result.Imported != 1 || !result.Partial || len(result.Errors) == 0 {
		t.Fatalf("invalid item coverage: %+v %v", result, err)
	}
	mode = "scope"
	result, err = s.Sync(context.Background())
	if err != nil || !result.Partial || len(result.Errors) == 0 {
		t.Fatalf("cross-origin coverage: %+v %v", result, err)
	}
	mode = "rate"
	result, err = s.Sync(context.Background())
	if err != nil || !result.Partial || !strings.Contains(strings.Join(result.Errors, " "), "rate limited") {
		t.Fatalf("rate coverage: %+v %v", result, err)
	}
}
