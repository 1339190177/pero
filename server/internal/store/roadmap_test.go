package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func roadmapFixture() (GitHubRepository, RoadmapSnapshot) {
	return GitHubRepository{ProjectID: "p", Owner: "acme", Repo: "repo", WorkspaceKey: "repo", AgentID: "fixture", Acceptance: "Review evidence", AutoDispatch: true}, RoadmapSnapshot{RepositoryID: "200", Repository: "acme/repo", Path: "docs/ROADMAP.md", CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BlobSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Items: []RoadmapItem{{ID: "parent", Title: "Parent", Text: "Parent", Line: 3, Headings: []string{"Plan"}, Tags: []string{"platform"}}, {ID: "child", ParentID: "parent", Title: "Child", Text: "Child goal", Line: 4, Headings: []string{"Plan"}, Tags: []string{"go"}}, {ID: "done", Title: "Done", Text: "Previously done", Line: 5, Checked: true}}}
}

func TestRoadmapDocumentOrderPagingAndScopedCursors(t *testing.T) {
	s, _ := integrationStore(t)
	ctx := context.Background()
	config, snapshot := roadmapFixture()
	snapshot.Items = nil
	// Deliberately import out of document order. Parent/child cross the 50-row page.
	for line := 65; line >= 1; line-- {
		item := RoadmapItem{ID: fmt.Sprintf("item-%02d", line), Title: fmt.Sprintf("Item %d", line), Text: "Goal", Line: line, Tags: []string{"go"}}
		if line == 51 {
			item.ParentID = "item-50"
		}
		if line == 1 {
			item.Tags = []string{"docs"}
		}
		snapshot.Items = append(snapshot.Items, item)
	}
	if _, err := s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	privateConfig, privateSnapshot := roadmapFixture()
	privateConfig.ProjectID = "private"
	privateSnapshot.Items = privateSnapshot.Items[:1]
	if _, err := s.SyncRoadmap(ctx, privateConfig, privateSnapshot); err != nil {
		t.Fatal(err)
	}
	var privateID string
	if err := s.pool.QueryRow(ctx, `SELECT task_id FROM roadmap_items WHERE project_id='private'`).Scan(&privateID); err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateTask(ctx, Spec{Title: "Issue", Prompt: "Issue", ProjectID: "p", Source: &Source{Kind: "github"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	client, _ := mustClient(t, s, "Reader", "p")
	filter := TaskFilter{SourceKind: "roadmap"}
	first, err := s.ClientTasks(ctx, &client, "", 50, filter)
	if err != nil || len(first.Tasks) != 50 || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("first document page: %+v %v", first, err)
	}
	for i, task := range first.Tasks {
		if task.LatestSource.Line != i+1 {
			t.Fatalf("document order: index=%d line=%d", i, task.LatestSource.Line)
		}
	}
	second, err := s.ClientTasks(ctx, &client, *first.NextCursor, 50, filter)
	if err != nil || len(second.Tasks) != 15 || second.HasMore || second.NextCursor != nil {
		t.Fatalf("second document page: %+v %v", second, err)
	}
	if second.Tasks[0].ParentTaskID != first.Tasks[49].ID || second.Tasks[0].LatestSource.Line != 51 {
		t.Fatal("parent did not precede its child across the page boundary")
	}
	for i, task := range second.Tasks {
		if task.LatestSource.Line != i+51 {
			t.Fatalf("second document order: index=%d line=%d", i, task.LatestSource.Line)
		}
	}
	for _, cursor := range []string{privateID, issue.ID} {
		if _, err = s.ClientTasks(ctx, &client, cursor, 50, filter); !errors.Is(err, ErrNotFound) {
			t.Fatalf("private/other-source cursor accepted: %v", err)
		}
	}
	if _, err = s.ClientTasks(ctx, &client, first.Tasks[0].ID, 50, TaskFilter{SourceKind: "roadmap", Tag: "go"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cursor outside active tag filter accepted: %v", err)
	}
	if _, err = s.Tasks(ctx, issue.ID, 50, filter); !errors.Is(err, ErrNotFound) {
		t.Fatalf("operator other-source cursor accepted: %v", err)
	}
	// Current observed position controls ordering while a claimed goal stays frozen.
	if _, err = s.ClaimTask(ctx, &client, first.Tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	snapshot.Items[len(snapshot.Items)-1].Line = 1000
	snapshot.Items[len(snapshot.Items)-1].Tags = []string{}
	if _, err = s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	ordered, err := s.ClientTasks(ctx, &client, "", 100, filter)
	if err != nil || len(ordered.Tasks) != 65 {
		t.Fatalf("current positions: %+v %v", ordered, err)
	}
	last := ordered.Tasks[64]
	if ordered.Tasks[0].LatestSource.Line != 2 || last.ID != first.Tasks[0].ID || last.Source.Line != 1 || last.LatestSource.Line != 1000 {
		t.Fatal("list sorted by frozen goal instead of observed document position")
	}
	cleared, err := s.ClientTasks(ctx, &client, "", 50, TaskFilter{SourceKind: "roadmap", Tag: "docs"})
	if err != nil || len(cleared.Tasks) != 0 {
		t.Fatalf("cleared current tags fell back to frozen tags: %+v %v", cleared, err)
	}
	// Repository and path precede line in the sort tuple.
	alpha := snapshot
	alpha.RepositoryID, alpha.Repository, alpha.Path = "201", "acme/alpha", "z.md"
	alpha.Items = []RoadmapItem{{ID: "alpha", Title: "Alpha", Text: "Goal", Line: 900}}
	alphaConfig := config
	alphaConfig.Repo = "alpha"
	if _, err = s.SyncRoadmap(ctx, alphaConfig, alpha); err != nil {
		t.Fatal(err)
	}
	otherPath := snapshot
	otherPath.Path = "z.md"
	otherPath.Items = []RoadmapItem{{ID: "other-path", Title: "Other path", Text: "Goal", Line: 1}}
	if _, err = s.SyncRoadmap(ctx, config, otherPath); err != nil {
		t.Fatal(err)
	}
	operator, err := s.Tasks(ctx, "", 100, filter)
	if err != nil || len(operator.Tasks) != 68 {
		t.Fatalf("operator document order: %+v %v", operator, err)
	}
	previous := ""
	for _, task := range operator.Tasks {
		position := fmt.Sprintf("%s\x00%s\x00%08d\x00%s", task.LatestSource.Repository, task.LatestSource.Path, task.LatestSource.Line, task.ID)
		if position < previous {
			t.Fatal("operator list is not in document order")
		}
		previous = position
	}
	alphaTask := operator.Tasks[0]
	if alphaTask.LatestSource.Repository != "acme/alpha" || operator.Tasks[len(operator.Tasks)-1].LatestSource.Path != "z.md" {
		t.Fatal("repository/path precedence lost")
	}
	continued, err := s.Tasks(ctx, alphaTask.ID, 1, filter)
	if err != nil || len(continued.Tasks) != 1 || continued.Tasks[0].LatestSource.Path != "docs/ROADMAP.md" || continued.Tasks[0].LatestSource.Line != 2 {
		t.Fatalf("cursor did not preserve full repository/path tuple: %+v %v", continued, err)
	}
}
func roadmapTask(t *testing.T, s *Store, item string) *Task {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(context.Background(), `SELECT task_id FROM roadmap_items WHERE item_id=$1`, item).Scan(&id); err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestRoadmapHierarchyStableMetadataAndNoAutomaticExecution(t *testing.T) {
	s, _ := integrationStore(t)
	ctx := context.Background()
	config, snapshot := roadmapFixture()
	result, err := s.SyncRoadmap(ctx, config, snapshot)
	if err != nil || result.Imported != 3 {
		t.Fatalf("import: %+v %v", result, err)
	}
	parent, child, done := roadmapTask(t, s, "parent"), roadmapTask(t, s, "child"), roadmapTask(t, s, "done")
	if child.ParentTaskID != parent.ID || child.Source.ParentItemID != "parent" || child.Tags[0] != "go" || child.Source.Headings[0] != "Plan" || done.Source.State != "closed" {
		t.Fatal("source hierarchy/tags/check state lost")
	}
	for _, task := range []*Task{parent, child, done} {
		if task.State != "draft" || task.Run != nil {
			t.Fatal("roadmap configuration auto-executed an item")
		}
	}
	oldRevision := child.Source.Revision
	snapshot.CommitSHA = "cccccccccccccccccccccccccccccccccccccccc"
	snapshot.BlobSHA = "dddddddddddddddddddddddddddddddddddddddd"
	snapshot.Items[1].Line = 40
	result, err = s.SyncRoadmap(ctx, config, snapshot)
	if err != nil || result.Imported != 0 || result.Updated != 0 {
		t.Fatalf("line/commit-only update changed goal identity: %+v %v", result, err)
	}
	current := roadmapTask(t, s, "child")
	if current.ID != child.ID || current.Source.Revision != oldRevision || current.LatestSource.Line != 40 {
		t.Fatal("stable semantic identity/provenance failed")
	}
	client, _ := mustClient(t, s, "Alice", "p")
	if _, err = s.ClaimTask(ctx, &client, done.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("checked roadmap item claimable: %v", err)
	}
	page, err := s.ClientTasks(ctx, &client, "", 50, TaskFilter{SourceKind: "roadmap", Tag: "go"})
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].ID != child.ID {
		t.Fatalf("scoped source/tag filter: %+v %v", page, err)
	}
}

func TestRoadmapPartialReadCannotRemoveAndFrozenGoalCannotBeRelabelled(t *testing.T) {
	s, _ := integrationStore(t)
	ctx := context.Background()
	config, snapshot := roadmapFixture()
	if _, err := s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	child := roadmapTask(t, s, "child")
	run, err := s.Dispatch(ctx, child.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Items[1].Text = "Changed child goal"
	snapshot.Items[1].Tags = []string{"new-tag"}
	snapshot.Items = snapshot.Items[:2]
	snapshot.Partial = true
	if _, err = s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	if roadmapTask(t, s, "done").LatestSource.State == "missing" {
		t.Fatal("partial parse manufactured removal")
	}
	latest := roadmapTask(t, s, "child")
	if latest.Prompt != child.Prompt || latest.Source.Revision != run.Spec.Source.Revision || latest.LatestSource.Revision == latest.Source.Revision {
		t.Fatal("changed source relabelled frozen input")
	}
	observed, err := s.Tasks(ctx, "", 50, TaskFilter{SourceKind: "roadmap", Tag: "new-tag"})
	if err != nil || len(observed.Tasks) != 1 || observed.Tasks[0].ID != child.ID || observed.Tasks[0].Tags[0] != "go" {
		t.Fatalf("latest tag filter changed frozen tags or missed source: %+v %v", observed, err)
	}
	oldTags, err := s.Tasks(ctx, "", 50, TaskFilter{Tag: "go"})
	if err != nil || len(oldTags.Tasks) != 0 {
		t.Fatalf("stale frozen tag used for observed source filtering: %+v %v", oldTags, err)
	}
	snapshot.Partial = false
	if _, err = s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	if roadmapTask(t, s, "done").LatestSource.State != "missing" {
		t.Fatal("complete parse did not record removal")
	}
	snapshot.Items = snapshot.Items[:1]
	snapshot.CommitSHA = strings.Repeat("e", 40)
	result, err := s.SyncRoadmap(ctx, config, snapshot)
	if err != nil || result.Missing != 1 {
		t.Fatalf("remove child: %+v %v", result, err)
	}
	removed := roadmapTask(t, s, "child")
	if removed.State != "cancelled" || removed.LatestSource.State != "missing" || removed.Run.Spec.Source.State != "open" {
		t.Fatal("removal changed original Run or left queued execution")
	}
	if removed.LatestSource.CommitSHA != child.Source.CommitSHA || removed.LatestSource.URL != child.Source.URL || removed.LatestSource.MissingAtCommit != snapshot.CommitSHA {
		t.Fatal("removal relabelled last-known source navigation or lost absence provenance")
	}
	if _, err = s.Dispatch(ctx, child.ID, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed source redispatched: %v", err)
	}
}
