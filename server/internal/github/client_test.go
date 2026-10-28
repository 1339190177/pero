package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/phodal/pero/server/internal/store"
)

func config() store.GitHubRepository {
	return store.GitHubRepository{ProjectID: "project", Owner: "acme", Repo: "repo", WorkspaceKey: "repo", AgentID: "fixture", Acceptance: "Tests pass", RequiredLabel: "pero:ready"}
}

func TestConfigurationAndFixedTransport(t *testing.T) {
	for _, raw := range []string{`null`, `[{}]`, `[] {}`, `[{"origin":"https://evil.test"}]`} {
		if _, err := ParseRepositories(raw); err == nil {
			t.Fatalf("accepted invalid config %s", raw)
		}
	}
	if _, err := ParseRepositories(`[]`); err != nil {
		t.Fatal(err)
	}
	called := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.Write([]byte(`{}`)) }))
	defer destination.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-GitHub-Api-Version") != apiVersion {
			t.Error("request contract drift")
		}
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer provider.Close()
	s, err := New(nil, []store.GitHubRepository{config()}, "secret", Options{BaseURL: provider.URL, Client: provider.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.get(context.Background(), "/repos/acme/repo", nil, &map[string]any{}); err == nil {
		t.Fatal("redirect accepted")
	}
	if called != 0 {
		t.Fatal("credentials followed redirect")
	}
	if _, err = s.hasNext(`<https://evil.test/repos/acme/repo/issues?page=2>; rel="next"`, "/repos/acme/repo/issues", 2); err == nil {
		t.Fatal("cross-origin pagination accepted")
	}
	if next, err := s.hasNext(fmt.Sprintf(`<%s/repos/acme/repo/issues?page=2>; rel="next"`, provider.URL), "/repos/acme/repo/issues", 2); err != nil || !next {
		t.Fatalf("same-scope page rejected: %v", err)
	}
	if _, err = s.get(context.Background(), "/repos/acme/repo", url.Values{"page": {"2"}}, &map[string]any{}); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestIssueNormalizationKeepsStableIDsAndRejectsGoalTruncation(t *testing.T) {
	raw := rawIssue{ID: "9007199254740994", NodeID: "I_node", Number: 7, URL: "https://github.com/acme/repo/issues/7", Title: "Goal", Body: "Body", State: "open", UpdatedAt: "2026-10-28T00:00:00Z"}
	issue, err := normalizeIssue(config(), "9007199254740993", raw)
	if err != nil {
		t.Fatal(err)
	}
	if issue.ID != "9007199254740994" || issue.RepositoryID != "9007199254740993" {
		t.Fatal("provider ids lost precision")
	}
	raw.Body = strings.Repeat("x", 64000)
	if _, err = normalizeIssue(config(), "1", raw); err == nil {
		t.Fatal("oversized combined goal silently truncated")
	}
	raw.Body = "body"
	raw.URL = "https://evil.test/acme/repo/issues/7"
	if _, err = normalizeIssue(config(), "1", raw); err == nil {
		t.Fatal("cross-repository link accepted")
	}
}
