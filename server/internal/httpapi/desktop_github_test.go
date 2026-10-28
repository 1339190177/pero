package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	source "github.com/phodal/pero/server/internal/github"
	"github.com/phodal/pero/server/internal/httpapi"
	"github.com/phodal/pero/server/internal/store"
)

// This acceptance path is explicit: ordinary Go tests must not launch Desktop.
// GitHub transport is deterministic, while PostgreSQL, Go API, Electron and the
// standalone Worker are the real implementations exercised by the Node harness.
func TestDesktopGitHubEndToEnd(t *testing.T) {
	if os.Getenv("PERO_DESKTOP_E2E") != "1" {
		t.Skip("prerequisite: set PERO_DESKTOP_E2E=1 to launch the real Desktop joint test")
	}
	dsn := os.Getenv("PERO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("prerequisite: set PERO_TEST_DATABASE_URL to an isolated real PostgreSQL test database")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("prerequisite: Node.js is required to run tests/e2e/github.mjs")
	}
	root := desktopTestRoot(t)
	if _, err = os.Stat(filepath.Join(root, "tests/e2e/github.mjs")); err != nil {
		t.Fatal("missing tests/e2e/github.mjs in the selected worktree")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "desktop_github_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("remove joint-test schema: %v", err)
		}
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(ctx, u.String(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	worker, workerToken, err := db.CreateWorker(ctx, "GitHub Desktop fixture Worker", []string{"default"}, []string{"fixture"})
	if err != nil {
		t.Fatal(err)
	}
	_, aliceToken, err := db.CreateClient(ctx, "Alice", []string{"default"})
	if err != nil {
		t.Fatal(err)
	}
	_, bobToken, err := db.CreateClient(ctx, "Bob", []string{"default"})
	if err != nil {
		t.Fatal(err)
	}
	privateTask, err := db.CreateTask(ctx, store.Spec{Title: "Private fixture task", Prompt: "Must remain outside contributor scope", Acceptance: "Not visible to default-project clients", ProjectID: "private", WorkspaceKey: "default", AgentID: "fixture"}, "")
	if err != nil {
		t.Fatal(err)
	}

	var repositoryReads, issueReads atomic.Int32
	var controls atomic.Int32
	var clientDispatches atomic.Int32
	var fixtureMu sync.RWMutex
	humanBody, humanState, humanUpdated := "Complete the manually claimed fixture task.", "open", "2026-10-28T00:00:00Z"
	controlToken := "fixture-control-" + schema
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+controlToken {
			http.Error(w, "fixture control denied", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/metrics" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int32{"client_dispatch_requests": clientDispatches.Load()})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/human" {
			http.Error(w, "fixture control denied", http.StatusForbidden)
			return
		}
		var in struct {
			Phase string `json:"phase"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
			http.Error(w, "invalid fixture control", 400)
			return
		}
		fixtureMu.Lock()
		defer fixtureMu.Unlock()
		switch in.Phase {
		case "changed":
			humanBody, humanUpdated = "The provider goal changed after execution; retain the original goal and execution evidence.", "2026-10-28T01:00:00Z"
		case "closed":
			humanState, humanUpdated = "closed", "2026-10-28T02:00:00Z"
		default:
			http.Error(w, "unknown fixture phase", 400)
			return
		}
		controls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"phase": in.Phase, "client_dispatch_requests": clientDispatches.Load()})
	}))
	defer control.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			t.Errorf("GitHub fixture received forbidden method %s", r.Method)
			http.Error(w, "read-only fixture", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("GitHub transport authentication missing")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/acme/repo":
			repositoryReads.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 501, "full_name": "acme/repo", "html_url": "https://github.com/acme/repo"})
		case "/repos/acme/repo/issues":
			if r.URL.Query().Get("state") != "all" {
				t.Error("GitHub intake did not request open and closed issues")
				http.Error(w, "state=all required", http.StatusBadRequest)
				return
			}
			issueReads.Add(1)
			fixtureMu.RLock()
			defer fixtureMu.RUnlock()
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 9001, "node_id": "I_9001", "number": 1, "title": "Automatic GitHub task", "body": "Complete the automatic fixture task.", "state": "open", "html_url": "https://github.com/acme/repo/issues/1", "updated_at": "2026-10-28T00:00:00Z", "labels": []any{map[string]string{"name": "pero:ready"}}},
				map[string]any{"id": 9002, "node_id": "I_9002", "number": 2, "title": "Claimable GitHub task", "body": humanBody, "state": humanState, "html_url": "https://github.com/acme/repo/issues/2", "updated_at": humanUpdated, "labels": []any{}},
			})
		default:
			t.Errorf("unexpected GitHub fixture path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service, err := source.New(db, []store.GitHubRepository{{ProjectID: "default", Owner: "acme", Repo: "repo", WorkspaceKey: "default", AgentID: "fixture", Acceptance: "Fixture Agent turn completes; no independent verification is implied", AutoDispatch: true, RequiredLabel: "pero:ready", WorkerID: worker.ID}}, "test-secret", source.Options{BaseURL: provider.URL, Client: provider.Client()})
	if err != nil {
		t.Fatal(err)
	}
	operatorToken := "desktop-github-operator"
	actualAPI := httpapi.Handler(db, operatorToken, service)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/client/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/dispatch") {
			clientDispatches.Add(1)
		}
		actualAPI.ServeHTTP(w, r)
	}))
	defer api.Close()
	// Do not call Start or Sync here: the operator's visible Sync action must cause
	// the first provider request and create the initially absent GitHub tasks.
	commandContext, stopCommand := context.WithTimeout(ctx, 90*time.Second)
	defer stopCommand()
	cmd := exec.CommandContext(commandContext, node, filepath.Join(root, "tests/e2e/github.mjs"))
	cmd.Dir = root
	cmd.WaitDelay = 2 * time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PERO_CLIENT_TOKEN" || key == "PERO_OPERATOR_TOKEN" || key == "PERO_CLOUD_URL" || strings.HasPrefix(key, "PERO_GITHUB_E2E_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "PERO_GITHUB_E2E_URL="+api.URL, "PERO_GITHUB_E2E_OPERATOR="+operatorToken, "PERO_GITHUB_E2E_ALICE="+aliceToken, "PERO_GITHUB_E2E_BOB="+bobToken, "PERO_GITHUB_E2E_WORKER="+workerToken, "PERO_GITHUB_E2E_WORKER_ID="+worker.ID, "PERO_GITHUB_E2E_PRIVATE_TASK_ID="+privateTask.ID, "PERO_GITHUB_E2E_ROOT="+root)
	cmd.Env = append(cmd.Env, "PERO_GITHUB_E2E_CONTROL_URL="+control.URL+"/human", "PERO_GITHUB_E2E_CONTROL_TOKEN="+controlToken)
	stdout, stderr := &desktopBoundedOutput{}, &desktopBoundedOutput{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	for _, output := range []struct {
		name   string
		buffer *desktopBoundedOutput
	}{{"stdout", stdout}, {"stderr", stderr}} {
		text := output.buffer.String()
		for _, secret := range []string{operatorToken, aliceToken, bobToken, workerToken, controlToken, "test-secret"} {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
		if text != "" {
			t.Logf("Desktop %s:\n%s", output.name, text)
		}
	}
	if err != nil {
		if commandContext.Err() != nil {
			t.Fatalf("Desktop joint test exceeded its 90-second deadline: %v", commandContext.Err())
		}
		t.Fatalf("Desktop joint test failed: %v", err)
	}
	if repositoryReads.Load() == 0 || issueReads.Load() == 0 {
		t.Fatal("Desktop never triggered actual GitHub Service transport reads")
	}
	if controls.Load() != 2 || issueReads.Load() < 4 {
		t.Fatal("Desktop did not verify both changed and closed source phases through fresh GitHub observations")
	}
	if clientDispatches.Load() != 2 {
		t.Fatalf("unexpected client dispatch requests: got%d, want2 (one denied ownership probe, one Bob execution)", clientDispatches.Load())
	}
	t.Logf("fixture reads: repository=%d issues=%d; controlled source transitions=%d; client dispatch requests=%d", repositoryReads.Load(), issueReads.Load(), controls.Load(), clientDispatches.Load())
}

func desktopTestRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "package.json")); err == nil {
			if _, err := os.Stat(filepath.Join(directory, "server/go.mod")); err == nil {
				return directory
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("cannot locate the Pero worktree root from the Go test directory")
		}
		directory = parent
	}
}

// Child output stays bounded without making writes fail and blocking its pipes.
type desktopBoundedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *desktopBoundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := len(p)
	remaining := 32*1024 - len(b.data)
	if remaining < len(p) {
		b.truncated = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return count, nil
}
func (b *desktopBoundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.truncated {
		return fmt.Sprintf("%s\n[output truncated at 32 KiB]", b.data)
	}
	return string(b.data)
}
