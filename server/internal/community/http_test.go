package community

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/phodal/pero/server/internal/httpapi"
	"github.com/phodal/pero/server/internal/store"
)

type fixture struct {
	db                            *store.Store
	app                           *httptest.Server
	provider                      *httptest.Server
	taskID, workerID, workerToken string
}

func communityHTTP(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("PERO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PERO_TEST_DATABASE_URL for PostgreSQL community tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "community_" + random()[:20]
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewUnstartedServer(nil)
	origin := "http://" + app.Listener.Addr().String()
	s, err := newServer(db, "fixture-operator", Config{BaseURL: origin, ClientID: "fixture-client", ClientSecret: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			http.Redirect(w, r, origin+"/auth/github/callback?"+url.Values{"state": {r.URL.Query().Get("state")}, "code": {"fixture-code"}}.Encode(), 302)
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("client_secret") != "fixture-secret" || r.Form.Get("code") != "fixture-code" {
				http.Error(w, "invalid", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"fixture-github-token"}`)
		case "/user":
			if r.Header.Get("Authorization") != "Bearer fixture-github-token" {
				http.Error(w, "invalid", 401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":101,"login":"alice"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	s.oauthAuthorize, s.oauthToken, s.oauthUser = provider.URL+"/authorize", provider.URL+"/token", provider.URL+"/user"
	handler := s.routes(httpapi.Handler(db, "fixture-operator"))
	app.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__fixture/unavailable" {
			s.fail(w, r, errors.New("fixture backend unavailable"))
			return
		}
		handler.ServeHTTP(w, r)
	})
	app.Start()
	task, err := db.CreateTask(ctx, store.Spec{Title: "Private task title", Prompt: "PRIVATE_PROMPT_SENTINEL", Acceptance: "PRIVATE_ACCEPTANCE_SENTINEL", ProjectID: "pero", WorkspaceKey: "PRIVATE_WORKSPACE_SENTINEL", AgentID: "fixture"}, "")
	if err != nil {
		t.Fatal(err)
	}
	worker, workerToken, err := db.CreateWorker(ctx, "Alice’s Worker", []string{"pero"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.Close()
		provider.Close()
		db.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	return &fixture{db, app, provider, task.ID, worker.ID, workerToken}
}
func fetch(t *testing.T, client *http.Client, method, target, origin string, form url.Values) (int, string) {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(data)
}
func TestCommunityHTTPPrivacyOAuthAndCSRF(t *testing.T) {
	f := communityHTTP(t)
	ctx := context.Background()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	status, body := fetch(t, client, "GET", f.app.URL+"/tasks", "", nil)
	if status != 200 || !strings.Contains(body, "还没有公开任务") {
		t.Fatalf("empty board: %d %s", status, body)
	}
	status, _ = fetch(t, client, "GET", f.app.URL+"/tasks/"+f.taskID, "", nil)
	if status != 404 {
		t.Fatalf("private detail: %d", status)
	}
	if err := f.db.CommunityPublish(ctx, f.taskID, "Public <script>alert(1)</script>", "Public requirements", "https://example.com/issue"); err != nil {
		t.Fatal(err)
	}
	status, body = fetch(t, client, "GET", f.app.URL+"/tasks/"+f.taskID, "", nil)
	for _, secret := range []string{"PRIVATE_PROMPT_SENTINEL", "PRIVATE_ACCEPTANCE_SENTINEL", "PRIVATE_WORKSPACE_SENTINEL", "Private task title", "<script>alert(1)</script>"} {
		if strings.Contains(body, secret) {
			t.Fatalf("public leak: %s", secret)
		}
	}
	if status != 200 || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("HTML was not escaped")
	}
	status, _ = fetch(t, client, "GET", f.app.URL+"/auth/github/callback?state=wrong&code=fixture-code", "", nil)
	if status != 400 {
		t.Fatalf("OAuth state accepted: %d", status)
	}
	status, body = fetch(t, client, "GET", f.app.URL+"/auth/github", "", nil)
	if status != 200 || !strings.Contains(body, "alice") {
		t.Fatalf("OAuth fixture login: %d %s", status, body)
	}
	u, _ := url.Parse(f.app.URL)
	var sessionToken string
	for _, c := range jar.Cookies(u) {
		if c.Name == "pero_community" {
			sessionToken = c.Value
		}
	}
	session, err := f.db.CommunitySession(ctx, sessionToken)
	if err != nil || session.ID != 101 {
		t.Fatalf("GitHub identity: %+v %v", session, err)
	}
	if err = f.db.CommunityBindWorker(ctx, f.workerID, "alice"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {session.CSRF}, "worker": {f.workerID}}
	status, _ = fetch(t, client, "POST", f.app.URL+"/tasks/"+f.taskID+"/claim", "https://evil.example", form)
	if status != 403 {
		t.Fatalf("cross-origin claim: %d", status)
	}
	form.Set("csrf", "wrong")
	status, _ = fetch(t, client, "POST", f.app.URL+"/tasks/"+f.taskID+"/claim", f.app.URL, form)
	if status != 403 {
		t.Fatalf("missing CSRF: %d", status)
	}
	form.Set("csrf", session.CSRF)
	status, body = fetch(t, client, "POST", f.app.URL+"/tasks/"+f.taskID+"/claim", f.app.URL, form)
	if status != 200 || !strings.Contains(body, "排队中") {
		t.Fatalf("claim: %d %s", status, body)
	}
	status, _ = fetch(t, client, "POST", f.app.URL+"/community/admin/accept", f.app.URL, form)
	if status != 403 {
		t.Fatalf("member can accept: %d", status)
	}
	status, _ = fetch(t, client, "POST", f.app.URL+"/auth/logout", f.app.URL, url.Values{"csrf": {session.CSRF}})
	if status != 200 {
		t.Fatalf("logout: %d", status)
	}
	if _, err = f.db.CommunitySession(ctx, sessionToken); err == nil {
		t.Fatal("logout retained session")
	}
	status, body = fetch(t, client, "GET", f.app.URL+"/community/assets/community.css", "", nil)
	if status != 200 || !strings.Contains(body, "var(--surface)") {
		t.Fatal("embedded CSS unavailable")
	}
	status, body = fetch(t, client, "GET", f.app.URL+"/api/v1/tasks", "", nil)
	if status != 401 || strings.Contains(body, "PRIVATE_PROMPT") {
		t.Fatalf("API auth bypassed: %d", status)
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Get(f.app.URL + "/auth/github")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	next, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	f.provider.Close()
	status, body = fetch(t, client, "GET", f.app.URL+"/auth/github/callback?"+url.Values{"state": {next.Query().Get("state")}, "code": {"fixture-code"}}.Encode(), "", nil)
	if status != 502 || !strings.Contains(body, "GitHub 授权失败") {
		t.Fatalf("provider failure: %d %s", status, body)
	}
}
func TestCommunityConfiguration(t *testing.T) {
	for _, config := range []Config{{BaseURL: "http://public.example"}, {BaseURL: "https://example.com/path"}, {ClientID: "missing-secret"}} {
		if _, err := newServer(nil, "token", config); err == nil {
			t.Fatalf("unsafe configuration: %+v", config)
		}
	}
}

// The browser suite starts this fixture explicitly; it is never a production route.
func TestCommunityBrowserFixture(t *testing.T) {
	file := os.Getenv("PERO_COMMUNITY_BROWSER_FIXTURE")
	if file == "" {
		t.Skip("browser fixture is opt-in")
	}
	f := communityHTTP(t)
	metadata, _ := json.Marshal(map[string]string{"url": f.app.URL, "task": f.taskID, "worker": f.workerID, "workerToken": f.workerToken})
	if err := os.WriteFile(file, metadata, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture timed out")
		case <-tick.C:
			if _, err := os.Stat(file + ".stop"); err == nil {
				return
			}
		}
	}
}
